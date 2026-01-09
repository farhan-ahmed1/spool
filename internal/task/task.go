package task

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Priority defines the urgency level of a task.
// Higher priority tasks are dequeued and processed before lower priority ones.
type Priority int

const (
	// PriorityLow (1) is for non-urgent background tasks
	PriorityLow Priority = iota
	// PriorityNormal (2) is the default priority for most tasks
	PriorityNormal
	// PriorityHigh (3) is for important tasks that should be processed quickly
	PriorityHigh
	// PriorityCritical (4) is for urgent tasks requiring immediate attention
	PriorityCritical
)

// State represents the lifecycle stage of a task.
// Tasks transition through these states as they are processed.
type State string

const (
	// StatePending indicates task is waiting in queue
	StatePending    State = "pending"
	// StateProcessing indicates task is currently being executed by a worker
	StateProcessing State = "processing"
	// StateCompleted indicates task finished successfully
	StateCompleted  State = "completed"
	// StateFailed indicates task failed after all retry attempts
	StateFailed     State = "failed"
	// StateRetrying indicates task is waiting for retry after a failure
	StateRetrying   State = "retrying"
	// StateDeadLetter indicates task was moved to dead letter queue after exhausting retries
	StateDeadLetter State = "dead_letter"
)

// Task represents a unit of work to be executed by a worker.
// Tasks are the core data structure in Spool, containing all information
// needed to execute, retry, and track a job.
type Task struct {
	// ID is a unique identifier for this task (UUID v4)
	ID          string                 `json:"id"`
	// Type identifies which handler should process this task
	Type        string                 `json:"type"`
	// Payload contains the task-specific data as JSON
	Payload     json.RawMessage        `json:"payload"`
	// Priority determines processing order (higher = more urgent)
	Priority    Priority               `json:"priority"`
	// State tracks the current lifecycle stage of the task
	State       State                  `json:"state"`
	// MaxRetries is the maximum number of retry attempts allowed
	MaxRetries  int                    `json:"max_retries"`
	// RetryCount tracks how many times this task has been retried
	RetryCount  int                    `json:"retry_count"`
	// Timeout is the maximum execution duration before task is killed
	Timeout     time.Duration          `json:"timeout"`
	// CreatedAt is when the task was first created
	CreatedAt   time.Time              `json:"created_at"`
	// ScheduledAt is when the task should be executed (for delayed tasks)
	ScheduledAt time.Time              `json:"scheduled_at,omitempty"`
	// StartedAt is when task execution began (nil if not started)
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	// CompletedAt is when task finished (success or failure)
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	// Error contains the error message if task failed
	Error       string                 `json:"error,omitempty"`
	// Metadata stores arbitrary key-value data for tracking/debugging
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NewTask creates a new task with sensible defaults.
// The task is initialized with:
//   - A unique UUID v4 identifier
//   - Normal priority (can be changed with WithPriority)
//   - Pending state
//   - 3 maximum retries (can be changed with WithMaxRetries)
//   - 30 second timeout (can be changed with WithTimeout)
//
// Example:
//   task, err := NewTask("send_email", map[string]interface{}{
//       "to": "user@example.com",
//       "subject": "Welcome",
//   })
//   task.WithPriority(PriorityHigh).WithTimeout(60 * time.Second)
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
