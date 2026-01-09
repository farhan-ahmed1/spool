package broker

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/logger"
	"github.com/farhan-ahmed1/spool/internal/task"
)

// mockQueue implements queue.Queue interface for testing
type mockQueue struct {
	tasks      map[string]*task.Task
	dlqTasks   map[string]*task.Task
	mu         sync.RWMutex
	enqueueErr error
	dequeueErr error
	sizeErr    error
	purgeErr   error
	healthErr  error
}

func newMockQueue() *mockQueue {
	return &mockQueue{
		tasks:    make(map[string]*task.Task),
		dlqTasks: make(map[string]*task.Task),
	}
}

func (m *mockQueue) Enqueue(ctx context.Context, t *task.Task) error {
	if m.enqueueErr != nil {
		return m.enqueueErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[t.ID] = t
	return nil
}

func (m *mockQueue) Dequeue(ctx context.Context) (*task.Task, error) {
	if m.dequeueErr != nil {
		return nil, m.dequeueErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, t := range m.tasks {
		delete(m.tasks, id)
		return t, nil
	}
	return nil, errors.New("no task available")
}

func (m *mockQueue) Peek(ctx context.Context) (*task.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, t := range m.tasks {
		return t, nil
	}
	return nil, errors.New("no task available")
}

func (m *mockQueue) Ack(ctx context.Context, taskID string) error {
	return nil
}

func (m *mockQueue) Nack(ctx context.Context, taskID string, requeue bool) error {
	return nil
}

func (m *mockQueue) Size(ctx context.Context) (int64, error) {
	if m.sizeErr != nil {
		return 0, m.sizeErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.tasks)), nil
}

func (m *mockQueue) SizeByPriority(ctx context.Context) (map[task.Priority]int64, error) {
	return map[task.Priority]int64{}, nil
}

func (m *mockQueue) Purge(ctx context.Context) error {
	if m.purgeErr != nil {
		return m.purgeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = make(map[string]*task.Task)
	return nil
}

func (m *mockQueue) EnqueueDLQ(ctx context.Context, t *task.Task, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dlqTasks[t.ID] = t
	return nil
}

func (m *mockQueue) GetDLQSize(ctx context.Context) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.dlqTasks)), nil
}

func (m *mockQueue) GetDLQTasks(ctx context.Context, limit int) ([]*task.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var tasks []*task.Task
	count := 0
	for _, t := range m.dlqTasks {
		if count >= limit {
			break
		}
		tasks = append(tasks, t)
		count++
	}
	return tasks, nil
}

func (m *mockQueue) Health(ctx context.Context) error {
	return m.healthErr
}

func (m *mockQueue) Close() error {
	return nil
}

// Helper method to set errors for testing
func (m *mockQueue) setEnqueueError(err error) {
	m.enqueueErr = err
}

func (m *mockQueue) setSizeError(err error) {
	m.sizeErr = err
}

func (m *mockQueue) setPurgeError(err error) {
	m.purgeErr = err
}

func (m *mockQueue) setHealthError(err error) {
	m.healthErr = err
}

// mockStorage implements storage.Storage interface for testing
type mockStorage struct {
	tasks              map[string]*task.Task
	results            map[string]*task.Result
	mu                 sync.RWMutex
	getTaskErr         error
	getTasksByStateErr error
	saveTaskErr        error
	updateTaskStateErr error
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		tasks:   make(map[string]*task.Task),
		results: make(map[string]*task.Result),
	}
}

