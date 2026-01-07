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
