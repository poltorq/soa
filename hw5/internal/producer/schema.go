package producer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hamba/avro/v2"
)

type schemaCodec struct {
	schema avro.Schema
	id     int
}

func loadSchema(ctx context.Context, registryURL, topic, path string) (schemaCodec, error) {
	schemaText, err := os.ReadFile(path)
	if err != nil {
		return schemaCodec{}, fmt.Errorf("read event schema: %w", err)
	}
	parsed, err := avro.Parse(string(schemaText))
	if err != nil {
		return schemaCodec{}, fmt.Errorf("parse event schema: %w", err)
	}
	body, err := json.Marshal(map[string]string{
		"schema":     string(schemaText),
		"schemaType": "AVRO",
	})
	if err != nil {
		return schemaCodec{}, fmt.Errorf("encode schema request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	subject := url.PathEscape(topic + "-value")
	compatibility, err := http.NewRequestWithContext(ctx, http.MethodPut,
		registryURL+"/config/"+subject, strings.NewReader(`{"compatibility":"BACKWARD_TRANSITIVE"}`))
	if err != nil {
		return schemaCodec{}, fmt.Errorf("create compatibility request: %w", err)
	}
	compatibility.Header.Set("Content-Type", "application/vnd.schemaregistry.v1+json")
	compatibilityResponse, err := client.Do(compatibility)
	if err != nil {
		return schemaCodec{}, fmt.Errorf("configure schema compatibility: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(compatibilityResponse.Body, 1<<20))
	compatibilityResponse.Body.Close()
	if compatibilityResponse.StatusCode < 200 || compatibilityResponse.StatusCode >= 300 {
		return schemaCodec{}, fmt.Errorf("configure schema compatibility: status %d", compatibilityResponse.StatusCode)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		registryURL+"/subjects/"+subject+"/versions",
		bytes.NewReader(body),
	)
	if err != nil {
		return schemaCodec{}, fmt.Errorf("create schema request: %w", err)
	}
	request.Header.Set("Content-Type", "application/vnd.schemaregistry.v1+json")
	response, err := client.Do(request)
	if err != nil {
		return schemaCodec{}, fmt.Errorf("register schema: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return schemaCodec{}, fmt.Errorf("read schema registry response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return schemaCodec{}, fmt.Errorf("register schema: status %d: %s", response.StatusCode, responseBody)
	}
	var result struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return schemaCodec{}, fmt.Errorf("decode schema registry response: %w", err)
	}
	if result.ID <= 0 {
		return schemaCodec{}, fmt.Errorf("schema registry returned invalid id %d", result.ID)
	}
	return schemaCodec{schema: parsed, id: result.ID}, nil
}
