package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestQueue creates a test queue and cleans it up
func setupTestQueue(t *testing.T) *RedisQueue {
	// Connect to Redis (assumes Redis is running on localhost:6379)
	q, err := NewRedisQueue("localhost:6379", "", 1, 5)
	require.NoError(t, err, "Failed to create test queue")

	// Purge any existing data
	ctx := context.Background()
	err = q.Purge(ctx)
	require.NoError(t, err, "Failed to purge test queue")

	// Also clean up DLQ data
	err = purgeDLQ(q, ctx)
	require.NoError(t, err, "Failed to purge DLQ")

	return q
}

// purgeDLQ cleans up the dead letter queue for testing
func purgeDLQ(q *RedisQueue, ctx context.Context) error {
	// Delete DLQ tasks
	dlqKey := dlqKeyPrefix + "tasks"
	if err := q.client.Del(ctx, dlqKey).Err(); err != nil {
		return err
	}

	// Delete DLQ metadata (using pattern matching)
	iter := q.client.Scan(ctx, 0, dlqMetaPrefix+"*", 0).Iterator()
	for iter.Next(ctx) {
		q.client.Del(ctx, iter.Val())
	}
	return iter.Err()
}

func TestNewRedisQueue(t *testing.T) {
	tests := []struct {
		name      string
		addr      string
		password  string
		db        int
		poolSize  int
		wantError bool
	}{
		{
			name:      "valid connection",
			addr:      "localhost:6379",
			password:  "",
			db:        0,
			poolSize:  5,
			wantError: false,
		},
		{
			name:      "empty address",
			addr:      "",
			password:  "",
			db:        0,
			poolSize:  5,
			wantError: true,
		},
		{
			name:      "invalid address",
			addr:      "invalid:99999",
			password:  "",
			db:        0,
			poolSize:  5,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := NewRedisQueue(tt.addr, tt.password, tt.db, tt.poolSize)
			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, q)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, q)
				if q != nil {
					defer q.Close()
				}
			}
		})
	}
}

func TestEnqueueDequeue(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Create a test task
	testTask, err := task.NewTask("test_task", map[string]interface{}{
		"message": "hello world",
	})
	require.NoError(t, err)

	// Enqueue the task
	err = q.Enqueue(ctx, testTask)
	assert.NoError(t, err)

	// Verify queue size
	size, err := q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), size)

	// Dequeue the task
	dequeuedTask, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, dequeuedTask)
	assert.Equal(t, testTask.ID, dequeuedTask.ID)
	assert.Equal(t, testTask.Type, dequeuedTask.Type)

	// Verify queue is empty
	size, err = q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestPriorityOrdering(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Create tasks with different priorities
	lowTask, _ := task.NewTask("low_task", map[string]string{"priority": "low"})
	lowTask.Priority = task.PriorityLow

	normalTask, _ := task.NewTask("normal_task", map[string]string{"priority": "normal"})
	normalTask.Priority = task.PriorityNormal

	highTask, _ := task.NewTask("high_task", map[string]string{"priority": "high"})
	highTask.Priority = task.PriorityHigh

	criticalTask, _ := task.NewTask("critical_task", map[string]string{"priority": "critical"})
	criticalTask.Priority = task.PriorityCritical

	// Enqueue in mixed order
	require.NoError(t, q.Enqueue(ctx, normalTask))
	require.NoError(t, q.Enqueue(ctx, lowTask))
	require.NoError(t, q.Enqueue(ctx, criticalTask))
	require.NoError(t, q.Enqueue(ctx, highTask))

	// Dequeue should return in priority order
	task1, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.Equal(t, criticalTask.ID, task1.ID)

	task2, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.Equal(t, highTask.ID, task2.ID)

	task3, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.Equal(t, normalTask.ID, task3.ID)

	task4, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.Equal(t, lowTask.ID, task4.ID)

	// Queue should be empty
	task5, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.Nil(t, task5)
}

func TestPeek(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Empty queue should return nil
	peekedTask, err := q.Peek(ctx)
	assert.NoError(t, err)
	assert.Nil(t, peekedTask)

	// Enqueue a task
	testTask, _ := task.NewTask("test_task", map[string]string{"data": "test"})
	require.NoError(t, q.Enqueue(ctx, testTask))

	// Peek should return the task without removing it
	peekedTask, err = q.Peek(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, peekedTask)
	assert.Equal(t, testTask.ID, peekedTask.ID)

	// Queue size should still be 1
	size, err := q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), size)

	// Peek again should return the same task
	peekedTask2, err := q.Peek(ctx)
	assert.NoError(t, err)
	assert.Equal(t, peekedTask.ID, peekedTask2.ID)
}

