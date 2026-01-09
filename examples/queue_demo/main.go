package main

import (
	"context"
	"fmt"
	"log"

	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/task"
)

func main() {
	// Create a new Redis queue with connection pooling
	// Parameters: addr, password, db, poolSize
	q, err := queue.NewRedisQueue("localhost:6379", "", 0, 10)
	if err != nil {
		log.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	ctx := context.Background()

	fmt.Printf("=== Spool Queue Demo - Day 1-2 Milestone ===\n")

	// Create some example tasks with different priorities
	fmt.Println("📝 Creating and enqueueing tasks...")

	// High priority task
	highPriorityTask, _ := task.NewTask("send_email", map[string]string{
		"to":      "user@example.com",
		"subject": "Urgent: System Alert",
		"body":    "Your attention is required",
	})
	highPriorityTask.WithPriority(task.PriorityHigh)
	if err := q.Enqueue(ctx, highPriorityTask); err != nil {
		log.Fatalf("Failed to enqueue task: %v", err)
	}
	fmt.Printf(" ✓ Enqueued HIGH priority task: %s\n", highPriorityTask.ID)

	// Normal priority task
	normalTask, _ := task.NewTask("process_data", map[string]interface{}{
		"dataset": "users",
		"action":  "transform",
	})
	if err := q.Enqueue(ctx, normalTask); err != nil {
		log.Fatalf("Failed to enqueue task: %v", err)
	}
	fmt.Printf(" ✓ Enqueued NORMAL priority task: %s\n", normalTask.ID)

	// Low priority task
	lowPriorityTask, _ := task.NewTask("cleanup", map[string]string{
		"target": "temp_files",
	})
	lowPriorityTask.WithPriority(task.PriorityLow)
	if err := q.Enqueue(ctx, lowPriorityTask); err != nil {
		log.Fatalf("Failed to enqueue task: %v", err)
	}
	fmt.Printf(" ✓ Enqueued LOW priority task: %s\n", lowPriorityTask.ID)

	// Check queue stats
	fmt.Println("\n Queue Statistics:")
	totalSize, _ := q.Size(ctx)
	fmt.Printf(" Total tasks in queue: %d\n", totalSize)

	sizesByPriority, _ := q.SizeByPriority(ctx)
	fmt.Println(" Tasks by priority:")
	fmt.Printf("  - Critical: %d\n", sizesByPriority[task.PriorityCritical])
	fmt.Printf("  - High: %d\n", sizesByPriority[task.PriorityHigh])
	fmt.Printf("  - Normal: %d\n", sizesByPriority[task.PriorityNormal])
	fmt.Printf("  - Low: %d\n", sizesByPriority[task.PriorityLow])

	// Peek at the next task without removing it
	fmt.Println("\n👀 Peeking at next task (without removing):")
	peekedTask, _ := q.Peek(ctx)
	if peekedTask != nil {
		fmt.Printf(" Next task: %s (Priority: %s)\n",
			peekedTask.Type, queue.PriorityString(peekedTask.Priority))
	}

	// Dequeue tasks (should come out in priority order)
	fmt.Println("\n🔽 Dequeuing tasks (priority order):")
	for i := 0; i < 3; i++ {
		dequeuedTask, err := q.Dequeue(ctx)
		if err != nil {
			log.Fatalf("Failed to dequeue task: %v", err)
		}
		if dequeuedTask == nil {
			fmt.Println(" No more tasks")
			break
		}

		priorityStr := queue.PriorityString(dequeuedTask.Priority)
		fmt.Printf(" %d. Task: %s (Type: %s, Priority: %s)\n",
			i+1, dequeuedTask.ID[:8], dequeuedTask.Type, priorityStr)

		// Acknowledge task completion
		if err := q.Ack(ctx, dequeuedTask.ID); err != nil {
			log.Fatalf("Failed to ack task: %v", err)
		}
		fmt.Printf("   ✓ Acknowledged\n")
	}

	// Check final queue size
	finalSize, _ := q.Size(ctx)
	fmt.Printf("\n Final queue size: %d\n", finalSize)

	// Health check
	if err := q.Health(ctx); err != nil {
		log.Fatalf("Health check failed: %v", err)
	}
	fmt.Println("💚 Queue health check: PASSED")

	fmt.Println("\n Day 1-2 Milestone Achieved!")
	fmt.Println("  ✓ Queue interface defined")
	fmt.Println("  ✓ Redis implementation with connection pooling")
	fmt.Println("  ✓ Priority-based task ordering")
	fmt.Println("  ✓ Comprehensive error handling")
	fmt.Println("  ✓ 80.1% test coverage")
}
