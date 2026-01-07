package queue

import (
	"context"
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

	return q
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
