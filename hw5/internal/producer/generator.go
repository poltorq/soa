package producer

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/google/uuid"

	"online-cinema-hw5/internal/event"
)

const sessionPauseDuration = time.Minute

type Generator struct {
	publisher eventPublisher
	interval  time.Duration
	logger    *slog.Logger
	random    *rand.Rand
}

func NewGenerator(publisher eventPublisher, interval time.Duration, logger *slog.Logger) *Generator {
	return &Generator{
		publisher: publisher,
		interval:  interval,
		logger:    logger,
		random:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (g *Generator) Run(ctx context.Context) {
	ticker := time.NewTicker(g.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			g.publishSession(ctx, now.UTC())
		}
	}
}

func (g *Generator) publishSession(ctx context.Context, now time.Time) {
	userID := fmt.Sprintf("user-%03d", g.random.Intn(100)+1)
	movieID := fmt.Sprintf("movie-%03d", g.random.Intn(30)+1)
	device := []event.Device{event.Mobile, event.Desktop, event.TV, event.Tablet}[g.random.Intn(4)]
	duration := int32(g.random.Intn(6600) + 600)
	startedAt := now.Add(-time.Duration(duration+1)*time.Second - sessionPauseDuration)
	items := SessionEvents(startedAt, userID, movieID, uuid.NewString(), device, duration)
	for _, item := range items {
		if err := g.publisher.Publish(ctx, item); err != nil {
			g.logger.Error("publish generated event", "event_id", item.EventID, "error", err)
			return
		}
	}
}

func SessionEvents(
	startedAt time.Time,
	userID string,
	movieID string,
	sessionID string,
	device event.Device,
	duration int32,
) []event.Event {
	zero := int32(0)
	pause := duration / 3
	finished := duration
	pauseAt := startedAt.Add(time.Duration(pause) * time.Second)
	resumeAt := pauseAt.Add(sessionPauseDuration)
	return []event.Event{
		newGeneratedEvent(startedAt.Add(-time.Minute), userID, movieID, sessionID, event.Searched, device, nil),
		newGeneratedEvent(startedAt, userID, movieID, sessionID, event.ViewStarted, device, &zero),
		newGeneratedEvent(pauseAt, userID, movieID, sessionID, event.ViewPaused, device, &pause),
		newGeneratedEvent(resumeAt, userID, movieID, sessionID, event.ViewResumed, device, &pause),
		newGeneratedEvent(startedAt.Add(time.Duration(duration)*time.Second+sessionPauseDuration), userID, movieID, sessionID, event.ViewFinished, device, &finished),
		newGeneratedEvent(startedAt.Add(time.Duration(duration+1)*time.Second+sessionPauseDuration), userID, movieID, sessionID, event.Liked, device, nil),
	}
}

func newGeneratedEvent(
	timestamp time.Time,
	userID string,
	movieID string,
	sessionID string,
	eventType event.Type,
	device event.Device,
	progress *int32,
) event.Event {
	return event.Event{
		EventID:         uuid.NewString(),
		UserID:          userID,
		MovieID:         movieID,
		EventType:       eventType,
		Timestamp:       timestamp,
		DeviceType:      device,
		SessionID:       sessionID,
		ProgressSeconds: progress,
	}
}
