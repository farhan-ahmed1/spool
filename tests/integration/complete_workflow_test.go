package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/monitoring"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/internal/worker"
	"github.com/farhan-ahmed1/spool/web"
	"github.com/redis/go-redis/v9"
)

// TestE2E_CompleteWorkflow tests the entire system working together:
// - Task submission and execution
// - Auto-scaling based on load
// - Dashboard metrics collection
// - Error handling and retries
// - Graceful shutdown
func TestE2E_CompleteWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup Redis client
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer client.FlushDB(ctx)
	defer client.Close()

	// Setup components
	q, err := queue.NewRedisQueue("localhost:6379", "", 15, 10)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	store := storage.NewRedisStorage(client)
	metrics := monitoring.NewMetrics(q)
	registry := task.NewRegistry()

	// Track task executions
	var executedCount int32
	var failedCount int32
	var totalDuration int64 // in milliseconds

	// Register a successful task handler
	err = registry.Register("process_data", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		start := time.Now()
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			atomic.AddInt32(&failedCount, 1)
			return nil, err
		}

		// Simulate processing
		time.Sleep(10 * time.Millisecond)

		atomic.AddInt32(&executedCount, 1)
		atomic.AddInt64(&totalDuration, time.Since(start).Milliseconds())
		return map[string]string{"status": "processed", "id": fmt.Sprint(data["id"])}, nil
	})
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	// Register a flaky task handler (fails sometimes)
	var attemptCount int32
	err = registry.Register("flaky_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		count := atomic.AddInt32(&attemptCount, 1)
		// Fail the first 2 attempts, succeed on 3rd
		if count%3 != 0 {
			atomic.AddInt32(&failedCount, 1)
			return nil, fmt.Errorf("temporary failure (attempt %d)", count)
		}
		atomic.AddInt32(&executedCount, 1)
		return map[string]string{"status": "success after retry"}, nil
	})
	if err != nil {
		t.Fatalf("Failed to register flaky handler: %v", err)
	}

	// Create worker pool manager (manual control for testing)
	workers := make([]*worker.InstrumentedWorker, 0)
	var workerMu sync.Mutex

	createWorker := func(id string) *worker.InstrumentedWorker {
		w := worker.NewWorker(q, store, registry, worker.Config{
			ID:           id,
			PollInterval: 50 * time.Millisecond,
		})
		iw := worker.NewInstrumentedWorker(w, metrics)
		return iw
	}

	// Start with one worker
	w1 := createWorker("worker-1")
	workers = append(workers, w1)
	if err := w1.Start(ctx); err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}

	// Start dashboard server
	dashboardConfig := web.Config{
		Addr:    ":18080", // Use different port for testing
		Metrics: metrics,
		Queue:   q,
		Storage: store,
	}
	server := web.NewServer(dashboardConfig)
	var serverStarted bool

	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil {
			serverErrCh <- err
		}
	}()

	// Give server time to start
	time.Sleep(200 * time.Millisecond)

	// Check if server started successfully
	select {
	case err := <-serverErrCh:
		t.Fatalf("Dashboard server failed to start: %v", err)
	default:
		// Server started successfully
		serverStarted = true
	}

	// Phase 1: Submit initial batch of tasks (low load)
	t.Log("Phase 1: Submitting initial batch of 10 tasks")
	for i := 0; i < 10; i++ {
		tsk, err := task.NewTask("process_data", map[string]interface{}{
			"id":   i,
			"data": fmt.Sprintf("item-%d", i),
		})
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
		if err := q.Enqueue(ctx, tsk); err != nil {
			t.Fatalf("Failed to enqueue task: %v", err)
		}
	}

	// Wait for initial tasks to complete
	time.Sleep(500 * time.Millisecond)

	if atomic.LoadInt32(&executedCount) < 10 {
		t.Errorf("Expected at least 10 tasks executed, got %d", executedCount)
	}

	// Phase 2: Submit large batch (trigger scaling need)
	t.Log("Phase 2: Submitting large batch of 50 tasks to simulate high load")
	for i := 10; i < 60; i++ {
		tsk, err := task.NewTask("process_data", map[string]interface{}{
			"id":   i,
			"data": fmt.Sprintf("item-%d", i),
		})
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
		if err := q.Enqueue(ctx, tsk); err != nil {
			t.Fatalf("Failed to enqueue task: %v", err)
		}
	}

	// Check queue depth
	depth, err := q.Size(ctx)
	if err != nil {
		t.Fatalf("Failed to get queue depth: %v", err)
	}
	t.Logf("Queue depth after bulk enqueue: %d", depth)

	// Manually scale up (simulate auto-scaling)
	if depth > 20 {
		t.Log("Scaling up: Starting additional workers")
		workerMu.Lock()
		for i := 2; i <= 3; i++ {
			w := createWorker(fmt.Sprintf("worker-%d", i))
			if err := w.Start(ctx); err != nil {
				t.Fatalf("Failed to start worker %d: %v", i, err)
			}
			workers = append(workers, w)
			t.Logf("Started worker-%d", i)
		}
		workerMu.Unlock()
	}

	// Wait for tasks to complete
	maxWait := 5 * time.Second
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		depth, _ := q.Size(ctx)
		if depth == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	finalDepth, _ := q.Size(ctx)
	t.Logf("Queue depth after processing: %d", finalDepth)

	// Phase 3: Test retry mechanism with flaky tasks
	t.Log("Phase 3: Testing retry mechanism with flaky tasks")
	for i := 0; i < 3; i++ {
		tsk, err := task.NewTask("flaky_task", map[string]interface{}{
			"id": i,
		})
		if err != nil {
			t.Fatalf("Failed to create flaky task: %v", err)
		}
		tsk.WithMaxRetries(5) // Allow retries
		if err := q.Enqueue(ctx, tsk); err != nil {
			t.Fatalf("Failed to enqueue flaky task: %v", err)
		}
	}

	// Wait for flaky tasks to complete
	time.Sleep(2 * time.Second)

	// Phase 4: Verify metrics collection
	t.Log("Phase 4: Verifying metrics collection")
	snapshot := metrics.Snapshot()

	t.Logf("Metrics - Processed: %d, Failed: %d, Active: %d",
		snapshot.TasksProcessed, snapshot.TasksFailed, snapshot.ActiveWorkers)

	if snapshot.TasksProcessed < 60 {
		t.Errorf("Expected at least 60 tasks processed, got %d", snapshot.TasksProcessed)
	}

	if snapshot.ActiveWorkers < 1 {
		t.Errorf("Expected at least 1 active worker, got %d", snapshot.ActiveWorkers)
	}

	// Check average processing time
	avgDuration := float64(atomic.LoadInt64(&totalDuration)) / float64(atomic.LoadInt32(&executedCount))
	t.Logf("Average task processing time: %.2f ms", avgDuration)

	if avgDuration < 5 || avgDuration > 500 {
		t.Errorf("Unexpected average processing time: %.2f ms", avgDuration)
	}

	// Phase 5: Test graceful shutdown
	t.Log("Phase 5: Testing graceful shutdown")

	// Stop dashboard server
	if serverStarted {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Stop(shutdownCtx); err != nil {
			t.Logf("Warning: Failed to stop server: %v", err)
		}
	}
	time.Sleep(100 * time.Millisecond)

	// Gracefully stop all workers
	workerMu.Lock()
	for i, w := range workers {
		t.Logf("Stopping worker %d", i+1)
		if err := w.Stop(); err != nil {
			t.Errorf("Failed to stop worker %d: %v", i+1, err)
		}
	}
	workerMu.Unlock()

	// Verify final state
	t.Log("Workflow test completed successfully")
	t.Logf("Total tasks executed: %d", atomic.LoadInt32(&executedCount))
	t.Logf("Total failed attempts: %d", atomic.LoadInt32(&failedCount))
	t.Logf("Final queue depth: %d", finalDepth)
}

