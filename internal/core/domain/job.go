package domain

import (
	"time"

	"gorm.io/plugin/optimisticlock"
)

type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusSuccess   JobStatus = "success"
	JobStatusFailure   JobStatus = "failure"
	JobStatusCancelled JobStatus = "cancelled"
)

type Job struct {
	ID            string                 `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Status        JobStatus              `json:"status"`
	Priority      int                    `json:"priority" gorm:"default:0;index"` // 0-10, higher = more priority
	UserID        string                 `json:"user_id" gorm:"index"`            // User ID for tracking
	Name          string                 `json:"name"`                            // Job name/identifier for tracking
	TimeLimit     int64                  `json:"time_limit"`                      // Execution time limit in seconds (0 = no limit)
	InputURL      string                 `json:"input_url"`
	OutputURL     string                 `json:"output_url"`
	TotalDuration int64                  `json:"total_duration"` // In microseconds
	CurrentTime   int64                  `json:"current_time"`   // In microseconds
	Progress      float64                `json:"progress"`
	AgentID       *string                `json:"agent_id" gorm:"type:uuid"`
	Logs          string                 `json:"logs"`
	Config        string                 `json:"config"`                         // YAML config for Woodpecker
	Cancelled     bool                   `json:"cancelled" gorm:"default:false"` // Cancellation flag
	RetryCount    int                    `json:"retry_count" gorm:"default:0"`   // Number of retries
	MaxRetries    int                    `json:"max_retries" gorm:"default:3"`   // Max retry attempts
	Version       optimisticlock.Version `json:"-"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// TableName overrides the table name used by User to `profiles`
func (Job) TableName() string {
	return "jobs"
}
