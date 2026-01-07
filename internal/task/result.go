package task

import (
	"encoding/json"
	"time"
)

// Result represents the outcome of task execution
type Result struct {
	TaskID      string          `json:"task_id"`
	Success     bool            `json:"success"`
	Output      json.RawMessage `json:"output,omitempty"`
	Error       string          `json:"error,omitempty"`
	CompletedAt time.Time       `json:"completed_at"`
	Duration    time.Duration   `json:"duration"`
}
