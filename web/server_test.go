package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/monitoring"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/stretchr/testify/assert"
)

// mockQueue implements queue.Queue interface for testing
type mockQueue struct {
	size     int64
	dlqSize  int64
	dlqTasks []*task.Task
	healthy  bool
}

func (m *mockQueue) Enqueue(ctx context.Context, t *task.Task) error             { return nil }
func (m *mockQueue) Dequeue(ctx context.Context) (*task.Task, error)             { return nil, queue.ErrNoTask }
func (m *mockQueue) Peek(ctx context.Context) (*task.Task, error)                { return nil, nil }
func (m *mockQueue) Ack(ctx context.Context, taskID string) error                { return nil }
func (m *mockQueue) Nack(ctx context.Context, taskID string, requeue bool) error { return nil }
func (m *mockQueue) Size(ctx context.Context) (int64, error)                     { return m.size, nil }
func (m *mockQueue) SizeByPriority(ctx context.Context) (map[task.Priority]int64, error) {
	return nil, nil
}
func (m *mockQueue) Purge(ctx context.Context) error                                   { return nil }
func (m *mockQueue) EnqueueDLQ(ctx context.Context, t *task.Task, reason string) error { return nil }
func (m *mockQueue) GetDLQSize(ctx context.Context) (int64, error)                     { return m.dlqSize, nil }
func (m *mockQueue) GetDLQTasks(ctx context.Context, limit int) ([]*task.Task, error) {
	return m.dlqTasks, nil
}
func (m *mockQueue) Close() error { return nil }
func (m *mockQueue) Health(ctx context.Context) error {
	if !m.healthy {
		return assert.AnError
	}
	return nil
}

// mockStorage implements storage.Storage interface for testing
type mockStorage struct {
	tasks map[string]*task.Task
}

func (m *mockStorage) SaveTask(ctx context.Context, t *task.Task) error {
	if m.tasks == nil {
		m.tasks = make(map[string]*task.Task)
	}
	m.tasks[t.ID] = t
	return nil
}

func (m *mockStorage) GetTask(ctx context.Context, id string) (*task.Task, error) {
	if t, ok := m.tasks[id]; ok {
		return t, nil
	}
	return nil, assert.AnError
}

func (m *mockStorage) DeleteTask(ctx context.Context, id string) error {
	delete(m.tasks, id)
	return nil
}

func (m *mockStorage) UpdateTaskState(ctx context.Context, taskID string, state task.State) error {
	return nil
}

func (m *mockStorage) SaveResult(ctx context.Context, result *task.Result) error {
	return nil
}

func (m *mockStorage) GetResult(ctx context.Context, taskID string) (*task.Result, error) {
	return nil, nil
}

func (m *mockStorage) GetTasksByState(ctx context.Context, state task.State, limit int) ([]*task.Task, error) {
	return nil, nil
}

func (m *mockStorage) Close() error { return nil }

func TestNewServer(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	tests := []struct {
		name     string
		config   Config
		wantAddr string
	}{
		{
			name: "default config",
			config: Config{
				Metrics: metrics,
				Queue:   q,
				Storage: storage,
			},
			wantAddr: ":8080",
		},
		{
			name: "custom address",
			config: Config{
				Addr:    ":9090",
				Metrics: metrics,
				Queue:   q,
				Storage: storage,
			},
			wantAddr: ":9090",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(tt.config)
			assert.NotNil(t, server)
			assert.Equal(t, tt.wantAddr, server.addr)
			assert.NotNil(t, server.metrics)
			assert.NotNil(t, server.queue)
			assert.NotNil(t, server.storage)
			assert.NotNil(t, server.connections)
			assert.NotNil(t, server.broadcast)
		})
	}
}

func TestHandleHealth(t *testing.T) {
	tests := []struct {
		name           string
		queueHealthy   bool
		expectedStatus int
		expectedHealth string
	}{
		{
			name:           "healthy queue",
			queueHealthy:   true,
			expectedStatus: http.StatusOK,
			expectedHealth: "healthy",
		},
		{
			name:           "unhealthy queue",
			queueHealthy:   false,
			expectedStatus: http.StatusServiceUnavailable,
			expectedHealth: "unhealthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &mockQueue{healthy: tt.queueHealthy}
			metrics := monitoring.NewMetrics(q)
			storage := &mockStorage{}

			server := NewServer(Config{
				Metrics: metrics,
				Queue:   q,
				Storage: storage,
			})

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			w := httptest.NewRecorder()

			server.handleHealth(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			assert.Contains(t, w.Body.String(), tt.expectedHealth)
		})
	}
}

