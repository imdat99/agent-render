package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"picpic.render/internal/core/domain"
	"picpic.render/internal/core/services"
	"picpic.render/proto"
)

// Mock Repositories
type mockAgentRepo struct {
	GetAgentFunc func(ctx context.Context, id string) (*domain.Agent, error)
	CreateFunc   func(ctx context.Context, agent *domain.Agent) error
}

func (m *mockAgentRepo) CreateOrUpdate(ctx context.Context, agent *domain.Agent) error {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, agent)
	}
	return nil
}

func (m *mockAgentRepo) GetAgent(ctx context.Context, id string) (*domain.Agent, error) {
	if m.GetAgentFunc != nil {
		return m.GetAgentFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockAgentRepo) GetAgentByName(ctx context.Context, name string) (*domain.Agent, error) {
	return nil, nil
}

func (m *mockAgentRepo) UpdateHeartbeat(ctx context.Context, id string) error {
	return nil
}

func (m *mockAgentRepo) ListAgents(ctx context.Context) ([]*domain.Agent, error) {
	return nil, nil
}

type mockJobRepo struct{}

func (m *mockJobRepo) Create(ctx context.Context, job *domain.Job) error          { return nil }
func (m *mockJobRepo) GetJob(ctx context.Context, id string) (*domain.Job, error) { return nil, nil }
func (m *mockJobRepo) Update(ctx context.Context, job *domain.Job) error          { return nil }
func (m *mockJobRepo) ListJobs(ctx context.Context, offset, limit int) ([]*domain.Job, error) {
	return nil, nil
}
func (m *mockJobRepo) CountJobs(ctx context.Context) (int64, error) { return 0, nil }
func (m *mockJobRepo) CountActiveJobsByAgent(ctx context.Context, agentID string) (int64, error) {
	return 0, nil
}
func (m *mockJobRepo) CountJobsByAgent(ctx context.Context, agentID string) (int64, error) {
	return 0, nil
}
func (m *mockJobRepo) ListJobsByAgent(ctx context.Context, agentID string, offset, limit int) ([]*domain.Job, error) {
	return nil, nil
}

// Simplified queues for test
type mockQueue struct{}

func (m *mockQueue) Enqueue(ctx context.Context, job *domain.Job) error { return nil }
func (m *mockQueue) Dequeue(ctx context.Context) (*domain.Job, error)   { return nil, nil }

type mockPubSub struct{}

func (m *mockPubSub) Publish(ctx context.Context, jobID string, logLine string, progress float64) error {
	return nil
}
func (m *mockPubSub) Subscribe(ctx context.Context, jobID string) (<-chan domain.LogEntry, error) {
	return nil, nil
}
func (m *mockPubSub) PublishResource(ctx context.Context, agentID string, data []byte) error {
	return nil
}
func (m *mockPubSub) SubscribeResources(ctx context.Context) (<-chan domain.SystemResource, error) {
	return nil, nil
}
func (m *mockPubSub) PublishCancel(ctx context.Context, agentID string, jobID string) error {
	return nil
}
func (m *mockPubSub) SubscribeCancel(ctx context.Context, agentID string) (<-chan string, error) {
	return nil, nil
}
func (m *mockPubSub) PublishJobUpdate(ctx context.Context, jobID string, status string) error {
	return nil
}
func (m *mockPubSub) SubscribeJobUpdates(ctx context.Context) (<-chan string, error) { return nil, nil }

func TestServer_Auth_StripsPrefix(t *testing.T) {
	// Setup
	pureUUID := uuid.New().String()
	prefixedID := "agent-" + pureUUID

	agentRepo := &mockAgentRepo{
		GetAgentFunc: func(ctx context.Context, id string) (*domain.Agent, error) {
			if id == pureUUID {
				return &domain.Agent{
					ID:            pureUUID,
					Name:          "Test Agent",
					LastHeartbeat: time.Now(),
				}, nil
			}
			if id == prefixedID {
				t.Errorf("GetAgent called with prefixed ID: %s", id)
			}
			return nil, nil // Not found
		},
	}

	jobService := services.NewJobService(&mockJobRepo{}, agentRepo, &mockQueue{}, &mockPubSub{})
	server := NewServer(jobService)

	// Execute Auth with prefixed ID
	req := &proto.AuthRequest{
		AgentToken: "secret",
		AgentId:    prefixedID,
		Hostname:   "test-host",
	}

	resp, err := server.Auth(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("Auth failed: %v", err)
	}

	if resp.AgentId != pureUUID {
		t.Errorf("Expected Auth to return pure UUID %s, got %s", pureUUID, resp.AgentId)
	}
}
