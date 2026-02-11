package domain

import (
	"time"

	"gorm.io/plugin/optimisticlock"
)

type AgentStatus string

const (
	AgentStatusOnline  AgentStatus = "online"
	AgentStatusOffline AgentStatus = "offline"
	AgentStatusBusy    AgentStatus = "busy"
)

type Agent struct {
	ID            string                 `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Name          string                 `json:"name"`
	Platform      string                 `json:"platform"`
	Backend       string                 `json:"backend"`
	Version       string                 `json:"version"`
	LockVersion   optimisticlock.Version `json:"-"` // Optimistic Lock
	Capacity      int32                  `json:"capacity"`
	Status        AgentStatus            `json:"status" gorm:"-"` // Status is transient (Redis)
	CPU           float64                `json:"cpu" gorm:"-"`
	RAM           float64                `json:"ram" gorm:"-"`
	LastHeartbeat time.Time              `json:"last_heartbeat"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

func (Agent) TableName() string {
	return "agents"
}
