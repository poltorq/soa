package aggregation

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"reflect"
	"testing"
	"time"
)

type sourceStub struct {
	trace *[]string
	fail  bool
}

func (s *sourceStub) Recalculate(context.Context, time.Time, int64, time.Time) (int64, error) {
	*s.trace = append(*s.trace, "calculate")
	if s.fail {
		s.fail = false
		return 0, errors.New("ClickHouse unavailable")
	}
	return 6, nil
}
func (s *sourceStub) Metrics(context.Context, time.Time, int64) ([]Metric, error) {
	*s.trace = append(*s.trace, "read CH")
	return nil, nil
}
func (s *sourceStub) Publish(context.Context, time.Time, int64, time.Time) error {
	*s.trace = append(*s.trace, "publish")
	return nil
}
func (s *sourceStub) Ping(context.Context) error { return nil }

type transactionStub struct {
	trace   *[]string
	version int64
}

func (s *transactionStub) NextVersion(context.Context) (int64, error) {
	s.version++
	return s.version, nil
}
func (s *transactionStub) Save(context.Context, time.Time, []Metric) error {
	*s.trace = append(*s.trace, "save")
	return nil
}
func (s *transactionStub) Metrics(context.Context, time.Time) ([]Metric, error) {
	*s.trace = append(*s.trace, "read PG")
	return nil, nil
}
func (s *transactionStub) MarkExported(context.Context, time.Time) error {
	*s.trace = append(*s.trace, "mark exported")
	return nil
}

type storeStub struct {
	tx    *transactionStub
	dates []time.Time
}

func (s *storeStub) WithDateLock(_ context.Context, _ time.Time, fn func(metricsTransaction) error) error {
	*s.tx.trace = append(*s.tx.trace, "lock")
	err := fn(s.tx)
	if err != nil {
		*s.tx.trace = append(*s.tx.trace, "rollback")
	} else {
		*s.tx.trace = append(*s.tx.trace, "commit")
	}
	return err
}
func (s *storeStub) PendingExports(context.Context, time.Time) ([]time.Time, error) {
	return s.dates, nil
}
func (s *storeStub) Ping(context.Context) error { return nil }

type exporterStub struct {
	trace *[]string
	fail  bool
}

func (s *exporterStub) Export(context.Context, time.Time, []Metric) (string, error) {
	*s.trace = append(*s.trace, "upload")
	if s.fail {
		return "", errors.New("S3 unavailable")
	}
	return "s3://test/daily/test/aggregates.csv", nil
}
func (s *exporterStub) Ping(context.Context) error {
	if s.fail {
		return errors.New("S3 unavailable")
	}
	return nil
}
func serviceFixture(trace *[]string) (*Service, *sourceStub, *storeStub, *exporterStub) {
	source := &sourceStub{trace: trace}
	store := &storeStub{tx: &transactionStub{trace: trace}}
	exporter := &exporterStub{trace: trace}
	return NewService(source, store, exporter, 2, time.Nanosecond, 8, slog.New(slog.NewTextHandler(io.Discard, nil))), source, store, exporter
}
func TestRecalculationRetriesAnEntireFailedTransaction(t *testing.T) {
	var trace []string
	service, source, store, _ := serviceFixture(&trace)
	source.fail = true
	if err := service.Recalculate(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	want := []string{"lock", "calculate", "rollback", "lock", "calculate", "read CH", "save", "publish", "commit"}
	if !reflect.DeepEqual(trace, want) || store.tx.version != 2 {
		t.Fatalf("trace=%v version=%d", trace, store.tx.version)
	}
}
func TestFailedUploadIsNotMarkedExportedAndBacklogRetries(t *testing.T) {
	var trace []string
	service, _, store, exporter := serviceFixture(&trace)
	store.dates = []time.Time{day(time.Now()).AddDate(0, 0, -20)}
	exporter.fail = true
	service.exportPending(context.Background(), time.Now())
	for _, step := range trace {
		if step == "mark exported" || step == "commit" {
			t.Fatalf("marked failed upload: %v", trace)
		}
	}
	trace = nil
	exporter.fail = false
	service.exportPending(context.Background(), time.Now())
	want := []string{"lock", "read PG", "upload", "mark exported", "commit"}
	if !reflect.DeepEqual(trace, want) {
		t.Fatalf("trace=%v", trace)
	}
}
func TestReadinessIncludesS3(t *testing.T) {
	var trace []string
	service, _, _, exporter := serviceFixture(&trace)
	exporter.fail = true
	if err := service.Ready(context.Background()); err == nil {
		t.Fatal("ready with unavailable S3")
	}
}
func TestValidateMetricSnapshot(t *testing.T) {
	date := day(time.Now())
	value := 1.0
	valid := []Metric{}
	for _, name := range []string{"dau", "average_watch_seconds", "view_conversion", "retention_d1", "retention_d7"} {
		valid = append(valid, Metric{Date: date, Name: name, Value: &value, ComputedAt: date})
	}
	if err := validateMetrics(date, valid); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		metrics []Metric
	}{
		{"empty", nil}, {"incomplete", valid[:3]}, {"duplicate", append(append([]Metric{}, valid...), valid[0])},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateMetrics(date, test.metrics); err == nil {
				t.Fatal("accepted corrupt snapshot")
			}
		})
	}
	bad := append([]Metric{}, valid...)
	nan := math.NaN()
	bad[0].Value = &nan
	if err := validateMetrics(date, bad); err == nil {
		t.Fatal("accepted NaN")
	}
}
