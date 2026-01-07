package task

import (
	"encoding/json"
	"testing"
	"time"
)

// TestResultCreation tests creating a result
func TestResultCreation(t *testing.T) {
	result := &Result{
		TaskID:      "test_task_123",
		Success:     true,
		Output:      json.RawMessage(`{"message":"success"}`),
		CompletedAt: time.Now(),
		Duration:    5 * time.Second,
	}

	if result.TaskID != "test_task_123" {
		t.Errorf("Expected task ID 'test_task_123', got '%s'", result.TaskID)
	}

	if !result.Success {
		t.Error("Expected success to be true")
	}

	if result.Duration != 5*time.Second {
		t.Errorf("Expected duration 5s, got %v", result.Duration)
	}
}

// TestFailedResultCreation tests creating a failed result
func TestFailedResultCreation(t *testing.T) {
	result := &Result{
		TaskID:      "failed_task_456",
		Success:     false,
		Error:       "task execution failed",
		CompletedAt: time.Now(),
		Duration:    2 * time.Second,
	}

	if result.Success {
		t.Error("Expected success to be false")
	}

	if result.Error != "task execution failed" {
		t.Errorf("Expected error 'task execution failed', got '%s'", result.Error)
	}
}

// TestResultJSONSerialization tests marshaling and unmarshaling result
func TestResultJSONSerialization(t *testing.T) {
	original := &Result{
		TaskID:  "test_task",
		Success: true,
		Output:  json.RawMessage(`{"key":"value","number":42}`),
		CompletedAt: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Duration:    10 * time.Second,
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal result: %v", err)
	}

	// Unmarshal back
	var decoded Result
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify fields
	if decoded.TaskID != original.TaskID {
		t.Errorf("TaskID mismatch: expected %s, got %s", original.TaskID, decoded.TaskID)
	}

	if decoded.Success != original.Success {
		t.Error("Success mismatch")
	}

	if string(decoded.Output) != string(original.Output) {
		t.Error("Output mismatch")
	}

	if decoded.Duration != original.Duration {
		t.Errorf("Duration mismatch: expected %v, got %v", original.Duration, decoded.Duration)
	}
}

// TestResultWithComplexOutput tests result with complex nested output
func TestResultWithComplexOutput(t *testing.T) {
	output := map[string]interface{}{
		"status": "completed",
		"data": map[string]interface{}{
			"processed_items": 100,
			"errors":          0,
			"metadata": map[string]string{
				"source": "api",
				"version": "1.0",
			},
		},
		"metrics": []interface{}{
			map[string]interface{}{"name": "latency", "value": 123},
			map[string]interface{}{"name": "throughput", "value": 456},
		},
	}

	outputBytes, _ := json.Marshal(output)

	result := &Result{
		TaskID:      "complex_task",
		Success:     true,
		Output:      outputBytes,
		CompletedAt: time.Now(),
		Duration:    15 * time.Second,
	}

	// Verify output can be unmarshaled
	var decoded map[string]interface{}
	err := json.Unmarshal(result.Output, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal complex output: %v", err)
	}

	if decoded["status"] != "completed" {
		t.Error("Complex output status mismatch")
	}
}

// TestResultWithEmptyOutput tests result with no output
func TestResultWithEmptyOutput(t *testing.T) {
	result := &Result{
		TaskID:      "no_output_task",
		Success:     true,
		Output:      nil,
		CompletedAt: time.Now(),
		Duration:    1 * time.Second,
	}

	if result.Output != nil {
		t.Error("Expected nil output")
	}

	// Verify it can be marshaled
	_, err := json.Marshal(result)
	if err != nil {
		t.Errorf("Failed to marshal result with nil output: %v", err)
	}
}

// TestResultWithError tests result with error message
func TestResultWithError(t *testing.T) {
	errorMsg := "connection timeout after 30 seconds"

	result := &Result{
		TaskID:      "error_task",
		Success:     false,
		Error:       errorMsg,
		CompletedAt: time.Now(),
		Duration:    30 * time.Second,
	}

	if result.Error != errorMsg {
		t.Errorf("Expected error '%s', got '%s'", errorMsg, result.Error)
	}

	if result.Success {
		t.Error("Expected success to be false when error is present")
	}
}

// TestResultDuration tests various duration values
func TestResultDuration(t *testing.T) {
	durations := []time.Duration{
		0,
		100 * time.Millisecond,
		1 * time.Second,
		1 * time.Minute,
		1 * time.Hour,
	}

	for _, duration := range durations {
		result := &Result{
			TaskID:      "duration_test",
			Success:     true,
			CompletedAt: time.Now(),
			Duration:    duration,
		}

		if result.Duration != duration {
			t.Errorf("Expected duration %v, got %v", duration, result.Duration)
		}
	}
}

// TestResultCompletedAt tests the CompletedAt timestamp
func TestResultCompletedAt(t *testing.T) {
	now := time.Now()

	result := &Result{
		TaskID:      "timestamp_test",
		Success:     true,
		CompletedAt: now,
		Duration:    5 * time.Second,
	}

	// Check that timestamp is within a reasonable range
	if result.CompletedAt.Before(now.Add(-1*time.Second)) || 
	   result.CompletedAt.After(now.Add(1*time.Second)) {
		t.Error("CompletedAt timestamp is out of expected range")
	}
}

// TestMultipleResults tests creating multiple result objects
func TestMultipleResults(t *testing.T) {
	results := make([]*Result, 5)
	
	for i := 0; i < 5; i++ {
		results[i] = &Result{
			TaskID:      string(rune('a' + i)),
			Success:     i%2 == 0, // Alternate success/failure
			CompletedAt: time.Now(),
			Duration:    time.Duration(i) * time.Second,
		}
	}

	// Verify all results are independent
	for i, result := range results {
		expectedSuccess := i%2 == 0
		if result.Success != expectedSuccess {
			t.Errorf("Result %d: expected success=%v, got %v", i, expectedSuccess, result.Success)
		}
	}
}

// TestResultFieldZeroValues tests zero values for result fields
func TestResultFieldZeroValues(t *testing.T) {
	result := &Result{}

	if result.TaskID != "" {
		t.Error("Expected empty TaskID")
	}

	if result.Success {
		t.Error("Expected Success to be false by default")
	}

	if result.Output != nil {
		t.Error("Expected nil Output")
	}

	if result.Error != "" {
		t.Error("Expected empty Error")
	}

	if !result.CompletedAt.IsZero() {
		t.Error("Expected zero CompletedAt")
	}

	if result.Duration != 0 {
		t.Error("Expected zero Duration")
	}
}
