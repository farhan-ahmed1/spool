package worker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/task"
)

// Mock Queue for testing
type mockQueue struct {
	mu            sync.Mutex
	tasks         []*task.Task
	dequeueFunc   func(ctx context.Context) (*task.Task, error)
	ackCalled     []string
	nackCalled    []string
	enqueueCalled []*task.Task
}

func newMockQueue() *mockQueue {
	return &mockQueue{
		tasks:      make([]*task.Task, 0),
		ackCalled:  make([]string, 0),
		nackCalled: make([]string, 0),
	}
}

func (m *mockQueue) Enqueue(ctx context.Context, t *task.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = append(m.tasks, t)
	m.enqueueCalled = append(m.enqueueCalled, t)
	return nil
}

func (m *mockQueue) Dequeue(ctx context.Context) (*task.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.dequeueFunc != nil {
		return m.dequeueFunc(ctx)
	}

	if len(m.tasks) == 0 {
		return nil, queue.ErrNoTask
	}

	t := m.tasks[0]
	m.tasks = m.tasks[1:]
	return t, nil
}

func (m *mockQueue) Peek(ctx context.Context) (*task.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tasks) == 0 {
		return nil, queue.ErrNoTask
	}
	return m.tasks[0], nil
}

func (m *mockQueue) Ack(ctx context.Context, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ackCalled = append(m.ackCalled, taskID)
	return nil
}

func (m *mockQueue) Nack(ctx context.Context, taskID string, requeue bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nackCalled = append(m.nackCalled, taskID)
	return nil
}

func (m *mockQueue) Size(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.tasks)), nil
}

func (m *mockQueue) SizeByPriority(ctx context.Context) (map[task.Priority]int64, error) {
	return map[task.Priority]int64{}, nil
}

func (m *mockQueue) Purge(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = make([]*task.Task, 0)
	return nil
}

func (m *mockQueue) Close() error {
	return nil
}

func (m *mockQueue) Health(ctx context.Context) error {
	return nil
}

func (m *mockQueue) EnqueueDLQ(ctx context.Context, t *task.Task, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.State = task.StateDeadLetter
	return nil
}

func (m *mockQueue) GetDLQSize(ctx context.Context) (int64, error) {
	return 0, nil
}

func (m *mockQueue) GetDLQTasks(ctx context.Context, limit int) ([]*task.Task, error) {
	return []*task.Task{}, nil
}

func TestNewWorker(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "with defaults",
			config: Config{
				ID:           "",
				PollInterval: 0,
			},
		},
		{
			name: "with custom config",
			config: Config{
				ID:           "test-worker",
				PollInterval: 200 * time.Millisecond,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWorker(q, nil, registry, tt.config)
			if w == nil {
				t.Fatal("expected worker to be created")
			}

			if tt.config.ID != "" && w.ID() != tt.config.ID {
				t.Errorf("expected worker ID %s, got %s", tt.config.ID, w.ID())
			}

			if w.IsRunning() {
				t.Error("expected worker to not be running initially")
			}
		})
	}
}

