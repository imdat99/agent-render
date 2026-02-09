package domain

type LogEntry struct {
	JobID    string  `json:"job_id"`
	Line     string  `json:"line"`
	Progress float64 `json:"progress"`
}

type SystemResource struct {
	AgentID int64   `json:"agent_id"`
	CPU     float64 `json:"cpu"`
	RAM     float64 `json:"ram"` // In MB
}
