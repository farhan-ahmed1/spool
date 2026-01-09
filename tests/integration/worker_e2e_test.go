package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/internal/worker"
)

// E2E test that demonstrates complete workflow:
// 1. Create a Redis queue
// 2. Register task handlers
// 3. Start a worker
// 4. Enqueue tasks
// 5. Wait for processing
// 6. Verify results

func TestE2E_WorkerExecutesTasks(t *testing.T) {
	ctx := context.Background()

	// Setup components
	q := setupRedisQueue(t, ctx)
	defer q.Close()

	registry, executed, executedCh := setupTaskHandlers(t)

	// Create and start worker
	w := setupWorker(t, ctx, q, registry)
	defer stopE2EWorker(t, w)

	// Enqueue test tasks
	enqueueTestTasks(t, ctx, q)

	// Wait for execution and verify results
	verifyTaskExecution(t, executed, executedCh, w, q)

	t.Log("E2E test completed successfully!")
}

func TestE2E_WorkerHandlesFailures(t *testing.T) {
	ctx := context.Background()

	// Create a Redis queue
	q, err := queue.NewRedisQueue("localhost:6379", "", 1, 10)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer q.Close()

	// Clean up any leftover tasks from previous test runs
	if err := q.Purge(ctx); err != nil {
		t.Logf("Warning: failed to purge queue: %v", err)
	}

	// Create handler registry
	registry := task.NewRegistry()

	// Register a handler that fails
	failCount := 0
	err = registry.Register("failing_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		failCount++
		return nil, fmt.Errorf("intentional failure #%d", failCount)
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	// Create and start worker
	w := worker.NewWorker(q, nil, registry, worker.Config{
		ID:           "e2e-failure-worker",
		PollInterval: 50 * time.Millisecond,
	})

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}
	defer func() {
		if err := w.Stop(); err != nil {
			t.Logf("failed to stop worker: %v", err)
		}
	}()

	// Enqueue a task that will fail
	failingTask, err := task.NewTask("failing_task", map[string]string{"test": "data"})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	if err := q.Enqueue(ctx, failingTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	t.Log("Task enqueued, waiting for failure...")

	// Wait for task to be processed
	time.Sleep(300 * time.Millisecond)

	// Check worker stats
	processed, failed, _ := w.Stats()
	t.Logf("Worker stats: processed=%d, failed=%d", processed, failed)

	if failed != 1 {
		t.Errorf("expected 1 failed task, got %d", failed)
	}

	if processed != 0 {
		t.Errorf("expected 0 processed tasks, got %d", processed)
	}

	if failCount == 0 {
		t.Error("handler was never called")
	}

	t.Log("Failure handling test completed successfully!")
}

func TestE2E_WorkerTimeout(t *testing.T) {
	ctx := context.Background()

	// Create a Redis queue
	q, err := queue.NewRedisQueue("localhost:6379", "", 1, 10)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer q.Close()

	// Clean up any leftover tasks from previous test runs
	if err := q.Purge(ctx); err != nil {
		t.Logf("Warning: failed to purge queue: %v", err)
	}

	registry := task.NewRegistry()

	// Register a slow handler
	err = registry.Register("slow_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		select {
		case <-time.After(5 * time.Second):
			return "completed", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if err != nil {
		t.Fatalf("failed to register handler: %v", err)
	}

	w := worker.NewWorker(q, nil, registry, worker.Config{
		ID:           "e2e-timeout-worker",
		PollInterval: 50 * time.Millisecond,
	})

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}
	defer func() {
		if err := w.Stop(); err != nil {
			t.Logf("failed to stop worker: %v", err)
		}
	}()

	// Create a task with short timeout
	slowTask, err := task.NewTask("slow_task", map[string]string{"test": "data"})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	slowTask.Timeout = 100 * time.Millisecond // Will timeout

	if err := q.Enqueue(ctx, slowTask); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	t.Log("Slow task enqueued, waiting for timeout...")

	// Wait for task to timeout
	time.Sleep(500 * time.Millisecond)

	// Check worker stats
	processed, failed, _ := w.Stats()
	t.Logf("Worker stats: processed=%d, failed=%d", processed, failed)

	if failed != 1 {
		t.Errorf("expected 1 failed task due to timeout, got %d", failed)
	}

	t.Log("Timeout test completed successfully!")
}

// Helper functions for TestE2E_WorkerExecutesTasks

func setupRedisQueue(t *testing.T, ctx context.Context) *queue.RedisQueue {
	q, err := queue.NewRedisQueue("localhost:6379", "", 1, 10)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	if err := q.Purge(ctx); err != nil {
		t.Logf("Warning: failed to purge queue: %v", err)
	}

	return q
}

func setupTaskHandlers(t *testing.T) (*task.Registry, map[string]bool, chan string) {
	registry := task.NewRegistry()
	executed := make(map[string]bool)
	executedCh := make(chan string, 10)

	// Register "email" handler
	err := registry.Register("send_email", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var data map[string]string
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}

		time.Sleep(50 * time.Millisecond)

		result := fmt.Sprintf("Email sent to %s", data["to"])
		executedCh <- "send_email"
		return result, nil
	})
	if err != nil {
		t.Fatalf("failed to register email handler: %v", err)
	}

	// Register "notification" handler
	err = registry.Register("send_notification", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var data map[string]string
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}

		time.Sleep(30 * time.Millisecond)

		result := fmt.Sprintf("Notification sent: %s", data["message"])
		executedCh <- "send_notification"
		return result, nil
	})
	if err != nil {
		t.Fatalf("failed to register notification handler: %v", err)
	}

	// Register "process_image" handler
	err = registry.Register("process_image", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		var data map[string]string
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}

		time.Sleep(100 * time.Millisecond)

		result := fmt.Sprintf("Processed image: %s", data["url"])
		executedCh <- "process_image"
		return result, nil
	})
	if err != nil {
		t.Fatalf("failed to register image handler: %v", err)
	}

	return registry, executed, executedCh
}

