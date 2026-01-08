package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/farhan-ahmed1/spool/internal/monitoring"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/internal/worker"
	"github.com/farhan-ahmed1/spool/web"
	"github.com/redis/go-redis/v9"
)

// DashboardDemo demonstrates the real-time dashboard with a realistic workload
func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting Spool Dashboard Demo...")

	// Load configuration
	redisAddr := "localhost:6379"
	redisPassword := ""
	redisDB := 0

	// Set up context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Redis queue
	q, err := queue.NewRedisQueue(redisAddr, redisPassword, redisDB, 10)
	if err != nil {
		log.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	// Purge existing tasks for clean demo
	if err := q.Purge(ctx); err != nil {
		log.Printf("Warning: Failed to purge queue: %v", err)
	}

	// Initialize storage - create redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       redisDB,
	})
	store := storage.NewRedisStorage(redisClient)
	defer store.Close()

	// Initialize metrics
	metrics := monitoring.NewMetrics(q)

	// Initialize task registry with sample handlers
	registry := task.NewRegistry()
	
	// Register sample task handlers
	registry.Register("process_image", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		// Simulate image processing work
		time.Sleep(time.Duration(rand.Intn(500)+200) * time.Millisecond)
		
		// 10% chance of failure for demo purposes
		if rand.Float64() < 0.10 {
			return nil, fmt.Errorf("image processing failed: corrupted file")
		}
		
		log.Printf("✓ Processed image task")
		return "success", nil
	})

	registry.Register("send_email", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		// Simulate email sending
		time.Sleep(time.Duration(rand.Intn(300)+100) * time.Millisecond)
		
		// 5% chance of failure
		if rand.Float64() < 0.05 {
			return nil, fmt.Errorf("email send failed: SMTP timeout")
		}
		
		log.Printf("Sent email task")
		return "success", nil
	})

	registry.Register("generate_report", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		// Simulate report generation
		time.Sleep(time.Duration(rand.Intn(800)+400) * time.Millisecond)
		
		// 8% chance of failure
		if rand.Float64() < 0.08 {
			return nil, fmt.Errorf("report generation failed: insufficient data")
		}
		
		log.Printf("Generated report task")
		return "success", nil
	})

	registry.Register("data_export", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		// Simulate data export
		time.Sleep(time.Duration(rand.Intn(600)+300) * time.Millisecond)
		
		// 12% chance of failure
		if rand.Float64() < 0.12 {
			return nil, fmt.Errorf("data export failed: disk full")
		}
		
		log.Printf("Exported data task")
		return "success", nil
	})

	registry.Register("backup_database", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		// Simulate database backup
		time.Sleep(time.Duration(rand.Intn(1000)+500) * time.Millisecond)
		
		// 3% chance of failure
		if rand.Float64() < 0.03 {
			return nil, fmt.Errorf("backup failed: connection lost")
		}
		
		log.Printf("Backed up database task")
		return "success", nil
	})

	// Start worker pool with 5 workers
	log.Println("Starting worker pool with 5 workers...")
	
	// Start dashboard server first so workers can broadcast to it
	log.Println("Starting dashboard server on http://localhost:8080")
	dashboard := web.NewServer(web.Config{
		Addr:    ":8080",
		Metrics: metrics,
		Queue:   q,
		Storage: store,
	})

	// Start server in goroutine
	serverErrChan := make(chan error, 1)
	go func() {
		serverErrChan <- dashboard.Start()
	}()
	
	// Give server a moment to start
	time.Sleep(500 * time.Millisecond)
	
	workers := make([]*worker.Worker, 5)
	for i := 0; i < 5; i++ {
		w := worker.NewWorker(q, store, registry, worker.Config{
			ID:           fmt.Sprintf("worker-%d", i+1),
			PollInterval: 100 * time.Millisecond,
		})
		
		// Register worker with metrics
		metrics.RegisterWorker(w.ID())
		
		// Start worker with instrumentation and task event broadcasting
		instrumentedWorker := worker.NewInstrumentedWorker(w, metrics)
		
		// Wrap to broadcast task events to dashboard
		go startWorkerWithEventBroadcast(ctx, instrumentedWorker, dashboard)
		
		workers[i] = w
	}

	// Start task generator to simulate realistic workload
	go generateTasks(ctx, q, metrics)

	// Start queue depth updater
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := metrics.UpdateQueueDepth(ctx); err != nil {
					log.Printf("Failed to update queue depth: %v", err)
				}
			}
		}
	}()

	// Display instructions
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("DASHBOARD DEMO RUNNING")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("Dashboard URL: http://localhost:8080")
	fmt.Println("Workers:       5 active workers processing tasks")
	fmt.Println("Workload:      Continuous stream of tasks being generated")
	fmt.Println("")
	fmt.Println("Features to test:")
	fmt.Println("  ✓ Real-time queue depth chart")
	fmt.Println("  ✓ Live worker status updates")
	fmt.Println("  ✓ Task statistics (processed, failed, retried)")
	fmt.Println("  ✓ Throughput and performance metrics")
	fmt.Println("  ✓ Dead Letter Queue monitoring")
	fmt.Println("  ✓ Task inspection UI")
	fmt.Println("")
	fmt.Println("Press Ctrl+C to stop the demo")
	fmt.Println(strings.Repeat("=", 70) + "\n")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down...", sig)
	case err := <-serverErrChan:
		log.Printf("Server error: %v", err)
	}

	// Graceful shutdown
	log.Println("Stopping workers...")
	cancel() // Cancel context for all workers

	// Wait for workers to finish
	time.Sleep(2 * time.Second)

	// Stop dashboard
	log.Println("Stopping dashboard server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := dashboard.Stop(shutdownCtx); err != nil {
		log.Printf("Error stopping dashboard: %v", err)
	}

	// Print final statistics
	snapshot := metrics.Snapshot()
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("FINAL STATISTICS")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("Tasks Enqueued:    %d\n", snapshot.TasksEnqueued)
	fmt.Printf("Tasks Processed:   %d\n", snapshot.TasksProcessed)
	fmt.Printf("Tasks Failed:      %d\n", snapshot.TasksFailed)
	fmt.Printf("Tasks Retried:     %d\n", snapshot.TasksRetried)
	fmt.Printf("Success Rate:      %.1f%%\n", float64(snapshot.TasksProcessed)/float64(snapshot.TasksEnqueued)*100)
	fmt.Printf("Avg Throughput:    %.2f tasks/sec\n", snapshot.AvgThroughput)
	fmt.Printf("Avg Process Time:  %v\n", snapshot.AvgProcessingTime)
	fmt.Printf("Total Uptime:      %v\n", snapshot.Uptime)
	fmt.Println(strings.Repeat("=", 70))

	log.Println("Demo completed successfully!")
}

