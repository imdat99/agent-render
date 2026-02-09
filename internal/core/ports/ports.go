package ports

import (
	"context"

	"picpic.render/internal/core/domain"
)

type JobRepository interface {
	Create(ctx context.Context, job *domain.Job) error
	GetJob(ctx context.Context, id string) (*domain.Job, error)
	Update(ctx context.Context, job *domain.Job) error
	ListJobs(ctx context.Context, offset, limit int) ([]*domain.Job, error)
	CountJobs(ctx context.Context) (int64, error)
}

type AgentRepository interface {
	CreateOrUpdate(ctx context.Context, agent *domain.Agent) error
	GetAgent(ctx context.Context, id int64) (*domain.Agent, error)
	GetAgentByName(ctx context.Context, name string) (*domain.Agent, error)
	ListAgents(ctx context.Context) ([]*domain.Agent, error)
}

type JobQueue interface {
	Enqueue(ctx context.Context, job *domain.Job) error
	Dequeue(ctx context.Context) (*domain.Job, error) // Blocking wait
}

type LogPubSub interface {
	Publish(ctx context.Context, jobID string, logLine string, progress float64) error
	Subscribe(ctx context.Context, jobID string) (<-chan domain.LogEntry, error)
}
