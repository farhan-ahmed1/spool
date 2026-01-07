package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/redis/go-redis/v9"
)

const (
	// Redis key prefixes for storage
	taskStorePrefix   = "spool:store:task:"
	resultStorePrefix = "spool:store:result:"
	stateIndexPrefix  = "spool:index:state:"

	// TTL for task data (7 days)
	taskTTL = 7 * 24 * time.Hour
	// TTL for result data (30 days)
	resultTTL = 30 * 24 * time.Hour
)

// RedisStorage implements Storage interface using Redis
type RedisStorage struct {
	client *redis.Client
}

// NewRedisStorage creates a new Redis storage backend
func NewRedisStorage(client *redis.Client) *RedisStorage {
	return &RedisStorage{
		client: client,
	}
}

// SaveTask persists a task to Redis
func (rs *RedisStorage) SaveTask(ctx context.Context, t *task.Task) error {
	if t == nil || t.ID == "" {
		return fmt.Errorf("invalid task")
	}

	// Serialize task
	data, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// Store task data
	taskKey := taskStorePrefix + t.ID
	if err := rs.client.Set(ctx, taskKey, data, taskTTL).Err(); err != nil {
		return fmt.Errorf("failed to save task: %w", err)
	}

	// Add to state index
	stateKey := stateIndexPrefix + string(t.State)
	if err := rs.client.SAdd(ctx, stateKey, t.ID).Err(); err != nil {
		return fmt.Errorf("failed to index task state: %w", err)
	}

	return nil
}

// GetTask retrieves a task by ID
func (rs *RedisStorage) GetTask(ctx context.Context, taskID string) (*task.Task, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task ID cannot be empty")
	}

	taskKey := taskStorePrefix + taskID
	data, err := rs.client.Get(ctx, taskKey).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	var t task.Task
	if err := json.Unmarshal([]byte(data), &t); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return &t, nil
}

// UpdateTaskState updates the state of a task
func (rs *RedisStorage) UpdateTaskState(ctx context.Context, taskID string, state task.State) error {
	if taskID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}

	// Get current task
	t, err := rs.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// Remove from old state index
	oldStateKey := stateIndexPrefix + string(t.State)
	rs.client.SRem(ctx, oldStateKey, taskID)

	// Update state
	t.State = state

	// Save updated task
	return rs.SaveTask(ctx, t)
}

// SaveResult persists a task result
func (rs *RedisStorage) SaveResult(ctx context.Context, result *task.Result) error {
	if result == nil || result.TaskID == "" {
		return fmt.Errorf("invalid result")
	}

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	resultKey := resultStorePrefix + result.TaskID
	if err := rs.client.Set(ctx, resultKey, data, resultTTL).Err(); err != nil {
		return fmt.Errorf("failed to save result: %w", err)
	}

	return nil
}

// GetResult retrieves a task result by task ID
func (rs *RedisStorage) GetResult(ctx context.Context, taskID string) (*task.Result, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task ID cannot be empty")
	}

	resultKey := resultStorePrefix + taskID
	data, err := rs.client.Get(ctx, resultKey).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("result not found for task: %s", taskID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get result: %w", err)
	}

	var result task.Result
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return &result, nil
}

// GetTasksByState retrieves tasks by their state
func (rs *RedisStorage) GetTasksByState(ctx context.Context, state task.State, limit int) ([]*task.Task, error) {
	stateKey := stateIndexPrefix + string(state)

	var taskIDs []string
	var err error

	if limit > 0 {
		// Get limited number of task IDs
		taskIDs, err = rs.client.SRandMemberN(ctx, stateKey, int64(limit)).Result()
	} else {
		// Get all task IDs
		taskIDs, err = rs.client.SMembers(ctx, stateKey).Result()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get task IDs: %w", err)
	}

	tasks := make([]*task.Task, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		t, err := rs.GetTask(ctx, taskID)
		if err != nil {
			// Skip tasks that can't be retrieved
			continue
		}
		tasks = append(tasks, t)
	}

	return tasks, nil
}

// DeleteTask removes a task from storage
func (rs *RedisStorage) DeleteTask(ctx context.Context, taskID string) error {
	if taskID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}

	// Get task to find its state
	t, err := rs.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// Remove from state index
	stateKey := stateIndexPrefix + string(t.State)
	rs.client.SRem(ctx, stateKey, taskID)

	// Delete task data
	taskKey := taskStorePrefix + taskID
	if err := rs.client.Del(ctx, taskKey).Err(); err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	// Delete result if exists
	resultKey := resultStorePrefix + taskID
	rs.client.Del(ctx, resultKey)

	return nil
}

// Close closes the Redis connection
func (rs *RedisStorage) Close() error {
	// Note: We don't close the client here as it might be shared
	// The caller should manage the Redis client lifecycle
	return nil
}
