package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/farhan-ahmed1/spool/internal/config"
	"github.com/farhan-ahmed1/spool/internal/monitoring"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/internal/worker"
	"github.com/redis/go-redis/v9"
)

// AutoScaling Demo
// This demonstrates the auto-scaling workers feature of Spool
//
// Run this with Redis:
//   make docker-up
//   go run examples/autoscaling/main.go

func main() {
	log.Println("=== Spool Auto-Scaling Demo ===")
	log.Println()

	// Load configuration
	cfg := config.Default()
	cfg.Worker.AutoScaling.Enabled = true
	cfg.Worker.AutoScaling.MinWorkers = 2
	cfg.Worker.AutoScaling.MaxWorkers = 10
	cfg.Worker.AutoScaling.ScaleUpThreshold = 20
	cfg.Worker.AutoScaling.ScaleDownIdleTime = 5 * time.Second
	cfg.Worker.AutoScaling.CheckInterval = 2 * time.Second

	log.Printf("Auto-scaling config: min=%d, max=%d, threshold=%d tasks, idle=%v\n",
		cfg.Worker.AutoScaling.MinWorkers,
		cfg.Worker.AutoScaling.MaxWorkers,
		cfg.Worker.AutoScaling.ScaleUpThreshold,
		cfg.Worker.AutoScaling.ScaleDownIdleTime)
	log.Println()

	// Setup Redis queue
	redisQueue, err := queue.NewRedisQueue("localhost:6379", "", 0, 10)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	// Setup Redis storage - create Redis client for storage
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		PoolSize: 10,
	})
	redisStorage := storage.NewRedisStorage(client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Clear queue
	if err := redisQueue.Purge(ctx); err != nil {
		log.Printf("Warning: failed to purge queue: %v", err)
	}

	// Create task registry
	registry := task.NewRegistry()
	
	// Register a CPU-intensive task
	registry.Register("heavy_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}
		
		id := data["id"]
		log.Printf("  [Task %v] Processing...", id)
		
		// Simulate CPU-intensive work
		time.Sleep(500 * time.Millisecond)
		
		log.Printf("  [Task %v] Completed", id)
		return map[string]interface{}{"result": "processed", "id": id}, nil
	})

	// Create metrics collector
	metrics := monitoring.NewMetrics(redisQueue)

	// Create worker pool
	pool := worker.NewPool(redisQueue, redisStorage, registry, metrics, worker.PoolConfig{
		MinWorkers:      cfg.Worker.AutoScaling.MinWorkers,
		MaxWorkers:      cfg.Worker.AutoScaling.MaxWorkers,
		PollInterval:    100 * time.Millisecond,
		ShutdownTimeout: 10 * time.Second,
	})

	// Start pool
	if err := pool.Start(); err != nil {
		log.Fatalf("Failed to start worker pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	log.Printf("✓ Worker pool started with %d workers\n", pool.GetWorkerCount())
	log.Println()

	// Create auto-scaler
	scaler := monitoring.NewAutoScaler(metrics, pool, cfg.Worker.AutoScaling)
	if err := scaler.Start(ctx); err != nil {
		log.Fatalf("Failed to start auto-scaler: %v", err)
	}
	defer scaler.Stop()

	log.Println("✓ Auto-scaler started")
	log.Println()

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start monitoring goroutine
	go monitorMetrics(ctx, metrics, pool, scaler)

	// Simulate workload phases
	go func() {
		time.Sleep(2 * time.Second)

		// Phase 1: Burst of tasks (trigger scale up)
		log.Println("📈 Phase 1: High load - enqueueing 50 tasks...")
		for i := 0; i < 50; i++ {
			t, err := task.NewTask("heavy_task", map[string]interface{}{"id": i + 1})
			if err != nil {
				log.Printf("Failed to create task: %v", err)
				continue
			}
			if err := redisQueue.Enqueue(ctx, t); err != nil {
				log.Printf("Failed to enqueue task: %v", err)
			}
			metrics.RecordTaskEnqueued()
		}
		log.Println("  Tasks enqueued. Watch workers scale up...")
		log.Println()

		// Wait for processing
		time.Sleep(20 * time.Second)

		// Phase 2: No new tasks (trigger scale down)
		log.Println("📉 Phase 2: Low load - no new tasks...")
		log.Println("  Watch workers scale down after idle period...")
		log.Println()

		time.Sleep(15 * time.Second)

		// Final stats
		log.Println("=== Final Statistics ===")
		printFinalStats(metrics, scaler)
		
		// Wait a bit more to show continued monitoring, then auto-shutdown
		log.Println("\n⏱️  Continuing to monitor for 10 more seconds... (Press Ctrl+C to stop)")
		time.Sleep(10 * time.Second)
		
		// Trigger shutdown
		sigChan <- os.Interrupt
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("\n🛑 Shutdown signal received")
	cancel()
}

// monitorMetrics prints real-time metrics
func monitorMetrics(ctx context.Context, metrics *monitoring.Metrics, pool *worker.Pool, scaler *monitoring.AutoScaler) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snapshot := metrics.Snapshot()
			poolStats := pool.GetStats()

			fmt.Printf("\r[%s] Workers: %d (idle: %d, busy: %d) | Queue: %d | Processed: %d | Throughput: %.1f tps     \n",
				time.Now().Format("15:04:05"),
				poolStats["total_workers"],
				poolStats["idle_workers"],
				poolStats["busy_workers"],
				snapshot.QueueDepth,
				snapshot.TasksProcessed,
				snapshot.CurrentThroughput,
			)
		}
	}
}

