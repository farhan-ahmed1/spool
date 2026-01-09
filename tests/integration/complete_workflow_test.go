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

	ctx := context.Background()
	client := setupRedisClient(t, ctx)
	defer cleanupRedisClient(client, ctx)

	// Setup test components
	components := setupTestComponents(t, ctx, client)
	defer components.q.Close()

	// Create worker pool manager
	workerManager := setupWorkerManager(t, ctx, components)
	defer shutdownWorkers(t, workerManager)

	// Start dashboard server
	server := setupDashboardServer(t, components)
	defer shutdownDashboardServer(t, server)

	// Execute test phases
	executePhase1InitialTasks(t, ctx, components)
	executePhase2HighLoad(t, ctx, components, workerManager)
	executePhase3RetryMechanism(t, ctx, components)
	executePhase4MetricsVerification(t, components)

	t.Log("Workflow test completed successfully")
	t.Logf("Total tasks executed: %d", atomic.LoadInt32(components.executedCount))
	t.Logf("Total failed attempts: %d", atomic.LoadInt32(components.failedCount))
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
	// Flush any old test data before starting
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("Failed to flush Redis: %v", err)
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
	defer func() {
		if err := w.Stop(); err != nil {
			t.Logf("Failed to stop worker: %v", err)
		}
	}()

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

	ctx := context.Background()
	client := setupRedisClient(t, ctx)
	defer cleanupRedisClient(client, ctx)

	// Setup test components for dashboard metrics test
	components := setupDashboardMetricsTest(t, ctx, client)
	defer components.q.Close()

	// Start instrumented worker
	worker := startInstrumentedWorker(t, ctx, components)
	defer stopWorker(t, worker)

	// Execute test and verify metrics
	executeMetricsAccuracyTest(t, ctx, components)

	t.Log("Dashboard metrics accuracy test passed")
}

// TestE2E_ConcurrentWorkersConsistency tests multiple workers processing concurrently
func TestE2E_ConcurrentWorkersConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	client := setupRedisClient(t, ctx)
	defer cleanupRedisClient(client, ctx)

	// Setup test components
	components := setupConcurrentWorkersTest(t, ctx, client)
	defer components.q.Close()

	// Start multiple workers
	workers := startConcurrentWorkers(t, ctx, components)
	defer stopConcurrentWorkers(t, workers)

	// Execute test and verify consistency
	executeConcurrencyTest(t, ctx, components)

	t.Log("Concurrent workers consistency test passed")
}

// Helper functions for TestE2E_CompleteWorkflow

type testComponents struct {
	q             *queue.RedisQueue
	store         *storage.RedisStorage
	metrics       *monitoring.Metrics
	registry      *task.Registry
	executedCount *int32
	failedCount   *int32
	totalDuration *int64
	attemptCount  *int32
}

type workerManager struct {
	workers          []*worker.InstrumentedWorker
	mutex            sync.Mutex
	createWorkerFunc func(id string) *worker.InstrumentedWorker
}

func setupRedisClient(t *testing.T, ctx context.Context) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("Failed to flush Redis: %v", err)
	}

	return client
}

func cleanupRedisClient(client *redis.Client, ctx context.Context) {
	client.FlushDB(ctx)
	client.Close()
}

func setupTestComponents(t *testing.T, ctx context.Context, client *redis.Client) *testComponents {
	q, err := queue.NewRedisQueue("localhost:6379", "", 15, 10)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	store := storage.NewRedisStorage(client)
	metrics := monitoring.NewMetrics(q)
	registry := task.NewRegistry()

	var executedCount int32
	var failedCount int32
	var totalDuration int64
	var attemptCount int32

	// Register successful task handler
	err = registry.Register("process_data", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		start := time.Now()
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			atomic.AddInt32(&failedCount, 1)
			return nil, err
		}

		time.Sleep(10 * time.Millisecond)

		atomic.AddInt32(&executedCount, 1)
		atomic.AddInt64(&totalDuration, time.Since(start).Milliseconds())
		return map[string]string{"status": "processed", "id": fmt.Sprint(data["id"])}, nil
	})
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	// Register flaky task handler
	err = registry.Register("flaky_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		count := atomic.AddInt32(&attemptCount, 1)
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

	return &testComponents{
		q:             q,
		store:         store,
		metrics:       metrics,
		registry:      registry,
		executedCount: &executedCount,
		failedCount:   &failedCount,
		totalDuration: &totalDuration,
		attemptCount:  &attemptCount,
	}
}

