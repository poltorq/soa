package aggregation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"online-cinema-hw5/internal/retry"
)

type aggregateSource interface {
	Recalculate(context.Context, time.Time, int64, time.Time) (int64, error)
	Metrics(context.Context, time.Time, int64) ([]Metric, error)
	Publish(context.Context, time.Time, int64, time.Time) error
	Ping(context.Context) error
}
type metricStore interface {
	WithDateLock(context.Context, time.Time, func(metricsTransaction) error) error
	PendingExports(context.Context, time.Time) ([]time.Time, error)
	Ping(context.Context) error
}
type metricExporter interface {
	Export(context.Context, time.Time, []Metric) (string, error)
	Ping(context.Context) error
}

type Service struct {
	clickhouse aggregateSource
	postgres   metricStore
	exporter   metricExporter
	attempts   int
	backoff    time.Duration
	windowDays int
	logger     *slog.Logger
}

func NewService(clickhouse aggregateSource, postgres metricStore, exporter metricExporter,
	attempts int, backoff time.Duration, windowDays int, logger *slog.Logger) *Service {
	return &Service{clickhouse: clickhouse, postgres: postgres, exporter: exporter,
		attempts: attempts, backoff: backoff, windowDays: windowDays, logger: logger}
}

func (s *Service) Recalculate(ctx context.Context, date time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	startedAt := time.Now()
	date = day(date)
	s.logger.Info("aggregation started", "date", date.Format(time.DateOnly))
	var processed int64
	var metricRows int
	err := s.retry(ctx, func() error {
		return s.postgres.WithDateLock(ctx, date, func(tx metricsTransaction) error {
			version, err := tx.NextVersion(ctx)
			if err != nil {
				return err
			}
			computedAt := time.Now().UTC().Truncate(time.Millisecond)
			processed, err = s.clickhouse.Recalculate(ctx, date, version, computedAt)
			if err != nil {
				return err
			}
			metrics, err := s.clickhouse.Metrics(ctx, date, version)
			if err != nil {
				return err
			}
			metricRows = len(metrics)
			if err := tx.Save(ctx, date, metrics); err != nil {
				return err
			}
			return s.clickhouse.Publish(ctx, date, version, computedAt)
		})
	})
	if err != nil {
		s.logger.Error("aggregation failed", "date", date.Format(time.DateOnly), "duration", time.Since(startedAt), "error", err)
		return err
	}
	s.logger.Info("aggregation completed", "date", date.Format(time.DateOnly),
		"processed_events", processed, "metric_rows", metricRows, "duration", time.Since(startedAt))
	return nil
}

func (s *Service) Export(ctx context.Context, date time.Time) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	date = day(date)
	var location string
	err := s.retry(ctx, func() error {
		return s.postgres.WithDateLock(ctx, date, func(tx metricsTransaction) error {
			metrics, err := tx.Metrics(ctx, date)
			if err != nil {
				return err
			}
			location, err = s.exporter.Export(ctx, date, metrics)
			if err != nil {
				return err
			}
			return tx.MarkExported(ctx, date)
		})
	})
	if err != nil {
		return "", err
	}
	s.logger.Info("aggregates exported", "date", date.Format(time.DateOnly), "location", location)
	return location, nil
}

func (s *Service) Ready(ctx context.Context) error {
	if err := s.clickhouse.Ping(ctx); err != nil {
		return fmt.Errorf("ping ClickHouse: %w", err)
	}
	if err := s.postgres.Ping(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL: %w", err)
	}
	if err := s.exporter.Ping(ctx); err != nil {
		return fmt.Errorf("ping S3: %w", err)
	}
	return nil
}

func (s *Service) RunSchedule(ctx context.Context, aggregationInterval, exportInterval time.Duration) {
	s.runWindow(ctx, time.Now().UTC())
	s.exportPending(ctx, time.Now().UTC())
	aggregationTicker := time.NewTicker(aggregationInterval)
	exportTicker := time.NewTicker(exportInterval)
	defer aggregationTicker.Stop()
	defer exportTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-aggregationTicker.C:
			now := time.Now().UTC()
			s.runWindow(ctx, now)
			s.exportPending(ctx, time.Now().UTC())
		case <-exportTicker.C:
			s.exportPending(ctx, time.Now().UTC())
		}
	}
}
func (s *Service) runWindow(ctx context.Context, now time.Time) {
	for offset := s.windowDays - 1; offset >= 0; offset-- {
		if ctx.Err() != nil {
			return
		}
		date := day(now).AddDate(0, 0, -offset)
		if err := s.Recalculate(ctx, date); err != nil {
			s.logger.Error("scheduled aggregation", "date", date.Format(time.DateOnly), "error", err)
		}
	}
}
func (s *Service) exportPending(ctx context.Context, now time.Time) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	dates, err := s.postgres.PendingExports(ctx, day(now))
	if err != nil {
		s.logger.Error("read pending exports", "error", err)
		return
	}
	for _, date := range dates {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.Export(ctx, date); err != nil {
			s.logger.Error("scheduled export", "date", date.Format(time.DateOnly), "error", err)
		}
	}
}
func (s *Service) retry(ctx context.Context, operation func() error) error {
	attempt := 0
	return retry.Do(ctx, s.attempts, s.backoff, func() error {
		attempt++
		err := operation()
		if err != nil {
			s.logger.Warn("aggregation operation attempt failed", "attempt", attempt, "error", err)
		}
		return err
	})
}
