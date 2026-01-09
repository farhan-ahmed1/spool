package worker

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/monitoring"
	"github.com/farhan-ahmed1/spool/internal/task"
)

// TestNewInstrumentedWorker tests instrumented worker creation
func TestNewInstrumentedWorker(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	worker := NewWorker(q, nil, registry, Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	iw := NewInstrumentedWorker(worker, metrics)

	if iw == nil {
		t.Fatal("expected instrumented worker to be created")
	}

	if iw.worker != worker {
		t.Error("expected worker to be wrapped correctly")
	}

	if iw.metrics != metrics {
		t.Error("expected metrics to be set correctly")
	}

	if iw.GetID() != "test-worker" {
		t.Errorf("expected ID 'test-worker', got %s", iw.GetID())
	}

	if iw.Worker() != worker {
		t.Error("Worker() should return the wrapped worker")
	}

	if iw.GetWorker() != worker {
		t.Error("GetWorker() should return the wrapped worker")
	}
}

// TestInstrumentedWorker_StartStop tests instrumented worker lifecycle
func TestInstrumentedWorker_StartStop(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	worker := NewWorker(q, nil, registry, Config{
		ID:           "test-instrumented-worker",
		PollInterval: 50 * time.Millisecond,
	})

	iw := NewInstrumentedWorker(worker, metrics)
	ctx := context.Background()

	// Start the worker
	if err := iw.Start(ctx); err != nil {
		t.Fatalf("failed to start instrumented worker: %v", err)
	}

	// Verify worker is running
	if !iw.IsRunning() {
		t.Error("expected instrumented worker to be running")
	}

	// Give time for stats sync loop to run
	time.Sleep(200 * time.Millisecond)

	// Stop the worker
	if err := iw.Stop(); err != nil {
		t.Fatalf("failed to stop instrumented worker: %v", err)
	}

	// Verify worker is stopped
	if iw.IsRunning() {
		t.Error("expected instrumented worker to be stopped")
	}
}

// TestInstrumentedWorker_Shutdown tests graceful shutdown
func TestInstrumentedWorker_Shutdown(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	worker := NewWorker(q, nil, registry, Config{
		ID:           "shutdown-test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	iw := NewInstrumentedWorker(worker, metrics)
	ctx := context.Background()

	if err := iw.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := iw.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("failed to shutdown instrumented worker: %v", err)
	}

	if iw.IsRunning() {
		t.Error("expected worker to be stopped after shutdown")
	}
}

// TestInstrumentedWorker_Stats tests stats reporting
func TestInstrumentedWorker_Stats(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	// Register a handler
	err := registry.Register("test-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return "success", nil
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Create a task
	testTask, err := task.NewTask("test-task", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	worker := NewWorker(q, nil, registry, Config{
		ID:           "stats-test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	iw := NewInstrumentedWorker(worker, metrics)
	ctx := context.Background()

	if err := iw.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for task to be processed
	time.Sleep(300 * time.Millisecond)

	// Check stats
	processed, failed, retried := iw.Stats()
	if processed != 1 {
		t.Errorf("expected 1 processed task, got %d", processed)
	}
	if failed != 0 {
		t.Errorf("expected 0 failed tasks, got %d", failed)
	}
	if retried != 0 {
		t.Errorf("expected 0 retried tasks, got %d", retried)
	}

	if err := iw.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}
}

// TestInstrumentedWorker_SyncStatsLoop tests the stats sync loop
func TestInstrumentedWorker_SyncStatsLoop(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	// Register a handler
	var executedCount int
	var mu sync.Mutex
	err := registry.Register("sync-test-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		mu.Lock()
		executedCount++
		mu.Unlock()
		return "success", nil
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Create multiple tasks
	for i := 0; i < 3; i++ {
		testTask, err := task.NewTask("sync-test-task", map[string]int{"count": i})
		if err != nil {
			t.Fatalf("failed to create task: %v", err)
		}
		if err := q.Enqueue(context.Background(), testTask); err != nil {
			t.Fatalf("failed to enqueue task: %v", err)
		}
	}

	worker := NewWorker(q, nil, registry, Config{
		ID:           "sync-stats-worker",
		PollInterval: 50 * time.Millisecond,
	})

	iw := NewInstrumentedWorker(worker, metrics)
	ctx := context.Background()

	if err := iw.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for tasks to be processed and stats to sync
	time.Sleep(500 * time.Millisecond)

	// Verify tasks were executed
	mu.Lock()
	count := executedCount
	mu.Unlock()

	if count != 3 {
		t.Errorf("expected 3 tasks executed, got %d", count)
	}

	if err := iw.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}
}

// TestInstrumentedWorker_MetricsRegistration tests worker metrics registration
func TestInstrumentedWorker_MetricsRegistration(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	worker := NewWorker(q, nil, registry, Config{
		ID:           "metrics-reg-worker",
		PollInterval: 50 * time.Millisecond,
	})

	iw := NewInstrumentedWorker(worker, metrics)
	ctx := context.Background()

	// Check initial worker counts
	active, _, _ := metrics.GetWorkerCounts()
	initialCount := active

	// Start worker - should register with metrics
	if err := iw.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Give time for registration
	time.Sleep(50 * time.Millisecond)

	active, _, _ = metrics.GetWorkerCounts()
	if active != initialCount+1 {
		t.Errorf("expected active workers to increase by 1, got %d (was %d)", active, initialCount)
	}

	// Stop worker - should unregister from metrics
	if err := iw.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}

	// Give time for unregistration
	time.Sleep(50 * time.Millisecond)

	active, _, _ = metrics.GetWorkerCounts()
	if active != initialCount {
		t.Errorf("expected active workers to return to %d, got %d", initialCount, active)
	}
}

// TestNewInstrumentedPool tests instrumented pool creation
func TestNewInstrumentedPool(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	// Create some instrumented workers
	workers := make([]*InstrumentedWorker, 3)
	for i := 0; i < 3; i++ {
		worker := NewWorker(q, nil, registry, Config{
			ID:           string(rune('A' + i)),
			PollInterval: 50 * time.Millisecond,
		})
		workers[i] = NewInstrumentedWorker(worker, metrics)
	}

	pool := NewInstrumentedPool(workers, metrics)

	if pool == nil {
		t.Fatal("expected instrumented pool to be created")
	}

	if pool.Size() != 3 {
		t.Errorf("expected pool size 3, got %d", pool.Size())
	}

	if pool.GetMetrics() != metrics {
		t.Error("expected metrics to be returned correctly")
	}
}

// TestInstrumentedPool_StartAll tests starting all workers
func TestInstrumentedPool_StartAll(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	workers := make([]*InstrumentedWorker, 2)
	for i := 0; i < 2; i++ {
		worker := NewWorker(q, nil, registry, Config{
			ID:           string(rune('A' + i)),
			PollInterval: 50 * time.Millisecond,
		})
		workers[i] = NewInstrumentedWorker(worker, metrics)
	}

	pool := NewInstrumentedPool(workers, metrics)
	ctx := context.Background()

	if err := pool.StartAll(ctx); err != nil {
		t.Fatalf("failed to start all workers: %v", err)
	}

	// Verify all workers are running
	for i, w := range workers {
		if !w.IsRunning() {
			t.Errorf("worker %d should be running", i)
		}
	}

	// Cleanup
	if err := pool.StopAll(); err != nil {
		t.Fatalf("failed to stop all workers: %v", err)
	}
}

// TestInstrumentedPool_StopAll tests stopping all workers
func TestInstrumentedPool_StopAll(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	workers := make([]*InstrumentedWorker, 2)
	for i := 0; i < 2; i++ {
		worker := NewWorker(q, nil, registry, Config{
			ID:           string(rune('X' + i)),
			PollInterval: 50 * time.Millisecond,
		})
		workers[i] = NewInstrumentedWorker(worker, metrics)
	}

	pool := NewInstrumentedPool(workers, metrics)
	ctx := context.Background()

	// Start all workers
	if err := pool.StartAll(ctx); err != nil {
		t.Fatalf("failed to start all workers: %v", err)
	}

	// Stop all workers
	if err := pool.StopAll(); err != nil {
		t.Fatalf("failed to stop all workers: %v", err)
	}

	// Verify all workers are stopped
	for i, w := range workers {
		if w.IsRunning() {
			t.Errorf("worker %d should be stopped", i)
		}
	}
}

// TestInstrumentedPool_ShutdownAll tests graceful shutdown of all workers
func TestInstrumentedPool_ShutdownAll(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	workers := make([]*InstrumentedWorker, 2)
	for i := 0; i < 2; i++ {
		worker := NewWorker(q, nil, registry, Config{
			ID:           string(rune('M' + i)),
			PollInterval: 50 * time.Millisecond,
		})
		workers[i] = NewInstrumentedWorker(worker, metrics)
	}

	pool := NewInstrumentedPool(workers, metrics)
	ctx := context.Background()

	if err := pool.StartAll(ctx); err != nil {
		t.Fatalf("failed to start all workers: %v", err)
	}

	// Shutdown all with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pool.ShutdownAll(shutdownCtx); err != nil {
		t.Fatalf("failed to shutdown all workers: %v", err)
	}

	// Verify all workers are stopped
	for i, w := range workers {
		if w.IsRunning() {
			t.Errorf("worker %d should be stopped after shutdown", i)
		}
	}
}

// TestInstrumentedPool_Size tests pool size
func TestInstrumentedPool_Size(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	tests := []struct {
		name       string
		numWorkers int
	}{
		{"empty pool", 0},
		{"single worker", 1},
		{"multiple workers", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workers := make([]*InstrumentedWorker, tt.numWorkers)
			for i := 0; i < tt.numWorkers; i++ {
				worker := NewWorker(q, nil, registry, Config{
					ID:           string(rune('0' + i)),
					PollInterval: 50 * time.Millisecond,
				})
				workers[i] = NewInstrumentedWorker(worker, metrics)
			}

			pool := NewInstrumentedPool(workers, metrics)

			if pool.Size() != tt.numWorkers {
				t.Errorf("expected pool size %d, got %d", tt.numWorkers, pool.Size())
			}
		})
	}
}

// TestNewMonitoredWorker tests monitored worker creation
func TestNewMonitoredWorker(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	worker := NewWorker(q, nil, registry, Config{
		ID:           "monitored-worker",
		PollInterval: 50 * time.Millisecond,
	})

	mw := NewMonitoredWorker(worker, metrics)

	if mw == nil {
		t.Fatal("expected monitored worker to be created")
	}

	if mw.InstrumentedWorker == nil {
		t.Error("expected embedded instrumented worker to exist")
	}

	if mw.GetID() != "monitored-worker" {
		t.Errorf("expected ID 'monitored-worker', got %s", mw.GetID())
	}
}

// TestMonitoredWorker_StartStop tests monitored worker lifecycle
func TestMonitoredWorker_StartStop(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	worker := NewWorker(q, nil, registry, Config{
		ID:           "monitored-lifecycle-worker",
		PollInterval: 50 * time.Millisecond,
	})

	mw := NewMonitoredWorker(worker, metrics)
	ctx := context.Background()

	// Start
	if err := mw.Start(ctx); err != nil {
		t.Fatalf("failed to start monitored worker: %v", err)
	}

	if !mw.IsRunning() {
		t.Error("expected monitored worker to be running")
	}

	// Wait for stats sync
	time.Sleep(200 * time.Millisecond)

	// Stop
	if err := mw.Stop(); err != nil {
		t.Fatalf("failed to stop monitored worker: %v", err)
	}

	if mw.IsRunning() {
		t.Error("expected monitored worker to be stopped")
	}
}

// TestMonitoredWorker_TaskProcessing tests task processing with monitoring
func TestMonitoredWorker_TaskProcessing(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	// Register handler
	var processed int
	var mu sync.Mutex
	err := registry.Register("monitored-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		mu.Lock()
		processed++
		mu.Unlock()
		return "done", nil
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Enqueue tasks
	for i := 0; i < 2; i++ {
		testTask, err := task.NewTask("monitored-task", map[string]int{"i": i})
		if err != nil {
			t.Fatalf("failed to create task: %v", err)
		}
		if err := q.Enqueue(context.Background(), testTask); err != nil {
			t.Fatalf("failed to enqueue task: %v", err)
		}
	}

	worker := NewWorker(q, nil, registry, Config{
		ID:           "task-processing-worker",
		PollInterval: 50 * time.Millisecond,
	})

	mw := NewMonitoredWorker(worker, metrics)
	ctx := context.Background()

	if err := mw.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for tasks
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	count := processed
	mu.Unlock()

	if count != 2 {
		t.Errorf("expected 2 tasks processed, got %d", count)
	}

	// Check stats
	processedCount, failedCount, _ := mw.Stats()
	if processedCount != 2 {
		t.Errorf("expected stats to show 2 processed, got %d", processedCount)
	}
	if failedCount != 0 {
		t.Errorf("expected stats to show 0 failed, got %d", failedCount)
	}

	if err := mw.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}
}

// TestInstrumentedWorker_ContextCancellation tests context cancellation handling
func TestInstrumentedWorker_ContextCancellation(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	worker := NewWorker(q, nil, registry, Config{
		ID:           "ctx-cancel-worker",
		PollInterval: 50 * time.Millisecond,
	})

	iw := NewInstrumentedWorker(worker, metrics)
	ctx, cancel := context.WithCancel(context.Background())

	if err := iw.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Cancel context
	cancel()

	// Give time for graceful shutdown
	time.Sleep(200 * time.Millisecond)

	if iw.IsRunning() {
		t.Error("expected worker to stop after context cancellation")
	}
}

// TestInstrumentedWorker_WithFailedTasks tests tracking of failed tasks
func TestInstrumentedWorker_WithFailedTasks(t *testing.T) {
	q := newMockQueue()
	registry := task.NewRegistry()
	metrics := monitoring.NewMetrics(q)

	// Register a failing handler
	err := registry.Register("failing-task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return nil, context.DeadlineExceeded
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Create a task with no retries
	testTask, err := task.NewTask("failing-task", nil)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	testTask.WithMaxRetries(0)
	if err := q.Enqueue(context.Background(), testTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	worker := NewWorker(q, nil, registry, Config{
		ID:           "failing-task-worker",
		PollInterval: 50 * time.Millisecond,
	})

	iw := NewInstrumentedWorker(worker, metrics)
	ctx := context.Background()

	if err := iw.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Wait for task to be processed
	time.Sleep(300 * time.Millisecond)

	// Check stats
	processed, failed, _ := iw.Stats()
	if processed != 0 {
		t.Errorf("expected 0 processed, got %d", processed)
	}
	if failed != 1 {
		t.Errorf("expected 1 failed, got %d", failed)
	}

	if err := iw.Stop(); err != nil {
		t.Fatalf("failed to stop worker: %v", err)
	}
}

// TestInstrumentedPool_EmptyPool tests operations on empty pool
func TestInstrumentedPool_EmptyPool(t *testing.T) {
	metrics := monitoring.NewMetrics(newMockQueue())

	pool := NewInstrumentedPool([]*InstrumentedWorker{}, metrics)

	if pool.Size() != 0 {
		t.Errorf("expected size 0, got %d", pool.Size())
	}

	ctx := context.Background()

	// Start on empty pool should succeed
	if err := pool.StartAll(ctx); err != nil {
		t.Errorf("StartAll on empty pool should succeed: %v", err)
	}

	// Stop on empty pool should succeed
	if err := pool.StopAll(); err != nil {
		t.Errorf("StopAll on empty pool should succeed: %v", err)
	}

	// Shutdown on empty pool should succeed
	if err := pool.ShutdownAll(ctx); err != nil {
		t.Errorf("ShutdownAll on empty pool should succeed: %v", err)
	}
}
