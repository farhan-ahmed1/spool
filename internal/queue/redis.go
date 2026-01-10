package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/redis/go-redis/v9"
)

const (
	// Redis key prefixes
	queueKeyPrefix      = "spool:queue:"
	processingKeyPrefix = "spool:processing:"
	taskKeyPrefix       = "spool:task:"
	statsKeyPrefix      = "spool:stats:"
	dlqKeyPrefix        = "spool:dlq:"
	dlqMetaPrefix       = "spool:dlq:meta:"

	// Default timeouts
	defaultEnqueueTimeout = 5 * time.Second
	defaultDequeueTimeout = 5 * time.Second
)

// RedisQueue implements Queue interface using Redis
type RedisQueue struct {
	client *redis.Client
	// Separate queues for each priority level
	queueKeys map[task.Priority]string
}

// NewRedisQueue creates a new Redis-backed queue with connection pooling
func NewRedisQueue(addr, password string, db, poolSize int) (*RedisQueue, error) {
	if addr == "" {
		return nil, fmt.Errorf("redis address cannot be empty")
	}

	// Create Redis client with connection pooling
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     poolSize,
		MinIdleConns: poolSize / 2,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	// Initialize queue keys for each priority
	queueKeys := make(map[task.Priority]string)
	queueKeys[task.PriorityCritical] = queueKeyPrefix + "critical"
	queueKeys[task.PriorityHigh] = queueKeyPrefix + "high"
	queueKeys[task.PriorityNormal] = queueKeyPrefix + "normal"
	queueKeys[task.PriorityLow] = queueKeyPrefix + "low"

	return &RedisQueue{
		client:    client,
		queueKeys: queueKeys,
	}, nil
}

// Enqueue adds a task to the appropriate priority queue
func (rq *RedisQueue) Enqueue(ctx context.Context, t *task.Task) error {
	if t == nil {
		return fmt.Errorf("task cannot be nil")
	}

	// Set timeout if not provided
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultEnqueueTimeout)
		defer cancel()
	}

	// Serialize task to JSON
	taskData, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// Store task data
	taskKey := taskKeyPrefix + t.ID
	if err := rq.client.Set(ctx, taskKey, taskData, 0).Err(); err != nil {
		return fmt.Errorf("failed to store task: %w", err)
	}

	// Add task ID to the appropriate priority queue
	queueKey := rq.queueKeys[t.Priority]
	if err := rq.client.RPush(ctx, queueKey, t.ID).Err(); err != nil {
		// Cleanup task data if queue push fails
		rq.client.Del(ctx, taskKey)
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	// Update stats
	rq.client.Incr(ctx, statsKeyPrefix+"total")
	rq.client.Set(ctx, statsKeyPrefix+"last_enqueue", time.Now().Unix(), 0)

	return nil
}

// Dequeue retrieves the highest priority task from the queue
func (rq *RedisQueue) Dequeue(ctx context.Context) (*task.Task, error) {
	// Set timeout if not provided
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultDequeueTimeout)
		defer cancel()
	}

	// Try each priority queue from highest to lowest
	priorities := []task.Priority{
		task.PriorityCritical,
		task.PriorityHigh,
		task.PriorityNormal,
		task.PriorityLow,
	}

	for _, priority := range priorities {
		queueKey := rq.queueKeys[priority]

		// Pop task ID from queue
		taskID, err := rq.client.LPop(ctx, queueKey).Result()
		if err == redis.Nil {
			// Queue is empty, try next priority
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to dequeue task: %w", err)
		}

		// Retrieve task data
		taskKey := taskKeyPrefix + taskID
		taskData, err := rq.client.Get(ctx, taskKey).Result()
		if err == redis.Nil {
			// Task data not found, skip this task
			continue
		}
		if err != nil {
			// Re-queue the task ID on error
			rq.client.LPush(ctx, queueKey, taskID)
			return nil, fmt.Errorf("failed to retrieve task data: %w", err)
		}

		// Deserialize task
		var t task.Task
		if err := json.Unmarshal([]byte(taskData), &t); err != nil {
			// Delete corrupted task data
			rq.client.Del(ctx, taskKey)
			return nil, fmt.Errorf("failed to unmarshal task: %w", err)
		}

		// Move task to processing set (for tracking)
		processingKey := processingKeyPrefix + taskID
		rq.client.Set(ctx, processingKey, time.Now().Unix(), 1*time.Hour)

		// Update stats
		rq.client.Set(ctx, statsKeyPrefix+"last_dequeue", time.Now().Unix(), 0)

		return &t, nil
	}

	// No tasks available in any queue
	return nil, nil
}

