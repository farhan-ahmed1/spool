package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

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

	return &Worker{
		id:           config.ID,
		queue:        q,
		storage:      storage,
		registry:     registry,
		pollFreq:     config.PollInterval,
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

	log.Printf("[Worker %s] Starting...", w.id)

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

	log.Printf("[Worker %s] Polling queue every %v", w.id, w.pollFreq)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Worker %s] Context cancelled, shutting down", w.id)
			return
		case <-w.shutdownChan:
			log.Printf("[Worker %s] Shutdown signal received", w.id)
			return
		case <-ticker.C:
			// Poll the queue for a task
			if err := w.pollAndExecute(ctx); err != nil {
				// Log error but continue running
				if err != queue.ErrNoTask {
					log.Printf("[Worker %s] Error: %v", w.id, err)
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

	log.Printf("[Worker %s] Picked up task %s (type: %s, attempt: %d/%d)",
		w.id, t.ID, t.Type, t.RetryCount+1, t.MaxRetries+1)

	// Save task state as processing
	t.MarkStarted()
	if w.storage != nil {
		if err := w.storage.SaveTask(ctx, t); err != nil {
			log.Printf("[Worker %s] Failed to save task state: %v", w.id, err)
		}
	}

	// Execute the task
	if err := w.executeTask(ctx, t); err != nil {
		w.mu.Lock()
		w.tasksFailed++
		w.mu.Unlock()
		log.Printf("[Worker %s] Task %s failed: %v", w.id, t.ID, err)

		// Handle failure with retry/DLQ logic
		return w.handleTaskFailure(ctx, t, err)
	}

	w.mu.Lock()
	w.tasksProcessed++
	w.mu.Unlock()

	log.Printf("[Worker %s] Task %s completed successfully", w.id, t.ID)

	// Mark task as completed (sets CompletedAt timestamp)
	t.MarkCompleted()

	// Save completed task state
	if w.storage != nil {
		if err := w.storage.SaveTask(ctx, t); err != nil {
			log.Printf("[Worker %s] Failed to save completed task state: %v", w.id, err)
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
			log.Printf("[Worker %s] Failed to save task state: %v", w.id, err)
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
		log.Printf("[Worker %s] Task %s will be retried in %v (attempt %d/%d)",
			w.id, t.ID, retryDelay, t.RetryCount, t.MaxRetries)

		// Sleep for the retry delay (exponential backoff)
		time.Sleep(retryDelay)

		// Re-enqueue the updated task (with incremented retry count)
		// This ensures the retried task has the correct state
		if err := w.queue.Enqueue(ctx, t); err != nil {
			log.Printf("[Worker %s] Failed to re-enqueue task %s: %v", w.id, t.ID, err)
			return err
		}

		return nil
	}

	// Task exceeded max retries, move to DLQ
	log.Printf("[Worker %s] Task %s exceeded max retries (%d), moving to DLQ",
		w.id, t.ID, t.MaxRetries)

	reason := fmt.Sprintf("Exceeded max retries (%d). Last error: %s", t.MaxRetries, execErr.Error())
	if err := w.queue.EnqueueDLQ(ctx, t, reason); err != nil {
		log.Printf("[Worker %s] Failed to move task %s to DLQ: %v", w.id, t.ID, err)
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
					log.Printf("[Worker %s] Failed to save timeout result for task %s: %v", w.id, t.ID, err)
				}
			}

			return errors.New(errMsg)
		}

		// Save error result
		if w.storage != nil && result != nil {
			result.Duration = duration
			if err := w.storage.SaveResult(parentCtx, result); err != nil {
				log.Printf("[Worker %s] Failed to save error result for task %s: %v", w.id, t.ID, err)
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
			log.Printf("[Worker %s] Failed to save result for task %s: %v", w.id, t.ID, err)
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

	log.Printf("[Worker %s] Stopping...", w.id)
	close(w.shutdownChan)
	w.wg.Wait()
	log.Printf("[Worker %s] Stopped", w.id)
	return nil
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
	log.Printf("[Worker %s] Received signal: %v", w.id, sig)

	if err := w.Stop(); err != nil {
		log.Printf("[Worker %s] Error during shutdown: %v", w.id, err)
	}
}
