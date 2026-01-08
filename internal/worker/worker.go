package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/farhan-ahmed1/spool/internal/logger"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
)

// Worker polls the queue and executes tasks
type Worker struct {
	id       string
	queue    queue.Queue
	storage  storage.Storage
	registry *task.Registry
	pollFreq time.Duration
	logger   *logger.Logger

	// Graceful shutdown
	shutdownChan chan struct{}
	wg           sync.WaitGroup
	mu           sync.RWMutex
	running      bool

	// Stats
	tasksProcessed int64
	tasksFailed    int64
	tasksRetried   int64
}

// Config holds worker configuration
type Config struct {
	ID           string
	PollInterval time.Duration // How often to poll the queue
}

// NewWorker creates a new worker instance
func NewWorker(q queue.Queue, storage storage.Storage, registry *task.Registry, config Config) *Worker {
	if config.PollInterval == 0 {
		config.PollInterval = 100 * time.Millisecond
	}
	if config.ID == "" {
		config.ID = fmt.Sprintf("worker-%d", time.Now().UnixNano())
	}

	// Create logger for this worker
	workerLogger := logger.GetDefault()
	if workerLogger == nil {
		workerLogger = logger.New("info", "text", fmt.Sprintf("worker-%s", config.ID))
	} else {
		workerLogger = workerLogger.WithComponent(fmt.Sprintf("worker-%s", config.ID))
	}

	return &Worker{
		id:           config.ID,
		queue:        q,
		storage:      storage,
		registry:     registry,
		pollFreq:     config.PollInterval,
		logger:       workerLogger,
		shutdownChan: make(chan struct{}),
		running:      false,
	}
}

// Start begins the worker loop
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return fmt.Errorf("worker %s is already running", w.id)
	}
	w.running = true
	w.mu.Unlock()

	w.logger.Info("Worker starting", logger.Fields{
		"worker_id":     w.id,
		"poll_interval": w.pollFreq.String(),
	})

	w.wg.Add(1)
	go w.run(ctx)

	return nil
}

// run is the main worker loop
func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()
	defer func() {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
	}()

	ticker := time.NewTicker(w.pollFreq)
	defer ticker.Stop()

	w.logger.Debug("Worker started polling", logger.Fields{
		"poll_frequency": w.pollFreq.String(),
	})

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Worker context cancelled, shutting down")
			return
		case <-w.shutdownChan:
			w.logger.Info("Worker shutdown signal received")
			return
		case <-ticker.C:
			// Poll the queue for a task
			if err := w.pollAndExecute(ctx); err != nil {
				// Log error but continue running
				if err != queue.ErrNoTask {
					w.logger.Error("Error polling and executing task", logger.Fields{
						"error": err,
					})
				}
			}
		}
	}
}

// pollAndExecute polls for a task and executes it
func (w *Worker) pollAndExecute(ctx context.Context) error {
	// Dequeue a task
	t, err := w.queue.Dequeue(ctx)
	if err != nil {
		return err
	}
	if t == nil {
		return queue.ErrNoTask
	}

	w.logger.Info("Task picked up for execution", logger.Fields{
		"task_id":      t.ID,
		"type":         t.Type,
		"priority":     int(t.Priority),
		"attempt":      t.RetryCount + 1,
		"max_attempts": t.MaxRetries + 1,
	})

	// Save task state as processing
	t.MarkStarted()
	if w.storage != nil {
		if err := w.storage.SaveTask(ctx, t); err != nil {
			w.logger.Error("Failed to save task state", logger.Fields{
				"task_id": t.ID,
				"error":   err,
			})
		}
	}

	// Execute the task
	if err := w.executeTask(ctx, t); err != nil {
		w.mu.Lock()
		w.tasksFailed++
		w.mu.Unlock()
		w.logger.Warn("Task execution failed", logger.Fields{
			"task_id": t.ID,
			"type":    t.Type,
			"error":   err,
			"attempt": t.RetryCount + 1,
		})

		// Handle failure with retry/DLQ logic
		return w.handleTaskFailure(ctx, t, err)
	}

	w.mu.Lock()
	w.tasksProcessed++
	w.mu.Unlock()

	w.logger.Info("Task completed successfully", logger.Fields{
		"task_id": t.ID,
		"type":    t.Type,
	})

	// Mark task as completed (sets CompletedAt timestamp)
	t.MarkCompleted()

	// Save completed task state
	if w.storage != nil {
		if err := w.storage.SaveTask(ctx, t); err != nil {
			w.logger.Error("Failed to save completed task state", logger.Fields{
				"task_id": t.ID,
				"error":   err,
			})
		}
	}

	return w.queue.Ack(ctx, t.ID)
}

