package queuehub

import (
	"context"
)

type Result int

const (
	ACK   Result = 0
	NACK  Result = 1
	DEFER Result = 2
)

type Meta struct {
	AttemptNumber int64
}

type ConsumerFunc[T any] func(ctx context.Context, msg T, meta *Meta) (Result, error)

type Consumer[T any] interface {
	Consume(ctx context.Context, handler ConsumerFunc[T]) error
}

type Producer[T any] interface {
	Produce(ctx context.Context, msg T, delaySeconds int) error
}

type QueuesClient[T any] interface {
	Consumer[T]
	Producer[T]
}

