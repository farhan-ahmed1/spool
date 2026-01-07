package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/internal/worker"
	"github.com/redis/go-redis/v9"
)

// Test helpers
func setupTestRedis(t *testing.T) (*redis.Client, func()) {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15, // Use a separate DB for tests
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available, skipping test: %v", err)
	}

	// Cleanup function
	cleanup := func() {
		client.FlushDB(ctx)
		client.Close()
	}

	// Clear DB before test
	client.FlushDB(ctx)

	return client, cleanup
}

func setupTestWorker(q queue.Queue, store storage.Storage, registry *task.Registry) *worker.Worker {
	w := worker.NewWorker(q, store, registry, worker.Config{
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
	})
	return w
}

// TestRetryLogicWithExponentialBackoff tests the retry mechanism
func TestRetryLogicWithExponentialBackoff(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	// Setup queue and storage
	q, err := queue.NewRedisQueue("localhost:6379", "", 15, 10)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	store := storage.NewRedisStorage(client)

	// Create a task handler that fails the first 2 times
	attemptCount := 0
	registry := task.NewRegistry()
	registry.Register("retry_test", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		attemptCount++
		if attemptCount <= 2 {
			return nil, fmt.Errorf("intentional failure (attempt %d)", attemptCount)
		}
		return map[string]string{"status": "success"}, nil
	})

	// Create worker
	w := setupTestWorker(q, store, registry)

	// Create a task with retries
	testTask, err := task.NewTask("retry_test", map[string]string{"test": "data"})
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}
	testTask.WithMaxRetries(3)

	// Enqueue task
	if err := q.Enqueue(ctx, testTask); err != nil {
		t.Fatalf("Failed to enqueue task: %v", err)
	}

	// Start worker
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}

	// Wait for task to be processed (with retries)
	time.Sleep(15 * time.Second) // Wait for retries with exponential backoff

	// Stop worker
	if err := w.Stop(); err != nil {
		t.Fatalf("Failed to stop worker: %v", err)
	}

	// Verify task completed successfully after retries
	processed, _, retried := w.Stats()
	if processed != 1 {
		t.Errorf("Expected 1 processed task, got %d", processed)
	}
	if retried < 2 {
		t.Errorf("Expected at least 2 retries, got %d", retried)
	}

	// Verify the result was stored
	result, err := store.GetResult(ctx, testTask.ID)
	if err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected successful result, got error: %s", result.Error)
	}

	t.Logf("Task completed after %d attempts with %d retries", attemptCount, retried)
}

// TestDeadLetterQueue tests that tasks exceeding max retries go to DLQ
func TestDeadLetterQueue(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	// Setup queue and storage
	q, err := queue.NewRedisQueue("localhost:6379", "", 15, 10)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	store := storage.NewRedisStorage(client)

	// Create a task handler that always fails
	registry := task.NewRegistry()
	registry.Register("always_fail", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return nil, errors.New("persistent failure")
	})

	// Create worker
	w := setupTestWorker(q, store, registry)

	// Create a task with limited retries
	testTask, err := task.NewTask("always_fail", map[string]string{"test": "data"})
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}
	testTask.WithMaxRetries(2).WithTimeout(1 * time.Second)

	// Enqueue task
	if err := q.Enqueue(ctx, testTask); err != nil {
		t.Fatalf("Failed to enqueue task: %v", err)
	}

	// Start worker
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}

	// Wait for task to fail and move to DLQ
	time.Sleep(10 * time.Second)

	// Stop worker
	if err := w.Stop(); err != nil {
		t.Fatalf("Failed to stop worker: %v", err)
	}

	// Verify DLQ contains the task
	dlqSize, err := q.GetDLQSize(ctx)
	if err != nil {
		t.Fatalf("Failed to get DLQ size: %v", err)
	}
	if dlqSize != 1 {
		t.Errorf("Expected 1 task in DLQ, got %d", dlqSize)
	}

	// Retrieve tasks from DLQ
	dlqTasks, err := q.GetDLQTasks(ctx, 10)
	if err != nil {
		t.Fatalf("Failed to get DLQ tasks: %v", err)
	}
	if len(dlqTasks) != 1 {
		t.Fatalf("Expected 1 task in DLQ, got %d", len(dlqTasks))
	}

	dlqTask := dlqTasks[0]
	if dlqTask.ID != testTask.ID {
		t.Errorf("Expected task %s in DLQ, got %s", testTask.ID, dlqTask.ID)
	}
	if dlqTask.State != task.StateDeadLetter {
		t.Errorf("Expected task state to be %s, got %s", task.StateDeadLetter, dlqTask.State)
	}
	if dlqTask.RetryCount != 3 {
		t.Errorf("Expected retry count to be 3, got %d", dlqTask.RetryCount)
	}

	t.Logf("Task %s moved to DLQ after %d failures", dlqTask.ID, dlqTask.RetryCount)
}

