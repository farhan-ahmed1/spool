package task

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Handler is a function that processes a task.
// Handlers receive the task payload as JSON and return a result or error.
// The context can be used for cancellation and timeouts.
//
// Handler implementations should:
//   - Be idempotent (safe to retry)
//   - Respect context cancellation
//   - Return descriptive errors
//   - Clean up resources on error
//
// Example:
//
//	handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
//	    var data EmailPayload
//	    if err := json.Unmarshal(payload, &data); err != nil {
//	        return nil, err
//	    }
//	    return sendEmail(ctx, data)
//	}
type Handler func(ctx context.Context, payload json.RawMessage) (interface{}, error)

// Registry manages task handlers.
// It provides thread-safe registration and lookup of handlers by task type.
// Each task type can only have one registered handler.
type Registry struct {
	// mu protects concurrent access to handlers map
	mu sync.RWMutex
	// handlers maps task types to their handler functions
	handlers map[string]Handler
}

// NewRegistry creates a new handler registry
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
	}
}

// Register registers a handler for a specific task type.
// Returns an error if a handler for this type already exists.
//
// Example:
//
//	registry := NewRegistry()
//	err := registry.Register("send_email", emailHandler)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// Note: Register must be called before starting workers, as it is not
// safe to register handlers while workers are processing tasks.
func (r *Registry) Register(taskType string, handler Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[taskType]; exists {
		return fmt.Errorf("handler for task type '%s' already registered", taskType)
	}

	r.handlers[taskType] = handler
	return nil
}

// Get retrieves a handler for a task type
func (r *Registry) Get(taskType string) (Handler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	handler, exists := r.handlers[taskType]
	if !exists {
		return nil, fmt.Errorf("no handler registered for task type '%s'", taskType)
	}

	return handler, nil
}

// Types returns all registered task types
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.handlers))
	for t := range r.handlers {
		types = append(types, t)
	}
	return types
}

// Execute executes a task using the registered handler
func (r *Registry) Execute(ctx context.Context, task *Task) (*Result, error) {
	handler, err := r.Get(task.Type)
	if err != nil {
		return nil, err
	}

	// Execute the handler
	output, err := handler(ctx, task.Payload)

	result := &Result{
		TaskID:      task.ID,
		CompletedAt: time.Now().UTC(),
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		return result, err
	}

	// Marshal output
	outputBytes, err := json.Marshal(output)
	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("failed to marshal output: %v", err)
		return result, err
	}

	result.Success = true
	result.Output = outputBytes
	return result, nil
}