// Peek returns the next task without removing it from the queue
func (rq *RedisQueue) Peek(ctx context.Context) (*task.Task, error) {
	priorities := []task.Priority{
		task.PriorityCritical,
		task.PriorityHigh,
		task.PriorityNormal,
		task.PriorityLow,
	}

	for _, priority := range priorities {
		queueKey := rq.queueKeys[priority]

		// Get first task ID without removing
		taskID, err := rq.client.LIndex(ctx, queueKey, 0).Result()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to peek task: %w", err)
		}

		// Retrieve task data
		taskKey := taskKeyPrefix + taskID
		taskData, err := rq.client.Get(ctx, taskKey).Result()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve task data: %w", err)
		}

		// Deserialize task
		var t task.Task
		if err := json.Unmarshal([]byte(taskData), &t); err != nil {
			return nil, fmt.Errorf("failed to unmarshal task: %w", err)
		}

		return &t, nil
	}

	return nil, nil
}

// Ack acknowledges successful task processing
func (rq *RedisQueue) Ack(ctx context.Context, taskID string) error {
	if taskID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}

	// Remove from processing
	processingKey := processingKeyPrefix + taskID
	rq.client.Del(ctx, processingKey)

	// Remove task data
	taskKey := taskKeyPrefix + taskID
	rq.client.Del(ctx, taskKey)

	// Update stats
	rq.client.Incr(ctx, statsKeyPrefix+"completed")

	return nil
}

// Nack marks task as failed and optionally re-queues it
func (rq *RedisQueue) Nack(ctx context.Context, taskID string, requeue bool) error {
	if taskID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}

	// Remove from processing
	processingKey := processingKeyPrefix + taskID
	rq.client.Del(ctx, processingKey)

	if !requeue {
		// Remove task data
		taskKey := taskKeyPrefix + taskID
		rq.client.Del(ctx, taskKey)

		// Update stats
		rq.client.Incr(ctx, statsKeyPrefix+"failed")
		return nil
	}

	// Retrieve task to re-queue
	taskKey := taskKeyPrefix + taskID
	taskData, err := rq.client.Get(ctx, taskKey).Result()
	if err != nil {
		return fmt.Errorf("failed to retrieve task for requeue: %w", err)
	}

	var t task.Task
	if err := json.Unmarshal([]byte(taskData), &t); err != nil {
		return fmt.Errorf("failed to unmarshal task: %w", err)
	}

	// Re-queue the task
	queueKey := rq.queueKeys[t.Priority]
	if err := rq.client.RPush(ctx, queueKey, taskID).Err(); err != nil {
		return fmt.Errorf("failed to requeue task: %w", err)
	}

	return nil
}

// Size returns the total number of tasks in all queues
func (rq *RedisQueue) Size(ctx context.Context) (int64, error) {
	var total int64

	for _, queueKey := range rq.queueKeys {
		size, err := rq.client.LLen(ctx, queueKey).Result()
		if err != nil {
			return 0, fmt.Errorf("failed to get queue size: %w", err)
		}
		total += size
	}

	return total, nil
}

