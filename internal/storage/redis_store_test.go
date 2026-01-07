package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/redis/go-redis/v9"
)

// setupTestRedis creates a test Redis server using miniredis
func setupTestRedis(t *testing.T) (*RedisStorage, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	storage := NewRedisStorage(client)
	return storage, mr
}

// TestNewRedisStorage tests creating a new Redis storage
func TestNewRedisStorage(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	storage := NewRedisStorage(client)
	if storage == nil {
		t.Fatal("Expected storage to be created")
	}

	if storage.client == nil {
		t.Error("Expected client to be set")
	}
}

// TestSaveTask tests saving a task to Redis
func TestSaveTask(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	tsk, _ := task.NewTask("test_task", map[string]string{"key": "value"})
	tsk.WithPriority(task.PriorityHigh)

	err := storage.SaveTask(context.Background(), tsk)
	if err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	// Verify task was saved
	taskKey := taskStorePrefix + tsk.ID
	exists := mr.Exists(taskKey)
	if !exists {
		t.Error("Task was not saved to Redis")
	}

	// Verify state index was updated
	stateKey := stateIndexPrefix + string(task.StatePending)
	isMember, _ := mr.SIsMember(stateKey, tsk.ID)
	if !isMember {
		t.Error("Task was not added to state index")
	}
}

// TestSaveNilTask tests saving a nil task
func TestSaveNilTask(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	err := storage.SaveTask(context.Background(), nil)
	if err == nil {
		t.Error("Expected error when saving nil task")
	}
}

// TestSaveTaskWithEmptyID tests saving a task with empty ID
func TestSaveTaskWithEmptyID(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	task := &task.Task{
		ID:   "",
		Type: "test",
	}

	err := storage.SaveTask(context.Background(), task)
	if err == nil {
		t.Error("Expected error when saving task with empty ID")
	}
}

