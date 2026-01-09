package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/farhan-ahmed1/spool/internal/task"
)

func TestBroker_handleSubmit(t *testing.T) {
	tests := []struct {
		name           string
		payload        interface{}
		setupQueue     func()
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "valid task submission",
			payload: map[string]interface{}{
				"id":       "test-123",
				"type":     "email",
				"payload":  json.RawMessage(`{"to":"test@example.com"}`),
				"priority": 1,
			},
			setupQueue:     func() {}, // No error setup
			expectedStatus: http.StatusCreated,
			expectedBody:   `"status":"enqueued"`,
		},
		{
			name:           "invalid JSON payload",
			payload:        "invalid json",
			setupQueue:     func() {},
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid task format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broker, mockQueue, _ := setupTestBroker(t)

			// Setup queue error if needed
			if tt.name == "queue enqueue error" {
				mockQueue.setEnqueueError(errors.New("queue error"))
			}
			tt.setupQueue()

			// Create request
			var body []byte
			var err error
			if tt.payload != "invalid json" {
				body, err = json.Marshal(tt.payload)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				body = []byte("invalid json")
			}

			req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Execute
			broker.handleSubmit(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("handleSubmit() status = %v, want %v", w.Code, tt.expectedStatus)
			}

			if !strings.Contains(w.Body.String(), tt.expectedBody) {
				t.Errorf("handleSubmit() body = %v, want to contain %v", w.Body.String(), tt.expectedBody)
			}

			// Reset queue error
			mockQueue.setEnqueueError(nil)
		})
	}

	// Test with queue enqueue error
	t.Run("queue enqueue error", func(t *testing.T) {
		broker, mockQueue, _ := setupTestBroker(t)
		mockQueue.setEnqueueError(errors.New("queue error"))

		payload := map[string]interface{}{
			"id":   "test-123",
			"type": "email",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		broker.handleSubmit(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("handleSubmit() status = %v, want %v", w.Code, http.StatusInternalServerError)
		}

		if !strings.Contains(w.Body.String(), "Failed to enqueue task") {
			t.Errorf("handleSubmit() body = %v, want to contain 'Failed to enqueue task'", w.Body.String())
		}
	})
}

func TestBroker_handleBatchSubmit(t *testing.T) {
	broker, mockQueue, _ := setupTestBroker(t)

	tests := []struct {
		name            string
		method          string
		payload         interface{}
		setEnqueueError bool
		expectedStatus  int
		expectedBody    string
	}{
		{
			name:   "valid batch submission",
			method: http.MethodPost,
			payload: []map[string]interface{}{
				{
					"id":   "task-1",
					"type": "email",
				},
				{
					"id":   "task-2",
					"type": "sms",
				},
			},
			setEnqueueError: false,
			expectedStatus:  http.StatusCreated,
			expectedBody:    `"enqueued":2`,
		},
		{
			name:            "invalid method",
			method:          http.MethodGet,
			payload:         nil,
			setEnqueueError: false,
			expectedStatus:  http.StatusMethodNotAllowed,
			expectedBody:    "Method not allowed",
		},
		{
			name:            "invalid JSON",
			method:          http.MethodPost,
			payload:         "invalid",
			setEnqueueError: false,
			expectedStatus:  http.StatusBadRequest,
			expectedBody:    "Invalid task format",
		},
		{
			name:   "partial failure in batch",
			method: http.MethodPost,
			payload: []map[string]interface{}{
				{"id": "task-1", "type": "email"},
				{"id": "task-2", "type": "sms"},
			},
			setEnqueueError: true,
			expectedStatus:  http.StatusCreated,
			expectedBody:    `"enqueued":0`, // No tasks enqueued due to error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup queue error if needed
			if tt.setEnqueueError {
				mockQueue.setEnqueueError(errors.New("queue error"))
			}

			// Create request
			var body []byte
			var err error
			if tt.payload != nil && tt.payload != "invalid" {
				body, err = json.Marshal(tt.payload)
				if err != nil {
					t.Fatal(err)
				}
			} else if tt.payload == "invalid" {
				body = []byte("invalid json")
			}

			req := httptest.NewRequest(tt.method, "/api/tasks/batch", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Execute
			broker.handleBatchSubmit(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("handleBatchSubmit() status = %v, want %v", w.Code, tt.expectedStatus)
			}

			if !strings.Contains(w.Body.String(), tt.expectedBody) {
				t.Errorf("handleBatchSubmit() body = %v, want to contain %v", w.Body.String(), tt.expectedBody)
			}

			// Reset
			mockQueue.setEnqueueError(nil)
		})
	}
}

