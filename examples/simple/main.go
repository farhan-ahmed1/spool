package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/internal/worker"
	"github.com/redis/go-redis/v9"
)

// EmailPayload demonstrates a typed task payload
type EmailPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	ctx := context.Background()

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

	// Register task handlers
	registry := task.NewRegistry()
	registry.Register("send_email", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var email EmailPayload
		if err := json.Unmarshal(payload, &email); err != nil {
			return nil, err
		}
		log.Printf("Sending email to %s: %s", email.To, email.Subject)
		time.Sleep(100 * time.Millisecond) // Simulate work
		return map[string]string{"status": "sent"}, nil
	})

	// Create a single worker to process tasks
	w := worker.NewWorker(q, store, registry, worker.Config{
		ID:           "demo-worker",
		PollInterval: 50 * time.Millisecond,
	})

	// Start worker in background
	if err := w.Start(ctx); err != nil {
		log.Fatalf("Failed to start worker: %v", err)
	}
	defer w.Shutdown(ctx)

	// Submit a task
	emailTask, err := task.NewTask("send_email", EmailPayload{
		To:      "user@example.com",
		Subject: "Hello from Spool",
		Body:    "Your task queue is working!",
	})
	if err != nil {
		log.Fatalf("Failed to create task: %v", err)
	}
	emailTask.WithPriority(task.PriorityHigh)

	if err := q.Enqueue(ctx, emailTask); err != nil {
		log.Fatalf("Failed to enqueue task: %v", err)
	}
	fmt.Printf("Submitted task: %s\n", emailTask.ID)

	// Wait for task to complete
	time.Sleep(500 * time.Millisecond)

	// Check result
	result, err := store.GetResult(ctx, emailTask.ID)
	if err != nil {
		log.Printf("Task still processing or failed: %v", err)
	} else {
		fmt.Printf("Task completed: %+v\n", result)
	}

	fmt.Println("Done!")
}
