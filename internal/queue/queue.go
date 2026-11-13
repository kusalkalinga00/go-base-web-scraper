package queue

import (
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// StreamKey is the Redis stream that holds pending task IDs.
	StreamKey = "scrape:stream"
	// ConsumerGroup is shared by every worker process.
	ConsumerGroup = "scrape-workers"
	// reclaimIdle is how long a message may sit unacknowledged before another
	// worker claims it. It must be longer than the maximum job runtime.
	reclaimIdle = 10 * time.Minute
)

// Message identifies a stream entry and the job it carries.
type Message struct {
	ID     string
	TaskID string
}

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
		if lastErr == nil {
			lastErr = ensureGroup(ctx, client)
		}
		cancel()
		if lastErr == nil {
			return client, nil
		}
		time.Sleep(time.Second)
	}
	_ = client.Close()
	return nil, lastErr
}

func ensureGroup(ctx context.Context, client *redis.Client) error {
	err := client.XGroupCreateMkStream(ctx, StreamKey, ConsumerGroup, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

// Enqueue adds a task to the stream.
func Enqueue(ctx context.Context, client *redis.Client, taskID string) error {
	return client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamKey,
		Values: map[string]any{"task_id": taskID},
	}).Err()
}

// Dequeue blocks until a task is available for the given consumer.
//
// Messages stay pending until Ack is called, so a worker crash does not lose
// the job: another worker reclaims it after reclaimIdle.
func Dequeue(ctx context.Context, client *redis.Client, consumer string, block time.Duration) (Message, error) {
	streams, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    ConsumerGroup,
		Consumer: consumer,
		Streams:  []string{StreamKey, ">"},
		Count:    1,
		Block:    block,
	}).Result()
	if err != nil {
		if strings.Contains(err.Error(), "NOGROUP") {
			if gerr := ensureGroup(ctx, client); gerr == nil {
				return Dequeue(ctx, client, consumer, block)
			}
		}
		return Message{}, err
	}
	for _, stream := range streams {
		for _, m := range stream.Messages {
			if taskID, ok := m.Values["task_id"].(string); ok && taskID != "" {
				return Message{ID: m.ID, TaskID: taskID}, nil
			}
		}
	}
	return Message{}, redis.Nil
}

// Ack marks a message as processed and removes it from the stream.
func Ack(ctx context.Context, client *redis.Client, id string) error {
	pipe := client.TxPipeline()
	pipe.XAck(ctx, StreamKey, ConsumerGroup, id)
	pipe.XDel(ctx, StreamKey, id)
	_, err := pipe.Exec(ctx)
	return err
}

// Reclaim takes over messages that have been pending for too long (for
// example because their worker crashed).
func Reclaim(ctx context.Context, client *redis.Client, consumer string, count int64) ([]Message, error) {
	var claimed []Message
	start := "0-0"
	for {
		messages, next, err := client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   StreamKey,
			Group:    ConsumerGroup,
			Consumer: consumer,
			MinIdle:  reclaimIdle,
			Start:    start,
			Count:    count,
		}).Result()
		if err != nil {
			if strings.Contains(err.Error(), "NOGROUP") {
				return claimed, nil
			}
			return claimed, err
		}
		for _, m := range messages {
			if taskID, ok := m.Values["task_id"].(string); ok && taskID != "" {
				claimed = append(claimed, Message{ID: m.ID, TaskID: taskID})
			}
		}
		if next == "0-0" || next == "" || len(messages) == 0 {
			return claimed, nil
		}
		start = next
	}
}

// RecoverPending requeues messages that were pending when the process last
// stopped. Call it once at startup, before consumers begin reading. It moves
// each pending entry back to the head of the stream so normal consumers pick
// it up, then removes the original entry.
func RecoverPending(ctx context.Context, client *redis.Client) (int, error) {
	recovered := 0
	start := "0-0"
	for {
		messages, next, err := client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   StreamKey,
			Group:    ConsumerGroup,
			Consumer: "recovery",
			MinIdle:  0,
			Start:    start,
			Count:    100,
		}).Result()
		if err != nil {
			if strings.Contains(err.Error(), "NOGROUP") {
				return recovered, nil
			}
			return recovered, err
		}
		for _, m := range messages {
			taskID, ok := m.Values["task_id"].(string)
			if ok && taskID != "" {
				if err := Enqueue(ctx, client, taskID); err != nil {
					return recovered, err
				}
				recovered++
			}
			if err := Ack(ctx, client, m.ID); err != nil {
				return recovered, err
			}
		}
		if next == "0-0" || next == "" || len(messages) == 0 {
			return recovered, nil
		}
		start = next
	}
}

// Length reports how many tasks are queued or currently being processed.
func Length(ctx context.Context, client *redis.Client) (int64, error) {
	return client.XLen(ctx, StreamKey).Result()
}

func Ping(ctx context.Context, client *redis.Client) bool {
	pong, err := client.Ping(ctx).Result()
	return err == nil && pong == "PONG"
}