func (m *mockStorage) SaveTask(ctx context.Context, t *task.Task) error {
	if m.saveTaskErr != nil {
		return m.saveTaskErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[t.ID] = t
	return nil
}

func (m *mockStorage) GetTask(ctx context.Context, taskID string) (*task.Task, error) {
	if m.getTaskErr != nil {
		return nil, m.getTaskErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, exists := m.tasks[taskID]
	if !exists {
		return nil, errors.New("task not found")
	}
	return t, nil
}

func (m *mockStorage) UpdateTaskState(ctx context.Context, taskID string, state task.State) error {
	if m.updateTaskStateErr != nil {
		return m.updateTaskStateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, exists := m.tasks[taskID]; exists {
		t.State = state
	}
	return nil
}

func (m *mockStorage) SaveResult(ctx context.Context, result *task.Result) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[result.TaskID] = result
	return nil
}

func (m *mockStorage) GetResult(ctx context.Context, taskID string) (*task.Result, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, exists := m.results[taskID]
	if !exists {
		return nil, errors.New("result not found")
	}
	return r, nil
}

func (m *mockStorage) GetTasksByState(ctx context.Context, state task.State, limit int) ([]*task.Task, error) {
	if m.getTasksByStateErr != nil {
		return nil, m.getTasksByStateErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var tasks []*task.Task
	count := 0
	for _, t := range m.tasks {
		if count >= limit {
			break
		}
		if t.State == state {
			tasks = append(tasks, t)
			count++
		}
	}
	return tasks, nil
}

func (m *mockStorage) DeleteTask(ctx context.Context, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, taskID)
	return nil
}

func (m *mockStorage) Close() error {
	return nil
}

// Helper methods to set errors for testing
func (m *mockStorage) setGetTaskError(err error) {
	m.getTaskErr = err
}

func (m *mockStorage) setGetTasksByStateError(err error) {
	m.getTasksByStateErr = err
}

func setupTestBroker(_ *testing.T) (*Broker, *mockQueue, *mockStorage) {
	// Initialize logger for testing
	logger.Init("error", "text", "test")

	mockQueue := newMockQueue()
	mockStorage := newMockStorage()

	broker := NewBroker(Config{
		Addr:    ":0", // Use random port for testing
		Queue:   mockQueue,
		Storage: mockStorage,
	})

	return broker, mockQueue, mockStorage
}

func TestNewBroker(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   string // expected address
	}{
		{
			name: "default address when empty",
			config: Config{
				Addr:    "",
				Queue:   newMockQueue(),
				Storage: newMockStorage(),
			},
			want: ":8000",
		},
		{
			name: "custom address",
			config: Config{
				Addr:    ":9000",
				Queue:   newMockQueue(),
				Storage: newMockStorage(),
			},
			want: ":9000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broker := NewBroker(tt.config)

			if broker == nil {
				t.Fatal("NewBroker() returned nil")
			}

			if broker.addr != tt.want {
				t.Errorf("NewBroker() addr = %v, want %v", broker.addr, tt.want)
			}

			if broker.queue != tt.config.Queue {
				t.Error("NewBroker() queue not set correctly")
			}

			if broker.storage != tt.config.Storage {
				t.Error("NewBroker() storage not set correctly")
			}

			if broker.logger == nil {
				t.Error("NewBroker() logger not initialized")
			}

			if broker.done == nil {
				t.Error("NewBroker() done channel not initialized")
			}

			if broker.ready == nil {
				t.Error("NewBroker() ready channel not initialized")
			}
		})
	}
}

func TestBroker_StartAndStop(t *testing.T) {
	broker, _, _ := setupTestBroker(t)

	// Test starting broker in goroutine
	go func() {
		err := broker.Start()
		// Server will return error when shutdown, which is expected
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("Unexpected error starting broker: %v", err)
		}
	}()

	// Wait for broker to be ready
	select {
	case <-broker.Ready():
		// Broker is ready
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Broker failed to start within timeout")
	}

	// Test stopping broker
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := broker.Stop(ctx)
	if err != nil {
		t.Errorf("Error stopping broker: %v", err)
	}
}

func TestBroker_StopWithoutStart(t *testing.T) {
	broker, _, _ := setupTestBroker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Should not error when stopping a broker that was never started
	err := broker.Stop(ctx)
	if err != nil {
		t.Errorf("Error stopping non-started broker: %v", err)
	}
}

func TestBroker_Ready(t *testing.T) {
	broker, _, _ := setupTestBroker(t)

	// Ready channel should not be closed before Start
	select {
	case <-broker.Ready():
		t.Fatal("Ready channel should not be closed before Start")
	case <-time.After(10 * time.Millisecond):
		// Expected
	}

	// Start broker
	go func() {
		if err := broker.Start(); err != nil {
			t.Errorf("broker.Start() error: %v", err)
		}
	}()

	// Ready channel should be closed after Start
	select {
	case <-broker.Ready():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Ready channel should be closed after Start")
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := broker.Stop(ctx); err != nil {
		t.Errorf("broker.Stop() error: %v", err)
	}
}
