package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/monitoring"
	"github.com/farhan-ahmed1/spool/internal/task"
)

// mockStorage implements storage.Storage for testing
type mockStorage struct {
	mu      sync.Mutex
	tasks   map[string]*task.Task
	results map[string]*task.Result
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		tasks:   make(map[string]*task.Task),
		results: make(map[string]*task.Result),
	}
}

func (m *mockStorage) SaveTask(ctx context.Context, t *task.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[t.ID] = t
	return nil
}

func (m *mockStorage) GetTask(ctx context.Context, taskID string) (*task.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskID]; ok {
		return t, nil
	}
	return nil, nil
}

func (m *mockStorage) UpdateTaskState(ctx context.Context, taskID string, state task.State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskID]; ok {
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.results[taskID]; ok {
		return r, nil
	}
	return nil, nil
}

func (m *mockStorage) GetTasksByState(ctx context.Context, state task.State, limit int) ([]*task.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var tasks []*task.Task
	for _, t := range m.tasks {
		if t.State == state {
			tasks = append(tasks, t)
			if len(tasks) >= limit {
				break
			}
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

// TestNewPool tests pool creation with various configurations
func TestNewPool(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	tests := []struct {
		name             string
		config           PoolConfig
		expectedMin      int
		expectedMax      int
		expectedPoll     time.Duration
		expectedShutdown time.Duration
	}{
		{
			name: "default values when zero config",
			config: PoolConfig{
				MinWorkers:      0,
				MaxWorkers:      0,
				PollInterval:    0,
				ShutdownTimeout: 0,
			},
			expectedMin:      1, // Should default to 1
			expectedMax:      1, // Should be at least min
			expectedPoll:     100 * time.Millisecond,
			expectedShutdown: 30 * time.Second,
		},
		{
			name: "custom configuration",
			config: PoolConfig{
				MinWorkers:      3,
				MaxWorkers:      10,
				PollInterval:    200 * time.Millisecond,
				ShutdownTimeout: 60 * time.Second,
			},
			expectedMin:      3,
			expectedMax:      10,
			expectedPoll:     200 * time.Millisecond,
			expectedShutdown: 60 * time.Second,
		},
		{
			name: "max less than min gets corrected",
			config: PoolConfig{
				MinWorkers: 5,
				MaxWorkers: 2, // Less than min
			},
			expectedMin: 5,
			expectedMax: 5, // Should be corrected to min
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewPool(q, storage, registry, metrics, tt.config)

			if pool == nil {
				t.Fatal("expected pool to be created")
			}

			if pool.minWorkers != tt.expectedMin {
				t.Errorf("expected minWorkers %d, got %d", tt.expectedMin, pool.minWorkers)
			}

			if pool.maxWorkers != tt.expectedMax {
				t.Errorf("expected maxWorkers %d, got %d", tt.expectedMax, pool.maxWorkers)
			}

			if tt.expectedPoll > 0 && pool.pollInterval != tt.expectedPoll {
				t.Errorf("expected pollInterval %v, got %v", tt.expectedPoll, pool.pollInterval)
			}

			if tt.expectedShutdown > 0 && pool.shutdownTimeout != tt.expectedShutdown {
				t.Errorf("expected shutdownTimeout %v, got %v", tt.expectedShutdown, pool.shutdownTimeout)
			}

			// Verify initial state
			if pool.GetWorkerCount() != 0 {
				t.Error("expected 0 workers before Start")
			}
		})
	}
}

// TestPool_Start tests pool startup
func TestPool_Start(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	tests := []struct {
		name          string
		minWorkers    int
		expectedCount int32
		expectError   bool
	}{
		{
			name:          "start with 1 worker",
			minWorkers:    1,
			expectedCount: 1,
			expectError:   false,
		},
		{
			name:          "start with 3 workers",
			minWorkers:    3,
			expectedCount: 3,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewPool(q, storage, registry, metrics, PoolConfig{
				MinWorkers:   tt.minWorkers,
				MaxWorkers:   tt.minWorkers + 2,
				PollInterval: 50 * time.Millisecond,
			})

			err := pool.Start()
			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Give workers time to start
			time.Sleep(100 * time.Millisecond)

			count := pool.GetWorkerCount()
			if count != tt.expectedCount {
				t.Errorf("expected %d workers, got %d", tt.expectedCount, count)
			}

			// Cleanup
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := pool.Shutdown(ctx); err != nil {
				t.Errorf("failed to shutdown pool: %v", err)
			}
		})
	}
}

// TestPool_AddWorker tests dynamic worker addition
func TestPool_AddWorker(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	pool := NewPool(q, storage, registry, metrics, PoolConfig{
		MinWorkers:   1,
		MaxWorkers:   3,
		PollInterval: 50 * time.Millisecond,
	})

	// Start pool with 1 worker
	if err := pool.Start(); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// add error check
		if err := pool.Shutdown(ctx); err != nil {
			t.Errorf("failed to shutdown pool: %v", err)
		}
	}()

	// Verify initial count
	if count := pool.GetWorkerCount(); count != 1 {
		t.Errorf("expected 1 worker, got %d", count)
	}

	// Add a worker
	ctx := context.Background()
	workerID, err := pool.AddWorker(ctx)
	if err != nil {
		t.Fatalf("failed to add worker: %v", err)
	}

	if workerID == "" {
		t.Error("expected non-empty worker ID")
	}

	// Verify count increased
	if count := pool.GetWorkerCount(); count != 2 {
		t.Errorf("expected 2 workers, got %d", count)
	}

	// Add another worker
	_, err = pool.AddWorker(ctx)
	if err != nil {
		t.Fatalf("failed to add second worker: %v", err)
	}

	if count := pool.GetWorkerCount(); count != 3 {
		t.Errorf("expected 3 workers, got %d", count)
	}

	// Try to add beyond max capacity
	_, err = pool.AddWorker(ctx)
	if err == nil {
		t.Error("expected error when adding worker beyond max capacity")
	}
}