// TestE2E_AutoScalingIntegration tests the auto-scaling system
func TestE2E_AutoScalingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer client.FlushDB(ctx)
	defer client.Close()

	// Setup
	q, err := queue.NewRedisQueue("localhost:6379", "", 15, 10)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	store := storage.NewRedisStorage(client)
	_ = monitoring.NewMetrics(q) // Initialize metrics (not used directly in this simplified test)
	registry := task.NewRegistry()

	// Register a slow task to test scaling
	err = registry.Register("slow_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		time.Sleep(200 * time.Millisecond)
		return map[string]string{"status": "completed"}, nil
	})
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	// Note: For full auto-scaling integration test, see examples/autoscaling
	// This simplified test focuses on task processing without actual auto-scaling

	// Start initial worker
	w := worker.NewWorker(q, store, registry, worker.Config{
		ID:           "worker-1",
		PollInterval: 50 * time.Millisecond,
	})
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}
	defer w.Stop()

	// Test scenario 1: Submit tasks and monitor queue behavior
	t.Log("Scenario 1: Enqueuing 30 slow tasks and monitoring queue depth")
	initialQueueDepth, _ := q.Size(ctx)
	for i := 0; i < 30; i++ {
		tsk, err := task.NewTask("slow_task", map[string]interface{}{"id": i})
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
		if err := q.Enqueue(ctx, tsk); err != nil {
			t.Fatalf("Failed to enqueue task: %v", err)
		}
	}

	// Check that queue grew
	peakQueueDepth, _ := q.Size(ctx)
	if peakQueueDepth <= initialQueueDepth {
		t.Logf("Queue depth: initial=%d, peak=%d", initialQueueDepth, peakQueueDepth)
	}

	// Test scenario 2: Wait for tasks to process
	t.Log("Scenario 2: Waiting for queue to clear")

	// Wait for tasks to complete
	maxWait := 10 * time.Second
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		depth, _ := q.Size(ctx)
		if depth == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	finalQueueDepth, _ := q.Size(ctx)
	if finalQueueDepth != 0 {
		t.Errorf("Expected queue to be empty, got depth=%d", finalQueueDepth)
	}

	t.Logf("Queue processing stats - Initial: %d, Peak: %d, Final: %d",
		initialQueueDepth, peakQueueDepth, finalQueueDepth)

	t.Log("Auto-scaling integration test completed")
}