// handleTaskFailure manages task failures with retry logic or DLQ
func (w *Worker) handleTaskFailure(ctx context.Context, t *task.Task, execErr error) error {
	// Mark task as failed (this checks if we should retry and sets the state)
	t.MarkFailed(execErr)

	// Save updated task state
	if w.storage != nil {
		if err := w.storage.SaveTask(ctx, t); err != nil {
			w.logger.Error("Failed to save failed task state", logger.Fields{
				"task_id": t.ID,
				"error":   err,
			})
		}
	}

	// Check task state to determine if we should retry
	// MarkFailed() already determined this and set the appropriate state
	if t.State == task.StateRetrying {
		w.mu.Lock()
		w.tasksRetried++
		w.mu.Unlock()

		// Calculate retry delay
		retryDelay := t.GetRetryDelay()
		w.logger.Info("Task will be retried", logger.Fields{
			"task_id":     t.ID,
			"delay":       retryDelay.String(),
			"attempt":     t.RetryCount,
			"max_retries": t.MaxRetries,
		})

		// Sleep for the retry delay (exponential backoff)
		time.Sleep(retryDelay)

		// Re-enqueue the updated task (with incremented retry count)
		// This ensures the retried task has the correct state
		if err := w.queue.Enqueue(ctx, t); err != nil {
			w.logger.Error("Failed to re-enqueue task for retry", logger.Fields{
				"task_id": t.ID,
				"error":   err,
			})
			return err
		}

		return nil
	}

	// Task exceeded max retries, move to DLQ
	w.logger.Warn("Task exceeded max retries, moving to DLQ", logger.Fields{
		"task_id":     t.ID,
		"max_retries": t.MaxRetries,
		"type":        t.Type,
	})

	reason := fmt.Sprintf("Exceeded max retries (%d). Last error: %s", t.MaxRetries, execErr.Error())
	if err := w.queue.EnqueueDLQ(ctx, t, reason); err != nil {
		w.logger.Error("Failed to move task to DLQ", logger.Fields{
			"task_id": t.ID,
			"error":   err,
		})
	}

	// Remove from main queue
	return w.queue.Nack(ctx, t.ID, false)
}

// executeTask executes a single task with timeout
func (w *Worker) executeTask(parentCtx context.Context, t *task.Task) error {
	// Create a context with timeout
	timeout := t.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second // Default timeout
	}

	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	// Track execution start time
	startTime := time.Now()

	// Execute via registry
	result, err := w.registry.Execute(ctx, t)

	// Calculate duration
	duration := time.Since(startTime)

	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			errMsg := fmt.Sprintf("task timed out after %v", timeout)

			// Save timeout result using parent context (task context is cancelled)
			if w.storage != nil {
				timeoutResult := &task.Result{
					TaskID:      t.ID,
					Success:     false,
					Error:       errMsg,
					CompletedAt: time.Now(),
					Duration:    duration,
				}
				if err := w.storage.SaveResult(parentCtx, timeoutResult); err != nil {
					w.logger.Error("Failed to save timeout result", logger.Fields{
						"task_id": t.ID,
						"error":   err,
					})
				}
			}

			return errors.New(errMsg)
		}

		// Save error result
		if w.storage != nil && result != nil {
			result.Duration = duration
			if err := w.storage.SaveResult(parentCtx, result); err != nil {
				w.logger.Error("Failed to save error result", logger.Fields{
					"task_id": t.ID,
					"error":   err,
				})
			}
		}

		return err
	}

	// Mark task as completed
	t.MarkCompleted()

	// Save successful result
	if w.storage != nil && result != nil {
		result.Duration = duration
		result.CompletedAt = time.Now()
		if err := w.storage.SaveResult(parentCtx, result); err != nil {
			w.logger.Error("Failed to save success result", logger.Fields{
				"task_id": t.ID,
				"error":   err,
			})
		}
	}

	if result != nil && !result.Success {
		return fmt.Errorf("task execution failed: %s", result.Error)
	}

	return nil
}

// Stop gracefully stops the worker
func (w *Worker) Stop() error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return fmt.Errorf("worker %s is not running", w.id)
	}
	w.running = false
	w.mu.Unlock()

	w.logger.Info("Worker stopping")
	close(w.shutdownChan)
	w.wg.Wait()
	w.logger.Info("Worker stopped")
	return nil
}

// Shutdown gracefully stops the worker with a context for timeout
func (w *Worker) Shutdown(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return fmt.Errorf("worker %s is not running", w.id)
	}
	w.running = false
	w.mu.Unlock()

	w.logger.Info("Worker shutting down gracefully")
	close(w.shutdownChan)

	// Wait for worker to finish with timeout
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		w.logger.Info("Worker shutdown complete")
		return nil
	case <-ctx.Done():
		w.logger.Warn("Worker shutdown timeout")
		return ctx.Err()
	}
}

// IsRunning returns whether the worker is currently running
func (w *Worker) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

// Stats returns worker statistics
func (w *Worker) Stats() (processed, failed, retried int64) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.tasksProcessed, w.tasksFailed, w.tasksRetried
}

// ID returns the worker's unique identifier
func (w *Worker) ID() string {
	return w.id
}

// WaitForShutdown blocks until a SIGTERM or SIGINT signal is received
func (w *Worker) WaitForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	sig := <-sigChan
	w.logger.Info("Shutdown signal received", logger.Fields{
		"signal": sig.String(),
	})

	if err := w.Stop(); err != nil {
		w.logger.Error("Error during shutdown", logger.Fields{
			"error": err,
		})
	}
}
