package queue

import (
	"context"
	"errors"
	"time"

	"github.com/farhan-ahmed1/spool/internal/task"
)

// Common errors
var (
	ErrNoTask = errors.New("no task available")
)

// Queue defines the interface for task queue operations
type Queue interface {
	// Enqueue adds a task to the queue with its priority
	Enqueue(ctx context.Context, t *task.Task) error

	// Dequeue retrieves the highest priority task from the queue
	// Returns nil if no task is available
	Dequeue(ctx context.Context) (*task.Task, error)

	// Peek returns the next task without removing it from the queue
	Peek(ctx context.Context) (*task.Task, error)

	// Ack acknowledges that a task has been successfully processed
	Ack(ctx context.Context, taskID string) error

	// Nack marks a task as failed and optionally re-queues it
	Nack(ctx context.Context, taskID string, requeue bool) error

	// Size returns the total number of tasks in the queue
	Size(ctx context.Context) (int64, error)

	// SizeByPriority returns the number of tasks for each priority level
	SizeByPriority(ctx context.Context) (map[task.Priority]int64, error)

	// Purge removes all tasks from the queue
	Purge(ctx context.Context) error

	// EnqueueDLQ moves a task to the dead letter queue
	EnqueueDLQ(ctx context.Context, t *task.Task, reason string) error

	// GetDLQSize returns the number of tasks in the dead letter queue
	GetDLQSize(ctx context.Context) (int64, error)

	// GetDLQTasks retrieves tasks from the dead letter queue
	GetDLQTasks(ctx context.Context, limit int) ([]*task.Task, error)

	// Close closes the queue and releases resources
	Close() error

	// Health checks if the queue is healthy and ready to serve requests
	Health(ctx context.Context) error
}

// QueueStats holds statistics about the queue
type QueueStats struct {
	TotalTasks      int64
	PendingTasks    int64
	ProcessingTasks int64
	CompletedTasks  int64
	FailedTasks     int64
	LastEnqueueTime time.Time
	LastDequeueTime time.Time
}
