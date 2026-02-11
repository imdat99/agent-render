package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"picpic.render/internal/core/domain"
	"picpic.render/internal/core/ports"
)

const (
	JobQueueKey      = "job:queue"
	LogChannel       = "job:logs"
	ResourceChannel  = "agent:resources"
	CancelChannel    = "job:cancels" // Channel for job cancellation signals
	JobUpdateChannel = "job:updates" // Channel for job status updates
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

// Queue Implementation with Priority Support
func (r *RedisAdapter) Enqueue(ctx context.Context, job *domain.Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}

	// Use sorted set with score = -(priority * 1000000 + timestamp)
	// This ensures higher priority jobs come first, and within same priority, FIFO
	timestamp := time.Now().UnixNano()
	score := float64(-(int64(job.Priority) * 1000000000) - timestamp)

	return r.client.ZAdd(ctx, JobQueueKey, redis.Z{
		Score:  score,
		Member: data,
	}).Err()
}

func (r *RedisAdapter) Dequeue(ctx context.Context) (*domain.Job, error) {
	// Use sorted set to get highest priority job
	for {
		// Check context before call
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Get the job with lowest score (highest priority)
		// ZPOPMIN is atomic and removes the element
		res, err := r.client.ZPopMin(ctx, JobQueueKey, 1).Result()
		if err != nil {
			if err == redis.Nil {
				// Queue is empty, wait a bit and retry
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(1 * time.Second):
					continue
				}
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}

		if len(res) == 0 {
			// Queue is empty, wait and retry
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(1 * time.Second):
				continue
			}
		}

		// res[0].Member is the job data
		var job domain.Job
		if err := json.Unmarshal([]byte(res[0].Member.(string)), &job); err != nil {
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

func (r *RedisAdapter) PublishResource(ctx context.Context, agentID string, data []byte) error {
	// data is assumed to be JSON {"cpu":..., "ram":...}
	// We want to wrap it in SystemResource struct
	var res map[string]interface{}
	if err := json.Unmarshal(data, &res); err != nil {
		return err
	}

	// Better parsing if we trust the source or just pass it as is?
	// The problem is `data` is `[]byte`.
	// Let's decode to `domain.SystemResource` partially?
	// Or just create a struct.
	// Let's assume input JSON matches `cpu` and `ram` keys.

	resource := domain.SystemResource{
		AgentID: agentID,
	}
	if cpu, ok := res["cpu"].(float64); ok {
		resource.CPU = cpu
	}
	if ram, ok := res["ram"].(float64); ok {
		resource.RAM = ram
	}

	payload, err := json.Marshal(resource)
	if err != nil {
		return err
	}

	return r.client.Publish(ctx, ResourceChannel, payload).Err()
}

func (r *RedisAdapter) SubscribeResources(ctx context.Context) (<-chan domain.SystemResource, error) {
	pubsub := r.client.Subscribe(ctx, ResourceChannel)
	ch := make(chan domain.SystemResource)

	go func() {
		defer pubsub.Close()
		defer close(ch)

		for msg := range pubsub.Channel() {
			var entry domain.SystemResource
			if err := json.Unmarshal([]byte(msg.Payload), &entry); err != nil {
				continue
			}
			ch <- entry
		}
	}()
	return ch, nil
}

// Cancel PubSub implementation
func (r *RedisAdapter) PublishCancel(ctx context.Context, agentID string, jobID string) error {
	channel := fmt.Sprintf("agent:%s:cancels", agentID)
	// Publish just the job ID
	return r.client.Publish(ctx, channel, jobID).Err()
}

func (r *RedisAdapter) SubscribeCancel(ctx context.Context, agentID string) (<-chan string, error) {
	channel := fmt.Sprintf("agent:%s:cancels", agentID)
	pubsub := r.client.Subscribe(ctx, channel)
	ch := make(chan string)

	go func() {
		defer pubsub.Close()
		defer close(ch)

		for msg := range pubsub.Channel() {
			ch <- msg.Payload // Payload is jobID
		}
	}()
	return ch, nil
}

// Job Updates PubSub implementation
func (r *RedisAdapter) PublishJobUpdate(ctx context.Context, jobID string, status string) error {
	payload := map[string]string{
		"job_id": jobID,
		"status": status,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return r.client.Publish(ctx, JobUpdateChannel, data).Err()
}

func (r *RedisAdapter) SubscribeJobUpdates(ctx context.Context) (<-chan string, error) {
	pubsub := r.client.Subscribe(ctx, JobUpdateChannel)
	ch := make(chan string)

	go func() {
		defer pubsub.Close()
		defer close(ch)

		for msg := range pubsub.Channel() {
			ch <- msg.Payload
		}
	}()
	return ch, nil
}
