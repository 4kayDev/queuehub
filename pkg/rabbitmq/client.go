package rabbitmq

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	queuehub "github.com/4kayDev/queuehub/interface"
	"github.com/4kayDev/queuehub/pkg/config"
	amqp "github.com/rabbitmq/amqp091-go"
)

type QueueClient[T any] struct {
	mu sync.RWMutex

	cfg *config.RabbitMQQueueConfig

	driver *Driver

	channel *amqp.Channel
	queue   *amqp.Queue
	dlQueue *amqp.Queue
}

func NewQueueClient[T any](driver *Driver, cfg *config.RabbitMQQueueConfig) (*QueueClient[T], error) {
	ch, err := driver.Connectiion.Channel()
	if err != nil {
		return nil, err
	}

	return &QueueClient[T]{cfg: cfg, driver: driver, channel: ch}, nil
}

func MustNewQueueClient[T any](driver *Driver, cfg *config.RabbitMQQueueConfig) *QueueClient[T] {
	q, err := NewQueueClient[T](driver, cfg)
	if err != nil {
		panic(err)
	}

	return q
}

func (c *QueueClient[T]) createQueue(ctx context.Context) error {
	c.mu.Lock()
	if c.queue != nil {
		c.mu.Unlock()
		return nil
	}

	// Declaring master queue
	q, err := c.channel.QueueDeclare(
		c.cfg.Name,
		c.cfg.IsDurable,
		c.cfg.IsAutoDelete,
		c.cfg.IsExclusive,
		false,
		buildQueueArgs(c.cfg),
	)
	if err != nil {
		c.mu.Unlock()
		return err
	}

	// Declaring exchange for communication with master and DL queues
	err = c.channel.ExchangeDeclare(
		c.cfg.DlxName,
		"direct",
		c.cfg.IsDurable,
		c.cfg.IsAutoDelete,
		false,
		false,
		nil,
	)
	if err != nil {
		c.mu.Unlock()
		return err
	}

	// Binding exchange with master queue
	err = c.channel.QueueBind(
		q.Name,
		c.cfg.RoutingKey,
		c.cfg.DlxName,
		false,
		nil,
	)
	if err != nil {
		c.mu.Unlock()
		return err
	}

	// Declaring Dead-Letter queue
	dlq, err := c.channel.QueueDeclare(
		q.Name+"_dlq",
		c.cfg.IsDurable,
		c.cfg.IsAutoDelete,
		c.cfg.IsExclusive,
		false,
		buildDLQArgs(c.cfg),
	)
	if err != nil {
		c.mu.Unlock()
		return err
	}

	// Binding exchange with DLQ
	err = c.channel.QueueBind(
		q.Name+"_dlq",
		c.cfg.RoutingKey+"_dlq",
		c.cfg.DlxName,
		false,
		nil,
	)
	if err != nil {
		c.mu.Unlock()
		return err
	}

	c.dlQueue = &dlq
	c.queue = &q
	c.mu.Unlock()

	return nil
}

func (c *QueueClient[T]) ensureQueueExists(ctx context.Context) error {
	c.mu.RLock()
	if c.queue == nil {
		c.mu.RUnlock()
		return c.createQueue(ctx)
	}

	c.mu.RUnlock()
	return nil
}

func (c *QueueClient[T]) Produce(ctx context.Context, msg T) error {
	err := c.ensureQueueExists(ctx)
	if err != nil {
		return err
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	err = c.channel.PublishWithContext(
		ctx,
		c.cfg.DlxName,    // Exchange
		c.cfg.RoutingKey, // RoutingKey
		false,            // Mandatory
		false,            // Immediate
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			Type:         "plain/text",
			Body:         body,
		},
	)

	return err
}

func (c *QueueClient[T]) produceToDLQ(ctx context.Context, body []byte, retriesCount int32) error {
	err := c.ensureQueueExists(ctx)
	if err != nil {
		return err
	}

	exp := c.cfg.TTL * int32(retriesCount)

	err = c.channel.PublishWithContext(
		ctx,
		c.cfg.DlxName,
		c.cfg.RoutingKey+"_dlq",
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			Type:         "plain/text",
			Body:         body,
			Expiration:   string(exp),
		},
	)

	return err
}

func (c *QueueClient[T]) Consume(ctx context.Context, handler queuehub.ConsumerFunc[T]) error {
	err := c.ensureQueueExists(ctx)
	if err != nil {
		return err
	}

	msgs, err := c.channel.Consume(
		c.dlQueue.Name,
		"",
		false, // auto-ACK
		false, // IsExclusive
		false, // no-local
		false, // no-await
		nil,
	)
	var forever chan struct {
	}
	for msg := range msgs {
		var retriesCount int32 = 0
		xDeath, ok := msg.Headers["x-death"].([]interface{})
		if !ok || xDeath == nil {
			log.Printf("Failed to get retriesCount on message with ID: %s", msg.MessageId)
			retriesCount = 0
		} else {
			retriesCount = xDeath[0].(amqp.Table)["count"].(int32)
		}

		if retriesCount >= c.cfg.MaxRerties {
			log.Printf("Message with ID: %s reached the maximum retries", msg.MessageId)
			err = msg.Ack(false)
			if err != nil {
				return err
			}
		}

		dest := new(T)

		err := json.Unmarshal(msg.Body, dest)
		if err != nil {
			return err
		}

		result, err := handler(ctx, *dest, &queuehub.Meta{
			AttemptNumber: retriesCount,
		})
		if err != nil {
			return err
		}

		switch result {
		case queuehub.ACK:
			err = msg.Ack(false)
			if err != nil {
				return err
			}
		case queuehub.NACK:
			err = msg.Nack(false, false)
			if err != nil {
				return err
			}
		case queuehub.DEFER:
			err = c.produceToDLQ(ctx, msg.Body, retriesCount)
			if err != nil {
				return err
			}
		}
	}
	<-forever
	return nil
}

// ----------- Utils ----------- //

func buildQueueArgs(cfg *config.RabbitMQQueueConfig) amqp.Table {
	args := amqp.Table{}

	if cfg.MaxLength > 0 {
		args["x-max-length"] = cfg.MaxLength
	}

	if cfg.IsLazyMode {
		args["x-queue-mode"] = ""
	}

	args["x-dead-letter-exchange"] = cfg.DlxName

	return args
}

func buildDLQArgs(cfg *config.RabbitMQQueueConfig) amqp.Table {
	return amqp.Table{
		"x-dead-letter-exchange":    cfg.DlxName,
		"x-dead-letter-routing-key": cfg.RoutingKey + "_dlq",
	}
}
