package producer

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"online-cinema-hw5/internal/event"
)

func TestSessionEventsAreOrderedAndProgressive(t *testing.T) {
	startedAt := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	items := SessionEvents(startedAt, "user-1", "movie-1", "session-1", event.TV, 3600)
	if len(items) != 6 {
		t.Fatalf("len(SessionEvents()) = %d, want 6", len(items))
	}
	wantTypes := []event.Type{
		event.Searched,
		event.ViewStarted,
		event.ViewPaused,
		event.ViewResumed,
		event.ViewFinished,
		event.Liked,
	}
	var previousProgress int32
	for index, item := range items {
		if item.EventType != wantTypes[index] {
			t.Fatalf("event %d type = %q, want %q", index, item.EventType, wantTypes[index])
		}
		if index > 0 && !item.Timestamp.After(items[index-1].Timestamp) {
			t.Fatalf("event %d timestamp is not after previous event", index)
		}
		if item.ProgressSeconds != nil {
			if *item.ProgressSeconds < previousProgress {
				t.Fatalf("event %d progress decreased", index)
			}
			previousProgress = *item.ProgressSeconds
		}
		if err := item.Validate(); err != nil {
			t.Fatalf("event %d is invalid: %v", index, err)
		}
	}
}

func TestViewingProgressExcludesPausedTime(t *testing.T) {
	items := SessionEvents(time.Now().UTC(), "user", "movie", "session", event.TV, 3600)
	elapsed := items[4].Timestamp.Sub(items[1].Timestamp)
	paused := items[3].Timestamp.Sub(items[2].Timestamp)
	if elapsed-paused != time.Duration(*items[4].ProgressSeconds)*time.Second {
		t.Fatalf("watch progress does not match playing time: elapsed=%s paused=%s progress=%d", elapsed, paused, *items[4].ProgressSeconds)
	}
}

func TestGeneratedSessionDoesNotReachIntoFuture(t *testing.T) {
	publisher := &publisherStub{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	generator := NewGenerator(publisher, time.Second, logger)
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	generator.publishSession(context.Background(), now)
	if len(publisher.published) != 6 {
		t.Fatal("incomplete generated session")
	}
	for _, item := range publisher.published {
		if item.Timestamp.After(now) {
			t.Fatalf("future generated event: %+v", item)
		}
	}
}