// printFinalStats prints comprehensive statistics
func printFinalStats(metrics *monitoring.Metrics, scaler *monitoring.AutoScaler) {
	snapshot := metrics.Snapshot()
	scalerStats := scaler.GetStats()

	fmt.Printf("\n")
	fmt.Printf("Tasks Statistics:\n")
	fmt.Printf("  Enqueued:    %d\n", snapshot.TasksEnqueued)
	fmt.Printf("  Processed:   %d\n", snapshot.TasksProcessed)
	fmt.Printf("  Failed:      %d\n", snapshot.TasksFailed)
	fmt.Printf("  In Progress: %d\n", snapshot.TasksInProgress)
	fmt.Printf("\n")
	fmt.Printf("Worker Statistics:\n")
	fmt.Printf("  Active:      %d\n", snapshot.ActiveWorkers)
	fmt.Printf("  Idle:        %d\n", snapshot.IdleWorkers)
	fmt.Printf("  Busy:        %d\n", snapshot.BusyWorkers)
	fmt.Printf("\n")
	fmt.Printf("Performance:\n")
	fmt.Printf("  Throughput:  %.2f tasks/sec (current)\n", snapshot.CurrentThroughput)
	fmt.Printf("  Throughput:  %.2f tasks/sec (average)\n", snapshot.AvgThroughput)
	fmt.Printf("  Uptime:      %v\n", snapshot.Uptime.Round(time.Second))
	fmt.Printf("\n")
	fmt.Printf("Auto-Scaling:\n")
	fmt.Printf("  Scale-up:    %d times\n", scalerStats["scale_up_count"])
	fmt.Printf("  Scale-down:  %d times\n", scalerStats["scale_down_count"])
	fmt.Printf("\n")

	// Print recent decisions
	history := scaler.GetDecisionHistory(5)
	if len(history) > 0 {
		fmt.Printf("Recent Scaling Decisions:\n")
		for i, decision := range history {
			if decision.Action != monitoring.NoAction {
				fmt.Printf("  %d. %s: %d -> %d workers (Queue: %d, Reason: %s)\n",
					i+1,
					decision.Action,
					decision.CurrentCount,
					decision.DesiredCount,
					decision.QueueDepth,
					decision.Reason)
			}
		}
		fmt.Printf("\n")
	}

	fmt.Println("✓ Demo completed successfully!")
	fmt.Println()
	fmt.Println("Key Observations:")
	fmt.Println("  • Workers scaled UP when queue depth exceeded threshold")
	fmt.Println("  • Workers scaled DOWN after being idle for the configured period")
	fmt.Println("  • System maintained min/max worker boundaries")
	fmt.Println("  • Throughput adjusted dynamically based on workload")
}
