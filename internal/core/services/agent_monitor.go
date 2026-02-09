package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"picpic.render/internal/core/domain"
	"picpic.render/internal/core/ports"
)

const (
	agentHeartbeatTimeout = 90 * time.Second // Consider agent offline after 90s
)

type AgentMonitor struct {
	agentRepo ports.AgentRepository
	alertChan chan AgentAlert
}

type AgentAlert struct {
	AgentID   int64
	AgentName string
	Event     string // "offline", "online", "unhealthy"
	Timestamp time.Time
}

func NewAgentMonitor(agentRepo ports.AgentRepository) *AgentMonitor {
	return &AgentMonitor{
		agentRepo: agentRepo,
		alertChan: make(chan AgentAlert, 100),
	}
}

// Start begins monitoring agents
func (am *AgentMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			am.checkAgents(ctx)
		}
	}
}

// checkAgents checks all agents for offline status
func (am *AgentMonitor) checkAgents(ctx context.Context) {
	agents, err := am.agentRepo.ListAgents(ctx)
	if err != nil {
		log.Printf("Failed to list agents for monitoring: %v", err)
		return
	}

	now := time.Now()
	for _, agent := range agents {
		timeSinceHeartbeat := now.Sub(agent.LastHeartbeat)

		if timeSinceHeartbeat > agentHeartbeatTimeout {
			// Agent is offline
			am.alertChan <- AgentAlert{
				AgentID:   agent.ID,
				AgentName: agent.Name,
				Event:     "offline",
				Timestamp: now,
			}
		}
	}
}

// Alerts returns the alert channel
func (am *AgentMonitor) Alerts() <-chan AgentAlert {
	return am.alertChan
}

// SendWebhook sends alert to a webhook URL
func (am *AgentMonitor) SendWebhook(webhookURL string, alert AgentAlert) error {
	// TODO: Implement webhook sending
	// For now, just log
	log.Printf("ALERT: Agent %s (%d) is %s at %v",
		alert.AgentName, alert.AgentID, alert.Event, alert.Timestamp)
	return nil
}

// GetAgentStatus returns the current status of an agent
func (am *AgentMonitor) GetAgentStatus(ctx context.Context, agentID int64) (string, error) {
	agent, err := am.agentRepo.GetAgent(ctx, agentID)
	if err != nil {
		return "", fmt.Errorf("failed to get agent: %w", err)
	}

	timeSinceHeartbeat := time.Since(agent.LastHeartbeat)
	if timeSinceHeartbeat > agentHeartbeatTimeout {
		return "offline", nil
	}

	return "online", nil
}

// GetAgentHealth returns health metrics for an agent
func (am *AgentMonitor) GetAgentHealth(ctx context.Context, agentID int64) (*domain.Agent, error) {
	return am.agentRepo.GetAgent(ctx, agentID)
}