// TestGetTask tests retrieving a task from Redis
func TestGetTask(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	originalTask, _ := task.NewTask("test_task", map[string]string{"key": "value"})
	originalTask.WithPriority(task.PriorityHigh).WithMaxRetries(5)

	// Save the task first
	if err := storage.SaveTask(context.Background(), originalTask); err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	// Retrieve the task
	retrievedTask, err := storage.GetTask(context.Background(), originalTask.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	if retrievedTask.ID != originalTask.ID {
		t.Errorf("Expected task ID %s, got %s", originalTask.ID, retrievedTask.ID)
	}

	if retrievedTask.Type != originalTask.Type {
		t.Errorf("Expected task type %s, got %s", originalTask.Type, retrievedTask.Type)
	}

	if retrievedTask.Priority != originalTask.Priority {
		t.Errorf("Expected priority %d, got %d", originalTask.Priority, retrievedTask.Priority)
	}

	if retrievedTask.MaxRetries != originalTask.MaxRetries {
		t.Errorf("Expected max retries %d, got %d", originalTask.MaxRetries, retrievedTask.MaxRetries)
	}
}

// TestGetNonExistentTask tests retrieving a task that doesn't exist
func TestGetNonExistentTask(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	task, err := storage.GetTask(context.Background(), "non_existent_id")
	if err == nil {
		t.Error("Expected error when getting non-existent task")
	}

	if task != nil {
		t.Error("Expected nil task for non-existent ID")
	}
}

// TestGetTaskWithEmptyID tests retrieving a task with empty ID
func TestGetTaskWithEmptyID(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	task, err := storage.GetTask(context.Background(), "")
	if err == nil {
		t.Error("Expected error when getting task with empty ID")
	}

	if task != nil {
		t.Error("Expected nil task for empty ID")
	}
}

// TestUpdateTaskState tests updating a task's state
func TestUpdateTaskState(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	tsk, _ := task.NewTask("test_task", map[string]string{})
	if err := storage.SaveTask(context.Background(), tsk); err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	// Update state
	newState := task.StateProcessing
	err := storage.UpdateTaskState(context.Background(), tsk.ID, newState)
	if err != nil {
		t.Fatalf("Failed to update task state: %v", err)
	}

	// Verify state was updated
	updatedTask, _ := storage.GetTask(context.Background(), tsk.ID)
	if updatedTask.State != newState {
		t.Errorf("Expected state %s, got %s", newState, updatedTask.State)
	}

	// Verify old state index was updated
	oldStateKey := stateIndexPrefix + string(task.StatePending)
	isMemberOld, _ := mr.SIsMember(oldStateKey, tsk.ID)
	if isMemberOld {
		t.Error("Task was not removed from old state index")
	}

	// Verify new state index was updated
	newStateKey := stateIndexPrefix + string(newState)
	isMemberNew, _ := mr.SIsMember(newStateKey, tsk.ID)
	if !isMemberNew {
		t.Error("Task was not added to new state index")
	}
}

// TestUpdateTaskStateWithEmptyID tests updating state with empty ID
func TestUpdateTaskStateWithEmptyID(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	err := storage.UpdateTaskState(context.Background(), "", task.StateProcessing)
	if err == nil {
		t.Error("Expected error when updating state with empty ID")
	}
}

// TestSaveResult tests saving a task result
func TestSaveResult(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	result := &task.Result{
		TaskID:      "test_task_id",
		Success:     true,
		Output:      json.RawMessage(`{"result":"success"}`),
		CompletedAt: time.Now(),
		Duration:    5 * time.Second,
	}

	err := storage.SaveResult(context.Background(), result)
	if err != nil {
		t.Fatalf("Failed to save result: %v", err)
	}

	// Verify result was saved
	resultKey := resultStorePrefix + result.TaskID
	exists := mr.Exists(resultKey)
	if !exists {
		t.Error("Result was not saved to Redis")
	}
}

// TestSaveNilResult tests saving a nil result
func TestSaveNilResult(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	err := storage.SaveResult(context.Background(), nil)
	if err == nil {
		t.Error("Expected error when saving nil result")
	}
}

// TestSaveResultWithEmptyTaskID tests saving a result with empty task ID
func TestSaveResultWithEmptyTaskID(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	result := &task.Result{
		TaskID:  "",
		Success: true,
	}

	err := storage.SaveResult(context.Background(), result)
	if err == nil {
		t.Error("Expected error when saving result with empty task ID")
	}
}

// TestGetResult tests retrieving a task result
func TestGetResult(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	originalResult := &task.Result{
		TaskID:      "test_task_id",
		Success:     true,
		Output:      json.RawMessage(`{"result":"success","value":42}`),
		CompletedAt: time.Now().UTC(),
		Duration:    5 * time.Second,
	}

	// Save the result first
	if err := storage.SaveResult(context.Background(), originalResult); err != nil {
		t.Fatalf("Failed to save result: %v", err)
	}

	// Retrieve the result
	retrievedResult, err := storage.GetResult(context.Background(), originalResult.TaskID)
	if err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	if retrievedResult.TaskID != originalResult.TaskID {
		t.Errorf("Expected task ID %s, got %s", originalResult.TaskID, retrievedResult.TaskID)
	}

	if retrievedResult.Success != originalResult.Success {
		t.Errorf("Expected success %v, got %v", originalResult.Success, retrievedResult.Success)
	}

	if string(retrievedResult.Output) != string(originalResult.Output) {
		t.Error("Output mismatch")
	}
}

// TestGetNonExistentResult tests retrieving a result that doesn't exist
func TestGetNonExistentResult(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	result, err := storage.GetResult(context.Background(), "non_existent_id")
	if err == nil {
		t.Error("Expected error when getting non-existent result")
	}

	if result != nil {
		t.Error("Expected nil result for non-existent ID")
	}
}

// TestGetResultWithEmptyID tests retrieving a result with empty ID
func TestGetResultWithEmptyID(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	result, err := storage.GetResult(context.Background(), "")
	if err == nil {
		t.Error("Expected error when getting result with empty ID")
	}

	if result != nil {
		t.Error("Expected nil result for empty ID")
	}
}

// TestGetTasksByState tests retrieving tasks by state
func TestGetTasksByState(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	// Create and save multiple tasks with different states
	pendingTasks := make([]*task.Task, 3)
	for i := 0; i < 3; i++ {
		pendingTasks[i], _ = task.NewTask("test", map[string]int{"id": i})
		if err := storage.SaveTask(context.Background(), pendingTasks[i]); err != nil {
			t.Fatalf("Failed to save task: %v", err)
		}
	}

	processingTask, _ := task.NewTask("test", map[string]string{})
	processingTask.State = task.StateProcessing
	if err := storage.SaveTask(context.Background(), processingTask); err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	// Get pending tasks
	tasks, err := storage.GetTasksByState(context.Background(), task.StatePending, 0)
	if err != nil {
		t.Fatalf("Failed to get tasks by state: %v", err)
	}

	if len(tasks) != 3 {
		t.Errorf("Expected 3 pending tasks, got %d", len(tasks))
	}
}

// TestGetTasksByStateWithLimit tests retrieving tasks with limit
func TestGetTasksByStateWithLimit(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	// Create and save 5 pending tasks
	for i := 0; i < 5; i++ {
		task, _ := task.NewTask("test", map[string]int{"id": i})
		if err := storage.SaveTask(context.Background(), task); err != nil {
			t.Fatalf("Failed to save task: %v", err)
		}
	}

	// Get only 2 tasks
	tasks, err := storage.GetTasksByState(context.Background(), task.StatePending, 2)
	if err != nil {
		t.Fatalf("Failed to get tasks by state: %v", err)
	}

	if len(tasks) > 2 {
		t.Errorf("Expected at most 2 tasks, got %d", len(tasks))
	}
}

// TestDeleteTask tests deleting a task
func TestDeleteTask(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	tsk, _ := task.NewTask("test_task", map[string]string{})
	if err := storage.SaveTask(context.Background(), tsk); err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	// Save a result too
	result := &task.Result{
		TaskID:  tsk.ID,
		Success: true,
	}
	if err := storage.SaveResult(context.Background(), result); err != nil {
		t.Fatalf("Failed to save result: %v", err)
	}

	// Delete the task
	err := storage.DeleteTask(context.Background(), tsk.ID)
	if err != nil {
		t.Fatalf("Failed to delete task: %v", err)
	}

	// Verify task was deleted
	taskKey := taskStorePrefix + tsk.ID
	if mr.Exists(taskKey) {
		t.Error("Task was not deleted from Redis")
	}

	// Verify task was removed from state index
	stateKey := stateIndexPrefix + string(tsk.State)
	isMember, _ := mr.SIsMember(stateKey, tsk.ID)
	if isMember {
		t.Error("Task was not removed from state index")
	}

	// Verify result was deleted
	resultKey := resultStorePrefix + tsk.ID
	if mr.Exists(resultKey) {
		t.Error("Result was not deleted")
	}
}

// TestDeleteTaskWithEmptyID tests deleting a task with empty ID
func TestDeleteTaskWithEmptyID(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	err := storage.DeleteTask(context.Background(), "")
	if err == nil {
		t.Error("Expected error when deleting task with empty ID")
	}
}

// TestDeleteNonExistentTask tests deleting a task that doesn't exist
func TestDeleteNonExistentTask(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	err := storage.DeleteTask(context.Background(), "non_existent_id")
	if err == nil {
		t.Error("Expected error when deleting non-existent task")
	}
}

// TestClose tests closing the storage connection
func TestClose(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	err := storage.Close()
	if err != nil {
		t.Errorf("Failed to close storage: %v", err)
	}
}

// TestTaskRoundTrip tests saving and retrieving a complex task
func TestTaskRoundTrip(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	original, _ := task.NewTask("complex_task", map[string]interface{}{
		"nested": map[string]interface{}{
			"key": "value",
			"num": 42,
		},
		"array": []string{"a", "b", "c"},
	})

	original.WithPriority(task.PriorityCritical).
		WithMaxRetries(10).
		WithTimeout(2 * time.Minute)

	now := time.Now()
	original.StartedAt = &now
	original.Metadata = map[string]interface{}{
		"user":   "test_user",
		"source": "api",
	}

	// Save
	err := storage.SaveTask(context.Background(), original)
	if err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	// Retrieve
	retrieved, err := storage.GetTask(context.Background(), original.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	// Verify all fields
	if retrieved.ID != original.ID {
		t.Error("ID mismatch")
	}
	if retrieved.Type != original.Type {
		t.Error("Type mismatch")
	}
	if retrieved.Priority != original.Priority {
		t.Error("Priority mismatch")
	}
	if retrieved.MaxRetries != original.MaxRetries {
		t.Error("MaxRetries mismatch")
	}
	if retrieved.Timeout != original.Timeout {
		t.Error("Timeout mismatch")
	}
	if string(retrieved.Payload) != string(original.Payload) {
		t.Error("Payload mismatch")
	}
}

// TestResultRoundTrip tests saving and retrieving a complex result
func TestResultRoundTrip(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	original := &task.Result{
		TaskID:  "test_task_123",
		Success: true,
		Output: json.RawMessage(`{
			"data": {
				"processed": true,
				"items": [1, 2, 3],
				"metadata": {
					"timestamp": "2024-01-01T00:00:00Z"
				}
			}
		}`),
		CompletedAt: time.Now().UTC(),
		Duration:    15 * time.Second,
	}

	// Save
	err := storage.SaveResult(context.Background(), original)
	if err != nil {
		t.Fatalf("Failed to save result: %v", err)
	}

	// Retrieve
	retrieved, err := storage.GetResult(context.Background(), original.TaskID)
	if err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	// Verify all fields
	if retrieved.TaskID != original.TaskID {
		t.Error("TaskID mismatch")
	}
	if retrieved.Success != original.Success {
		t.Error("Success mismatch")
	}
	if retrieved.Duration != original.Duration {
		t.Error("Duration mismatch")
	}

	// Verify output can be unmarshaled
	var outputData map[string]interface{}
	err = json.Unmarshal(retrieved.Output, &outputData)
	if err != nil {
		t.Errorf("Failed to unmarshal output: %v", err)
	}
}

// TestMultipleStateTransitions tests updating task state multiple times
func TestMultipleStateTransitions(t *testing.T) {
	storage, mr := setupTestRedis(t)
	defer mr.Close()

	tsk, _ := task.NewTask("test", map[string]string{})
	if err := storage.SaveTask(context.Background(), tsk); err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	states := []task.State{
		task.StateProcessing,
		task.StateRetrying,
		task.StateProcessing,
		task.StateCompleted,
	}

	for _, state := range states {
		err := storage.UpdateTaskState(context.Background(), tsk.ID, state)
		if err != nil {
			t.Fatalf("Failed to update state to %s: %v", state, err)
		}

		retrieved, _ := storage.GetTask(context.Background(), tsk.ID)
		if retrieved.State != state {
			t.Errorf("Expected state %s, got %s", state, retrieved.State)
		}
	}
}