func TestBroker_handleList(t *testing.T) {
	broker, _, mockStorage := setupTestBroker(t)

	tests := []struct {
		name           string
		setupStorage   func()
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "successful task listing",
			setupStorage: func() {
				// Task will be added in test execution
			},
			expectedStatus: http.StatusOK,
			expectedBody:   `"tasks"`,
		},
		{
			name: "storage error",
			setupStorage: func() {
				// Error will be set in test execution
			},
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   "Failed to list tasks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup storage error if needed
			if tt.name == "storage error" {
				mockStorage.setGetTasksByStateError(errors.New("storage error"))
			}

			req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
			w := httptest.NewRecorder()

			// Execute
			broker.handleList(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("handleList() status = %v, want %v", w.Code, tt.expectedStatus)
			}

			if !strings.Contains(w.Body.String(), tt.expectedBody) {
				t.Errorf("handleList() body = %v, want to contain %v", w.Body.String(), tt.expectedBody)
			}

			// Reset
			mockStorage.setGetTasksByStateError(nil)
		})
	}
}

func TestBroker_handleListWithoutStorage(t *testing.T) {
	// Create broker without storage
	mockQueue := newMockQueue()
	broker := NewBroker(Config{
		Addr:    ":0",
		Queue:   mockQueue,
		Storage: nil, // No storage
	})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	w := httptest.NewRecorder()

	broker.handleList(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("handleList() without storage status = %v, want %v", w.Code, http.StatusServiceUnavailable)
	}

	if !strings.Contains(w.Body.String(), "Storage not configured") {
		t.Errorf("handleList() without storage body = %v, want to contain 'Storage not configured'", w.Body.String())
	}
}

func TestBroker_handleTaskByID(t *testing.T) {
	broker, _, mockStorage := setupTestBroker(t)

	tests := []struct {
		name           string
		method         string
		url            string
		setupStorage   func()
		expectedStatus int
		expectedBody   string
	}{
		{
			name:   "valid task retrieval",
			method: http.MethodGet,
			url:    "/api/tasks/test-task-1",
			setupStorage: func() {
				// Task will be added in test execution
			},
			expectedStatus: http.StatusOK,
			expectedBody:   `"id":"test-task-1"`,
		},
		{
			name:           "invalid method",
			method:         http.MethodPost,
			url:            "/api/tasks/test-task-1",
			setupStorage:   func() {},
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method not allowed",
		},
		{
			name:           "empty task ID",
			method:         http.MethodGet,
			url:            "/api/tasks/",
			setupStorage:   func() {},
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Task ID required",
		},
		{
			name:   "task not found",
			method: http.MethodGet,
			url:    "/api/tasks/nonexistent",
			setupStorage: func() {
				// Error will be set in test execution
			},
			expectedStatus: http.StatusNotFound,
			expectedBody:   "Task not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup storage for specific test cases
			switch tt.name {
			case "valid task retrieval":
				testTask := &task.Task{
					ID:   "test-task-1",
					Type: "email",
				}
				mockStorage.SaveTask(context.TODO(), testTask)
			case "task not found":
				mockStorage.setGetTaskError(errors.New("not found"))
			}

			req := httptest.NewRequest(tt.method, tt.url, nil)
			w := httptest.NewRecorder()

			// Execute
			broker.handleTaskByID(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("handleTaskByID() status = %v, want %v", w.Code, tt.expectedStatus)
			}

			if !strings.Contains(w.Body.String(), tt.expectedBody) {
				t.Errorf("handleTaskByID() body = %v, want to contain %v", w.Body.String(), tt.expectedBody)
			}

			// Reset
			mockStorage.setGetTaskError(nil)
		})
	}
}

func TestBroker_handleTaskByIDWithoutStorage(t *testing.T) {
	// Create broker without storage
	mockQueue := newMockQueue()
	broker := NewBroker(Config{
		Addr:    ":0",
		Queue:   mockQueue,
		Storage: nil, // No storage
	})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/test-task-1", nil)
	w := httptest.NewRecorder()

	broker.handleTaskByID(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("handleTaskByID() without storage status = %v, want %v", w.Code, http.StatusServiceUnavailable)
	}
}

