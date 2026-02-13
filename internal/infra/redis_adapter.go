// Package infra provides concrete infrastructure adapters for Redis.
//
// This adapter wraps go-redis v9 and implements both the fabric.RedisClient
// and fabric.RedisPubSubClient interfaces. If go-redis is not available,
// the app falls back to in-memory stores in main.go.
package infra

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// GoRedisAdapter wraps go-redis v9 to implement the minimal interfaces
// expected by RedisHubStore and RedisEventBus.
type GoRedisAdapter struct {
	rdb *redis.Client
}

// NewGoRedisAdapter attempts to connect to Redis using the provided options.
// Returns the adapter and any connection error (caller decides whether to
// fall back to in-memory).
func NewGoRedisAdapter(addr, password string, db int) (*GoRedisAdapter, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		PoolSize:     20,
	})

	// Ping to verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		return nil, fmt.Errorf("redis ping failed (%s): %w", addr, err)
	}

	slog.Info("Redis connected", "addr", addr, "db", db)
	return &GoRedisAdapter{rdb: rdb}, nil
}

// Close shuts down the underlying redis client.
func (a *GoRedisAdapter) Close() error {
	return a.rdb.Close()
}

// =============================================================================
// fabric.RedisClient implementation
// =============================================================================

func (a *GoRedisAdapter) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return a.rdb.Set(ctx, key, value, ttl).Err()
}

func (a *GoRedisAdapter) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := a.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return val, err
}

func (a *GoRedisAdapter) Del(ctx context.Context, keys ...string) error {
	return a.rdb.Del(ctx, keys...).Err()
}

func (a *GoRedisAdapter) SAdd(ctx context.Context, key string, members ...string) error {
	ifaces := make([]interface{}, len(members))
	for i, m := range members {
		ifaces[i] = m
	}
	return a.rdb.SAdd(ctx, key, ifaces...).Err()
}

func (a *GoRedisAdapter) SRem(ctx context.Context, key string, members ...string) error {
	ifaces := make([]interface{}, len(members))
	for i, m := range members {
		ifaces[i] = m
	}
	return a.rdb.SRem(ctx, key, ifaces...).Err()
}

func (a *GoRedisAdapter) SMembers(ctx context.Context, key string) ([]string, error) {
	return a.rdb.SMembers(ctx, key).Result()
}

func (a *GoRedisAdapter) Publish(ctx context.Context, channel string, message []byte) error {
	return a.rdb.Publish(ctx, channel, message).Err()
}

// =============================================================================
// fabric.RedisPubSubClient implementation
// =============================================================================

// Subscribe registers a handler for messages on a Redis Pub/Sub channel.
// Returns an unsubscribe function.
func (a *GoRedisAdapter) Subscribe(ctx context.Context, channel string, handler func([]byte)) (func(), error) {
	sub := a.rdb.Subscribe(ctx, channel)

	// Wait for subscription confirmation
	_, err := sub.Receive(ctx)
	if err != nil {
		sub.Close()
		return nil, fmt.Errorf("subscribe to %s: %w", channel, err)
	}

	// Process messages in a goroutine
	ch := sub.Channel()
	go func() {
		for msg := range ch {
			handler([]byte(msg.Payload))
		}
	}()

	unsub := func() {
		sub.Close()
	}

	return unsub, nil
}
