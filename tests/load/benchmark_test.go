package load

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/internal/worker"
	"github.com/redis/go-redis/v9"
)

// TestPayload is a simple payload for benchmark tasks
type TestPayload struct {
	Message string `json:"message"`
	Number  int    `json:"number"`
}

// setupBenchmarkEnvironment creates a test queue, storage, and worker pool
func setupBenchmarkEnvironment(workerCount int) (*queue.RedisQueue, storage.Storage, []*worker.Worker, *task.Registry, *redis.Client, error) {
	// Create Redis client for storage
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		PoolSize: 10,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	// Create Redis queue
	q, err := queue.NewRedisQueue("localhost:6379", "", 0, 10)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to create queue: %w", err)
	}

	// Create storage
	store := storage.NewRedisStorage(client)

	// Create registry and register a simple handler
	registry := task.NewRegistry()
	err = registry.Register("benchmark_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		// Simulate some work
		var p TestPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		// Minimal processing to test throughput
		return map[string]interface{}{
			"processed": true,
			"message":   p.Message,
		}, nil
	})
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to register handler: %w", err)
	}

	// Create workers
	workers := make([]*worker.Worker, workerCount)
	for i := 0; i < workerCount; i++ {
		workers[i] = worker.NewWorker(q, store, registry, worker.Config{
			ID:           fmt.Sprintf("benchmark-worker-%d", i),
			PollInterval: 10 * time.Millisecond, // Fast polling for benchmark
		})
	}

	return q, store, workers, registry, client, nil
}

// getCompletedCount returns the number of completed tasks from storage
func getCompletedCount(ctx context.Context, store storage.Storage) (int64, error) {
	tasks, err := store.GetTasksByState(ctx, task.StateCompleted, 0)
	if err != nil {
		return 0, err
	}
	return int64(len(tasks)), nil
}

// cleanupBenchmarkEnvironment stops workers and clears the queue
func cleanupBenchmarkEnvironment(q *queue.RedisQueue, workers []*worker.Worker, client *redis.Client) {
	// Stop all workers
	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		go func(worker *worker.Worker) {
			defer wg.Done()
			if err := worker.Stop(); err != nil {
				log.Printf("Warning: failed to stop worker: %v", err)
			}
		}(w)
	}
	wg.Wait()

	// Clear the queue (best effort)
	if q != nil {
		q.Close()
	}

	// Close Redis client
	if client != nil {
		client.Close()
	}
}

// BenchmarkTaskThroughput measures tasks processed per second
func BenchmarkTaskThroughput(b *testing.B) {
	workerCounts := []int{1, 2, 4, 8}

	for _, numWorkers := range workerCounts {
		b.Run(fmt.Sprintf("Workers=%d", numWorkers), func(b *testing.B) {
			q, store, workers, _, client, err := setupBenchmarkEnvironment(numWorkers)
			if err != nil {
				b.Fatalf("Failed to setup benchmark: %v", err)
			}
			defer cleanupBenchmarkEnvironment(q, workers, client)
			ctx := context.Background()
			// Start workers
			for _, w := range workers {
				if err := w.Start(ctx); err != nil {
					b.Fatalf("Failed to start worker: %v", err)
				}
			}

			// Wait for workers to be ready
			time.Sleep(100 * time.Millisecond)

			b.ResetTimer()
			b.ReportAllocs()

			// Enqueue tasks
			for i := 0; i < b.N; i++ {
				t, err := task.NewTask("benchmark_task", TestPayload{
					Message: fmt.Sprintf("Task %d", i),
					Number:  i,
				})
				if err != nil {
					b.Fatalf("Failed to create task: %v", err)
				}

				if err := q.Enqueue(ctx, t); err != nil {
					b.Fatalf("Failed to enqueue task: %v", err)
				}
			}

			// Wait for all tasks to complete
			for {
				completed, _ := getCompletedCount(ctx, store)
				if completed >= int64(b.N) {
					break
				}

				time.Sleep(10 * time.Millisecond)
			}

			b.StopTimer()
		})
	}
}

