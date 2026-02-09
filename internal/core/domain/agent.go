package domain

import "time"

type AgentStatus string

const (
	AgentStatusOnline  AgentStatus = "online"
	AgentStatusOffline AgentStatus = "offline"
	AgentStatusBusy    AgentStatus = "busy"
)

type Agent struct {
	ID            int64       `json:"id" gorm:"primaryKey"`
	Name          string      `json:"name"`
	Platform      string      `json:"platform"`
	Backend       string      `json:"backend"`
	Version       string      `json:"version"`
	Capacity      int32       `json:"capacity"`
	Status        AgentStatus `json:"status" gorm:"-"` // Status is transient (Redis)
	LastHeartbeat time.Time   `json:"last_heartbeat"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

func (Agent) TableName() string {
	return "agents"
}
