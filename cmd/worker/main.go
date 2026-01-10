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

	"github.com/farhan-ahmed1/spool/internal/monitoring"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/internal/worker"
	"github.com/redis/go-redis/v9"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting Spool Worker...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Redis queue
	q, err := queue.NewRedisQueue("localhost:6379", "", 0, 10)
	if err != nil {
		log.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	// Initialize storage
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	store := storage.NewRedisStorage(redisClient)
	defer store.Close()

	// Initialize metrics
	metrics := monitoring.NewMetrics(q)

	// Register task handlers
	registry := task.NewRegistry()
	if err := registry.Register("default", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		log.Printf("Processing task with payload: %s", string(payload))
		time.Sleep(100 * time.Millisecond)
		return "completed", nil
	}); err != nil {
		log.Fatalf("Failed to register handler: %v", err)
	}

	// Create worker pool
	pool := worker.NewPool(q, store, registry, metrics, worker.PoolConfig{
		MinWorkers:      2,
		MaxWorkers:      10,
		PollInterval:    100 * time.Millisecond,
		ShutdownTimeout: 30 * time.Second,
	})

	// Start pool
	if err := pool.Start(); err != nil {
		log.Fatalf("Failed to start worker pool: %v", err)
	}

	log.Println("Worker pool started. Press Ctrl+C to stop.")

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down workers...")
	if err := pool.Shutdown(ctx); err != nil {
		log.Printf("Error shutting down pool: %v", err)
	}
}