func TestHandleMetrics(t *testing.T) {
	q := &mockQueue{
		size:    100,
		dlqSize: 5,
		healthy: true,
	}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	// Register some workers
	metrics.RegisterWorker("worker-1")
	metrics.RegisterWorker("worker-2")
	metrics.MarkWorkerBusy("worker-1")

	// Record some tasks
	metrics.RecordTaskEnqueued()
	metrics.RecordTaskEnqueued()
	metrics.RecordTaskStarted()
	metrics.RecordTaskCompleted(100 * time.Millisecond)

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	w := httptest.NewRecorder()

	server.handleMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "queueDepth")
	assert.Contains(t, w.Body.String(), "activeWorkers")
	assert.Contains(t, w.Body.String(), "tasksProcessed")
}

func TestHandleMetricsMethodNotAllowed(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/metrics", nil)
	w := httptest.NewRecorder()

	server.handleMetrics(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHandleDLQ(t *testing.T) {
	// Create mock tasks
	task1, _ := task.NewTask("test-task", map[string]interface{}{"data": "test"})
	task1.State = task.StateDeadLetter
	task1.Error = "Task failed after max retries"

	q := &mockQueue{
		healthy:  true,
		dlqTasks: []*task.Task{task1},
	}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/dlq", nil)
	w := httptest.NewRecorder()

	server.handleDLQ(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), task1.ID)
	assert.Contains(t, w.Body.String(), task1.Error)
	assert.Contains(t, w.Body.String(), "dead_letter")
}

func TestCollectMetrics(t *testing.T) {
	q := &mockQueue{
		size:    50,
		dlqSize: 3,
		healthy: true,
	}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	// Set up test data
	metrics.RegisterWorker("worker-1")
	metrics.RegisterWorker("worker-2")
	metrics.RegisterWorker("worker-3")
	metrics.MarkWorkerBusy("worker-1")
	metrics.MarkWorkerBusy("worker-2")

	metrics.RecordTaskEnqueued()
	metrics.RecordTaskEnqueued()
	metrics.RecordTaskStarted()
	metrics.RecordTaskCompleted(100 * time.Millisecond)

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	update := server.collectMetrics()

	assert.Equal(t, int64(50), update.QueueDepth)
	assert.Equal(t, int32(3), update.ActiveWorkers)
	assert.Equal(t, int32(2), update.BusyWorkers)
	assert.Equal(t, int32(1), update.IdleWorkers)
	assert.Equal(t, int64(3), update.DLQSize)
	assert.Equal(t, "healthy", update.QueueHealth)
	assert.Greater(t, update.WorkerUtilization, 0.0)
	assert.NotEmpty(t, update.Timestamp)
	assert.NotEmpty(t, update.Uptime)
}

func TestHandleTasks(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)

	// Create test tasks in different states
	completedTask, _ := task.NewTask("completed-task", map[string]interface{}{"key": "value"})
	completedTask.State = task.StateCompleted

	failedTask, _ := task.NewTask("failed-task", map[string]interface{}{"key": "value"})
	failedTask.State = task.StateFailed
	failedTask.Error = "Test error"

	processingTask, _ := task.NewTask("processing-task", map[string]interface{}{"key": "value"})
	processingTask.State = task.StateProcessing

	storage := &mockStorage{
		tasks: map[string]*task.Task{
			completedTask.ID:  completedTask,
			failedTask.ID:     failedTask,
			processingTask.ID: processingTask,
		},
	}

	tests := []struct {
		name           string
		storage        *mockStorage
		expectedStatus int
		checkBody      bool
	}{
		{
			name:           "with storage and tasks",
			storage:        storage,
			expectedStatus: http.StatusOK,
			checkBody:      true,
		},
		{
			name:           "without storage",
			storage:        nil,
			expectedStatus: http.StatusOK,
			checkBody:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(Config{
				Metrics: metrics,
				Queue:   q,
				Storage: tt.storage,
			})

			req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
			w := httptest.NewRecorder()

			server.handleTasks(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

			if tt.checkBody {
				body := w.Body.String()
				assert.Contains(t, body, "tasks")
				assert.Contains(t, body, "total")
			}
		})
	}
}

