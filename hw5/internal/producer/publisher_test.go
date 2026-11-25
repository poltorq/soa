package producer

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hamba/avro/v2"
	"github.com/segmentio/kafka-go"
	"online-cinema-hw5/internal/event"
)

type recordingWriter struct {
	mu        sync.Mutex
	messages  []kafka.Message
	failFirst bool
	firstCall chan struct{}
}

func (w *recordingWriter) WriteMessages(_ context.Context, messages ...kafka.Message) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages = append(w.messages, messages...)
	if len(w.messages) == 1 {
		if w.firstCall != nil {
			close(w.firstCall)
		}
		if w.failFirst {
			return errors.New("ack lost")
		}
	}
	return nil
}
func (w *recordingWriter) Close() error { return nil }

func testPublisher(t *testing.T, writer messageWriter) *Publisher {
	t.Helper()
	text, err := os.ReadFile("../../schema/movie_event.avsc")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := avro.Parse(string(text))
	if err != nil {
		t.Fatal(err)
	}
	publisher := NewPublisher([]string{"unused:9092"}, "movie-events", schemaCodec{schema: schema, id: 42},
		3, time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	publisher.writer = writer
	return publisher
}
func decodeMessage(t *testing.T, publisher *Publisher, message kafka.Message) event.Event {
	t.Helper()
	if len(message.Value) < 5 || message.Value[0] != 0 || binary.BigEndian.Uint32(message.Value[1:5]) != 42 {
		t.Fatalf("invalid Confluent wire header: %v", message.Value)
	}
	var item event.Event
	if err := avro.Unmarshal(publisher.codec.schema, message.Value[5:], &item); err != nil {
		t.Fatal(err)
	}
	return item
}
func TestPublishRetriesPreserveIDAndUserOrder(t *testing.T) {
	writer := &recordingWriter{failFirst: true, firstCall: make(chan struct{})}
	publisher := testPublisher(t, writer)
	items := SessionEvents(time.Now().UTC(), "user-1", "movie-1", "session-1", event.TV, 3600)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	firstDone := make(chan error, 1)
	go func() { firstDone <- publisher.Publish(ctx, items[1]) }()
	<-writer.firstCall
	secondDone := make(chan error, 1)
	go func() { secondDone <- publisher.Publish(ctx, items[4]) }()
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	if len(writer.messages) != 3 {
		t.Fatalf("messages=%d", len(writer.messages))
	}
	for i, want := range []event.Event{items[1], items[1], items[4]} {
		got := decodeMessage(t, publisher, writer.messages[i])
		if got.EventID != want.EventID || got.EventType != want.EventType || string(writer.messages[i].Key) != want.UserID {
			t.Fatalf("message %d=%+v, want %+v", i, got, want)
		}
	}
}
func TestPublisherValidatesEveryCaller(t *testing.T) {
	writer := &recordingWriter{}
	publisher := testPublisher(t, writer)
	if err := publisher.Publish(context.Background(), event.Event{}); err == nil {
		t.Fatal("accepted invalid event")
	}
	if len(writer.messages) != 0 {
		t.Fatal("sent invalid event")
	}
}
