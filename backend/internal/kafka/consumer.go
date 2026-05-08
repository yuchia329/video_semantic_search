package kafka

import (
	"context"
	"log"

	"github.com/segmentio/kafka-go"
)

type Consumer struct {
	reader *kafka.Reader
}

func NewConsumer(brokers []string, groupID, topic string) *Consumer {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  brokers,
		GroupID:  groupID,
		Topic:    topic,
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
	})
	return &Consumer{reader: r}
}

// Start listens for messages and passes them to the handler function
func (c *Consumer) Start(ctx context.Context, handler func(ctx context.Context, key, value []byte) error) {
	for {
		m, err := c.reader.FetchMessage(ctx)
		if err != nil {
			log.Printf("error fetching message: %v\n", err)
			if ctx.Err() != nil {
				return
			}
			continue
		}

		err = handler(ctx, m.Key, m.Value)
		if err != nil {
			log.Printf("error handling message: %v\n", err)
			// Depending on requirements, we can retry or DLQ (Dead Letter Queue) here.
			// For now, we simply log and commit to avoid getting stuck.
		}

		if err := c.reader.CommitMessages(ctx, m); err != nil {
			log.Printf("failed to commit message: %v\n", err)
		}
	}
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}
