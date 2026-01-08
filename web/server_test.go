package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/monitoring"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/stretchr/testify/assert"
)

// mockQueue implements queue.Queue interface for testing
type mockQueue struct {
	size       int64
	dlqSize    int64
	dlqTasks   []*task.Task
	healthy    bool
}

func (m *mockQueue) Enqueue(ctx context.Context, t *task.Task) error { return nil }
func (m *mockQueue) Dequeue(ctx context.Context) (*task.Task, error) { return nil, queue.ErrNoTask }
func (m *mockQueue) Peek(ctx context.Context) (*task.Task, error)    { return nil, nil }
func (m *mockQueue) Ack(ctx context.Context, taskID string) error    { return nil }
func (m *mockQueue) Nack(ctx context.Context, taskID string, requeue bool) error { return nil }
func (m *mockQueue) Size(ctx context.Context) (int64, error)         { return m.size, nil }
func (m *mockQueue) SizeByPriority(ctx context.Context) (map[task.Priority]int64, error) {
	return nil, nil
}
func (m *mockQueue) Purge(ctx context.Context) error { return nil }
func (m *mockQueue) EnqueueDLQ(ctx context.Context, t *task.Task, reason string) error { return nil }
func (m *mockQueue) GetDLQSize(ctx context.Context) (int64, error) { return m.dlqSize, nil }
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

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

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
