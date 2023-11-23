package yandexqueue

import (
	"errors"
)

var (
	ErrConnectionNotEstablished = errors.New("Connetion was not established")
	ErrInvalidEntity            = errors.New("Invalid entity")
	ErrProduce                  = errors.New("Error occured while producing message")
	ErrConsume                  = errors.New("Error occured while consuming messages")
)