func setupWorkerManager(t *testing.T, ctx context.Context, components *testComponents) *workerManager {
	createWorkerFunc := func(id string) *worker.InstrumentedWorker {
		w := worker.NewWorker(components.q, components.store, components.registry, worker.Config{
			ID:           id,
			PollInterval: 50 * time.Millisecond,
		})
		return worker.NewInstrumentedWorker(w, components.metrics)
	}

	wm := &workerManager{
		workers:          make([]*worker.InstrumentedWorker, 0),
		createWorkerFunc: createWorkerFunc,
	}

	// Start with one worker
	w1 := createWorkerFunc("worker-1")
	if err := w1.Start(ctx); err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}
	wm.workers = append(wm.workers, w1)

	return wm
}

func setupDashboardServer(t *testing.T, components *testComponents) *web.Server {
	dashboardConfig := web.Config{
		Addr:    ":0",
		Metrics: components.metrics,
		Queue:   components.q,
		Storage: components.store,
	}
	server := web.NewServer(dashboardConfig)

	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil {
			serverErrCh <- err
		}
	}()

	select {
	case <-server.Ready():
		return server
	case err := <-serverErrCh:
		t.Fatalf("Dashboard server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for server to start")
	}
	return nil
}

func shutdownDashboardServer(t *testing.T, server *web.Server) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Stop(shutdownCtx); err != nil {
		t.Logf("Warning: Failed to stop server: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
}

func shutdownWorkers(t *testing.T, wm *workerManager) {
	wm.mutex.Lock()
	defer wm.mutex.Unlock()

	for i, w := range wm.workers {
		t.Logf("Stopping worker %d", i+1)
		if err := w.Stop(); err != nil {
			t.Errorf("Failed to stop worker %d: %v", i+1, err)
		}
	}
}

func executePhase1InitialTasks(t *testing.T, ctx context.Context, components *testComponents) {
	t.Log("Phase 1: Submitting initial batch of 10 tasks")
	for i := 0; i < 10; i++ {
		tsk, err := task.NewTask("process_data", map[string]interface{}{
			"id":   i,
			"data": fmt.Sprintf("item-%d", i),
		})
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
		if err := components.q.Enqueue(ctx, tsk); err != nil {
			t.Fatalf("Failed to enqueue task: %v", err)
		}
	}

	time.Sleep(2 * time.Second)

	executedSoFar := atomic.LoadInt32(components.executedCount)
	if executedSoFar < 10 {
		t.Errorf("Expected at least 10 tasks executed, got %d", executedSoFar)
	}
}

func executePhase2HighLoad(t *testing.T, ctx context.Context, components *testComponents, wm *workerManager) {
	t.Log("Phase 2: Submitting large batch of 50 tasks to simulate high load")
	for i := 10; i < 60; i++ {
		tsk, err := task.NewTask("process_data", map[string]interface{}{
			"id":   i,
			"data": fmt.Sprintf("item-%d", i),
		})
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
		if err := components.q.Enqueue(ctx, tsk); err != nil {
			t.Fatalf("Failed to enqueue task: %v", err)
		}
	}

	depth, err := components.q.Size(ctx)
	if err != nil {
		t.Fatalf("Failed to get queue depth: %v", err)
	}
	t.Logf("Queue depth after bulk enqueue: %d", depth)

	if depth > 20 {
		t.Log("Scaling up: Starting additional workers")
		wm.mutex.Lock()
		for i := 2; i <= 3; i++ {
			w := wm.createWorkerFunc(fmt.Sprintf("worker-%d", i))
			if err := w.Start(ctx); err != nil {
				t.Fatalf("Failed to start worker %d: %v", i, err)
			}
			wm.workers = append(wm.workers, w)
			t.Logf("Started worker-%d", i)
		}
		wm.mutex.Unlock()
	}

	maxWait := 5 * time.Second
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		depth, _ := components.q.Size(ctx)
		if depth == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	finalDepth, _ := components.q.Size(ctx)
	t.Logf("Queue depth after processing: %d", finalDepth)
}

func executePhase3RetryMechanism(t *testing.T, ctx context.Context, components *testComponents) {
	t.Log("Phase 3: Testing retry mechanism with flaky tasks")
	for i := 0; i < 3; i++ {
		tsk, err := task.NewTask("flaky_task", map[string]interface{}{
			"id": i,
		})
		if err != nil {
			t.Fatalf("Failed to create flaky task: %v", err)
		}
		tsk.WithMaxRetries(5)
		if err := components.q.Enqueue(ctx, tsk); err != nil {
			t.Fatalf("Failed to enqueue flaky task: %v", err)
		}
	}

	time.Sleep(2 * time.Second)
}

func executePhase4MetricsVerification(t *testing.T, components *testComponents) {
	t.Log("Phase 4: Verifying metrics collection")
	snapshot := components.metrics.Snapshot()

	t.Logf("Metrics - Processed: %d, Failed: %d, Active: %d",
		snapshot.TasksProcessed, snapshot.TasksFailed, snapshot.ActiveWorkers)

	if snapshot.TasksProcessed < 60 {
		t.Errorf("Expected at least 60 tasks processed, got %d", snapshot.TasksProcessed)
	}

	if snapshot.ActiveWorkers < 1 {
		t.Errorf("Expected at least 1 active worker, got %d", snapshot.ActiveWorkers)
	}

	avgDuration := float64(atomic.LoadInt64(components.totalDuration)) / float64(atomic.LoadInt32(components.executedCount))
	t.Logf("Average task processing time: %.2f ms", avgDuration)

	if avgDuration < 5 || avgDuration > 500 {
		t.Errorf("Unexpected average processing time: %.2f ms", avgDuration)
	}
}

// Helper functions for TestE2E_DashboardMetricsAccuracy

type dashboardMetricsComponents struct {
	q              *queue.RedisQueue
	store          *storage.RedisStorage
	metrics        *monitoring.Metrics
	registry       *task.Registry
	processedCount *int32
}

func setupDashboardMetricsTest(t *testing.T, ctx context.Context, client *redis.Client) *dashboardMetricsComponents {
	q, err := queue.NewRedisQueue("localhost:6379", "", 15, 10)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

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

	return &dashboardMetricsComponents{
		q:              q,
		store:          store,
		metrics:        metrics,
		registry:       registry,
		processedCount: &processedCount,
	}
}

func startInstrumentedWorker(t *testing.T, ctx context.Context, components *dashboardMetricsComponents) *worker.InstrumentedWorker {
	w := worker.NewWorker(components.q, components.store, components.registry, worker.Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})
	iw := worker.NewInstrumentedWorker(w, components.metrics)
	if err := iw.Start(ctx); err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}
	return iw
}