// TestTaskTimeout tests that tasks timing out are handled properly
func TestTaskTimeout(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	// Setup queue and storage
	q, err := queue.NewRedisQueue("localhost:6379", "", 15, 10)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	store := storage.NewRedisStorage(client)

	// Create a task handler that takes too long
	registry := task.NewRegistry()
	registry.Register("slow_task", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		select {
		case <-time.After(5 * time.Second):
			return map[string]string{"status": "completed"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	// Create worker
	w := setupTestWorker(q, store, registry)

	// Create a task with short timeout
	testTask, err := task.NewTask("slow_task", map[string]string{"test": "data"})
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}
	testTask.WithTimeout(1 * time.Second).WithMaxRetries(1)

	// Enqueue task
	if err := q.Enqueue(ctx, testTask); err != nil {
		t.Fatalf("Failed to enqueue task: %v", err)
	}

	// Start worker
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}

	// Wait for task to timeout and retry
	time.Sleep(6 * time.Second)

	// Stop worker
	if err := w.Stop(); err != nil {
		t.Fatalf("Failed to stop worker: %v", err)
	}

	// Verify task timed out
	_, failed, retried := w.Stats()
	if failed == 0 {
		t.Error("Expected at least 1 failed task")
	}
	if retried < 1 {
		t.Error("Expected at least 1 retry after timeout")
	}

	// Verify timeout result was stored
	result, err := store.GetResult(ctx, testTask.ID)
	if err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}
	if result.Success {
		t.Error("Expected unsuccessful result for timed out task")
	}
	if result.Error == "" {
		t.Error("Expected error message in result")
	}

	t.Logf("Task timed out with error: %s", result.Error)
}

// TestTaskStateTracking tests that task states are properly tracked
func TestTaskStateTracking(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	// Setup queue and storage
	q, err := queue.NewRedisQueue("localhost:6379", "", 15, 10)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	store := storage.NewRedisStorage(client)

	// Create a task handler
	registry := task.NewRegistry()
	registry.Register("state_test", func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		time.Sleep(100 * time.Millisecond)
		return map[string]string{"status": "completed"}, nil
	})

	// Create worker
	w := setupTestWorker(q, store, registry)

	// Create a task
	testTask, err := task.NewTask("state_test", map[string]string{"test": "data"})
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Verify initial state is pending
	if testTask.State != task.StatePending {
		t.Errorf("Expected initial state to be %s, got %s", task.StatePending, testTask.State)
	}

	// Enqueue task
	if err := q.Enqueue(ctx, testTask); err != nil {
		t.Fatalf("Failed to enqueue task: %v", err)
	}

	// Start worker
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}

	// Wait a bit for processing
	time.Sleep(500 * time.Millisecond)

	// Stop worker
	if err := w.Stop(); err != nil {
		t.Fatalf("Failed to stop worker: %v", err)
	}

	// Verify final state is completed
	finalTask, err := store.GetTask(ctx, testTask.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}
	if finalTask.State != task.StateCompleted {
		t.Errorf("Expected final state to be %s, got %s", task.StateCompleted, finalTask.State)
	}

	// Verify timestamps
	if finalTask.StartedAt == nil {
		t.Error("Expected StartedAt to be set")
	}
	if finalTask.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set")
	}
	if finalTask.CompletedAt.Before(*finalTask.StartedAt) {
		t.Error("CompletedAt should be after StartedAt")
	}

	t.Logf("Task state: %s -> %s -> %s", task.StatePending, task.StateProcessing, task.StateCompleted)
}

// TestExponentialBackoffCalculation tests the backoff timing
func TestExponentialBackoffCalculation(t *testing.T) {
	testTask, _ := task.NewTask("test", map[string]string{})

	tests := []struct {
		retryCount    int
		expectedDelay time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 32 * time.Second},
		{6, 64 * time.Second},
		{7, 128 * time.Second},
		{8, 256 * time.Second}, // Will be capped at 5 minutes
	}

	for _, tt := range tests {
		testTask.RetryCount = tt.retryCount
		delay := testTask.GetRetryDelay()

		// Allow for max cap at 5 minutes
		expected := tt.expectedDelay
		if expected > 5*time.Minute {
			expected = 5 * time.Minute
		}

		if delay != expected {
			t.Errorf("Retry count %d: expected delay %v, got %v", tt.retryCount, expected, delay)
		}
	}
}

// TestResultStorage tests that task results are properly stored and retrieved
func TestResultStorage(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()
	store := storage.NewRedisStorage(client)

	// Create a test result
	testResult := &task.Result{
		TaskID:      "test-task-123",
		Success:     true,
		Output:      json.RawMessage(`{"result": "success"}`),
		CompletedAt: time.Now(),
		Duration:    500 * time.Millisecond,
	}

	// Save result
	if err := store.SaveResult(ctx, testResult); err != nil {
		t.Fatalf("Failed to save result: %v", err)
	}

	// Retrieve result
	retrieved, err := store.GetResult(ctx, testResult.TaskID)
	if err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	// Verify result
	if retrieved.TaskID != testResult.TaskID {
		t.Errorf("Expected TaskID %s, got %s", testResult.TaskID, retrieved.TaskID)
	}
	if retrieved.Success != testResult.Success {
		t.Errorf("Expected Success %v, got %v", testResult.Success, retrieved.Success)
	}
	// Compare JSON output using bytes.Equal instead of string comparison
	// to handle differences in JSON formatting (spaces, etc.)
	if len(retrieved.Output) != len(testResult.Output) {
		// If lengths differ, try to unmarshal and compare
		var retrievedData, expectedData interface{}
		if err := json.Unmarshal(retrieved.Output, &retrievedData); err != nil {
			t.Errorf("Failed to unmarshal retrieved output: %v", err)
		}
		if err := json.Unmarshal(testResult.Output, &expectedData); err != nil {
			t.Errorf("Failed to unmarshal expected output: %v", err)
		}
		// Marshal both to get consistent formatting
		retrievedJSON, _ := json.Marshal(retrievedData)
		expectedJSON, _ := json.Marshal(expectedData)
		if string(retrievedJSON) != string(expectedJSON) {
			t.Errorf("Expected Output %s, got %s", expectedJSON, retrievedJSON)
		}
	}
}
