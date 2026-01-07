package task

import (
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
