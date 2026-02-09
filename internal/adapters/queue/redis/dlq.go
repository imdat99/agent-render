package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"picpic.render/internal/core/domain"
)

const (
	dlqKey        = "picpic:dlq"
	dlqMetaPrefix = "picpic:dlq:meta:"
)

type DeadLetterQueue struct {
	client *redis.Client
}

type DLQEntry struct {
	Job         *domain.Job `json:"job"`
	FailureTime time.Time   `json:"failure_time"`
	Reason      string      `json:"reason"`
	RetryCount  int         `json:"retry_count"`
}

func NewDeadLetterQueue(client *redis.Client) *DeadLetterQueue {
	return &DeadLetterQueue{client: client}
}

// Add adds a failed job to the DLQ
func (dlq *DeadLetterQueue) Add(ctx context.Context, job *domain.Job, reason string) error {
	entry := DLQEntry{
		Job:         job,
		FailureTime: time.Now(),
		Reason:      reason,
		RetryCount:  job.RetryCount,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal DLQ entry: %w", err)
	}

	// Add to sorted set with timestamp as score
	score := float64(time.Now().Unix())
	if err := dlq.client.ZAdd(ctx, dlqKey, redis.Z{
		Score:  score,
		Member: job.ID,
	}).Err(); err != nil {
		return fmt.Errorf("failed to add to DLQ: %w", err)
	}

	// Store metadata
	metaKey := dlqMetaPrefix + job.ID
	if err := dlq.client.Set(ctx, metaKey, data, 0).Err(); err != nil {
		return fmt.Errorf("failed to store DLQ metadata: %w", err)
	}

	return nil
}

// Get retrieves a job from the DLQ
func (dlq *DeadLetterQueue) Get(ctx context.Context, jobID string) (*DLQEntry, error) {
	metaKey := dlqMetaPrefix + jobID
	data, err := dlq.client.Get(ctx, metaKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("job not found in DLQ")
		}
		return nil, fmt.Errorf("failed to get DLQ entry: %w", err)
	}

	var entry DLQEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DLQ entry: %w", err)
	}

	return &entry, nil
}

// List returns all jobs in the DLQ
func (dlq *DeadLetterQueue) List(ctx context.Context, offset, limit int64) ([]*DLQEntry, error) {
	// Get job IDs from sorted set (newest first)
	jobIDs, err := dlq.client.ZRevRange(ctx, dlqKey, offset, offset+limit-1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list DLQ: %w", err)
	}

	entries := make([]*DLQEntry, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		entry, err := dlq.Get(ctx, jobID)
		if err != nil {
			// Skip if metadata not found
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// Remove removes a job from the DLQ
func (dlq *DeadLetterQueue) Remove(ctx context.Context, jobID string) error {
	// Remove from sorted set
	if err := dlq.client.ZRem(ctx, dlqKey, jobID).Err(); err != nil {
		return fmt.Errorf("failed to remove from DLQ: %w", err)
	}

	// Remove metadata
	metaKey := dlqMetaPrefix + jobID
	if err := dlq.client.Del(ctx, metaKey).Err(); err != nil {
		return fmt.Errorf("failed to remove DLQ metadata: %w", err)
	}

	return nil
}

// Count returns the total number of jobs in the DLQ
func (dlq *DeadLetterQueue) Count(ctx context.Context) (int64, error) {
	count, err := dlq.client.ZCard(ctx, dlqKey).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to count DLQ: %w", err)
	}
	return count, nil
}

// Retry removes a job from DLQ and returns it for retry
func (dlq *DeadLetterQueue) Retry(ctx context.Context, jobID string) (*domain.Job, error) {
	entry, err := dlq.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}

	// Remove from DLQ
	if err := dlq.Remove(ctx, jobID); err != nil {
		return nil, err
	}

	return entry.Job, nil
}
