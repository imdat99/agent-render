package grpc

import (
	"time"

	"picpic.render/internal/core/domain"
)

// GetActiveAgents returns all agents that are currently active (have recent heartbeat)
func (s *Server) GetActiveAgents(timeout time.Duration) []*domain.Agent {
	return s.agentManager.ListActive(timeout)
}

// GetAllAgents returns all registered agents regardless of status
func (s *Server) GetAllAgents() []*domain.Agent {
	return s.agentManager.ListAll()
}

// GetAgentCount returns the total number of registered agents
func (s *Server) GetAgentCount() int {
	return s.agentManager.Count()
}

// GetActiveAgentCount returns the number of active agents
func (s *Server) GetActiveAgentCount(timeout time.Duration) int {
	return s.agentManager.CountActive(timeout)
}
