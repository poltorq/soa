package aggregation

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Exporter struct {
	client *minio.Client
	bucket string
}

func NewExporter(
	ctx context.Context,
	endpoint string,
	accessKey string,
	secretKey string,
	bucket string,
) (*Exporter, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check S3 bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			exists, checkErr := client.BucketExists(ctx, bucket)
			if checkErr != nil || !exists {
				return nil, fmt.Errorf("create S3 bucket: %w", err)
			}
		}
	}
	return &Exporter{client: client, bucket: bucket}, nil
}

func (e *Exporter) Ping(ctx context.Context) error {
	exists, err := e.client.BucketExists(ctx, e.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("S3 bucket does not exist")
	}
	return nil
}

func (e *Exporter) Export(ctx context.Context, date time.Time, metrics []Metric) (string, error) {
	if len(metrics) == 0 {
		return "", fmt.Errorf("no metrics found for %s", day(date).Format(time.DateOnly))
	}

	var content bytes.Buffer
	writer := csv.NewWriter(&content)
	if err := writer.Write([]string{
		"metric_date", "metric_name", "dimension_key", "dimension_value",
		"metric_value", "computed_at",
	}); err != nil {
		return "", fmt.Errorf("write CSV header: %w", err)
	}
	for _, metric := range metrics {
		value := ""
		if metric.Value != nil {
			value = strconv.FormatFloat(*metric.Value, 'f', -1, 64)
		}
		if err := writer.Write([]string{
			metric.Date.Format(time.DateOnly),
			metric.Name,
			metric.DimensionKey,
			metric.DimensionValue,
			value,
			metric.ComputedAt.UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return "", fmt.Errorf("write CSV metric: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", fmt.Errorf("finish CSV: %w", err)
	}

	objectName := "daily/" + day(date).Format(time.DateOnly) + "/aggregates.csv"
	_, err := e.client.PutObject(
		ctx,
		e.bucket,
		objectName,
		bytes.NewReader(content.Bytes()),
		int64(content.Len()),
		minio.PutObjectOptions{ContentType: "text/csv"},
	)
	if err != nil {
		return "", fmt.Errorf("upload aggregates to S3: %w", err)
	}
	return "s3://" + e.bucket + "/" + objectName, nil
}
