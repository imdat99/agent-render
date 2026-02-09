package grpc

import (
	"sync"
	"time"

	"picpic.render/internal/core/domain"
)

// AgentInfo stores in-memory agent information
type AgentInfo struct {
	ID            int64
	Name          string
	Platform      string
	Backend       string
	Version       string
	Capacity      int32
	LastHeartbeat time.Time
	ConnectedAt   time.Time
}

// AgentManager manages active agent connections in memory
type AgentManager struct {
	mu     sync.RWMutex
	agents map[int64]*AgentInfo // AgentID -> AgentInfo
}

// NewAgentManager creates a new in-memory agent manager
func NewAgentManager() *AgentManager {
	return &AgentManager{
		agents: make(map[int64]*AgentInfo),
	}
}

// Register adds or updates an agent in memory
func (am *AgentManager) Register(id int64, name, platform, backend, version string, capacity int32) {
	am.mu.Lock()
	defer am.mu.Unlock()

	now := time.Now()
	if existing, ok := am.agents[id]; ok {
		// Update existing agent
		existing.Name = name
		existing.Platform = platform
		existing.Backend = backend
		existing.Version = version
		existing.Capacity = capacity
		existing.LastHeartbeat = now
	} else {
		// Register new agent
		am.agents[id] = &AgentInfo{
			ID:            id,
			Name:          name,
			Platform:      platform,
			Backend:       backend,
			Version:       version,
			Capacity:      capacity,
			LastHeartbeat: now,
			ConnectedAt:   now,
		}
	}
}

// UpdateHeartbeat updates the last heartbeat time for an agent
func (am *AgentManager) UpdateHeartbeat(id int64) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if agent, ok := am.agents[id]; ok {
		agent.LastHeartbeat = time.Now()
	}
}

// Unregister removes an agent from memory
func (am *AgentManager) Unregister(id int64) {
	am.mu.Lock()
	defer am.mu.Unlock()

	delete(am.agents, id)
}

// Get retrieves an agent by ID
func (am *AgentManager) Get(id int64) (*AgentInfo, bool) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	agent, ok := am.agents[id]
	return agent, ok
}

// ListActive returns all agents that have heartbeat within the timeout period
func (am *AgentManager) ListActive(timeout time.Duration) []*domain.Agent {
	am.mu.RLock()
	defer am.mu.RUnlock()

	now := time.Now()
	var active []*domain.Agent

	for _, info := range am.agents {
		if now.Sub(info.LastHeartbeat) < timeout {
			active = append(active, &domain.Agent{
				ID:            info.ID,
				Name:          info.Name,
				Platform:      info.Platform,
				Backend:       info.Backend,
				Version:       info.Version,
				Capacity:      info.Capacity,
				Status:        domain.AgentStatusOnline,
				LastHeartbeat: info.LastHeartbeat,
				CreatedAt:     info.ConnectedAt,
				UpdatedAt:     info.LastHeartbeat,
			})
		}
	}

	return active
}

// ListAll returns all registered agents regardless of heartbeat status
func (am *AgentManager) ListAll() []*domain.Agent {
	am.mu.RLock()
	defer am.mu.RUnlock()

	now := time.Now()
	var all []*domain.Agent

	for _, info := range am.agents {
		status := domain.AgentStatusOnline
		if now.Sub(info.LastHeartbeat) >= 60*time.Second {
			status = domain.AgentStatusOffline
		}

		all = append(all, &domain.Agent{
			ID:            info.ID,
			Name:          info.Name,
			Platform:      info.Platform,
			Backend:       info.Backend,
			Version:       info.Version,
			Capacity:      info.Capacity,
			Status:        status,
			LastHeartbeat: info.LastHeartbeat,
			CreatedAt:     info.ConnectedAt,
			UpdatedAt:     info.LastHeartbeat,
		})
	}

	return all
}

// Count returns the number of registered agents
func (am *AgentManager) Count() int {
	am.mu.RLock()
	defer am.mu.RUnlock()

	return len(am.agents)
}

// CountActive returns the number of active agents (with recent heartbeat)
func (am *AgentManager) CountActive(timeout time.Duration) int {
	am.mu.RLock()
	defer am.mu.RUnlock()

	now := time.Now()
	count := 0

	for _, info := range am.agents {
		if now.Sub(info.LastHeartbeat) < timeout {
			count++
		}
	}

	return count
}

// CleanupStale removes agents that haven't sent heartbeat for a long time
func (am *AgentManager) CleanupStale(timeout time.Duration) int {
	am.mu.Lock()
	defer am.mu.Unlock()

	now := time.Now()
	removed := 0

	for id, info := range am.agents {
		if now.Sub(info.LastHeartbeat) > timeout {
			delete(am.agents, id)
			removed++
		}
	}

	return removed
}