func TestBroker_handleQueueStats(t *testing.T) {
	broker, mockQueue, _ := setupTestBroker(t)

	tests := []struct {
		name           string
		method         string
		setupQueue     func()
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "valid stats request",
			method:         http.MethodGet,
			setupQueue:     func() {},
			expectedStatus: http.StatusOK,
			expectedBody:   `"queue_size"`,
		},
		{
			name:           "invalid method",
			method:         http.MethodPost,
			setupQueue:     func() {},
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method not allowed",
		},
		{
			name:   "queue error",
			method: http.MethodGet,
			setupQueue: func() {
				// Error will be set in test execution
			},
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   "Failed to get queue stats",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup queue error if needed
			if tt.name == "queue error" {
				mockQueue.setSizeError(errors.New("queue error"))
			}

			req := httptest.NewRequest(tt.method, "/api/queue/stats", nil)
			w := httptest.NewRecorder()

			// Execute
			broker.handleQueueStats(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("handleQueueStats() status = %v, want %v", w.Code, tt.expectedStatus)
			}

			if !strings.Contains(w.Body.String(), tt.expectedBody) {
				t.Errorf("handleQueueStats() body = %v, want to contain %v", w.Body.String(), tt.expectedBody)
			}

			// Reset
			mockQueue.setSizeError(nil)
		})
	}
}

func TestBroker_handlePurge(t *testing.T) {
	broker, mockQueue, _ := setupTestBroker(t)

	tests := []struct {
		name           string
		method         string
		setupQueue     func()
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "valid purge request",
			method:         http.MethodPost,
			setupQueue:     func() {},
			expectedStatus: http.StatusOK,
			expectedBody:   `"status":"purged"`,
		},
		{
			name:           "invalid method",
			method:         http.MethodGet,
			setupQueue:     func() {},
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method not allowed",
		},
		{
			name:   "queue purge error",
			method: http.MethodPost,
			setupQueue: func() {
				// Error will be set in test execution
			},
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   "Failed to purge queue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup queue error if needed
			if tt.name == "queue purge error" {
				mockQueue.setPurgeError(errors.New("purge error"))
			}

			req := httptest.NewRequest(tt.method, "/api/queue/purge", nil)
			w := httptest.NewRecorder()

			// Execute
			broker.handlePurge(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("handlePurge() status = %v, want %v", w.Code, tt.expectedStatus)
			}

			if !strings.Contains(w.Body.String(), tt.expectedBody) {
				t.Errorf("handlePurge() body = %v, want to contain %v", w.Body.String(), tt.expectedBody)
			}

			// Reset
			mockQueue.setPurgeError(nil)
		})
	}
}

func TestBroker_handleHealth(t *testing.T) {
	broker, mockQueue, _ := setupTestBroker(t)

	tests := []struct {
		name           string
		setupQueue     func()
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "healthy service",
			setupQueue:     func() {},
			expectedStatus: http.StatusOK,
			expectedBody:   `"status":"healthy"`,
		},
		{
			name: "unhealthy queue",
			setupQueue: func() {
				// Error will be set in test execution
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   `"status":"unhealthy"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup queue error if needed
			if tt.name == "unhealthy queue" {
				mockQueue.setHealthError(errors.New("queue unhealthy"))
			}

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			w := httptest.NewRecorder()

			// Execute
			broker.handleHealth(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("handleHealth() status = %v, want %v", w.Code, tt.expectedStatus)
			}

			if !strings.Contains(w.Body.String(), tt.expectedBody) {
				t.Errorf("handleHealth() body = %v, want to contain %v", w.Body.String(), tt.expectedBody)
			}

			// Reset
			mockQueue.setHealthError(nil)
		})
	}
}

func TestBroker_handleTasks(t *testing.T) {
	broker, _, _ := setupTestBroker(t)

	tests := []struct {
		name           string
		method         string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "POST method calls handleSubmit",
			method:         http.MethodPost,
			expectedStatus: http.StatusBadRequest, // Will fail JSON decode but routing works
			expectedBody:   "Invalid task format",
		},
		{
			name:           "GET method calls handleList",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK, // handleList will return tasks (empty)
			expectedBody:   `"tasks"`,
		},
		{
			name:           "invalid method",
			method:         http.MethodPut,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/tasks", nil)
			w := httptest.NewRecorder()

			// Execute
			broker.handleTasks(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("handleTasks() status = %v, want %v", w.Code, tt.expectedStatus)
			}

			if !strings.Contains(w.Body.String(), tt.expectedBody) {
				t.Errorf("handleTasks() body = %v, want to contain %v", w.Body.String(), tt.expectedBody)
			}
		})
	}
}