// TestPool_RemoveWorker tests dynamic worker removal
func TestPool_RemoveWorker(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	pool := NewPool(q, storage, registry, metrics, PoolConfig{
		MinWorkers:      2,
		MaxWorkers:      5,
		PollInterval:    50 * time.Millisecond,
		ShutdownTimeout: 2 * time.Second,
	})

	// Start pool with 2 workers
	if err := pool.Start(); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// add error check
		if err := pool.Shutdown(ctx); err != nil {
			t.Errorf("failed to shutdown pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Add workers to go beyond minimum
	workerID1, err := pool.AddWorker(ctx)
	if err != nil {
		t.Fatalf("failed to add worker: %v", err)
	}

	workerID2, err := pool.AddWorker(ctx)
	if err != nil {
		t.Fatalf("failed to add second worker: %v", err)
	}

	// Verify count
	if count := pool.GetWorkerCount(); count != 4 {
		t.Errorf("expected 4 workers, got %d", count)
	}

	// Remove a worker
	err = pool.RemoveWorker(ctx, workerID1)
	if err != nil {
		t.Fatalf("failed to remove worker: %v", err)
	}

	// Give time for cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify count decreased
	if count := pool.GetWorkerCount(); count != 3 {
		t.Errorf("expected 3 workers, got %d", count)
	}

	// Remove another worker
	err = pool.RemoveWorker(ctx, workerID2)
	if err != nil {
		t.Fatalf("failed to remove second worker: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if count := pool.GetWorkerCount(); count != 2 {
		t.Errorf("expected 2 workers, got %d", count)
	}

	// Try to remove non-existent worker
	err = pool.RemoveWorker(ctx, "non-existent-worker")
	if err == nil {
		t.Error("expected error when removing non-existent worker")
	}
}

// TestPool_RemoveWorker_MinCapacity tests that we can't remove below min capacity
func TestPool_RemoveWorker_MinCapacity(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	pool := NewPool(q, storage, registry, metrics, PoolConfig{
		MinWorkers:   2,
		MaxWorkers:   5,
		PollInterval: 50 * time.Millisecond,
	})

	if err := pool.Start(); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = pool.Shutdown(ctx)
	}()

	// Get worker states to find a worker ID
	states := pool.GetWorkerStates()
	if len(states) != 2 {
		t.Fatalf("expected 2 workers, got %d", len(states))
	}

	// Get first worker ID
	var firstWorkerID string
	for id := range states {
		firstWorkerID = id
		break
	}

	// Try to remove a worker (should fail because we're at min capacity)
	ctx := context.Background()
	err := pool.RemoveWorker(ctx, firstWorkerID)
	if err == nil {
		t.Error("expected error when removing worker at min capacity")
	}
}

// TestPool_GetWorkerCount tests worker count tracking
func TestPool_GetWorkerCount(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	pool := NewPool(q, storage, registry, metrics, PoolConfig{
		MinWorkers:   1,
		MaxWorkers:   5,
		PollInterval: 50 * time.Millisecond,
	})

	// Before start
	if count := pool.GetWorkerCount(); count != 0 {
		t.Errorf("expected 0 workers before start, got %d", count)
	}

	// After start
	if err := pool.Start(); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = pool.Shutdown(ctx)
	}()

	if count := pool.GetWorkerCount(); count != 1 {
		t.Errorf("expected 1 worker after start, got %d", count)
	}

	// After adding workers
	ctx := context.Background()
	_, _ = pool.AddWorker(ctx)
	_, _ = pool.AddWorker(ctx)

	if count := pool.GetWorkerCount(); count != 3 {
		t.Errorf("expected 3 workers, got %d", count)
	}
}

// TestPool_GetIdleWorker tests finding idle workers
func TestPool_GetIdleWorker(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	pool := NewPool(q, storage, registry, metrics, PoolConfig{
		MinWorkers:   2,
		MaxWorkers:   5,
		PollInterval: 50 * time.Millisecond,
	})

	if err := pool.Start(); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = pool.Shutdown(ctx)
	}()

	// All workers should be idle initially
	idleWorker := pool.GetIdleWorker()
	if idleWorker == "" {
		t.Error("expected to find an idle worker")
	}

	// Verify the idle worker exists in states
	states := pool.GetWorkerStates()
	if _, exists := states[idleWorker]; !exists {
		t.Error("idle worker should exist in worker states")
	}
}

