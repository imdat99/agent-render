package redis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"picpic.render/internal/core/domain"
	"picpic.render/internal/core/ports"
)

const (
	JobQueueKey    = "job:queue"
	LogChannel     = "job:logs"
	CancelChannel  = "job:cancels" // Channel for job cancellation signals
)

type RedisAdapter struct {
	client *redis.Client
}

func NewRedisAdapter(url string) (ports.JobQueue, ports.LogPubSub, *redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, nil, nil, err
	}
	client := redis.NewClient(opts)
	return &RedisAdapter{client: client}, &RedisAdapter{client: client}, client, nil
}

// Queue Implementation
func (r *RedisAdapter) Enqueue(ctx context.Context, job *domain.Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return r.client.RPush(ctx, JobQueueKey, data).Err()
}

func (r *RedisAdapter) Dequeue(ctx context.Context) (*domain.Job, error) {
	// Blocking pop with loop to respect context cancellation more reliably
	// We use a short timeout (1s) and check context in loop
	for {
		// Check context before call
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		res, err := r.client.BLPop(ctx, 1*time.Second, JobQueueKey).Result()
		if err != nil {
			if err == redis.Nil {
				continue // Timeout, retry
			}
			// If context is canceled, BLPop might return error, we should return ctx.Err() if possibly
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}

		// res[0] is key, res[1] is value
		var job domain.Job
		if err := json.Unmarshal([]byte(res[1]), &job); err != nil {
			return nil, err
		}
		return &job, nil
	}
}

// PubSub Implementation
func (r *RedisAdapter) Publish(ctx context.Context, jobID string, logLine string, progress float64) error {
	entry := domain.LogEntry{
		JobID:    jobID,
		Line:     logLine,
		Progress: progress,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return r.client.Publish(ctx, LogChannel, data).Err()
}

func (r *RedisAdapter) Subscribe(ctx context.Context, jobID string) (<-chan domain.LogEntry, error) {
	pubsub := r.client.Subscribe(ctx, LogChannel)
	ch := make(chan domain.LogEntry)

	go func() {
		defer pubsub.Close()
		defer close(ch)

		for msg := range pubsub.Channel() {
			var entry domain.LogEntry
			if err := json.Unmarshal([]byte(msg.Payload), &entry); err != nil {
				continue
			}
			// Filter by JobID if needed, though currently we subscribe to global channel
			// In a real app we might use specific channels per job
			// For simplicity let's filter here or use pattern subscribe
			if jobID == "" || entry.JobID == jobID {
				ch <- entry
			}
		}
	}()
	return ch, nil
}