// TestThroughputRequirement verifies the system can process 100+ tasks/second
func TestThroughputRequirement(t *testing.T) {
	const (
		targetTPS    = 100 // Tasks per second target
		testDuration = 10 * time.Second
		workerCount  = 4
		tolerance    = 0.9 // 90% of target is acceptable
	)

	q, store, workers, _, client, err := setupBenchmarkEnvironment(workerCount)
	if err != nil {
		t.Fatalf("Failed to setup benchmark: %v", err)
	}
	defer cleanupBenchmarkEnvironment(q, workers, client)

	ctx := context.Background()

	// Start workers
	for _, w := range workers {
		if err := w.Start(ctx); err != nil {
			t.Fatalf("Failed to start worker: %v", err)
		}
	}

	// Wait for workers to be ready
	time.Sleep(100 * time.Millisecond)

	log.Printf("Starting throughput test: target=%d TPS, duration=%v, workers=%d", targetTPS, testDuration, workerCount)

	var (
		submitted      int64
		completed      int64
		failed         int64
		startTime      = time.Now()
		stopTime       = startTime.Add(testDuration)
		submissionDone = make(chan struct{})
	)

	// Producer goroutine: continuously submit tasks
	go func() {
		defer close(submissionDone)
		taskNum := 0
		for time.Now().Before(stopTime) {
			t, err := task.NewTask("benchmark_task", TestPayload{
				Message: fmt.Sprintf("Task %d", taskNum),
				Number:  taskNum,
			})
			if err != nil {
				log.Printf("Failed to create task: %v", err)
				continue
			}

			if err := q.Enqueue(ctx, t); err != nil {
				atomic.AddInt64(&failed, 1)
				log.Printf("Failed to enqueue task: %v", err)
				continue
			}

			atomic.AddInt64(&submitted, 1)
			taskNum++

			// Small sleep to avoid overwhelming the system during submission
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Wait for submission to complete
	<-submissionDone

	// Wait for all submitted tasks to be processed (with timeout)
	processingTimeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

processingLoop:
	for {
		select {
		case <-ticker.C:
			completedNow, err := getCompletedCount(ctx, store)
			if err != nil {
				t.Logf("Warning: Failed to get completed count: %v", err)
				continue
			}

			if completedNow >= atomic.LoadInt64(&submitted) {
				completed = completedNow
				break processingLoop
			}

		case <-processingTimeout:
			t.Logf("Processing timeout reached")
			break processingLoop
		}
	}

	elapsed := time.Since(startTime)
	actualTPS := float64(completed) / elapsed.Seconds()
	requiredTPS := float64(targetTPS) * tolerance

	// Print detailed results
	t.Logf("=== Throughput Test Results ===")
	t.Logf("Duration:        %v", elapsed)
	t.Logf("Workers:         %d", workerCount)
	t.Logf("Tasks Submitted: %d", submitted)
	t.Logf("Tasks Completed: %d", completed)
	t.Logf("Tasks Failed:    %d", failed)
	t.Logf("Throughput:      %.2f tasks/second", actualTPS)
	t.Logf("Target:          %.2f tasks/second (%.0f%% of %d)", requiredTPS, tolerance*100, targetTPS)
	t.Logf("===============================")

	if actualTPS < requiredTPS {
		t.Errorf("❌ FAILED: Throughput %.2f TPS is below requirement of %.2f TPS", actualTPS, requiredTPS)
	} else {
		t.Logf("✅ PASSED: Throughput %.2f TPS exceeds requirement of %.2f TPS", actualTPS, requiredTPS)
	}
}

// TestConcurrentTaskSubmission tests high-concurrency task submission
func TestConcurrentTaskSubmission(t *testing.T) {
	q, store, workers, _, client, err := setupBenchmarkEnvironment(4)
	if err != nil {
		t.Fatalf("Failed to setup benchmark: %v", err)
	}
	defer cleanupBenchmarkEnvironment(q, workers, client)

	ctx := context.Background()

	// Start workers
	for _, w := range workers {
		if err := w.Start(ctx); err != nil {
			t.Fatalf("Failed to start worker: %v", err)
		}
	}

	time.Sleep(100 * time.Millisecond)

	const (
		numGoroutines   = 10
		tasksPerRoutine = 100
		totalTasks      = numGoroutines * tasksPerRoutine
	)

	var (
		wg        sync.WaitGroup
		submitted int64
		errors    int64
	)

	startTime := time.Now()

	// Launch concurrent task submitters
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()

			for i := 0; i < tasksPerRoutine; i++ {
				t, err := task.NewTask("benchmark_task", TestPayload{
					Message: fmt.Sprintf("Routine %d Task %d", routineID, i),
					Number:  i,
				})
				if err != nil {
					atomic.AddInt64(&errors, 1)
					continue
				}

				if err := q.Enqueue(ctx, t); err != nil {
					atomic.AddInt64(&errors, 1)
					continue
				}

				atomic.AddInt64(&submitted, 1)
			}
		}(g)
	}

	wg.Wait()
	submissionTime := time.Since(startTime)

	// Wait for processing
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(30 * time.Second)

	var completed int64
processingLoop:
	for {
		select {
		case <-ticker.C:
			completedNow, err := getCompletedCount(ctx, store)
			if err != nil {
				continue
			}

			if completedNow >= submitted {
				completed = completedNow
				break processingLoop
			}

		case <-timeout:
			completed, _ = getCompletedCount(ctx, store)
			break processingLoop
		}
	}

	totalTime := time.Since(startTime)

	t.Logf("=== Concurrent Submission Test ===")
	t.Logf("Goroutines:      %d", numGoroutines)
	t.Logf("Tasks/Routine:   %d", tasksPerRoutine)
	t.Logf("Total Expected:  %d", totalTasks)
	t.Logf("Submitted:       %d", submitted)
	t.Logf("Completed:       %d", completed)
	t.Logf("Errors:          %d", errors)
	t.Logf("Submission Time: %v", submissionTime)
	t.Logf("Total Time:      %v", totalTime)
	t.Logf("Throughput:      %.2f tasks/sec", float64(completed)/totalTime.Seconds())
	t.Logf("==================================")

	if completed < submitted*90/100 {
		t.Errorf("Only %d/%d tasks completed (%.1f%%)", completed, submitted, float64(completed)/float64(submitted)*100)
	}
}