func TestAckNack(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Create and enqueue a task
	testTask, _ := task.NewTask("test_task", map[string]string{"data": "test"})
	require.NoError(t, q.Enqueue(ctx, testTask))

	// Dequeue the task
	dequeuedTask, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, dequeuedTask)

	// Ack the task
	err = q.Ack(ctx, dequeuedTask.ID)
	assert.NoError(t, err)

	// Task should be completely removed
	size, err := q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestNackWithRequeue(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Create and enqueue a task
	testTask, _ := task.NewTask("test_task", map[string]string{"data": "test"})
	require.NoError(t, q.Enqueue(ctx, testTask))

	// Dequeue the task
	dequeuedTask, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, dequeuedTask)

	// Queue should be empty
	size, err := q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), size)

	// Nack with requeue
	err = q.Nack(ctx, dequeuedTask.ID, true)
	assert.NoError(t, err)

	// Task should be back in queue
	size, err = q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), size)

	// Should be able to dequeue it again
	requeuedTask, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, requeuedTask)
	assert.Equal(t, testTask.ID, requeuedTask.ID)
}

func TestNackWithoutRequeue(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Create and enqueue a task
	testTask, _ := task.NewTask("test_task", map[string]string{"data": "test"})
	require.NoError(t, q.Enqueue(ctx, testTask))

	// Dequeue the task
	dequeuedTask, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, dequeuedTask)

	// Nack without requeue
	err = q.Nack(ctx, dequeuedTask.ID, false)
	assert.NoError(t, err)

	// Queue should be empty
	size, err := q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestSizeByPriority(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Create tasks with different priorities
	for i := 0; i < 5; i++ {
		lowTask, _ := task.NewTask("low_task", map[string]int{"index": i})
		lowTask.Priority = task.PriorityLow
		require.NoError(t, q.Enqueue(ctx, lowTask))
	}

	for i := 0; i < 3; i++ {
		normalTask, _ := task.NewTask("normal_task", map[string]int{"index": i})
		normalTask.Priority = task.PriorityNormal
		require.NoError(t, q.Enqueue(ctx, normalTask))
	}

	for i := 0; i < 2; i++ {
		highTask, _ := task.NewTask("high_task", map[string]int{"index": i})
		highTask.Priority = task.PriorityHigh
		require.NoError(t, q.Enqueue(ctx, highTask))
	}

	// Check size by priority
	sizes, err := q.SizeByPriority(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(5), sizes[task.PriorityLow])
	assert.Equal(t, int64(3), sizes[task.PriorityNormal])
	assert.Equal(t, int64(2), sizes[task.PriorityHigh])
	assert.Equal(t, int64(0), sizes[task.PriorityCritical])

	// Total size should be 10
	totalSize, err := q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), totalSize)
}

func TestPurge(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Enqueue multiple tasks
	for i := 0; i < 10; i++ {
		testTask, _ := task.NewTask("test_task", map[string]int{"index": i})
		require.NoError(t, q.Enqueue(ctx, testTask))
	}

	// Verify tasks are in queue
	size, err := q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), size)

	// Purge the queue
	err = q.Purge(ctx)
	assert.NoError(t, err)

	// Queue should be empty
	size, err = q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestHealth(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Health check should pass
	err := q.Health(ctx)
	assert.NoError(t, err)
}

