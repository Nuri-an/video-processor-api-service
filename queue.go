package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rabbitmq/amqp091-go"
)

type Queue interface {
	Init() error
	Publish(VideoJob) error
}

type RabbitConfig struct {
	URL   string
	Queue string
}

type RabbitQueue struct {
	config  RabbitConfig
	conn    *amqp091.Connection
	channel *amqp091.Channel
}

func NewRabbitQueue(config RabbitConfig) (*RabbitQueue, error) {
	conn, err := amqp091.Dial(config.URL)
	if err != nil {
		return nil, err
	}
	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &RabbitQueue{config: config, conn: conn, channel: channel}, nil
}

func (q *RabbitQueue) Init() error {
	_, err := q.channel.QueueDeclare(q.config.Queue, true, false, false, false, nil)
	return err
}

func (q *RabbitQueue) Publish(job VideoJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return q.channel.PublishWithContext(context.Background(), "", q.config.Queue, false, false, amqp091.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp091.Persistent,
		Body:         data,
	})
}

func rabbitConfig() RabbitConfig {
	url := envOr("RABBITMQ_URL", fmt.Sprintf(
		"amqp://%s:%s@%s:%s/",
		envOr("RABBITMQ_USER", "video_processor"),
		envOr("RABBITMQ_PASSWORD", ""),
		envOr("RABBITMQ_HOST", "localhost"),
		envOr("RABBITMQ_PORT", "5672"),
	))
	return RabbitConfig{URL: url, Queue: envOr("RABBITMQ_QUEUE", "video_jobs")}
}