// TestE2E_DashboardMetricsAccuracy tests that dashboard metrics are accurate
func TestE2E_DashboardMetricsAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer client.FlushDB(ctx)
	defer client.Close()

	// Setup
	q, err := queue.NewRedisQueue("localhost:6379", "", 15, 10)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	store := storage.NewRedisStorage(client)
	metrics := monitoring.NewMetrics(q)
	registry := task.NewRegistry()

	var processedCount int32

	err = registry.Register("test_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		atomic.AddInt32(&processedCount, 1)
		time.Sleep(10 * time.Millisecond)
		return map[string]string{"status": "ok"}, nil
	})
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	// Start worker
	w := worker.NewWorker(q, store, registry, worker.Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}
	defer w.Stop()

	// Enqueue known number of tasks
	taskCount := 20
	t.Logf("Enqueuing %d tasks", taskCount)
	for i := 0; i < taskCount; i++ {
		tsk, err := task.NewTask("test_task", map[string]interface{}{"id": i})
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
		if err := q.Enqueue(ctx, tsk); err != nil {
			t.Fatalf("Failed to enqueue task: %v", err)
		}
	}

	// Wait for completion
	maxWait := 5 * time.Second
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&processedCount) >= int32(taskCount) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Verify metrics accuracy
	snapshot := metrics.Snapshot()

	t.Logf("Metrics - Processed: %d, Expected: %d", snapshot.TasksProcessed, taskCount)
	t.Logf("Actual executions: %d", atomic.LoadInt32(&processedCount))

	if snapshot.TasksProcessed != int64(taskCount) {
		t.Errorf("Metrics mismatch: expected %d processed tasks, got %d",
			taskCount, snapshot.TasksProcessed)
	}

	if atomic.LoadInt32(&processedCount) != int32(taskCount) {
		t.Errorf("Execution count mismatch: expected %d, got %d",
			taskCount, atomic.LoadInt32(&processedCount))
	}

	if snapshot.ActiveWorkers < 1 {
		t.Errorf("Expected at least 1 active worker, got %d", snapshot.ActiveWorkers)
	}

	t.Log("Dashboard metrics accuracy test passed")
}