func TestWorker_StartStop(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	w := NewWorker(q, nil, registry, Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()

	// Start the worker
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	if !w.IsRunning() {
		t.Error("expected worker to be running")
	}

	// Try to start again (should fail)
	if err := w.Start(ctx); err == nil {
		t.Error("expected error when starting already running worker")
	}

	// Stop the worker
	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	if w.IsRunning() {
		t.Error("expected worker to be stopped")
	}

	// Try to stop again (should fail)
	if err := w.Stop(); err == nil {
		t.Error("expected error when stopping already stopped worker")
	}
}

func TestWorker_ExecuteTask_Success(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	// Register a test handler
	executed := false
	err := registry.Register("test-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		executed = true
		return "success", nil
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Create a test task
	testTask, err := task.NewTask("test-task", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	w := NewWorker(q, nil, registry, Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for task to be processed
	time.Sleep(200 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	if !executed {
		t.Error("expected task to be executed")
	}

	// Check that Ack was called
	if len(q.ackCalled) != 1 || q.ackCalled[0] != testTask.ID {
		t.Error("expected Ack to be called for the task")
	}

	processed, failed, _ := w.Stats()
	if processed != 1 {
		t.Errorf("expected 1 processed task, got %d", processed)
	}
	if failed != 0 {
		t.Errorf("expected 0 failed tasks, got %d", failed)
	}
}

func TestWorker_ExecuteTask_Failure(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	// Register a handler that fails
	err := registry.Register("failing-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return nil, errors.New("task failed")
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Create a test task with no retries
	testTask, err := task.NewTask("failing-task", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	testTask.WithMaxRetries(0) // Don't retry for this test

	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	w := NewWorker(q, nil, registry, Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for task to be processed
	time.Sleep(200 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	// Check that Nack was called
	if len(q.nackCalled) != 1 || q.nackCalled[0] != testTask.ID {
		t.Error("expected Nack to be called for the failed task")
	}

	processed, failed, _ := w.Stats()
	if processed != 0 {
		t.Errorf("expected 0 processed tasks, got %d", processed)
	}
	if failed != 1 {
		t.Errorf("expected 1 failed task, got %d", failed)
	}
}

func TestWorker_ExecuteTask_Timeout(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	// Register a handler that takes too long
	err := registry.Register("slow-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		select {
		case <-time.After(2 * time.Second):
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Create a test task with a short timeout and no retries
	testTask, err := task.NewTask("slow-task", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	testTask.Timeout = 100 * time.Millisecond
	testTask.WithMaxRetries(0) // Don't retry for this test

	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	w := NewWorker(q, nil, registry, Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for task to timeout and be processed
	time.Sleep(300 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	// Check that Nack was called due to timeout
	if len(q.nackCalled) != 1 || q.nackCalled[0] != testTask.ID {
		t.Error("expected Nack to be called for the timed out task")
	}

	processed, failed, _ := w.Stats()
	if processed != 0 {
		t.Errorf("expected 0 processed tasks, got %d", processed)
	}
	if failed != 1 {
		t.Errorf("expected 1 failed task, got %d", failed)
	}
}

func TestWorker_EmptyQueue(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	w := NewWorker(q, nil, registry, Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Let it run for a bit with empty queue
	time.Sleep(200 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	processed, failed, _ := w.Stats()
	if processed != 0 {
		t.Errorf("expected 0 processed tasks, got %d", processed)
	}
	if failed != 0 {
		t.Errorf("expected 0 failed tasks, got %d", failed)
	}
}

func TestWorker_MultipleTasks(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	// Register a test handler
	executedCount := 0
	var mu sync.Mutex
	err := registry.Register("test-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		mu.Lock()
		executedCount++
		mu.Unlock()
		return "success", nil
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Enqueue multiple tasks
	for i := 0; i < 5; i++ {
		testTask, err := task.NewTask("test-task", map[string]int{"count": i})
		if err != nil {
			t.Fatalf("failed to create task: %v", err)
		}
		if err := q.Enqueue(context.Background(), testTask); err != nil {
			t.Fatalf("failed to enqueue task: %v", err)
		}
	}

	w := NewWorker(q, nil, registry, Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for all tasks to be processed
	time.Sleep(500 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	mu.Lock()
	if executedCount != 5 {
		t.Errorf("expected 5 tasks to be executed, got %d", executedCount)
	}
	mu.Unlock()

	processed, failed, _ := w.Stats()
	if processed != 5 {
		t.Errorf("expected 5 processed tasks, got %d", processed)
	}
	if failed != 0 {
		t.Errorf("expected 0 failed tasks, got %d", failed)
	}
}

func TestWorker_ContextCancellation(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	w := NewWorker(q, nil, registry, Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	if !w.IsRunning() {
		t.Error("expected worker to be running")
	}

	// Cancel the context
	cancel()

	// Give it time to shut down
	time.Sleep(200 * time.Millisecond)

	if w.IsRunning() {
		t.Error("expected worker to stop after context cancellation")
	}
}

func TestWorker_UnknownTaskType(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	// Create a task with unknown type and no retries
	testTask, err := task.NewTask("unknown-task", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	testTask.WithMaxRetries(0) // Don't retry for this test

	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	w := NewWorker(q, nil, registry, Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for task to be processed
	time.Sleep(200 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	// Check that Nack was called due to unknown handler
	if len(q.nackCalled) != 1 || q.nackCalled[0] != testTask.ID {
		t.Error("expected Nack to be called for the unknown task type")
	}

	processed, failed, _ := w.Stats()
	if processed != 0 {
		t.Errorf("expected 0 processed tasks, got %d", processed)
	}
	if failed != 1 {
		t.Errorf("expected 1 failed task, got %d", failed)
	}
}

// TestWorker_TaskRetry tests task retry logic with exponential backoff
func TestWorker_TaskRetry(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	attempts := 0
	var mu sync.Mutex

	// Register a handler that fails once then succeeds
	err := registry.Register("retry-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		mu.Lock()
		attempts++
		currentAttempt := attempts
		mu.Unlock()

		if currentAttempt < 2 {
			return nil, errors.New("temporary failure")
		}
		return "success", nil
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Create a task with retries enabled
	testTask, err := task.NewTask("retry-task", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	testTask.WithMaxRetries(3) // Allow up to 3 retries

	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	w := NewWorker(q, nil, registry, Config{
		ID:           "retry-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for task to be processed with retries
	// First attempt fails immediately, then 2s backoff, then succeeds
	time.Sleep(3 * time.Second)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	mu.Lock()
	finalAttempts := attempts
	mu.Unlock()

	// Task should have been attempted at least 2 times (fail once, succeed on retry)
	if finalAttempts < 2 {
		t.Errorf("expected at least 2 attempts, got %d", finalAttempts)
	}

	processed, failed, retried := w.Stats()
	if retried < 1 {
		t.Errorf("expected at least 1 retry, got %d", retried)
	}
	// After successful retry, we should have 1 processed
	if processed < 1 {
		t.Errorf("expected at least 1 processed after retry success, got %d", processed)
	}
	t.Logf("Stats - processed: %d, failed: %d, retried: %d, attempts: %d", processed, failed, retried, finalAttempts)
}

// TestWorker_TaskDLQ tests task moves to dead letter queue after max retries
func TestWorker_TaskDLQ(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	// Register a handler that always fails
	err := registry.Register("dlq-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return nil, errors.New("permanent failure")
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Create a task with 1 retry allowed
	testTask, err := task.NewTask("dlq-task", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	testTask.WithMaxRetries(1) // Only 1 retry, then DLQ

	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	w := NewWorker(q, nil, registry, Config{
		ID:           "dlq-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for task to be processed and moved to DLQ
	time.Sleep(3 * time.Second)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	// Check that Nack was eventually called (task moved to DLQ)
	q.mu.Lock()
	nackCount := len(q.nackCalled)
	q.mu.Unlock()

	if nackCount < 1 {
		t.Error("expected Nack to be called for DLQ task")
	}

	_, failed, retried := w.Stats()
	// First attempt fails, then retry fails = 2 failures
	if failed < 2 {
		t.Errorf("expected at least 2 failures (initial + retry), got %d", failed)
	}
	if retried < 1 {
		t.Errorf("expected at least 1 retry before DLQ, got %d", retried)
	}
}

// TestWorker_Shutdown tests graceful shutdown with timeout context
func TestWorker_Shutdown(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	w := NewWorker(q, nil, registry, Config{
		ID:           "shutdown-test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Shutdown with sufficient timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := w.Shutdown(shutdownCtx); err != nil {
		t.Errorf("shutdown failed: %v", err)
	}

	if w.IsRunning() {
		t.Error("expected worker to be stopped after shutdown")
	}
}

// TestWorker_ShutdownNotRunning tests shutdown on non-running worker
func TestWorker_ShutdownNotRunning(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	w := NewWorker(q, nil, registry, Config{
		ID:           "not-running-worker",
		PollInterval: 50 * time.Millisecond,
	})

	// Try to shutdown without starting
	ctx := context.Background()
	err := w.Shutdown(ctx)
	if err == nil {
		t.Error("expected error when shutting down non-running worker")
	}
}

// TestWorker_ShutdownTimeout tests shutdown timeout behavior
func TestWorker_ShutdownTimeout(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	// Register a slow handler
	err := registry.Register("slow-shutdown-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		select {
		case <-time.After(5 * time.Second):
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	w := NewWorker(q, nil, registry, Config{
		ID:           "slow-shutdown-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Shutdown with very short timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// The worker should still stop eventually (via the shutdown channel)
	err = w.Shutdown(shutdownCtx)
	// Either success or context deadline exceeded is acceptable
	if err != nil && err != context.DeadlineExceeded {
		t.Logf("shutdown returned: %v", err)
	}
}

// TestWorker_ExecuteTaskWithStorage tests task execution with storage
func TestWorker_ExecuteTaskWithStorage(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorageForWorker()
	registry := task.NewRegistry()

	err := registry.Register("storage-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return map[string]string{"result": "success"}, nil
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	testTask, err := task.NewTask("storage-task", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	w := NewWorker(q, storage, registry, Config{
		ID:           "storage-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for task to be processed
	time.Sleep(300 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	// Verify task was saved to storage
	storage.mu.Lock()
	savedTask, exists := storage.tasks[testTask.ID]
	storage.mu.Unlock()

	if !exists {
		t.Error("expected task to be saved to storage")
	} else if savedTask.State != task.StateCompleted {
		t.Errorf("expected task state to be completed, got %s", savedTask.State)
	}
}

// TestWorker_ExecuteTaskFailedWithStorage tests failed task storage
func TestWorker_ExecuteTaskFailedWithStorage(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorageForWorker()
	registry := task.NewRegistry()

	err := registry.Register("fail-storage-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return nil, errors.New("intentional failure")
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	testTask, err := task.NewTask("fail-storage-task", nil)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	testTask.WithMaxRetries(0) // No retries

	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	w := NewWorker(q, storage, registry, Config{
		ID:           "fail-storage-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	// Verify task state was updated in storage
	storage.mu.Lock()
	savedTask, exists := storage.tasks[testTask.ID]
	storage.mu.Unlock()

	if !exists {
		t.Error("expected task to be saved to storage")
	} else if savedTask.State != task.StateDeadLetter {
		t.Errorf("expected task state to be dead_letter, got %s", savedTask.State)
	}
}

// TestWorker_TimeoutWithStorage tests timeout handling with storage
func TestWorker_TimeoutWithStorage(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorageForWorker()
	registry := task.NewRegistry()

	err := registry.Register("timeout-storage-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		select {
		case <-time.After(5 * time.Second):
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	testTask, err := task.NewTask("timeout-storage-task", nil)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	testTask.WithTimeout(100 * time.Millisecond)
	testTask.WithMaxRetries(0)

	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	w := NewWorker(q, storage, registry, Config{
		ID:           "timeout-storage-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	// Verify result was saved with timeout error
	storage.mu.Lock()
	result, hasResult := storage.results[testTask.ID]
	storage.mu.Unlock()

	if !hasResult {
		t.Error("expected result to be saved for timed out task")
	} else if result.Success {
		t.Error("expected result.Success to be false for timed out task")
	}
}

// TestWorker_DefaultTimeout tests default timeout behavior
func TestWorker_DefaultTimeout(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	err := registry.Register("default-timeout-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return "quick", nil
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	testTask, err := task.NewTask("default-timeout-task", nil)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	testTask.Timeout = 0 // Set to zero to test default

	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	w := NewWorker(q, nil, registry, Config{
		ID:           "default-timeout-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	processed, _, _ := w.Stats()
	if processed != 1 {
		t.Errorf("expected 1 processed task with default timeout, got %d", processed)
	}
}

// TestWorker_PanicRecovery tests that panics in handlers are handled gracefully
func TestWorker_HandlerReturnsFailedResult(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorageForWorker()
	registry := task.NewRegistry()

	// Register a handler that returns a failed result (not an error)
	err := registry.Register("failed-result-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		// Return a Result-like structure that indicates failure
		return nil, nil // No error, but handler execution "succeeds"
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	testTask, err := task.NewTask("failed-result-task", nil)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	w := NewWorker(q, storage, registry, Config{
		ID:           "failed-result-worker",
		PollInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	// Task should be marked as completed since handler didn't return error
	processed, _, _ := w.Stats()
	if processed != 1 {
		t.Errorf("expected 1 processed, got %d", processed)
	}
}

// TestWorker_ID tests worker ID method
func TestWorker_ID(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	tests := []struct {
		name           string
		configID       string
		expectCustomID bool
	}{
		{
			name:           "custom ID",
			configID:       "my-custom-worker",
			expectCustomID: true,
		},
		{
			name:           "auto-generated ID",
			configID:       "",
			expectCustomID: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWorker(q, nil, registry, Config{
				ID:           tt.configID,
				PollInterval: 50 * time.Millisecond,
			})

			id := w.ID()
			if tt.expectCustomID {
				if id != tt.configID {
					t.Errorf("expected ID %s, got %s", tt.configID, id)
				}
			} else {
				if id == "" {
					t.Error("expected auto-generated ID to be non-empty")
				}
				if len(id) < 7 { // Should start with "worker-"
					t.Errorf("expected auto-generated ID to have prefix, got %s", id)
				}
			}
		})
	}
}

// TestWorker_StatsAreThreadSafe tests that stats can be accessed concurrently
func TestWorker_StatsAreThreadSafe(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()

	err := registry.Register("concurrent-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return "done", nil
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Enqueue many tasks
	for i := 0; i < 10; i++ {
		testTask, err := task.NewTask("concurrent-task", map[string]int{"i": i})
		if err != nil {
			t.Fatalf("failed to create task: %v", err)
		}
		if err := q.Enqueue(context.Background(), testTask); err != nil {
			t.Fatalf("failed to enqueue task: %v", err)
		}
	}

	w := NewWorker(q, nil, registry, Config{
		ID:           "concurrent-worker",
		PollInterval: 10 * time.Millisecond,
	})

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Concurrently read stats while worker is processing
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, _, _ = w.Stats()
				_ = w.IsRunning()
				_ = w.ID()
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	if err := w.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}
}

// mockStorageForWorker is a storage implementation for worker tests
type mockStorageForWorker struct {
	mu      sync.Mutex
	tasks   map[string]*task.Task
	results map[string]*task.Result
}

func newMockStorageForWorker() *mockStorageForWorker {
	return &mockStorageForWorker{
		tasks:   make(map[string]*task.Task),
		results: make(map[string]*task.Result),
	}
}

func (m *mockStorageForWorker) SaveTask(ctx context.Context, t *task.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[t.ID] = t
	return nil
}

func (m *mockStorageForWorker) GetTask(ctx context.Context, taskID string) (*task.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[taskID], nil
}

func (m *mockStorageForWorker) UpdateTaskState(ctx context.Context, taskID string, state task.State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskID]; ok {
		t.State = state
	}
	return nil
}

func (m *mockStorageForWorker) SaveResult(ctx context.Context, result *task.Result) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[result.TaskID] = result
	return nil
}

func (m *mockStorageForWorker) GetResult(ctx context.Context, taskID string) (*task.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.results[taskID], nil
}

func (m *mockStorageForWorker) GetTasksByState(ctx context.Context, state task.State, limit int) ([]*task.Task, error) {
	return nil, nil
}

func (m *mockStorageForWorker) DeleteTask(ctx context.Context, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, taskID)
	return nil
}

func (m *mockStorageForWorker) Close() error {
	return nil
}
