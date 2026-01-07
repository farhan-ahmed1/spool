package task

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Priority levels for tasks
type Priority int

const (
	PriorityLow Priority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

// State represents the current state of a task
type State string

const (
	StatePending    State = "pending"
	StateProcessing State = "processing"
	StateCompleted  State = "completed"
	StateFailed     State = "failed"
	StateRetrying   State = "retrying"
	StateDeadLetter State = "dead_letter"
)

// Task represents a unit of work to be executed
type Task struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Payload     json.RawMessage        `json:"payload"`
	Priority    Priority               `json:"priority"`
	State       State                  `json:"state"`
	MaxRetries  int                    `json:"max_retries"`
	RetryCount  int                    `json:"retry_count"`
	Timeout     time.Duration          `json:"timeout"`
	CreatedAt   time.Time              `json:"created_at"`
	ScheduledAt time.Time              `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NewTask creates a new task with sensible defaults
func NewTask(taskType string, payload interface{}) (*Task, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &Task{
		ID:         uuid.New().String(),
		Type:       taskType,
		Payload:    payloadBytes,
		Priority:   PriorityNormal,
		State:      StatePending,
		MaxRetries: 3,
		RetryCount: 0,
		Timeout:    30 * time.Second,
		CreatedAt:  time.Now(),
		Metadata:   make(map[string]interface{}),
	}, nil
}

// WithPriority sets the task priority
func (t *Task) WithPriority(p Priority) *Task {
	t.Priority = p
	return t
}

// WithMaxRetries sets the maximum retry attempts
func (t *Task) WithMaxRetries(max int) *Task {
	t.MaxRetries = max
	return t
}

// WithTimeout sets the execution timeout
func (t *Task) WithTimeout(d time.Duration) *Task {
	t.Timeout = d
	return t
}

// WithScheduleAt schedules the task for future execution
func (t *Task) WithScheduleAt(at time.Time) *Task {
	t.ScheduledAt = at
	return t
}

// ShouldRetry returns true if the task should be retried
func (t *Task) ShouldRetry() bool {
	return t.RetryCount < t.MaxRetries
}

// GetRetryDelay calculates the delay before the next retry using exponential backoff
// Formula: min(baseDelay * 2^retryCount, maxDelay)
func (t *Task) GetRetryDelay() time.Duration {
	baseDelay := 1 * time.Second
	maxDelay := 5 * time.Minute

	delay := baseDelay * (1 << uint(t.RetryCount)) // 2^retryCount
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

// MarkStarted marks the task as started
func (t *Task) MarkStarted() {
	now := time.Now()
	t.StartedAt = &now
	t.State = StateProcessing
}

// MarkCompleted marks the task as successfully completed
func (t *Task) MarkCompleted() {
	now := time.Now()
	t.CompletedAt = &now
	t.State = StateCompleted
}

// MarkFailed marks the task as failed
func (t *Task) MarkFailed(err error) {
	t.Error = err.Error()

	// Check if we should retry BEFORE incrementing retry count
	shouldRetry := t.RetryCount < t.MaxRetries

	// Increment retry count
	t.RetryCount++

	if shouldRetry {
		t.State = StateRetrying
	} else {
		t.State = StateDeadLetter
		now := time.Now()
		t.CompletedAt = &now
	}
}