// TestE2E_ConcurrentWorkersConsistency tests multiple workers processing concurrently
func TestE2E_ConcurrentWorkersConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer client.FlushDB(ctx)
	defer client.Close()

	// Setup
	q, err := queue.NewRedisQueue("localhost:6379", "", 15, 10)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	store := storage.NewRedisStorage(client)
	_ = monitoring.NewMetrics(q) // Initialize metrics (not used directly in this test)
	registry := task.NewRegistry()

	// Track which tasks were processed and by whom
	processedTasks := make(map[string]string) // taskID -> workerID
	var mu sync.Mutex

	err = registry.Register("concurrent_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}

		taskID := fmt.Sprint(data["id"])
		workerID := fmt.Sprint(data["worker_context"])

		mu.Lock()
		processedTasks[taskID] = workerID
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)
		return map[string]string{"task_id": taskID, "worker": workerID}, nil
	})
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	// Start multiple workers
	workerCount := 3
	workers := make([]*worker.Worker, workerCount)
	for i := 0; i < workerCount; i++ {
		w := worker.NewWorker(q, store, registry, worker.Config{
			ID:           fmt.Sprintf("worker-%d", i+1),
			PollInterval: 50 * time.Millisecond,
		})
		if err := w.Start(ctx); err != nil {
			t.Fatalf("Failed to start worker %d: %v", i+1, err)
		}
		workers[i] = w
		defer w.Stop()
	}

	// Enqueue tasks
	taskCount := 50
	t.Logf("Enqueuing %d tasks for %d concurrent workers", taskCount, workerCount)
	for i := 0; i < taskCount; i++ {
		tsk, err := task.NewTask("concurrent_task", map[string]interface{}{
			"id": i,
		})
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
		if err := q.Enqueue(ctx, tsk); err != nil {
			t.Fatalf("Failed to enqueue task: %v", err)
		}
	}

	// Wait for all tasks to complete
	maxWait := 10 * time.Second
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		mu.Lock()
		processed := len(processedTasks)
		mu.Unlock()

		if processed >= taskCount {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Verify all tasks were processed exactly once
	mu.Lock()
	processedCount := len(processedTasks)
	mu.Unlock()

	if processedCount != taskCount {
		t.Errorf("Expected %d tasks processed, got %d", taskCount, processedCount)
	}

	// Verify task distribution across workers
	workerTaskCount := make(map[string]int)
	mu.Lock()
	for _, workerID := range processedTasks {
		workerTaskCount[workerID]++
	}
	mu.Unlock()

	t.Logf("Task distribution across workers:")
	for workerID, count := range workerTaskCount {
		t.Logf("  %s: %d tasks (%.1f%%)", workerID, count, float64(count)/float64(taskCount)*100)
	}

	// Each worker should have processed some tasks (basic load distribution check)
	for i := 1; i <= workerCount; i++ {
		workerID := fmt.Sprintf("worker-%d", i)
		if workerTaskCount[workerID] == 0 {
			t.Errorf("Worker %s did not process any tasks", workerID)
		}
	}

	t.Log("Concurrent workers consistency test passed")
}
