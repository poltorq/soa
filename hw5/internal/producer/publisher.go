package producer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"time"

	"github.com/hamba/avro/v2"
	"github.com/segmentio/kafka-go"

	"online-cinema-hw5/internal/event"
	"online-cinema-hw5/internal/retry"
)

type Publisher struct {
	writer   messageWriter
	brokers  []string
	topic    string
	codec    schemaCodec
	attempts int
	backoff  time.Duration
	logger   *slog.Logger
	lanes    [64]chan struct{}
}

type messageWriter interface {
	WriteMessages(context.Context, ...kafka.Message) error
	Close() error
}

func ConnectPublisher(
	ctx context.Context,
	brokers []string,
	topic string,
	registryURL string,
	schemaPath string,
	attempts int,
	backoff time.Duration,
	logger *slog.Logger,
) (*Publisher, int, error) {
	codec, err := loadSchema(ctx, registryURL, topic, schemaPath)
	if err != nil {
		return nil, 0, err
	}
	return NewPublisher(brokers, topic, codec, attempts, backoff, logger), codec.id, nil
}

func NewPublisher(
	brokers []string,
	topic string,
	codec schemaCodec,
	attempts int,
	backoff time.Duration,
	logger *slog.Logger,
) *Publisher {
	writer := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireAll,
		MaxAttempts:  1,
		BatchTimeout: 10 * time.Millisecond,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
	publisher := &Publisher{
		writer:   writer,
		brokers:  brokers,
		topic:    topic,
		codec:    codec,
		attempts: attempts,
		backoff:  backoff,
		logger:   logger,
	}
	for i := range publisher.lanes {
		publisher.lanes[i] = make(chan struct{}, 1)
	}
	return publisher
}

func (p *Publisher) Ready(ctx context.Context) error {
	var lastErr error
	for _, broker := range p.brokers {
		connection, err := kafka.DialContext(ctx, "tcp", broker)
		if err != nil {
			lastErr = err
			continue
		}
		if deadline, ok := ctx.Deadline(); ok {
			_ = connection.SetDeadline(deadline)
		}
		_, err = connection.ReadPartitions(p.topic)
		closeErr := connection.Close()
		if err == nil && closeErr == nil {
			return nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = closeErr
		}
	}
	if lastErr == nil {
		return errors.New("Kafka has no configured brokers")
	}
	return fmt.Errorf("Kafka is unavailable: %w", lastErr)
}

func (p *Publisher) Publish(ctx context.Context, item event.Event) error {
	item.SetDefaults(time.Now())
	if err := item.Validate(); err != nil {
		return fmt.Errorf("validate event: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(item.UserID))
	lane := p.lanes[hash.Sum32()%uint32(len(p.lanes))]
	select {
	case lane <- struct{}{}:
		defer func() { <-lane }()
	case <-ctx.Done():
		return ctx.Err()
	}
	payload, err := avro.Marshal(p.codec.schema, item)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	message := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(message[1:5], uint32(p.codec.id))
	copy(message[5:], payload)

	attempt := 0
	publishErr := retry.Do(ctx, p.attempts, p.backoff, func() error {
		attempt++
		err := p.writer.WriteMessages(ctx, kafka.Message{
			Key:   []byte(item.UserID),
			Value: message,
			Time:  item.Timestamp,
		})
		if err == nil {
			p.logger.Info("event published",
				"event_id", item.EventID,
				"event_type", item.EventType,
				"timestamp", item.Timestamp,
			)
			return nil
		}
		p.logger.Warn("event publication attempt failed", "event_id", item.EventID, "attempt", attempt, "error", err)
		return err
	})
	if publishErr != nil {
		return fmt.Errorf("publish event after %d attempts: %w", attempt, publishErr)
	}
	return nil
}

func (p *Publisher) Close() error {
	return p.writer.Close()
}
