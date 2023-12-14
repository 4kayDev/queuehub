package rabbitmq

import (
	"context"
	"fmt"

	"github.com/4kayDev/queuehub/pkg/config"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Driver struct {
	ctx context.Context

	Connectiion *amqp.Connection
}

func NewDriver(ctx context.Context, cfg *config.RabbitMQDriverConfig) (*Driver, error) {
	conn, err := amqp.Dial(buildDSN(cfg))
	if err != nil {
		return nil, err
	}

	return &Driver{ctx: ctx, Connectiion: conn}, nil
}

func MustNewDriver(ctx context.Context, cfg *config.RabbitMQDriverConfig) *Driver {
	driver, err := NewDriver(ctx, cfg)
	if err != nil {
		panic(err)
	}

	return driver
}

func buildDSN(cfg *config.RabbitMQDriverConfig) string {
	return fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port)
}