func TestEnqueueNilTask(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Enqueuing nil task should error
	err := q.Enqueue(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task cannot be nil")
}

func TestAckEmptyTaskID(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Ack with empty task ID should error
	err := q.Ack(ctx, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task ID cannot be empty")
}

func TestConcurrentEnqueueDequeue(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Concurrently enqueue tasks
	numTasks := 100
	doneChan := make(chan bool, numTasks)

	for i := 0; i < numTasks; i++ {
		go func(index int) {
			testTask, _ := task.NewTask("concurrent_task", map[string]int{"index": index})
			err := q.Enqueue(ctx, testTask)
			assert.NoError(t, err)
			doneChan <- true
		}(i)
	}

	// Wait for all enqueues to complete
	for i := 0; i < numTasks; i++ {
		<-doneChan
	}

	// Verify all tasks were enqueued
	size, err := q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(numTasks), size)

	// Concurrently dequeue tasks
	dequeueCount := 0
	for i := 0; i < numTasks; i++ {
		go func() {
			_, err := q.Dequeue(ctx)
			if err == nil {
				doneChan <- true
			}
		}()
	}

	// Count successful dequeues (with timeout)
	timeout := time.After(5 * time.Second)
	for dequeueCount < numTasks {
		select {
		case <-doneChan:
			dequeueCount++
		case <-timeout:
			t.Fatalf("Timeout waiting for dequeues. Got %d of %d", dequeueCount, numTasks)
		}
	}

	assert.Equal(t, numTasks, dequeueCount)

	// Queue should be empty
	size, err = q.Size(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

// ============================================================
// Dead Letter Queue (DLQ) Tests
// ============================================================

func TestEnqueueDLQ(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Create a test task
	testTask, err := task.NewTask("dlq_test_task", map[string]interface{}{
		"message": "failed task",
	})
	require.NoError(t, err)
	testTask.RetryCount = 3
	testTask.MaxRetries = 3

	// Enqueue to DLQ with a reason
	err = q.EnqueueDLQ(ctx, testTask, "Max retries exceeded")
	assert.NoError(t, err)

	// Verify task state was updated
	assert.Equal(t, task.StateDeadLetter, testTask.State)
	assert.Equal(t, "Max retries exceeded", testTask.Error)

	// Verify DLQ size increased
	dlqSize, err := q.GetDLQSize(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), dlqSize)
}

func TestEnqueueDLQNilTask(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Enqueue nil task should error
	err := q.EnqueueDLQ(ctx, nil, "test reason")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task cannot be nil")
}

func TestEnqueueDLQEmptyReason(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Create a test task
	testTask, err := task.NewTask("dlq_test_task", map[string]interface{}{
		"message": "failed task",
	})
	require.NoError(t, err)
	testTask.Error = "original error"

	// Enqueue to DLQ without a reason - should keep original error
	err = q.EnqueueDLQ(ctx, testTask, "")
	assert.NoError(t, err)

	// Original error should be preserved since reason was empty
	assert.Equal(t, task.StateDeadLetter, testTask.State)
	assert.Equal(t, "original error", testTask.Error)
}

func TestEnqueueDLQMultipleTasks(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Enqueue multiple tasks to DLQ
	for i := 0; i < 5; i++ {
		testTask, err := task.NewTask("dlq_test_task", map[string]int{"index": i})
		require.NoError(t, err)

		err = q.EnqueueDLQ(ctx, testTask, fmt.Sprintf("Error %d", i))
		assert.NoError(t, err)
	}

	// Verify DLQ size
	dlqSize, err := q.GetDLQSize(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(5), dlqSize)
}

func TestGetDLQSize(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Initially DLQ should be empty
	dlqSize, err := q.GetDLQSize(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), dlqSize)

	// Add tasks to DLQ
	for i := 0; i < 3; i++ {
		testTask, _ := task.NewTask("dlq_task", map[string]int{"i": i})
		err := q.EnqueueDLQ(ctx, testTask, "test reason")
		require.NoError(t, err)
	}

	// Verify size
	dlqSize, err = q.GetDLQSize(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(3), dlqSize)
}

func TestGetDLQTasks(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Create and add tasks to DLQ
	taskIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		testTask, err := task.NewTask("dlq_task", map[string]int{"index": i})
		require.NoError(t, err)
		taskIDs[i] = testTask.ID

		err = q.EnqueueDLQ(ctx, testTask, fmt.Sprintf("Error reason %d", i))
		require.NoError(t, err)
	}

	// Get all DLQ tasks
	tasks, err := q.GetDLQTasks(ctx, 0)
	assert.NoError(t, err)
	assert.Len(t, tasks, 5)

	// Verify tasks are in dead letter state
	for _, dlqTask := range tasks {
		assert.Equal(t, task.StateDeadLetter, dlqTask.State)
	}
}

