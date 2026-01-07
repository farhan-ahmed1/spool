package task

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Handler is a function that processes a task
type Handler func(ctx context.Context, payload json.RawMessage) (interface{}, error)

// Registry manages task handlers
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry creates a new handler registry
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
	}
}

// Register registers a handler for a specific task type
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
