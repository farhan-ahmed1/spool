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

	// Setup CPU profiling
	cpuProfile, err := os.Create("cpu_profile.prof")
	if err != nil {
		log.Fatal("Could not create CPU profile: ", err)
	}
	defer cpuProfile.Close()

	if err := pprof.StartCPUProfile(cpuProfile); err != nil {
		log.Fatal("Could not start CPU profile: ", err)
	}
	defer pprof.StopCPUProfile()

	// Setup Redis client
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   14, // Use separate DB for profiling
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatal("Redis not available:", err)
	}
	defer client.FlushDB(ctx)
	defer client.Close()

	// Setup queue and storage
	q, err := queue.NewRedisQueue("localhost:6379", "", 14, 20)
	if err != nil {
		log.Fatal("Failed to create queue:", err)
	}
	defer q.Close()

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
		log.Fatal("Failed to register handler:", err)
	}

	// Create worker pool
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
			log.Fatalf("Failed to start worker %d: %v", i+1, err)
		}
	}
	log.Printf("Started %d workers", workerCount)

	// Enqueue large batch of tasks
	taskCount := 1000
	log.Printf("Enqueuing %d tasks...", taskCount)

	startEnqueue := time.Now()
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
	enqueueTime := time.Since(startEnqueue)
	log.Printf("Enqueued %d tasks in %v (%.2f tasks/sec)",
		taskCount, enqueueTime, float64(taskCount)/enqueueTime.Seconds())

	// Wait for processing
	log.Println("Processing tasks...")
	startProcess := time.Now()

	maxWait := 2 * time.Minute
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		depth, _ := q.Size(ctx)
		if depth == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	processTime := time.Since(startProcess)
	log.Printf("Processed %d tasks in %v (%.2f tasks/sec)",
		taskCount, processTime, float64(taskCount)/processTime.Seconds())

	// Get metrics snapshot
	snapshot := metrics.Snapshot()
	log.Printf("Metrics - Processed: %d, Failed: %d, Active Workers: %d",
		snapshot.TasksProcessed, snapshot.TasksFailed, snapshot.ActiveWorkers)
	log.Printf("Average processing time: %v", snapshot.AvgProcessingTime)

	// Stop workers
	log.Println("Stopping workers...")
	for i, w := range workers {
		if err := w.Stop(); err != nil {
			log.Printf("Warning: Failed to stop worker %d: %v", i+1, err)
		}
	}

	// Write memory profile
	memProfile, err := os.Create("mem_profile.prof")
	if err != nil {
		log.Fatal("Could not create memory profile:", err)
	}
	defer memProfile.Close()

	if err := pprof.WriteHeapProfile(memProfile); err != nil {
		log.Fatal("Could not write memory profile:", err)
	}

	log.Println("Profiling complete!")
	log.Println("To analyze profiles:")
	log.Println("  CPU:    go tool pprof cpu_profile.prof")
	log.Println("  Memory: go tool pprof mem_profile.prof")
}
