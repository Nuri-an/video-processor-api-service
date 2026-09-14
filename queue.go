package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

type Queue interface {
	Init() error
	Publish(VideoJob) error
}

type RabbitConfig struct {
	URL            string
	Queue          string
	DLX            string
	ConfirmTimeout time.Duration
}

type RabbitQueue struct {
	config  RabbitConfig
	conn    *amqp091.Connection
	channel *amqp091.Channel
	confirms <-chan amqp091.Confirmation
	mu      sync.Mutex
	timeout time.Duration
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
	if err := channel.Confirm(false); err != nil {
		channel.Close()
		conn.Close()
		return nil, fmt.Errorf("habilitar publisher confirms: %w", err)
	}
	return &RabbitQueue{
		config:   config,
		conn:     conn,
		channel:  channel,
		confirms: channel.NotifyPublish(make(chan amqp091.Confirmation, 1)),
		timeout:  config.ConfirmTimeout,
	}, nil
}

func (q *RabbitQueue) Init() error {
	if _, err := q.channel.ExchangeDeclare(q.config.DLX, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	deadLetterQueue := q.config.Queue + ".dead"
	if _, err := q.channel.QueueDeclare(deadLetterQueue, true, false, false, false, nil); err != nil {
		return err
	}
	if err := q.channel.QueueBind(deadLetterQueue, deadLetterQueue, q.config.DLX, false, nil); err != nil {
		return err
	}
	_, err := q.channel.QueueDeclare(q.config.Queue, true, false, false, false, amqp091.Table{
		"x-dead-letter-exchange":    q.config.DLX,
		"x-dead-letter-routing-key": deadLetterQueue,
	})
	return err
}

func (q *RabbitQueue) Publish(job VideoJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.channel.PublishWithContext(context.Background(), "", q.config.Queue, false, false, amqp091.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp091.Persistent,
		Body:         data,
	}); err != nil {
		return err
	}
	select {
	case confirmation := <-q.confirms:
		if !confirmation.Ack {
			return fmt.Errorf("rabbitmq rejeitou a mensagem do job %s", job.ID)
		}
		return nil
	case <-time.After(q.timeout):
		return fmt.Errorf("timeout aguardando confirmacao do job %s", job.ID)
	}
}

func rabbitConfig() RabbitConfig {
	url := envOr("RABBITMQ_URL", fmt.Sprintf(
		"amqp://%s:%s@%s:%s/",
		envOr("RABBITMQ_USER", "video_processor"),
		envOr("RABBITMQ_PASSWORD", ""),
		envOr("RABBITMQ_HOST", "localhost"),
		envOr("RABBITMQ_PORT", "5672"),
	))
	return RabbitConfig{
		URL:           url,
		Queue:         envOr("RABBITMQ_QUEUE", "video_jobs"),
		DLX:           envOr("RABBITMQ_DLX", "video_jobs.dlx"),
		ConfirmTimeout: envDuration("RABBITMQ_CONFIRM_TIMEOUT", 5*time.Second),
	}
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}
