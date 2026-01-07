package task

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestNewRegistry tests creating a new registry
func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()

	if registry == nil {
		t.Fatal("Expected registry to be created")
	}

	if registry.handlers == nil {
		t.Error("Expected handlers map to be initialized")
	}

	types := registry.Types()
	if len(types) != 0 {
		t.Errorf("Expected empty registry, got %d types", len(types))
	}
}

// TestRegisterHandler tests registering a handler
func TestRegisterHandler(t *testing.T) {
	registry := NewRegistry()

	handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return "success", nil
	}

	err := registry.Register("test_task", handler)
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	types := registry.Types()
	if len(types) != 1 {
		t.Errorf("Expected 1 registered type, got %d", len(types))
	}

	if types[0] != "test_task" {
		t.Errorf("Expected type 'test_task', got '%s'", types[0])
	}
}

// TestRegisterDuplicateHandler tests registering duplicate handler
func TestRegisterDuplicateHandler(t *testing.T) {
	registry := NewRegistry()

	handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return "success", nil
	}

	// Register first time
	err := registry.Register("test_task", handler)
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	// Try to register again
	err = registry.Register("test_task", handler)
	if err == nil {
		t.Error("Expected error when registering duplicate handler")
	}
}

// TestGetHandler tests retrieving a handler
func TestGetHandler(t *testing.T) {
	registry := NewRegistry()

	handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return "success", nil
	}

	if err := registry.Register("test_task", handler); err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	retrieved, err := registry.Get("test_task")
	if err != nil {
		t.Fatalf("Failed to get handler: %v", err)
	}

	if retrieved == nil {
		t.Error("Expected handler to be returned")
	}
}

// TestGetNonExistentHandler tests retrieving non-existent handler
func TestGetNonExistentHandler(t *testing.T) {
	registry := NewRegistry()

	handler, err := registry.Get("non_existent")
	if err == nil {
		t.Error("Expected error when getting non-existent handler")
	}

	if handler != nil {
		t.Error("Expected nil handler for non-existent type")
	}
}

// TestRegistryTypes tests listing all registered types
func TestRegistryTypes(t *testing.T) {
	registry := NewRegistry()

	handlers := map[string]Handler{
		"task_a": func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
			return "a", nil
		},
		"task_b": func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
			return "b", nil
		},
		"task_c": func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
			return "c", nil
		},
	}

	for taskType, handler := range handlers {
		if err := registry.Register(taskType, handler); err != nil {
			t.Fatalf("Failed to register handler: %v", err)
		}
	}

	types := registry.Types()
	if len(types) != 3 {
		t.Errorf("Expected 3 types, got %d", len(types))
	}

	// Check all types are present
	typeMap := make(map[string]bool)
	for _, typ := range types {
		typeMap[typ] = true
	}

	for expectedType := range handlers {
		if !typeMap[expectedType] {
			t.Errorf("Expected type '%s' not found in registry", expectedType)
		}
	}
}

// TestExecuteHandler tests executing a task handler
func TestExecuteHandler(t *testing.T) {
	registry := NewRegistry()

	expectedOutput := map[string]interface{}{
		"result": "success",
		"value":  42,
	}

	handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return expectedOutput, nil
	}

	if err := registry.Register("test_task", handler); err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	task, _ := NewTask("test_task", map[string]string{"input": "data"})

	result, err := registry.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Failed to execute handler: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result to be returned")
	}

	if result.TaskID != task.ID {
		t.Errorf("Expected task ID %s, got %s", task.ID, result.TaskID)
	}

	if !result.Success {
		t.Error("Expected result to be successful")
	}

	if result.Error != "" {
		t.Errorf("Expected no error, got: %s", result.Error)
	}

	var output map[string]interface{}
	err = json.Unmarshal(result.Output, &output)
	if err != nil {
		t.Fatalf("Failed to unmarshal output: %v", err)
	}

	if output["result"] != expectedOutput["result"] {
		t.Error("Output mismatch")
	}
}