func setupWorker(t *testing.T, ctx context.Context, q *queue.RedisQueue, registry *task.Registry) *worker.Worker {
	w := worker.NewWorker(q, nil, registry, worker.Config{
		ID:           "e2e-test-worker",
		PollInterval: 50 * time.Millisecond,
	})

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	return w
}

func stopE2EWorker(t *testing.T, w *worker.Worker) {
	if err := w.Stop(); err != nil {
		t.Logf("failed to stop worker: %v", err)
	}
}

func enqueueTestTasks(t *testing.T, ctx context.Context, q *queue.RedisQueue) {
	t.Log("Enqueuing tasks...")

	// Task 1: Send email (high priority)
	emailTask, err := task.NewTask("send_email", map[string]string{
		"to":      "user@example.com",
		"subject": "Welcome!",
	})
	if err != nil {
		t.Fatalf("failed to create email task: %v", err)
	}
	emailTask.WithPriority(task.PriorityHigh)

	if err := q.Enqueue(ctx, emailTask); err != nil {
		t.Fatalf("failed to enqueue email task: %v", err)
	}

	// Task 2: Send notification (normal priority)
	notifTask, err := task.NewTask("send_notification", map[string]string{
		"message": "Your account has been created",
		"user_id": "12345",
	})
	if err != nil {
		t.Fatalf("failed to create notification task: %v", err)
	}

	if err := q.Enqueue(ctx, notifTask); err != nil {
		t.Fatalf("failed to enqueue notification task: %v", err)
	}

	// Task 3: Process image (low priority)
	imageTask, err := task.NewTask("process_image", map[string]string{
		"url":    "https://example.com/image.jpg",
		"action": "resize",
	})
	if err != nil {
		t.Fatalf("failed to create image task: %v", err)
	}
	imageTask.WithPriority(task.PriorityLow)

	if err := q.Enqueue(ctx, imageTask); err != nil {
		t.Fatalf("failed to enqueue image task: %v", err)
	}

	t.Log("Tasks enqueued, waiting for processing...")
}

func verifyTaskExecution(t *testing.T, executed map[string]bool, executedCh chan string, w *worker.Worker, q *queue.RedisQueue) {
	// Wait for all tasks to be executed
	timeout := time.After(5 * time.Second)
	expectedTasks := 3

	for i := 0; i < expectedTasks; i++ {
		select {
		case taskType := <-executedCh:
			executed[taskType] = true
			t.Logf("Task executed: %s", taskType)
		case <-timeout:
			t.Fatalf("timeout waiting for tasks to execute (executed %d/%d)", i, expectedTasks)
		}
	}

	// Verify all tasks were executed
	expectedTypes := []string{"send_email", "send_notification", "process_image"}
	for _, taskType := range expectedTypes {
		if !executed[taskType] {
			t.Errorf("task type %s was not executed", taskType)
		}
	}

	// Check worker stats
	processed, failed, _ := w.Stats()
	t.Logf("Worker stats: processed=%d, failed=%d", processed, failed)

	if processed != int64(expectedTasks) {
		t.Errorf("expected %d processed tasks, got %d", expectedTasks, processed)
	}

	if failed != 0 {
		t.Errorf("expected 0 failed tasks, got %d", failed)
	}

	// Verify queue is empty
	ctx := context.Background()
	size, err := q.Size(ctx)
	if err != nil {
		t.Fatalf("failed to get queue size: %v", err)
	}

	if size != 0 {
		t.Errorf("expected empty queue, got size %d", size)
	}
}