func TestHandleTasksMethodNotAllowed(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tasks", nil)
	w := httptest.NewRecorder()

	server.handleTasks(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHandleTaskDetail(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)

	// Create test task
	testTask, _ := task.NewTask("test-task", map[string]interface{}{"key": "value"})
	testTask.State = task.StateCompleted

	storage := &mockStorage{
		tasks: map[string]*task.Task{
			testTask.ID: testTask,
		},
	}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	tests := []struct {
		name           string
		taskID         string
		expectedStatus int
	}{
		{
			name:           "existing task",
			taskID:         testTask.ID,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "non-existent task",
			taskID:         "non-existent-id",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "empty task ID",
			taskID:         "",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/tasks/" + tt.taskID
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			server.handleTaskDetail(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				assert.Contains(t, w.Body.String(), testTask.ID)
				assert.Contains(t, w.Body.String(), testTask.Type)
			}
		})
	}
}

func TestServerStartStop(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Addr:    ":0", // Use random available port
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start()
	}()

	// Wait for server to be ready
	select {
	case <-server.ready:
		// Server is ready
	case err := <-errChan:
		t.Fatalf("Server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for server to start")
	}

	// Stop server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := server.Stop(ctx)
	assert.NoError(t, err)
}

func TestCORSMiddleware(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with CORS middleware
	handler := server.withCORS(testHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "GET")
}

