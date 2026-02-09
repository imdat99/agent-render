package domain

import (
	"time"
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
	ID            string    `json:"id" gorm:"primaryKey"`
	Status        JobStatus `json:"status"`
	InputURL      string    `json:"input_url"`
	OutputURL     string    `json:"output_url"`
	TotalDuration int64     `json:"total_duration"` // In microseconds
	CurrentTime   int64     `json:"current_time"`   // In microseconds
	Progress      float64   `json:"progress"`
	AgentID       int64     `json:"agent_id"`
	Logs          string    `json:"logs"`
	Config        string    `json:"config"` // YAML config for Woodpecker
	Cancelled     bool      `json:"cancelled" gorm:"default:false"` // Cancellation flag
	RetryCount    int       `json:"retry_count" gorm:"default:0"`   // Number of retries
	MaxRetries    int       `json:"max_retries" gorm:"default:3"`   // Max retry attempts
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TableName overrides the table name used by User to `profiles`
func (Job) TableName() string {
	return "jobs"
}
