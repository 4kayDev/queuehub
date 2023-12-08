package yandexqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"

	queuehub "github.com/4kayDev/queuehub/interface"
	"github.com/4kayDev/queuehub/pkg/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

const (
	attemptNumberAttributeKey = "ApproximateReceiveCount"
)

type YandexQueueClient[T any] struct {
	mu       sync.RWMutex
	ctx      context.Context
	cfg      *config.AWSConfig
	queueURL *string
	client   *sqs.Client
}

func New[T any](ctx context.Context, cfg *config.AWSConfig) (*YandexQueueClient[T], error) {
	os.Setenv("AWS_ACCESS_KEY_ID", cfg.ACCESS_ID)
	os.Setenv("AWS_SECRET_ACCESS_KEY", cfg.ACCESS_SECRET_KEY)

	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:           cfg.URL,
			SigningRegion: cfg.Region,
		}, nil
	})

	awscfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithEndpointResolverWithOptions(customResolver),
	)

	if err != nil {
		return nil, err
	}

	return &YandexQueueClient[T]{
		ctx:    ctx,
		cfg:    cfg,
		client: sqs.NewFromConfig(awscfg),
	}, nil
}

func MustNew[T any](ctx context.Context, cfg *config.AWSConfig) *YandexQueueClient[T] {
	client, err := New[T](ctx, cfg)
	if err != nil {
		panic(err)
	}

	return client
}

func (c *YandexQueueClient[T]) StartConsuming(ctx context.Context, handler queuehub.ConsumerFunc[T]) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			break
		}
		err := c.Consume(ctx, handler)
		if err != nil {
			log.Println(err)
		}
	}
}

func (c *YandexQueueClient[T]) createQueue(ctx context.Context) error {
	c.mu.Lock()
	if c.queueURL != nil {
		c.mu.Unlock()
		return nil
	}

	queue, err := c.client.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: &c.cfg.QueueName,
	})
	if err != nil {
		c.mu.Unlock()
		return err
	}

	c.queueURL = queue.QueueUrl
	c.mu.Unlock()

	return nil
}

func (c *YandexQueueClient[T]) ensureConnection(ctx context.Context) error {
	c.mu.RLock()
	if c.queueURL == nil {
		c.mu.RUnlock()
		return c.createQueue(ctx)
	}

	c.mu.RUnlock()

	return nil
}

func (c *YandexQueueClient[T]) Produce(ctx context.Context, msg T) error {
	err := c.ensureConnection(ctx)
	if err != nil {
		return err
	}

	marshaled, err := json.Marshal(msg)
	if err != nil {
		return ErrInvalidEntity
	}

	marshaledStr := string(marshaled)

	_, err = c.client.SendMessage(ctx, &sqs.SendMessageInput{
		MessageBody:  &marshaledStr,
		QueueUrl:     c.queueURL,
		DelaySeconds: 0,
	})

	if err != nil {
		log.Println(err, "Producing message")
		return ErrProduce
	}

	return nil
}

func (c *YandexQueueClient[T]) Consume(ctx context.Context, handler queuehub.ConsumerFunc[T]) error {
	err := c.ensureConnection(ctx)
	if err != nil {
		return err
	}

	recieved, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl: c.queueURL,
	})

	if err != nil {
		log.Println(err, "Error while reciving message")
		return ErrConsume
	}

	for _, msg := range recieved.Messages {
		destination := new(T)
		msgBody := []byte(*msg.Body)
		err := json.Unmarshal(msgBody, destination)
		if err != nil {
			log.Println(err, "Error while consuming messages")
			return ErrInvalidEntity
		}

		attemptCount := 0
		v, ok := msg.Attributes[attemptNumberAttributeKey]
		if ok {
			attemptCount, err = strconv.Atoi(v)
			if err != nil {
				fmt.Println(err)
			}
		}

		result, err := handler(ctx, *destination, &queuehub.Meta{AttemptNumber: int32(attemptCount)})
		if err != nil {
			return err
		}

		switch result {
		case queuehub.ACK:
			_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      c.queueURL,
				ReceiptHandle: msg.ReceiptHandle,
			})

			if err != nil {
				log.Println(err, "Error occured in processing NACK case")
				return err
			}

			break
		case queuehub.NACK:
			_, err := c.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
				QueueUrl:          c.queueURL,
				ReceiptHandle:     msg.ReceiptHandle,
				VisibilityTimeout: 0,
			})

			if err != nil {
				log.Println(err, "Error occured in proccessing ACK case")
				return err
			}

			break
		case queuehub.DEFER:
			if attemptCount > 10 {
				_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
					QueueUrl:      c.queueURL,
					ReceiptHandle: msg.ReceiptHandle,
				})

				if err != nil {
					log.Println(err, "Error occured in processing NACK case")
					return err
				}
			}
			_, err := c.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
				QueueUrl:          c.queueURL,
				ReceiptHandle:     msg.ReceiptHandle,
				VisibilityTimeout: c.cfg.DelayStep * int32(attemptCount),
			})

			if err != nil {
				log.Println(err, "Error orcured in processing DEFER case")
				return err
			}

			break
		}
	}

	return nil
}
