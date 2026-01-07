package storage

import (
	"context"
	"time"

	"github.com/farhan-ahmed1/spool/internal/task"
)

// Storage defines the interface for persisting tasks and results
type Storage interface {
	// SaveTask persists a task
	SaveTask(ctx context.Context, t *task.Task) error

	// GetTask retrieves a task by ID
	GetTask(ctx context.Context, taskID string) (*task.Task, error)

	// UpdateTaskState updates the state of a task
	UpdateTaskState(ctx context.Context, taskID string, state task.State) error

	// SaveResult persists a task result
	SaveResult(ctx context.Context, result *task.Result) error

	// GetResult retrieves a task result by task ID
	GetResult(ctx context.Context, taskID string) (*task.Result, error)

	// GetTasksByState retrieves tasks by their state
	GetTasksByState(ctx context.Context, state task.State, limit int) ([]*task.Task, error)

	// DeleteTask removes a task from storage
	DeleteTask(ctx context.Context, taskID string) error

	// Close closes the storage connection
	Close() error
}

// TaskStats holds statistics about tasks
type TaskStats struct {
	Total      int64
	Pending    int64
	Processing int64
	Completed  int64
	Failed     int64
	Retrying   int64
	DeadLetter int64
	LastUpdate time.Time
}