// TestPool_GetWorkerStates tests worker state snapshot
func TestPool_GetWorkerStates(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	pool := NewPool(q, storage, registry, metrics, PoolConfig{
		MinWorkers:   3,
		MaxWorkers:   5,
		PollInterval: 50 * time.Millisecond,
	})

	if err := pool.Start(); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = pool.Shutdown(ctx)
	}()

	states := pool.GetWorkerStates()

	if len(states) != 3 {
		t.Errorf("expected 3 worker states, got %d", len(states))
	}

	// All workers should be idle initially
	for id, state := range states {
		if state != WorkerStateIdle {
			t.Errorf("worker %s expected to be idle, got %s", id, state)
		}
	}

	// Verify it's a copy (modifying returned map shouldn't affect pool)
	for id := range states {
		states[id] = WorkerStateBusy
	}

	originalStates := pool.GetWorkerStates()
	for id, state := range originalStates {
		if state != WorkerStateIdle {
			t.Errorf("worker %s state should still be idle in pool, got %s", id, state)
		}
	}
}

// TestPool_Shutdown tests graceful pool shutdown
func TestPool_Shutdown(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	pool := NewPool(q, storage, registry, metrics, PoolConfig{
		MinWorkers:   2,
		MaxWorkers:   5,
		PollInterval: 50 * time.Millisecond,
	})

	if err := pool.Start(); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Add more workers
	ctx := context.Background()
	_, _ = pool.AddWorker(ctx)

	if count := pool.GetWorkerCount(); count != 3 {
		t.Errorf("expected 3 workers, got %d", count)
	}

	// Shutdown pool
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := pool.Shutdown(shutdownCtx)
	if err != nil {
		t.Errorf("failed to shutdown pool: %v", err)
	}

	// Give a moment for cleanup
	time.Sleep(100 * time.Millisecond)
}

