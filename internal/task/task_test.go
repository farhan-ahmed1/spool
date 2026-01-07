package task

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestNewTask verifies that NewTask creates a task with correct defaults
func TestNewTask(t *testing.T) {
	taskType := "test_task"
	payload := map[string]string{"message": "hello"}

	task, err := NewTask(taskType, payload)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if task.ID == "" {
		t.Error("Expected task ID to be generated")
	}

	if task.Type != taskType {
		t.Errorf("Expected task type %s, got %s", taskType, task.Type)
	}

	if task.State != StatePending {
		t.Errorf("Expected task state to be %s, got %s", StatePending, task.State)
	}

	if task.Priority != PriorityNormal {
		t.Errorf("Expected priority to be %d, got %d", PriorityNormal, task.Priority)
	}

	if task.MaxRetries != 3 {
		t.Error("Expected default max retries to be 3")
	}

	if task.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}

	if task.Timeout != 30*time.Second {
		t.Errorf("Expected timeout to be 30s, got %v", task.Timeout)
	}

	if task.Metadata == nil {
		t.Error("Expected metadata to be initialized")
	}
}

// TestNewTaskWithInvalidPayload tests handling of invalid payload
func TestNewTaskWithInvalidPayload(t *testing.T) {
	// Create a payload that can't be marshaled to JSON
	invalidPayload := make(chan int)

	task, err := NewTask("test", invalidPayload)
	if err == nil {
		t.Error("Expected error when marshaling invalid payload")
	}
	if task != nil {
		t.Error("Expected nil task on error")
	}
}

// TestWithPriority tests setting task priority
func TestWithPriority(t *testing.T) {
	task, _ := NewTask("test", map[string]string{})

	tests := []Priority{
		PriorityLow,
		PriorityNormal,
		PriorityHigh,
		PriorityCritical,
	}

	for _, priority := range tests {
		result := task.WithPriority(priority)
		if result.Priority != priority {
			t.Errorf("Expected priority %d, got %d", priority, result.Priority)
		}
		if result != task {
			t.Error("Expected WithPriority to return same task instance")
		}
	}
}

// TestWithMaxRetries tests setting max retries
func TestWithMaxRetries(t *testing.T) {
	task, _ := NewTask("test", map[string]string{})

	tests := []int{0, 1, 5, 10}

	for _, maxRetries := range tests {
		result := task.WithMaxRetries(maxRetries)
		if result.MaxRetries != maxRetries {
			t.Errorf("Expected max retries %d, got %d", maxRetries, result.MaxRetries)
		}
		if result != task {
			t.Error("Expected WithMaxRetries to return same task instance")
		}
	}
}

// TestWithTimeout tests setting task timeout
func TestWithTimeout(t *testing.T) {
	task, _ := NewTask("test", map[string]string{})

	timeout := 5 * time.Minute
	result := task.WithTimeout(timeout)

	if result.Timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, result.Timeout)
	}
	if result != task {
		t.Error("Expected WithTimeout to return same task instance")
	}
}

// TestWithScheduleAt tests scheduling a task
func TestWithScheduleAt(t *testing.T) {
	task, _ := NewTask("test", map[string]string{})

	scheduleTime := time.Now().Add(1 * time.Hour)
	result := task.WithScheduleAt(scheduleTime)

	if result.ScheduledAt != scheduleTime {
		t.Errorf("Expected schedule time %v, got %v", scheduleTime, result.ScheduledAt)
	}
	if result != task {
		t.Error("Expected WithScheduleAt to return same task instance")
	}
}

// TestMarkStarted tests marking a task as started
func TestMarkStarted(t *testing.T) {
	task, _ := NewTask("test", map[string]string{})

	if task.StartedAt != nil {
		t.Error("Expected StartedAt to be nil initially")
	}

	beforeMark := time.Now()
	task.MarkStarted()
	afterMark := time.Now()

	if task.StartedAt == nil {
		t.Fatal("Expected StartedAt to be set")
	}

	if task.StartedAt.Before(beforeMark) || task.StartedAt.After(afterMark) {
		t.Error("StartedAt time is outside expected range")
	}

	if task.State != StateProcessing {
		t.Errorf("Expected state to be %s, got %s", StateProcessing, task.State)
	}
}

// TestMarkCompleted tests marking a task as completed
func TestMarkCompleted(t *testing.T) {
	task, _ := NewTask("test", map[string]string{})

	if task.CompletedAt != nil {
		t.Error("Expected CompletedAt to be nil initially")
	}

	beforeMark := time.Now()
	task.MarkCompleted()
	afterMark := time.Now()

	if task.CompletedAt == nil {
		t.Fatal("Expected CompletedAt to be set")
	}

	if task.CompletedAt.Before(beforeMark) || task.CompletedAt.After(afterMark) {
		t.Error("CompletedAt time is outside expected range")
	}

	if task.State != StateCompleted {
		t.Errorf("Expected state to be %s, got %s", StateCompleted, task.State)
	}
}

