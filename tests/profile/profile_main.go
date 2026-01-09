package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime/pprof"
	"time"

	"github.com/farhan-ahmed1/spool/internal/monitoring"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/internal/worker"
	"github.com/redis/go-redis/v9"
)

// Profile test runs a realistic workload and generates CPU and memory profiles
func main() {
	log.Println("Starting performance profiling...")

	// Setup profiling and components
	ctx := context.Background()
	components, cleanupFn, err := setupProfiling(ctx)
	if err != nil {
		log.Fatal("Setup failed:", err)
	}
	defer cleanupFn()

	// Run the profiling workload
	runProfilingWorkload(ctx, components)

	// Generate memory profile
	generateMemoryProfile()

	log.Println("Profiling complete!")
	log.Println("To analyze profiles:")
	log.Println("  CPU:    go tool pprof cpu_profile.prof")
	log.Println("  Memory: go tool pprof mem_profile.prof")
}

// ProfileComponents holds all components needed for profiling
type ProfileComponents struct {
	queue    queue.Queue
	store    storage.Storage
	metrics  *monitoring.Metrics
	registry *task.Registry
	workers  []*worker.InstrumentedWorker
}

// setupProfiling initializes CPU profiling and all components
func setupProfiling(ctx context.Context) (*ProfileComponents, func(), error) {
	// Setup CPU profiling
	cpuProfile, err := os.Create("cpu_profile.prof")
	if err != nil {
		return nil, nil, fmt.Errorf("could not create CPU profile: %w", err)
	}

	if err := pprof.StartCPUProfile(cpuProfile); err != nil {
		cpuProfile.Close()
		return nil, nil, fmt.Errorf("could not start CPU profile: %w", err)
	}

	cleanup := func() {
		pprof.StopCPUProfile()
		cpuProfile.Close()
	}

	// Setup Redis client
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   14, // Use separate DB for profiling
	})

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, cleanup, fmt.Errorf("redis not available: %w", err)
	}

	// Enhanced cleanup
	enhancedCleanup := func() {
		client.FlushDB(ctx)
		client.Close()
		cleanup()
	}

	components, err := initializeProfileComponents(ctx, client)
	if err != nil {
		return nil, enhancedCleanup, err
	}

	return components, enhancedCleanup, nil
}

// initializeProfileComponents sets up queue, storage, metrics and workers
func initializeProfileComponents(ctx context.Context, client *redis.Client) (*ProfileComponents, error) {
	// Setup queue and storage
	q, err := queue.NewRedisQueue("localhost:6379", "", 14, 20)
	if err != nil {
		return nil, fmt.Errorf("failed to create queue: %w", err)
	}

	store := storage.NewRedisStorage(client)
	metrics := monitoring.NewMetrics(q)
	registry := task.NewRegistry()

	// Register a lightweight task
	err = registry.Register("profile_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}

		// Simulate minimal processing
		time.Sleep(5 * time.Millisecond)
		return map[string]string{"status": "processed"}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to register handler: %w", err)
	}

	// Create worker pool
	workers, err := createWorkerPool(ctx, q, store, registry, metrics)
	if err != nil {
		return nil, err
	}

	return &ProfileComponents{
		queue:    q,
		store:    store,
		metrics:  metrics,
		registry: registry,
		workers:  workers,
	}, nil
}

// createWorkerPool creates and starts a pool of workers
func createWorkerPool(ctx context.Context, q queue.Queue, store storage.Storage, registry *task.Registry, metrics *monitoring.Metrics) ([]*worker.InstrumentedWorker, error) {
	workerCount := 5
	workers := make([]*worker.InstrumentedWorker, workerCount)

	for i := 0; i < workerCount; i++ {
		w := worker.NewWorker(q, store, registry, worker.Config{
			ID:           fmt.Sprintf("profile-worker-%d", i+1),
			PollInterval: 10 * time.Millisecond, // Aggressive polling for profiling
		})
		iw := worker.NewInstrumentedWorker(w, metrics)
		workers[i] = iw

		if err := iw.Start(ctx); err != nil {
			return nil, fmt.Errorf("failed to start worker %d: %w", i+1, err)
		}
	}
	log.Printf("Started %d workers", workerCount)
	return workers, nil
}

// runProfilingWorkload executes the main profiling workload
func runProfilingWorkload(ctx context.Context, components *ProfileComponents) {
	taskCount := 1000
	log.Printf("Enqueuing %d tasks...", taskCount)

	// Enqueue tasks and measure performance
	enqueueTime := enqueueTasks(ctx, components.queue, taskCount)
	log.Printf("Enqueued %d tasks in %v (%.2f tasks/sec)",
		taskCount, enqueueTime, float64(taskCount)/enqueueTime.Seconds())

	// Wait for processing and measure performance
	processTime := waitForProcessing(ctx, components.queue, taskCount)
	log.Printf("Processed %d tasks in %v (%.2f tasks/sec)",
		taskCount, processTime, float64(taskCount)/processTime.Seconds())

	// Log metrics
	logMetrics(components.metrics)

	// Stop workers
	stopWorkers(components.workers)
}

// enqueueTasks adds tasks to the queue and returns the time taken
func enqueueTasks(ctx context.Context, q queue.Queue, taskCount int) time.Duration {
	start := time.Now()
	for i := 0; i < taskCount; i++ {
		tsk, err := task.NewTask("profile_task", map[string]interface{}{
			"id":    i,
			"batch": "profile",
		})
		if err != nil {
			log.Fatal("Failed to create task:", err)
		}

		if err := q.Enqueue(ctx, tsk); err != nil {
			log.Fatal("Failed to enqueue task:", err)
		}
	}
	return time.Since(start)
}

// waitForProcessing waits for all tasks to be processed
func waitForProcessing(ctx context.Context, q queue.Queue, taskCount int) time.Duration {
	log.Println("Processing tasks...")
	start := time.Now()

	maxWait := 2 * time.Minute
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		depth, _ := q.Size(ctx)
		if depth == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	return time.Since(start)
}

// logMetrics displays the current metrics snapshot
func logMetrics(metrics *monitoring.Metrics) {
	snapshot := metrics.Snapshot()
	log.Printf("Metrics - Processed: %d, Failed: %d, Active Workers: %d",
		snapshot.TasksProcessed, snapshot.TasksFailed, snapshot.ActiveWorkers)
	log.Printf("Average processing time: %v", snapshot.AvgProcessingTime)
}

// stopWorkers gracefully stops all workers
func stopWorkers(workers []*worker.InstrumentedWorker) {
	log.Println("Stopping workers...")
	for i, w := range workers {
		if err := w.Stop(); err != nil {
			log.Printf("Warning: Failed to stop worker %d: %v", i+1, err)
		}
	}
}

// generateMemoryProfile creates and writes the memory profile
func generateMemoryProfile() {
	memProfile, err := os.Create("mem_profile.prof")
	if err != nil {
		log.Fatal("Could not create memory profile:", err)
	}
	defer memProfile.Close()

	if err := pprof.WriteHeapProfile(memProfile); err != nil {
		log.Fatal("Could not write memory profile:", err)
	}
}
