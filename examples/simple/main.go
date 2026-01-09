package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/pkg/client"
)

// Example payload structures
type EmailPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type ImagePayload struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func main() {
	// Initialize client
	c, err := client.New(client.Config{
		BrokerAddr: "localhost:6379",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	// Example 1: Submit a simple task
	emailTask, err := task.NewTask("send_email", EmailPayload{
		To:      "user@example.com",
		Subject: "Hello from Spool",
		Body:    "Your task queue is working!",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Submit with high priority
	taskID, err := c.Submit(context.Background(), emailTask.WithPriority(task.PriorityHigh))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf(" Submitted email task: %s\n", taskID)

	// Example 2: Submit with retry configuration
	imageTask, err := task.NewTask("resize_image", ImagePayload{
		URL:    "https://example.com/image.jpg",
		Width:  800,
		Height: 600,
	})
	if err != nil {
		log.Fatal(err)
	}

	taskID, err = c.Submit(
		context.Background(),
		imageTask.
			WithMaxRetries(5).
			WithTimeout(5*time.Minute).
			WithPriority(task.PriorityNormal),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf(" Submitted image task: %s\n", taskID)

	// Example 3: Submit a scheduled task
	scheduledTask, err := task.NewTask("cleanup", map[string]string{
		"target": "old_files",
	})
	if err != nil {
		log.Fatal(err)
	}

	futureTime := time.Now().Add(1 * time.Hour)
	taskID, err = c.Submit(
		context.Background(),
		scheduledTask.WithScheduleAt(futureTime),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf(" Scheduled cleanup task for %s: %s\n", futureTime.Format(time.RFC3339), taskID)

	// Example 4: Wait for task result
	fmt.Println("\n⏳ Waiting for email task result...")
	result, err := c.GetResult(context.Background(), emailTask.ID, 10*time.Second)
	if err != nil {
		log.Printf(" Failed to get result: %v\n", err)
	} else if result.Success {
		fmt.Printf(" Task completed successfully!\n")
		var output map[string]interface{}
		json.Unmarshal(result.Output, &output)
		fmt.Printf("  Output: %+v\n", output)
	} else {
		fmt.Printf(" Task failed: %s\n", result.Error)
	}

	// Example 5: Get task status
	status, err := c.GetStatus(context.Background(), imageTask.ID)
	if err != nil {
		log.Printf(" Failed to get status: %v\n", err)
	} else {
		fmt.Printf("\n Image task status: %s\n", status.State)
		fmt.Printf("  Retry count: %d/%d\n", status.RetryCount, status.MaxRetries)
	}

	// Example 6: Submit batch of tasks
	fmt.Println("\n📦 Submitting batch of tasks...")
	var batchTasks []*task.Task
	for i := 0; i < 10; i++ {
		t, _ := task.NewTask("process_item", map[string]interface{}{
			"item_id": i,
			"action":  "process",
		})
		batchTasks = append(batchTasks, t)
	}

	taskIDs, err := c.SubmitBatch(context.Background(), batchTasks)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf(" Submitted %d tasks\n", len(taskIDs))

	// Example 7: Get queue statistics
	stats, err := c.GetStats(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n Queue Statistics:\n")
	fmt.Printf("  Pending: %d\n", stats.Pending)
	fmt.Printf("  Processing: %d\n", stats.Processing)
	fmt.Printf("  Completed: %d\n", stats.Completed)
	fmt.Printf("  Failed: %d\n", stats.Failed)
	fmt.Printf("  Workers: %d\n", stats.ActiveWorkers)
}
