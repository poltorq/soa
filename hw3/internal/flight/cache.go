package flight

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Cache struct {
	client *redis.Client
	ttl    time.Duration
	logger *slog.Logger
}

func OpenCache(
	masterName string,
	sentinelAddresses []string,
	password string,
	ttl time.Duration,
	logger *slog.Logger,
) *Cache {
	client := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    masterName,
		SentinelAddrs: sentinelAddresses,
		Password:      password,
	})
	return &Cache{client: client, ttl: ttl, logger: logger}
}

func (c *Cache) Close() error {
	return c.client.Close()
}

func (c *Cache) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis sentinel: %w", err)
	}
	return nil
}

func (c *Cache) GetFlight(ctx context.Context, id uuid.UUID) (Flight, bool) {
	key := "flight:" + id.String()
	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		c.logger.Info("cache miss", "key", key)
		return Flight{}, false
	}
	if err != nil {
		c.logger.Warn("read flight cache", "key", key, "error", err)
		return Flight{}, false
	}
	var result Flight
	if err := json.Unmarshal(data, &result); err != nil {
		c.logger.Warn("decode flight cache", "key", key, "error", err)
		_ = c.client.Del(ctx, key).Err()
		return Flight{}, false
	}
	c.logger.Info("cache hit", "key", key)
	return result, true
}

func (c *Cache) SetFlight(ctx context.Context, item Flight) {
	c.set(ctx, "flight:"+item.ID.String(), item)
}

func (c *Cache) GetSearch(ctx context.Context, filter SearchFilter) ([]Flight, bool) {
	key := searchKey(filter)
	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		c.logger.Info("cache miss", "key", key)
		return nil, false
	}
	if err != nil {
		c.logger.Warn("read search cache", "key", key, "error", err)
		return nil, false
	}
	var result []Flight
	if err := json.Unmarshal(data, &result); err != nil {
		c.logger.Warn("decode search cache", "key", key, "error", err)
		_ = c.client.Del(ctx, key).Err()
		return nil, false
	}
	c.logger.Info("cache hit", "key", key)
	return result, true
}

func (c *Cache) SetSearch(ctx context.Context, filter SearchFilter, items []Flight) {
	c.set(ctx, searchKey(filter), items)
}

func (c *Cache) InvalidateFlightData(ctx context.Context, flightID uuid.UUID) {
	flightKey := "flight:" + flightID.String()
	if err := c.client.Del(ctx, flightKey).Err(); err != nil {
		c.logger.Warn("invalidate flight cache", "key", flightKey, "error", err)
	}
	var cursor uint64
	for {
		keys, next, err := c.client.Scan(ctx, cursor, "search:*", 100).Result()
		if err != nil {
			c.logger.Warn("scan search cache for invalidation", "error", err)
			return
		}
		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				c.logger.Warn("invalidate search cache", "error", err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	c.logger.Info("cache invalidated", "flight_id", flightID)
}

func (c *Cache) set(ctx context.Context, key string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		c.logger.Warn("encode cache value", "key", key, "error", err)
		return
	}
	if err := c.client.Set(ctx, key, data, c.ttl).Err(); err != nil {
		c.logger.Warn("write cache value", "key", key, "error", err)
	}
}

func searchKey(filter SearchFilter) string {
	date := "any"
	if filter.Date != nil {
		date = filter.Date.UTC().Format(time.DateOnly)
	}
	return strings.Join([]string{"search", filter.Origin, filter.Destination, date}, ":")
}