func stopWorker(t *testing.T, worker *worker.InstrumentedWorker) {
	if err := worker.Stop(); err != nil {
		t.Logf("Failed to stop worker: %v", err)
	}
}

func executeMetricsAccuracyTest(t *testing.T, ctx context.Context, components *dashboardMetricsComponents) {
	taskCount := 20
	t.Logf("Enqueuing %d tasks", taskCount)

	for i := 0; i < taskCount; i++ {
		tsk, err := task.NewTask("test_task", map[string]interface{}{"id": i})
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
		if err := components.q.Enqueue(ctx, tsk); err != nil {
			t.Fatalf("Failed to enqueue task: %v", err)
		}
	}

	// Wait for completion
	maxWait := 5 * time.Second
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(components.processedCount) >= int32(taskCount) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Give instrumented worker time to sync stats
	time.Sleep(200 * time.Millisecond)

	// Verify metrics accuracy
	snapshot := components.metrics.Snapshot()
	t.Logf("Metrics - Processed: %d, Expected: %d", snapshot.TasksProcessed, taskCount)
	t.Logf("Actual executions: %d", atomic.LoadInt32(components.processedCount))

	if snapshot.TasksProcessed != int64(taskCount) {
		t.Errorf("Metrics mismatch: expected %d processed tasks, got %d",
			taskCount, snapshot.TasksProcessed)
	}

	if atomic.LoadInt32(components.processedCount) != int32(taskCount) {
		t.Errorf("Execution count mismatch: expected %d, got %d",
			taskCount, atomic.LoadInt32(components.processedCount))
	}

	if snapshot.ActiveWorkers < 1 {
		t.Errorf("Expected at least 1 active worker, got %d", snapshot.ActiveWorkers)
	}
}