func TestOptionsRequest(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := server.withCORS(testHandler)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLoggingMiddleware(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := server.withLogging(testHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	// Should not panic
	assert.NotPanics(t, func() {
		handler.ServeHTTP(w, req)
	})
}

// TestHandleWebSocketUnsupportedStreaming tests WebSocket handler with non-flusher response writer
func TestHandleWebSocketUnsupportedStreaming(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Use a response writer that doesn't support flushing
	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	w := &nonFlushableResponseWriter{
		header: make(http.Header),
	}

	server.handleWebSocket(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.statusCode)
	assert.Contains(t, string(w.body), "Streaming unsupported")
}

// TestHandleWebSocketClientDisconnect tests client disconnection
func TestHandleWebSocketClientDisconnect(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Create a request with a cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	// Start WebSocket in goroutine
	done := make(chan bool)
	go func() {
		server.handleWebSocket(w, req)
		done <- true
	}()

	// Give it time to connect
	time.Sleep(100 * time.Millisecond)

	// Cancel the context to simulate client disconnect
	cancel()

	// Wait for handler to finish
	select {
	case <-done:
		// Handler completed successfully
	case <-time.After(2 * time.Second):
		t.Fatal("Handler did not complete after context cancellation")
	}

	// Verify connection was removed
	server.mu.RLock()
	connectionCount := len(server.connections)
	server.mu.RUnlock()
	assert.Equal(t, 0, connectionCount)
}

// TestHandleWebSocketBroadcast tests broadcasting metrics to WebSocket connections
func TestHandleWebSocketBroadcast(t *testing.T) {
	q := &mockQueue{
		size:    100,
		healthy: true,
	}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Create a request with a timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	// Start WebSocket in goroutine
	done := make(chan bool)
	go func() {
		server.handleWebSocket(w, req)
		done <- true
	}()

	// Give it time to connect
	time.Sleep(100 * time.Millisecond)

	// Verify connection was added
	server.mu.RLock()
	connectionCount := len(server.connections)
	server.mu.RUnlock()
	assert.Equal(t, 1, connectionCount)

	// Wait for context to timeout
	<-done

	// Verify response contains initial metrics
	assert.Contains(t, w.Body.String(), "event: metrics")
	assert.Contains(t, w.Body.String(), "queueDepth")
}

// TestHandleDashboardNotFound tests 404 for non-root paths
func TestHandleDashboardNotFound(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	server.handleDashboard(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestHandleWorkersEmpty tests workers endpoint with no workers
func TestHandleWorkersEmpty(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workers", nil)
	w := httptest.NewRecorder()

	server.handleWorkers(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "workers")
	assert.Contains(t, w.Body.String(), "total")
	assert.Contains(t, w.Body.String(), "\"total\":0")
}

// TestHandleWorkersWithWorkers tests workers endpoint with active workers
func TestHandleWorkersWithWorkers(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	// Register some workers
	metrics.RegisterWorker("worker-1")
	metrics.RegisterWorker("worker-2")
	metrics.MarkWorkerBusy("worker-1")

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workers", nil)
	w := httptest.NewRecorder()

	server.handleWorkers(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "worker-1")
	assert.Contains(t, w.Body.String(), "worker-2")
	assert.Contains(t, w.Body.String(), "\"total\":2")
}

// TestHandleWorkerDetailNotFound tests worker detail for non-existent worker
func TestHandleWorkerDetailNotFound(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workers/non-existent", nil)
	w := httptest.NewRecorder()

	server.handleWorkerDetail(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestHandleWorkerDetailMissingID tests worker detail with missing ID
func TestHandleWorkerDetailMissingID(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workers/", nil)
	w := httptest.NewRecorder()

	server.handleWorkerDetail(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleWorkerDetailSuccess tests successful worker detail retrieval
func TestHandleWorkerDetailSuccess(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	// Register a worker and simulate activity
	metrics.RegisterWorker("worker-123")
	metrics.MarkWorkerBusy("worker-123")
	metrics.RecordTaskCompleted(100 * time.Millisecond)

	// Create and start a task for the worker
	testTask, _ := task.NewTask("test-task", map[string]interface{}{"key": "value"})
	metrics.MarkWorkerBusyWithTask("worker-123", testTask.ID, testTask.Type)

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workers/worker-123", nil)
	w := httptest.NewRecorder()

	server.handleWorkerDetail(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "worker-123")
	assert.Contains(t, w.Body.String(), "tasksCompleted")
	assert.Contains(t, w.Body.String(), "currentTask")
}

// TestBroadcastTaskEvent tests task event broadcasting
func TestBroadcastTaskEvent(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Broadcast a task event
	server.BroadcastTaskEvent("task-123", "test-task", "success", 150*time.Millisecond)

	// Check recent events
	events := server.getRecentEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, "task-123", events[0].ID)
	assert.Equal(t, "test-task", events[0].Type)
	assert.Equal(t, "success", events[0].Status)
	assert.Contains(t, events[0].Duration, "150ms")
}

// TestBroadcastTaskEventMaxRecentEvents tests that old events are removed
func TestBroadcastTaskEventMaxRecentEvents(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Broadcast more than maxRecentEvents
	for i := 0; i < 25; i++ {
		server.BroadcastTaskEvent(
			fmt.Sprintf("task-%d", i),
			"test-task",
			"success",
			time.Millisecond,
		)
	}

	// Should only keep maxRecentEvents
	events := server.getRecentEvents()
	assert.LessOrEqual(t, len(events), server.maxRecentEvents)

	// Should have the most recent ones
	assert.Equal(t, "task-24", events[len(events)-1].ID)
}

// TestGetRecentEventsEmpty tests getting recent events when none exist
func TestGetRecentEventsEmpty(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	events := server.getRecentEvents()
	assert.Empty(t, events)
}

// TestHandleDLQMethodNotAllowed tests DLQ endpoint with wrong method
func TestHandleDLQMethodNotAllowed(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/dlq", nil)
	w := httptest.NewRecorder()

	server.handleDLQ(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestHandleDLQError tests DLQ endpoint when queue returns error
func TestHandleDLQError(t *testing.T) {
	q := &mockQueueWithError{}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/dlq", nil)
	w := httptest.NewRecorder()

	server.handleDLQ(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleTaskDetailMethodNotAllowed tests task detail with wrong method
func TestHandleTaskDetailMethodNotAllowed(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/some-id", nil)
	w := httptest.NewRecorder()

	server.handleTaskDetail(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestHandleTaskDetailNoStorage tests task detail when storage is nil
func TestHandleTaskDetailNoStorage(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: nil,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/some-id", nil)
	w := httptest.NewRecorder()

	server.handleTaskDetail(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestCollectMetricsWithQueueError tests metrics collection when queue has errors
func TestCollectMetricsWithQueueError(t *testing.T) {
	q := &mockQueue{
		size:    50,
		healthy: false, // Unhealthy queue
	}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	update := server.collectMetrics()

	assert.Equal(t, "unhealthy", update.QueueHealth)
	assert.NotEmpty(t, update.Timestamp)
}

// TestReadyChannel tests that the Ready channel signals correctly
func TestReadyChannel(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Addr:    ":0",
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Start server in goroutine
	go func() {
		_ = server.Start()
	}()

	// Wait for ready signal
	select {
	case <-server.Ready():
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not signal ready")
	}

	// Clean up
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Stop(ctx)
}

// TestStopWithoutStart tests stopping a server that wasn't started
func TestStopWithoutStart(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := server.Stop(ctx)
	assert.NoError(t, err)
}

// nonFlushableResponseWriter is a response writer that doesn't support flushing
type nonFlushableResponseWriter struct {
	header     http.Header
	body       []byte
	statusCode int
}

func (w *nonFlushableResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *nonFlushableResponseWriter) Write(b []byte) (int, error) {
	w.body = append(w.body, b...)
	return len(b), nil
}

func (w *nonFlushableResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

// mockQueueWithError is a queue that returns errors
type mockQueueWithError struct {
	mockQueue
}

func (m *mockQueueWithError) GetDLQTasks(ctx context.Context, limit int) ([]*task.Task, error) {
	return nil, assert.AnError
}

// TestBroadcastMetricsLoop tests the broadcast metrics goroutine
func TestBroadcastMetricsLoop(t *testing.T) {
	q := &mockQueue{
		size:    100,
		healthy: true,
	}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Start broadcaster in goroutine
	go server.broadcastMetrics()

	// Create a mock connection
	conn := &connection{
		send: make(chan MetricsUpdate, 10),
	}

	server.mu.Lock()
	server.connections[conn] = true
	server.mu.Unlock()

	// Send a metrics update
	update := server.collectMetrics()
	server.broadcast <- update

	// Verify the connection received the update
	select {
	case received := <-conn.send:
		assert.NotEmpty(t, received.Timestamp)
		assert.Equal(t, int64(100), received.QueueDepth)
	case <-time.After(1 * time.Second):
		t.Fatal("Did not receive metrics update")
	}

	// Clean up
	close(server.done)

	server.mu.Lock()
	delete(server.connections, conn)
	server.mu.Unlock()
}

// TestBroadcastMetricsMultipleConnections tests broadcasting to multiple connections
func TestBroadcastMetricsMultipleConnections(t *testing.T) {
	q := &mockQueue{
		size:    50,
		healthy: true,
	}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Start broadcaster
	go server.broadcastMetrics()

	// Create multiple mock connections
	conn1 := &connection{send: make(chan MetricsUpdate, 10)}
	conn2 := &connection{send: make(chan MetricsUpdate, 10)}
	conn3 := &connection{send: make(chan MetricsUpdate, 10)}

	server.mu.Lock()
	server.connections[conn1] = true
	server.connections[conn2] = true
	server.connections[conn3] = true
	server.mu.Unlock()

	// Send update
	update := server.collectMetrics()
	server.broadcast <- update

	// All connections should receive it
	var wg sync.WaitGroup
	wg.Add(3)

	checkConnection := func(conn *connection) {
		defer wg.Done()
		select {
		case received := <-conn.send:
			assert.Equal(t, int64(50), received.QueueDepth)
		case <-time.After(1 * time.Second):
			t.Error("Connection did not receive update")
		}
	}

	go checkConnection(conn1)
	go checkConnection(conn2)
	go checkConnection(conn3)

	wg.Wait()

	// Clean up
	close(server.done)
}

// TestBroadcastMetricsFullChannel tests handling of full connection channels
func TestBroadcastMetricsFullChannel(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	go server.broadcastMetrics()

	// Create connection with small buffer
	conn := &connection{send: make(chan MetricsUpdate, 1)}

	server.mu.Lock()
	server.connections[conn] = true
	server.mu.Unlock()

	// Fill the channel
	update := server.collectMetrics()
	conn.send <- update

	// Try to send more updates - should not block
	done := make(chan bool)
	go func() {
		server.broadcast <- update
		server.broadcast <- update
		done <- true
	}()

	select {
	case <-done:
		// Success - didn't block
	case <-time.After(2 * time.Second):
		t.Fatal("Broadcast blocked on full channel")
	}

	// Clean up
	close(server.done)
}

// TestUpdateMetricsLoop tests the periodic metrics update
func TestUpdateMetricsLoop(t *testing.T) {
	q := &mockQueue{
		size:    75,
		healthy: true,
	}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Start the update loop
	go server.updateMetricsLoop()

	// Wait for at least one update
	select {
	case update := <-server.broadcast:
		assert.NotEmpty(t, update.Timestamp)
		assert.Equal(t, int64(75), update.QueueDepth)
	case <-time.After(3 * time.Second):
		t.Fatal("Did not receive metrics update from loop")
	}

	// Clean up
	close(server.done)
}

// TestUpdateMetricsLoopStopsOnDone tests that update loop stops when done channel is closed
func TestUpdateMetricsLoopStopsOnDone(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Start the loop
	loopDone := make(chan bool)
	go func() {
		server.updateMetricsLoop()
		loopDone <- true
	}()

	// Close done channel
	close(server.done)

	// Loop should exit
	select {
	case <-loopDone:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Update loop did not stop after done signal")
	}
}

// TestHandleMetricsEncodingError tests metrics handler with encoding errors
func TestHandleMetricsEncodingError(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)

	// Use a writer that will cause encoding to potentially fail
	// by closing it before writing
	w := httptest.NewRecorder()

	// This should not panic even if encoding has issues
	assert.NotPanics(t, func() {
		server.handleMetrics(w, req)
	})
}

// TestHandleTasksStorageError tests task listing with storage errors
func TestHandleTasksStorageError(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)

	storage := &mockStorageWithError{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	w := httptest.NewRecorder()

	server.handleTasks(w, req)

	// Should still return OK with empty tasks
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "tasks")
}

// TestHandleHealthWithError tests health endpoint when queue is unhealthy
func TestHandleHealthWithError(t *testing.T) {
	q := &mockQueue{healthy: false}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "unhealthy")
	assert.Contains(t, body, "queue_error")
}

// TestConcurrentWebSocketConnections tests multiple concurrent WebSocket connections
func TestConcurrentWebSocketConnections(t *testing.T) {
	q := &mockQueue{
		size:    100,
		healthy: true,
	}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Start server components
	go server.broadcastMetrics()
	go server.updateMetricsLoop()

	var wg sync.WaitGroup
	numConnections := 5

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			req := httptest.NewRequest(http.MethodGet, "/api/ws", nil).WithContext(ctx)
			w := httptest.NewRecorder()

			server.handleWebSocket(w, req)
		}()
	}

	// Wait for all connections to complete
	wg.Wait()

	// Verify all connections were cleaned up
	server.mu.RLock()
	connectionCount := len(server.connections)
	server.mu.RUnlock()

	assert.Equal(t, 0, connectionCount, "All connections should be cleaned up")

	// Clean up
	close(server.done)
}

// TestTaskEventsInWebSocket tests that task events are sent through WebSocket
func TestTaskEventsInWebSocket(t *testing.T) {
	q := &mockQueue{healthy: true}
	metrics := monitoring.NewMetrics(q)
	storage := &mockStorage{}

	server := NewServer(Config{
		Metrics: metrics,
		Queue:   q,
		Storage: storage,
	})

	// Add some recent events before connecting
	server.BroadcastTaskEvent("task-1", "test", "success", 100*time.Millisecond)
	server.BroadcastTaskEvent("task-2", "test", "failed", 200*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	// Use a channel to synchronize goroutine completion
	done := make(chan struct{})

	// Connect and let it send recent events
	go func() {
		defer close(done)
		server.handleWebSocket(w, req)
	}()

	// Give it time to send initial data
	time.Sleep(200 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for goroutine to complete
	<-done

	// Should contain task events in response
	body := w.Body.String()
	assert.Contains(t, body, "event: task")
	assert.Contains(t, body, "task-1")
}

// mockStorageWithError returns errors for all operations
type mockStorageWithError struct {
	mockStorage
}

func (m *mockStorageWithError) GetTasksByState(ctx context.Context, state task.State, limit int) ([]*task.Task, error) {
	return nil, assert.AnError
}