// TestMarkFailed tests marking a task as failed
func TestMarkFailed(t *testing.T) {
	testErr := errors.New("test error")

	// Test failure with retries remaining
	task, _ := NewTask("test", map[string]string{})
	task.MaxRetries = 3
	task.RetryCount = 0

	task.MarkFailed(testErr)

	if task.Error != testErr.Error() {
		t.Errorf("Expected error %s, got %s", testErr.Error(), task.Error)
	}

	if task.RetryCount != 1 {
		t.Errorf("Expected retry count to be 1, got %d", task.RetryCount)
	}

	if task.State != StateRetrying {
		t.Errorf("Expected state to be %s, got %s", StateRetrying, task.State)
	}

	if task.CompletedAt != nil {
		t.Error("Expected CompletedAt to be nil when retrying")
	}
}

// TestMarkFailedMaxRetries tests failure after max retries
func TestMarkFailedMaxRetries(t *testing.T) {
	testErr := errors.New("permanent failure")

	task, _ := NewTask("test", map[string]string{})
	task.MaxRetries = 3
	task.RetryCount = 3 // Already at max retries

	task.MarkFailed(testErr)

	if task.RetryCount != 4 {
		t.Errorf("Expected retry count to be 4, got %d", task.RetryCount)
	}

	if task.State != StateDeadLetter {
		t.Errorf("Expected state to be %s, got %s", StateDeadLetter, task.State)
	}

	if task.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set for dead letter")
	}
}

// TestShouldRetry tests the ShouldRetry method
func TestShouldRetry(t *testing.T) {
	task, _ := NewTask("test", map[string]string{})
	task.MaxRetries = 3

	tests := []struct {
		retryCount int
		expected   bool
	}{
		{0, true},
		{1, true},
		{2, true},
		{3, false},
		{4, false},
	}

	for _, tt := range tests {
		task.RetryCount = tt.retryCount
		result := task.ShouldRetry()
		if result != tt.expected {
			t.Errorf("RetryCount %d: expected ShouldRetry=%v, got %v",
				tt.retryCount, tt.expected, result)
		}
	}
}

// TestGetRetryDelay tests the exponential backoff calculation
func TestGetRetryDelay(t *testing.T) {
	task, _ := NewTask("test", map[string]string{})

	tests := []struct {
		retryCount  int
		expectedMin time.Duration
		expectedMax time.Duration
	}{
		{0, 1 * time.Second, 1 * time.Second},  // 1 * 2^0 = 1s
		{1, 2 * time.Second, 2 * time.Second},  // 1 * 2^1 = 2s
		{2, 4 * time.Second, 4 * time.Second},  // 1 * 2^2 = 4s
		{3, 8 * time.Second, 8 * time.Second},  // 1 * 2^3 = 8s
		{10, 5 * time.Minute, 5 * time.Minute}, // Capped at max
	}

	for _, tt := range tests {
		task.RetryCount = tt.retryCount
		delay := task.GetRetryDelay()

		if delay < tt.expectedMin || delay > tt.expectedMax {
			t.Errorf("RetryCount %d: expected delay between %v and %v, got %v",
				tt.retryCount, tt.expectedMin, tt.expectedMax, delay)
		}
	}
}

// TestTaskJSONSerialization tests task marshaling/unmarshaling
func TestTaskJSONSerialization(t *testing.T) {
	original, _ := NewTask("test_type", map[string]interface{}{
		"key": "value",
		"num": 42,
	})
	original.WithPriority(PriorityHigh).WithMaxRetries(5)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal task: %v", err)
	}

	var decoded Task
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal task: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID mismatch: expected %s, got %s", original.ID, decoded.ID)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type mismatch: expected %s, got %s", original.Type, decoded.Type)
	}

	if decoded.Priority != original.Priority {
		t.Errorf("Priority mismatch: expected %d, got %d", original.Priority, decoded.Priority)
	}

	if decoded.MaxRetries != original.MaxRetries {
		t.Errorf("MaxRetries mismatch: expected %d, got %d", original.MaxRetries, decoded.MaxRetries)
	}
}

// TestTaskPriorityLevels verifies priority constants
func TestTaskPriorityLevels(t *testing.T) {
	tests := []struct {
		name     string
		priority Priority
		expected int
	}{
		{"Low", PriorityLow, 0},
		{"Normal", PriorityNormal, 1},
		{"High", PriorityHigh, 2},
		{"Critical", PriorityCritical, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.priority) != tt.expected {
				t.Errorf("Expected %s priority to be %d, got %d",
					tt.name, tt.expected, int(tt.priority))
			}
		})
	}
}

// TestTaskStates verifies state constants
func TestTaskStates(t *testing.T) {
	states := []State{
		StatePending,
		StateProcessing,
		StateCompleted,
		StateFailed,
		StateRetrying,
		StateDeadLetter,
	}

	for _, state := range states {
		if state == "" {
			t.Errorf("State should not be empty")
		}
	}
}

// TestTaskWithOptions verifies creating tasks with options
func TestTaskWithOptions(t *testing.T) {
	taskType := "priority_task"
	payload := map[string]string{"data": "test"}

	task, err := NewTask(taskType, payload)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	task.Priority = PriorityHigh
	task.MaxRetries = 5
	task.Timeout = 30 * time.Second

	if task.Priority != PriorityHigh {
		t.Error("Expected high priority")
	}

	if task.MaxRetries != 5 {
		t.Errorf("Expected 5 max retries, got %d", task.MaxRetries)
	}

	if task.Timeout != 30*time.Second {
		t.Errorf("Expected 30s timeout, got %v", task.Timeout)
	}
}