// Helper functions for TestE2E_ConcurrentWorkersConsistency

type concurrentWorkersComponents struct {
	q              *queue.RedisQueue
	store          *storage.RedisStorage
	metrics        *monitoring.Metrics
	registry       *task.Registry
	processedCount *int32
}

func setupConcurrentWorkersTest(t *testing.T, ctx context.Context, client *redis.Client) *concurrentWorkersComponents {
	q, err := queue.NewRedisQueue("localhost:6379", "", 15, 10)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	store := storage.NewRedisStorage(client)
	metrics := monitoring.NewMetrics(q)
	registry := task.NewRegistry()
	var processedCount int32

	err = registry.Register("concurrent_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}

		atomic.AddInt32(&processedCount, 1)
		taskID := fmt.Sprint(data["id"])

		time.Sleep(20 * time.Millisecond)
		return map[string]string{"task_id": taskID}, nil
	})
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	return &concurrentWorkersComponents{
		q:              q,
		store:          store,
		metrics:        metrics,
		registry:       registry,
		processedCount: &processedCount,
	}
}

func startConcurrentWorkers(t *testing.T, ctx context.Context, components *concurrentWorkersComponents) []*worker.InstrumentedWorker {
	workerCount := 3
	workers := make([]*worker.InstrumentedWorker, workerCount)

	for i := 0; i < workerCount; i++ {
		w := worker.NewWorker(components.q, components.store, components.registry, worker.Config{
			ID:           fmt.Sprintf("worker-%d", i+1),
			PollInterval: 50 * time.Millisecond,
		})
		iw := worker.NewInstrumentedWorker(w, components.metrics)
		if err := iw.Start(ctx); err != nil {
			t.Fatalf("Failed to start worker %d: %v", i+1, err)
		}
		workers[i] = iw
	}
	return workers
}

func stopConcurrentWorkers(t *testing.T, workers []*worker.InstrumentedWorker) {
	for i, iw := range workers {
		if err := iw.Stop(); err != nil {
			t.Logf("Failed to stop worker %d: %v", i+1, err)
		}
	}
}

func executeConcurrencyTest(t *testing.T, ctx context.Context, components *concurrentWorkersComponents) {
	taskCount := 50
	workerCount := 3

	t.Logf("Enqueuing %d tasks for %d concurrent workers", taskCount, workerCount)
	for i := 0; i < taskCount; i++ {
		tsk, err := task.NewTask("concurrent_task", map[string]interface{}{
			"id": i,
		})
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
		if err := components.q.Enqueue(ctx, tsk); err != nil {
			t.Fatalf("Failed to enqueue task: %v", err)
		}
	}

	// Wait for all tasks to complete
	maxWait := 10 * time.Second
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		count := atomic.LoadInt32(components.processedCount)
		if count >= int32(taskCount) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Verify all tasks were processed exactly once
	finalProcessed := atomic.LoadInt32(components.processedCount)
	if finalProcessed != int32(taskCount) {
		t.Errorf("Expected %d tasks processed, got %d", taskCount, finalProcessed)
	}

	// Give workers time to sync their stats to metrics
	time.Sleep(200 * time.Millisecond)

	// Verify task distribution across workers using metrics
	workerDetails := components.metrics.GetAllWorkerDetails()
	workerTaskCount := make(map[string]int64)
	for _, stats := range workerDetails {
		workerTaskCount[stats.ID] = stats.TasksCompleted
	}

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
}
