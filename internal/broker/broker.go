package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/farhan-ahmed1/spool/internal/logger"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
)

// Broker provides HTTP API for task submission and management
type Broker struct {
	addr     string
	queue    queue.Queue
	storage  storage.Storage
	server   *http.Server
	serverMu sync.RWMutex
	logger   *logger.Logger

	// Shutdown
	done  chan struct{}
	ready chan struct{}
}

// Config holds broker configuration
type Config struct {
	Addr    string // e.g., ":8000"
	Queue   queue.Queue
	Storage storage.Storage
}

// NewBroker creates a new broker instance
func NewBroker(cfg Config) *Broker {
	if cfg.Addr == "" {
		cfg.Addr = ":8000"
	}

	// Create logger for broker
	brokerLogger := logger.GetDefault()
	if brokerLogger == nil {
		brokerLogger = logger.New("info", "text", "broker")
	} else {
		brokerLogger = brokerLogger.WithComponent("broker")
	}

	return &Broker{
		addr:    cfg.Addr,
		queue:   cfg.Queue,
		storage: cfg.Storage,
		logger:  brokerLogger,
		done:    make(chan struct{}),
		ready:   make(chan struct{}),
	}
}

// Start starts the HTTP server
func (b *Broker) Start() error {
	mux := http.NewServeMux()

	// Task submission endpoints
	mux.HandleFunc("/api/tasks", b.handleTasks)
	mux.HandleFunc("/api/tasks/batch", b.handleBatchSubmit)
	mux.HandleFunc("/api/tasks/", b.handleTaskByID)

	// Queue management endpoints
	mux.HandleFunc("/api/queue/stats", b.handleQueueStats)
	mux.HandleFunc("/api/queue/purge", b.handlePurge)

	// Health check
	mux.HandleFunc("/health", b.handleHealth)

	server := &http.Server{
		Addr:         b.addr,
		Handler:      b.withLogging(b.withCORS(mux)),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	b.serverMu.Lock()
	b.server = server
	b.serverMu.Unlock()

	// Signal that broker is ready
	close(b.ready)

	b.logger.Info("Starting broker server", logger.Fields{
		"address": b.addr,
	})
	return server.ListenAndServe()
}

// Ready returns a channel that is closed when the broker is ready
func (b *Broker) Ready() <-chan struct{} {
	return b.ready
}

// Stop gracefully shuts down the broker
func (b *Broker) Stop(ctx context.Context) error {
	close(b.done)

	b.serverMu.RLock()
	server := b.server
	b.serverMu.RUnlock()

	if server == nil {
		return nil
	}

	b.logger.Info("Shutting down broker server", logger.Fields{})
	return server.Shutdown(ctx)
}

// handleTasks handles task submission (POST) and listing (GET)
func (b *Broker) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		b.handleSubmit(w, r)
	case http.MethodGet:
		b.handleList(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSubmit handles single task submission
func (b *Broker) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var t task.Task
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		b.logger.Warn("Failed to decode task", logger.Fields{
			"error": err.Error(),
		})
		http.Error(w, "Invalid task format", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if err := b.queue.Enqueue(ctx, &t); err != nil {
		b.logger.Error("Failed to enqueue task", logger.Fields{
			"error":   err.Error(),
			"task_id": t.ID,
			"type":    t.Type,
		})
		http.Error(w, "Failed to enqueue task", http.StatusInternalServerError)
		return
	}

	b.logger.Info("Task enqueued", logger.Fields{
		"task_id":  t.ID,
		"type":     t.Type,
		"priority": t.Priority,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     t.ID,
		"status": "enqueued",
	}); err != nil {
		b.logger.Error("Failed to encode response", logger.Fields{
			"error": err.Error(),
		})
	}
}

// handleBatchSubmit handles batch task submission
func (b *Broker) handleBatchSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var tasks []task.Task
	if err := json.NewDecoder(r.Body).Decode(&tasks); err != nil {
		b.logger.Warn("Failed to decode batch tasks", logger.Fields{
			"error": err.Error(),
		})
		http.Error(w, "Invalid task format", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	taskIDs := make([]string, 0, len(tasks))

	for i := range tasks {
		if err := b.queue.Enqueue(ctx, &tasks[i]); err != nil {
			b.logger.Error("Failed to enqueue task in batch", logger.Fields{
				"error":   err.Error(),
				"task_id": tasks[i].ID,
				"index":   i,
			})
			continue
		}
		taskIDs = append(taskIDs, tasks[i].ID)
	}

	b.logger.Info("Batch tasks enqueued", logger.Fields{
		"count":    len(taskIDs),
		"expected": len(tasks),
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"ids":      taskIDs,
		"enqueued": len(taskIDs),
		"total":    len(tasks),
	}); err != nil {
		b.logger.Error("Failed to encode response", logger.Fields{
			"error": err.Error(),
		})
	}
}

// handleList lists recent tasks
func (b *Broker) handleList(w http.ResponseWriter, r *http.Request) {
	if b.storage == nil {
		http.Error(w, "Storage not configured", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	tasks, err := b.storage.GetTasksByState(ctx, task.StatePending, 50)
	if err != nil {
		b.logger.Error("Failed to list tasks", logger.Fields{
			"error": err.Error(),
		})
		http.Error(w, "Failed to list tasks", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks": tasks,
		"total": len(tasks),
	}); err != nil {
		b.logger.Error("Failed to encode response", logger.Fields{
			"error": err.Error(),
		})
	}
}

// handleTaskByID handles task retrieval by ID
func (b *Broker) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.URL.Path[len("/api/tasks/"):]
	if taskID == "" {
		http.Error(w, "Task ID required", http.StatusBadRequest)
		return
	}

	if b.storage == nil {
		http.Error(w, "Storage not configured", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	t, err := b.storage.GetTask(ctx, taskID)
	if err != nil {
		b.logger.Warn("Task not found", logger.Fields{
			"task_id": taskID,
			"error":   err.Error(),
		})
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(t); err != nil {
		b.logger.Error("Failed to encode response", logger.Fields{
			"error": err.Error(),
		})
	}
}

// handleQueueStats returns queue statistics
func (b *Broker) handleQueueStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	size, err := b.queue.Size(ctx)
	if err != nil {
		b.logger.Error("Failed to get queue size", logger.Fields{
			"error": err.Error(),
		})
		http.Error(w, "Failed to get queue stats", http.StatusInternalServerError)
		return
	}

	dlqSize, _ := b.queue.GetDLQSize(ctx)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"queue_size": size,
		"dlq_size":   dlqSize,
		"timestamp":  time.Now().Format(time.RFC3339),
	}); err != nil {
		b.logger.Error("Failed to encode response", logger.Fields{
			"error": err.Error(),
		})
	}
}

// handlePurge purges the queue
func (b *Broker) handlePurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	if err := b.queue.Purge(ctx); err != nil {
		b.logger.Error("Failed to purge queue", logger.Fields{
			"error": err.Error(),
		})
		http.Error(w, "Failed to purge queue", http.StatusInternalServerError)
		return
	}

	b.logger.Warn("Queue purged", logger.Fields{})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "purged",
	}); err != nil {
		b.logger.Error("Failed to encode response", logger.Fields{
			"error": err.Error(),
		})
	}
}

// handleHealth returns health status
func (b *Broker) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// Check queue health
	if err := b.queue.Health(ctx); err != nil {
		b.logger.Error("Queue health check failed", logger.Fields{
			"error": err.Error(),
		})
		health["status"] = "unhealthy"
		health["queue_error"] = err.Error()
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(health); err != nil {
		b.logger.Error("Failed to encode health response", logger.Fields{
			"error": err.Error(),
		})
	}
}

// Middleware: withLogging logs all HTTP requests
func (b *Broker) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		b.logger.Debug("HTTP request", logger.Fields{
			"method":   r.Method,
			"path":     r.URL.Path,
			"duration": time.Since(start).String(),
		})
	})
}

// Middleware: withCORS adds CORS headers
func (b *Broker) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
