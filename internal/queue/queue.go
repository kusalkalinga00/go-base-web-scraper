package queue

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const QueueKey = "scrape:queue"

func Connect(redisURL string) (*redis.Client, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opt)

	var lastErr error
	for attempt := 1; attempt <= 20; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		lastErr = client.Ping(ctx).Err()
		cancel()
		if lastErr == nil {
			return client, nil
		}
		time.Sleep(time.Second)
	}
	_ = client.Close()
	return nil, lastErr
}

func Enqueue(ctx context.Context, client *redis.Client, taskID string) error {
	return client.LPush(ctx, QueueKey, taskID).Err()
}

func Dequeue(ctx context.Context, client *redis.Client, block time.Duration) (string, error) {
	result, err := client.BRPop(ctx, block, QueueKey).Result()
	if err != nil {
		return "", err
	}
	if len(result) < 2 {
		return "", redis.Nil
	}
	return result[1], nil
}

func Length(ctx context.Context, client *redis.Client) (int64, error) {
	return client.LLen(ctx, QueueKey).Result()
}

func Ping(ctx context.Context, client *redis.Client) bool {
	pong, err := client.Ping(ctx).Result()
	return err == nil && pong == "PONG"
}