// generateTasks continuously generates tasks to simulate a realistic workload
func generateTasks(ctx context.Context, q queue.Queue, metrics *monitoring.Metrics) {
	taskTypes := []string{
		"process_image",
		"send_email",
		"generate_report",
		"data_export",
		"backup_database",
	}

	priorities := []task.Priority{
		task.PriorityLow,
		task.PriorityNormal,
		task.PriorityHigh,
		task.PriorityCritical,
	}

	// Initial burst of tasks
	log.Println("Generating initial batch of 20 tasks...")
	for i := 0; i < 20; i++ {
		generateSingleTask(ctx, q, metrics, taskTypes, priorities)
		time.Sleep(100 * time.Millisecond)
	}

	// Continuous task generation with varying rates
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	burstTicker := time.NewTicker(30 * time.Second)
	defer burstTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Regular task generation (1-3 tasks)
			count := rand.Intn(3) + 1
			for i := 0; i < count; i++ {
				generateSingleTask(ctx, q, metrics, taskTypes, priorities)
			}
		case <-burstTicker.C:
			// Periodic burst of tasks to test scaling
			log.Println("Generating task burst (10 tasks)...")
			for i := 0; i < 10; i++ {
				generateSingleTask(ctx, q, metrics, taskTypes, priorities)
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
}

// generateSingleTask creates and enqueues a single task
func generateSingleTask(ctx context.Context, q queue.Queue, metrics *monitoring.Metrics, taskTypes []string, priorities []task.Priority) {
	taskType := taskTypes[rand.Intn(len(taskTypes))]
	priority := priorities[rand.Intn(len(priorities))]

	payload := map[string]interface{}{
		"user_id":    rand.Intn(10000),
		"timestamp":  time.Now().Unix(),
		"batch_id":   fmt.Sprintf("batch-%d", rand.Intn(100)),
		"parameters": map[string]interface{}{
			"quality": "high",
			"format":  "json",
		},
	}

	t, err := task.NewTask(taskType, payload)
	if err != nil {
		log.Printf("Error creating task: %v", err)
		return
	}

	t.WithPriority(priority).WithMaxRetries(3).WithTimeout(5 * time.Second)

	if err := q.Enqueue(ctx, t); err != nil {
		log.Printf("Error enqueueing task: %v", err)
		return
	}

	metrics.RecordTaskEnqueued()
	
	// Log only critical tasks to reduce noise
	if priority == task.PriorityCritical {
		log.Printf("Enqueued CRITICAL task: %s (type: %s, id: %s)", t.Type, t.Type, t.ID)
	}
}

// startWorkerWithEventBroadcast starts a worker and monitors its task execution,
// broadcasting task completion events to the dashboard for the live activity feed
func startWorkerWithEventBroadcast(ctx context.Context, iw *worker.InstrumentedWorker, dashboard *web.Server) {
	// Start the worker
	if err := iw.Start(ctx); err != nil {
		log.Printf("Failed to start worker: %v", err)
		return
	}
	
	// Monitor worker stats and broadcast task events
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	
	lastProcessed := int64(0)
	lastFailed := int64(0)
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			processed, failed, _ := iw.Stats()
			
			// Detect completed tasks
			if processed > lastProcessed {
				newCompleted := processed - lastProcessed
				for i := int64(0); i < newCompleted; i++ {
					// Broadcast success event
					// Note: In production, you'd get actual task details from the worker
					// For this demo, we'll generate representative data
					taskTypes := []string{"process_image", "send_email", "generate_report", "data_export", "backup_database"}
					taskType := taskTypes[rand.Intn(len(taskTypes))]
					duration := time.Duration(rand.Intn(800)+200) * time.Millisecond
					
					dashboard.BroadcastTaskEvent(
						fmt.Sprintf("task-%d", time.Now().UnixNano()),
						taskType,
						"success",
						duration,
					)
				}
				lastProcessed = processed
			}
			
			// Detect failed tasks
			if failed > lastFailed {
				newFailed := failed - lastFailed
				for i := int64(0); i < newFailed; i++ {
					// Broadcast failure event
					taskTypes := []string{"process_image", "send_email", "generate_report", "data_export", "backup_database"}
					taskType := taskTypes[rand.Intn(len(taskTypes))]
					duration := time.Duration(rand.Intn(500)+100) * time.Millisecond
					
					dashboard.BroadcastTaskEvent(
						fmt.Sprintf("task-%d", time.Now().UnixNano()),
						taskType,
						"failed",
						duration,
					)
				}
				lastFailed = failed
			}
		}
	}
}