// TestExecuteHandlerWithError tests executing a handler that returns an error
func TestExecuteHandlerWithError(t *testing.T) {
	registry := NewRegistry()

	expectedError := errors.New("handler failed")

	handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return nil, expectedError
	}

	if err := registry.Register("failing_task", handler); err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	task, _ := NewTask("failing_task", map[string]string{"input": "data"})

	result, err := registry.Execute(context.Background(), task)
	if err == nil {
		t.Error("Expected error from handler execution")
	}

	if result == nil {
		t.Fatal("Expected result to be returned even on error")
	}

	if result.Success {
		t.Error("Expected result to be unsuccessful")
	}

	if result.Error != expectedError.Error() {
		t.Errorf("Expected error '%s', got '%s'", expectedError.Error(), result.Error)
	}
}

// TestExecuteNonExistentHandler tests executing with non-existent handler
func TestExecuteNonExistentHandler(t *testing.T) {
	registry := NewRegistry()

	task, _ := NewTask("non_existent", map[string]string{})

	result, err := registry.Execute(context.Background(), task)
	if err == nil {
		t.Error("Expected error when executing non-existent handler")
	}

	if result != nil {
		t.Error("Expected nil result for non-existent handler")
	}
}

// TestExecuteWithContext tests context propagation
func TestExecuteWithContext(t *testing.T) {
	registry := NewRegistry()

	type contextKey string
	ctxKey := contextKey("test_key")
	ctxValue := "test_value"

	handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		value := ctx.Value(ctxKey)
		if value == nil {
			return nil, errors.New("context value not found")
		}
		return value, nil
	}

	if err := registry.Register("context_task", handler); err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	ctx := context.WithValue(context.Background(), ctxKey, ctxValue)
	task, _ := NewTask("context_task", map[string]string{})

	result, err := registry.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Failed to execute handler: %v", err)
	}

	if !result.Success {
		t.Error("Expected successful result")
	}

	var output string
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("Failed to unmarshal output: %v", err)
	}
	if output != ctxValue {
		t.Errorf("Expected output '%s', got '%s'", ctxValue, output)
	}
}

// TestExecuteWithCanceledContext tests context cancellation
func TestExecuteWithCanceledContext(t *testing.T) {
	registry := NewRegistry()

	handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		// Simulate work
		select {
		case <-time.After(100 * time.Millisecond):
			return "completed", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err := registry.Register("cancelable_task", handler); err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	task, _ := NewTask("cancelable_task", map[string]string{})

	result, err := registry.Execute(ctx, task)
	if err == nil {
		t.Error("Expected error from canceled context")
	}

	if result == nil {
		t.Fatal("Expected result to be returned")
	}

	if result.Success {
		t.Error("Expected unsuccessful result")
	}
}

// TestConcurrentRegistration tests thread-safe registration
func TestConcurrentRegistration(t *testing.T) {
	registry := NewRegistry()

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(id int) {
			handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
				return id, nil
			}

			taskType := string(rune('a' + id))
			if err := registry.Register(taskType, handler); err != nil {
				t.Errorf("Failed to register handler: %v", err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	types := registry.Types()
	if len(types) != 10 {
		t.Errorf("Expected 10 registered types, got %d", len(types))
	}
}

// TestConcurrentGet tests thread-safe retrieval
func TestConcurrentGet(t *testing.T) {
	registry := NewRegistry()

	handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		return "success", nil
	}

	if err := registry.Register("test_task", handler); err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	done := make(chan bool)

	for i := 0; i < 50; i++ {
		go func() {
			_, err := registry.Get("test_task")
			if err != nil {
				t.Errorf("Failed to get handler: %v", err)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 50; i++ {
		<-done
	}
}

// TestExecuteWithUnmarshalableOutput tests handling of unmarshalable output
func TestExecuteWithUnmarshalableOutput(t *testing.T) {
	registry := NewRegistry()

	handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		// Return something that can't be marshaled to JSON
		return make(chan int), nil
	}

	if err := registry.Register("bad_output_task", handler); err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	task, _ := NewTask("bad_output_task", map[string]string{})

	result, err := registry.Execute(context.Background(), task)
	if err == nil {
		t.Error("Expected error when marshaling invalid output")
	}

	if result == nil {
		t.Fatal("Expected result to be returned")
	}

	if result.Success {
		t.Error("Expected unsuccessful result")
	}

	if result.Error == "" {
		t.Error("Expected error message in result")
	}
}
