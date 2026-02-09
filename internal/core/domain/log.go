package domain

type LogEntry struct {
	JobID    string  `json:"job_id"`
	Line     string  `json:"line"`
	Progress float64 `json:"progress"`
}