// SizeByPriority returns the number of tasks for each priority level
func (rq *RedisQueue) SizeByPriority(ctx context.Context) (map[task.Priority]int64, error) {
	sizes := make(map[task.Priority]int64)

	for priority, queueKey := range rq.queueKeys {
		size, err := rq.client.LLen(ctx, queueKey).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get queue size for priority %d: %w", priority, err)
		}
		sizes[priority] = size
	}

	return sizes, nil
}

// Purge removes all tasks from all queues
func (rq *RedisQueue) Purge(ctx context.Context) error {
	// Delete all queue keys
	for _, queueKey := range rq.queueKeys {
		if err := rq.client.Del(ctx, queueKey).Err(); err != nil {
			return fmt.Errorf("failed to purge queue: %w", err)
		}
	}

	// Delete all task data (using pattern matching)
	iter := rq.client.Scan(ctx, 0, taskKeyPrefix+"*", 0).Iterator()
	for iter.Next(ctx) {
		rq.client.Del(ctx, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to delete task data: %w", err)
	}

	// Delete all processing keys
	iter = rq.client.Scan(ctx, 0, processingKeyPrefix+"*", 0).Iterator()
	for iter.Next(ctx) {
		rq.client.Del(ctx, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to delete processing data: %w", err)
	}

	return nil
}

// Close closes the Redis connection
func (rq *RedisQueue) Close() error {
	if rq.client != nil {
		return rq.client.Close()
	}
	return nil
}

// Health checks if the queue is healthy
func (rq *RedisQueue) Health(ctx context.Context) error {
	return rq.client.Ping(ctx).Err()
}

// EnqueueDLQ moves a task to the dead letter queue
func (rq *RedisQueue) EnqueueDLQ(ctx context.Context, t *task.Task, reason string) error {
	if t == nil {
		return fmt.Errorf("task cannot be nil")
	}

	// Update task state
	t.State = task.StateDeadLetter
	if reason != "" {
		t.Error = reason
	}

	// Serialize task
	taskData, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("failed to marshal task for DLQ: %w", err)
	}

	// Store task in DLQ
	dlqKey := dlqKeyPrefix + "tasks"
	if err := rq.client.RPush(ctx, dlqKey, taskData).Err(); err != nil {
		return fmt.Errorf("failed to enqueue task to DLQ: %w", err)
	}

	// Store DLQ metadata
	metaKey := dlqMetaPrefix + t.ID
	metadata := map[string]interface{}{
		"task_id":     t.ID,
		"reason":      reason,
		"moved_at":    time.Now().Unix(),
		"retry_count": t.RetryCount,
	}
	metaData, _ := json.Marshal(metadata)
	rq.client.Set(ctx, metaKey, metaData, 30*24*time.Hour) // Keep for 30 days

	// Update stats
	rq.client.Incr(ctx, statsKeyPrefix+"dlq")

	return nil
}

// GetDLQSize returns the number of tasks in the dead letter queue
func (rq *RedisQueue) GetDLQSize(ctx context.Context) (int64, error) {
	dlqKey := dlqKeyPrefix + "tasks"
	size, err := rq.client.LLen(ctx, dlqKey).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get DLQ size: %w", err)
	}
	return size, nil
}

// GetDLQTasks retrieves tasks from the dead letter queue
func (rq *RedisQueue) GetDLQTasks(ctx context.Context, limit int) ([]*task.Task, error) {
	dlqKey := dlqKeyPrefix + "tasks"

	var taskDataList []string
	var err error

	if limit > 0 {
		taskDataList, err = rq.client.LRange(ctx, dlqKey, 0, int64(limit-1)).Result()
	} else {
		taskDataList, err = rq.client.LRange(ctx, dlqKey, 0, -1).Result()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get DLQ tasks: %w", err)
	}

	tasks := make([]*task.Task, 0, len(taskDataList))
	for _, taskData := range taskDataList {
		var t task.Task
		if err := json.Unmarshal([]byte(taskData), &t); err != nil {
			// Skip corrupted tasks
			continue
		}
		tasks = append(tasks, &t)
	}

	return tasks, nil
}