func TestGetDLQTasksWithLimit(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Add 10 tasks to DLQ
	for i := 0; i < 10; i++ {
		testTask, _ := task.NewTask("dlq_task", map[string]int{"index": i})
		err := q.EnqueueDLQ(ctx, testTask, "error")
		require.NoError(t, err)
	}

	// Get only first 3 tasks
	tasks, err := q.GetDLQTasks(ctx, 3)
	assert.NoError(t, err)
	assert.Len(t, tasks, 3)

	// Get only first 5 tasks
	tasks, err = q.GetDLQTasks(ctx, 5)
	assert.NoError(t, err)
	assert.Len(t, tasks, 5)
}

func TestGetDLQTasksEmpty(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Get tasks from empty DLQ
	tasks, err := q.GetDLQTasks(ctx, 0)
	assert.NoError(t, err)
	assert.Empty(t, tasks)
}

// ============================================================
// Enqueue Edge Cases
// ============================================================

func TestEnqueueWithCancelledContext(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	testTask, _ := task.NewTask("test_task", map[string]string{"key": "value"})

	// Enqueue should still work (uses fallback timeout)
	err := q.Enqueue(ctx, testTask)
	assert.NoError(t, err)

	// Verify task was enqueued
	size, err := q.Size(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, int64(1), size)
}

func TestEnqueueAllPriorityLevels(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	priorities := []task.Priority{
		task.PriorityLow,
		task.PriorityNormal,
		task.PriorityHigh,
		task.PriorityCritical,
	}

	// Enqueue tasks at all priority levels
	for _, priority := range priorities {
		testTask, _ := task.NewTask("priority_task", map[string]int{
			"priority": int(priority),
		})
		testTask.Priority = priority

		err := q.Enqueue(ctx, testTask)
		assert.NoError(t, err)
	}

	// Verify sizes by priority
	sizes, err := q.SizeByPriority(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), sizes[task.PriorityLow])
	assert.Equal(t, int64(1), sizes[task.PriorityNormal])
	assert.Equal(t, int64(1), sizes[task.PriorityHigh])
	assert.Equal(t, int64(1), sizes[task.PriorityCritical])
}

func TestEnqueueLargePayload(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Create a task with a large payload
	largePayload := make(map[string]interface{})
	for i := 0; i < 100; i++ {
		largePayload[fmt.Sprintf("key_%d", i)] = fmt.Sprintf("value_%d with some extra text to make it larger", i)
	}

	testTask, err := task.NewTask("large_payload_task", largePayload)
	require.NoError(t, err)

	// Enqueue should work
	err = q.Enqueue(ctx, testTask)
	assert.NoError(t, err)

	// Dequeue and verify payload integrity
	dequeuedTask, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, dequeuedTask)
	assert.Equal(t, testTask.ID, dequeuedTask.ID)
}

// ============================================================
// Dequeue Edge Cases
// ============================================================

func TestDequeueWithCancelledContext(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	// Enqueue a task first
	ctx := context.Background()
	testTask, _ := task.NewTask("test_task", map[string]string{"key": "value"})
	err := q.Enqueue(ctx, testTask)
	require.NoError(t, err)

	// Create a cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	// Dequeue should still work (uses fallback timeout)
	dequeuedTask, err := q.Dequeue(cancelledCtx)
	assert.NoError(t, err)
	assert.NotNil(t, dequeuedTask)
	assert.Equal(t, testTask.ID, dequeuedTask.ID)
}

func TestDequeueEmptyQueue(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Dequeue from empty queue should return nil
	dequeuedTask, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.Nil(t, dequeuedTask)
}

func TestDequeueMultipleFromEmptyQueue(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Multiple dequeues from empty queue should all return nil
	for i := 0; i < 5; i++ {
		dequeuedTask, err := q.Dequeue(ctx)
		assert.NoError(t, err)
		assert.Nil(t, dequeuedTask)
	}
}

func TestDequeuePreservesFIFOWithinPriority(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Enqueue multiple tasks with same priority
	taskIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		testTask, _ := task.NewTask("fifo_task", map[string]int{"order": i})
		testTask.Priority = task.PriorityNormal
		taskIDs[i] = testTask.ID
		err := q.Enqueue(ctx, testTask)
		require.NoError(t, err)
	}

	// Dequeue should return in FIFO order
	for i := 0; i < 5; i++ {
		dequeuedTask, err := q.Dequeue(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, dequeuedTask)
		assert.Equal(t, taskIDs[i], dequeuedTask.ID, "Task %d should be dequeued in order", i)
	}
}