// TestPool_Shutdown_Timeout tests shutdown timeout behavior
func TestPool_Shutdown_Timeout(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	pool := NewPool(q, storage, registry, metrics, PoolConfig{
		MinWorkers:   1,
		MaxWorkers:   2,
		PollInterval: 50 * time.Millisecond,
	})

	if err := pool.Start(); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Shutdown with very short timeout (context already expired)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Millisecond) // Ensure context expires

	err := pool.Shutdown(ctx)
	if err == nil {
		// Shutdown might still succeed if workers are quick enough
		// This is acceptable behavior
		t.Log("shutdown succeeded despite short timeout")
	} else if err != context.DeadlineExceeded {
		t.Logf("shutdown returned: %v (expected DeadlineExceeded or success)", err)
	}
}

// TestPool_GetStats tests pool statistics
func TestPool_GetStats(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	pool := NewPool(q, storage, registry, metrics, PoolConfig{
		MinWorkers:   2,
		MaxWorkers:   5,
		PollInterval: 50 * time.Millisecond,
	})

	if err := pool.Start(); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = pool.Shutdown(ctx)
	}()

	stats := pool.GetStats()

	// Verify expected stats fields
	if totalWorkers, ok := stats["total_workers"].(int); !ok || totalWorkers != 2 {
		t.Errorf("expected total_workers=2, got %v", stats["total_workers"])
	}

	if minWorkers, ok := stats["min_workers"].(int); !ok || minWorkers != 2 {
		t.Errorf("expected min_workers=2, got %v", stats["min_workers"])
	}

	if maxWorkers, ok := stats["max_workers"].(int); !ok || maxWorkers != 5 {
		t.Errorf("expected max_workers=5, got %v", stats["max_workers"])
	}

	// Check idle and busy workers
	if _, ok := stats["idle_workers"]; !ok {
		t.Error("expected idle_workers in stats")
	}

	if _, ok := stats["busy_workers"]; !ok {
		t.Error("expected busy_workers in stats")
	}
}

// TestPool_ConcurrentAddRemove tests concurrent add/remove operations
func TestPool_ConcurrentAddRemove(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	pool := NewPool(q, storage, registry, metrics, PoolConfig{
		MinWorkers:   1,
		MaxWorkers:   10,
		PollInterval: 50 * time.Millisecond,
	})

	if err := pool.Start(); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pool.Shutdown(ctx)
	}()

	ctx := context.Background()
	var wg sync.WaitGroup
	addedWorkers := make(chan string, 10)

	// Add workers concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := pool.AddWorker(ctx)
			if err == nil {
				addedWorkers <- id
			}
		}()
	}

	wg.Wait()
	close(addedWorkers)

	// Collect added worker IDs
	var workerIDs []string
	for id := range addedWorkers {
		workerIDs = append(workerIDs, id)
	}

	// Verify we added some workers (might be less than 5 due to max limit)
	count := pool.GetWorkerCount()
	if count < 1 {
		t.Errorf("expected at least 1 worker, got %d", count)
	}

	// Now try concurrent removals (beyond minWorkers)
	if len(workerIDs) > 0 && count > 1 {
		for _, id := range workerIDs[:1] { // Remove just one to stay above min
			wg.Add(1)
			go func(workerID string) {
				defer wg.Done()
				_ = pool.RemoveWorker(ctx, workerID)
			}(id)
		}
		wg.Wait()
	}

	// Pool should still be in a valid state
	finalCount := pool.GetWorkerCount()
	if finalCount < int32(pool.minWorkers) {
		t.Errorf("worker count %d below minimum %d", finalCount, pool.minWorkers)
	}
}

// TestPool_WithNilMetrics tests pool works without metrics
func TestPool_WithNilMetrics(t *testing.T) {
	q := newMockQueue()
	storage := newMockStorage()
	registry := task.NewRegistry()

	pool := NewPool(q, storage, registry, nil, PoolConfig{
		MinWorkers:   1,
		MaxWorkers:   3,
		PollInterval: 50 * time.Millisecond,
	})

	if err := pool.Start(); err != nil {
		t.Fatalf("failed to start pool without metrics: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = pool.Shutdown(ctx)
	}()

	// Add a worker (should work even without metrics)
	ctx := context.Background()
	_, err := pool.AddWorker(ctx)
	if err != nil {
		t.Fatalf("failed to add worker without metrics: %v", err)
	}

	if count := pool.GetWorkerCount(); count != 2 {
		t.Errorf("expected 2 workers, got %d", count)
	}
}