func TestDequeueWithMixedOperations(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Enqueue, dequeue, enqueue pattern
	task1, _ := task.NewTask("task1", nil)
	task2, _ := task.NewTask("task2", nil)
	task3, _ := task.NewTask("task3", nil)

	err := q.Enqueue(ctx, task1)
	require.NoError(t, err)

	err = q.Enqueue(ctx, task2)
	require.NoError(t, err)

	// Dequeue first task
	dequeued, err := q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.Equal(t, task1.ID, dequeued.ID)

	// Enqueue another
	err = q.Enqueue(ctx, task3)
	require.NoError(t, err)

	// Continue dequeuing
	dequeued, err = q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.Equal(t, task2.ID, dequeued.ID)

	dequeued, err = q.Dequeue(ctx)
	assert.NoError(t, err)
	assert.Equal(t, task3.ID, dequeued.ID)
}

// ============================================================
// Nack Edge Cases
// ============================================================

func TestNackEmptyTaskID(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Nack with empty task ID should error
	err := q.Nack(ctx, "", true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task ID cannot be empty")

	err = q.Nack(ctx, "", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task ID cannot be empty")
}

func TestNackNonExistentTask(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Nack with requeue for non-existent task should error
	err := q.Nack(ctx, "non-existent-task-id", true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve task")
}

func TestNackWithoutRequeueNonExistentTask(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// Nack without requeue for non-existent task should succeed (just updates stats)
	err := q.Nack(ctx, "non-existent-task-id", false)
	assert.NoError(t, err)
}

// ============================================================
// Complete DLQ Workflow Test
// ============================================================

func TestDLQCompleteWorkflow(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	// 1. Create and enqueue a task
	testTask, err := task.NewTask("workflow_task", map[string]string{
		"step": "initial",
	})
	require.NoError(t, err)
	testTask.MaxRetries = 2

	err = q.Enqueue(ctx, testTask)
	require.NoError(t, err)

	// 2. Dequeue the task
	dequeuedTask, err := q.Dequeue(ctx)
	require.NoError(t, err)
	require.NotNil(t, dequeuedTask)

	// 3. Simulate failure and requeue
	dequeuedTask.RetryCount = 1
	err = q.Nack(ctx, dequeuedTask.ID, true)
	require.NoError(t, err)

	// 4. Dequeue again
	dequeuedTask, err = q.Dequeue(ctx)
	require.NoError(t, err)
	require.NotNil(t, dequeuedTask)

	// 5. Simulate another failure - this time move to DLQ
	dequeuedTask.RetryCount = 2
	err = q.EnqueueDLQ(ctx, dequeuedTask, "Max retries exceeded after 2 attempts")
	require.NoError(t, err)

	// 6. Verify DLQ contains the task
	dlqTasks, err := q.GetDLQTasks(ctx, 10)
	require.NoError(t, err)
	require.Len(t, dlqTasks, 1)
	assert.Equal(t, dequeuedTask.ID, dlqTasks[0].ID)
	assert.Equal(t, task.StateDeadLetter, dlqTasks[0].State)
	assert.Equal(t, "Max retries exceeded after 2 attempts", dlqTasks[0].Error)

	// 7. Verify main queue is empty
	mainSize, err := q.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), mainSize)
}

// ============================================================
// Stress Tests
// ============================================================

func TestDLQConcurrentEnqueue(t *testing.T) {
	q := setupTestQueue(t)
	defer q.Close()

	ctx := context.Background()

	numTasks := 50
	doneChan := make(chan bool, numTasks)

	// Concurrently enqueue tasks to DLQ
	for i := 0; i < numTasks; i++ {
		go func(index int) {
			testTask, _ := task.NewTask("concurrent_dlq_task", map[string]int{"index": index})
			err := q.EnqueueDLQ(ctx, testTask, fmt.Sprintf("Error %d", index))
			assert.NoError(t, err)
			doneChan <- true
		}(i)
	}

	// Wait for all enqueues to complete
	for i := 0; i < numTasks; i++ {
		<-doneChan
	}

	// Verify all tasks were enqueued to DLQ
	dlqSize, err := q.GetDLQSize(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(numTasks), dlqSize)
}
