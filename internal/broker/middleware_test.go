package broker

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBroker_withLogging(t *testing.T) {
	broker, _, _ := setupTestBroker(t)

	// Create a test handler that we can wrap
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	// Wrap the handler with logging middleware
	wrappedHandler := broker.withLogging(testHandler)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "GET request logging",
			method: http.MethodGet,
			path:   "/api/tasks",
		},
		{
			name:   "POST request logging",
			method: http.MethodPost,
			path:   "/api/tasks",
		},
		{
			name:   "PUT request logging",
			method: http.MethodPut,
			path:   "/api/tasks/123",
		},
		{
			name:   "DELETE request logging",
			method: http.MethodDelete,
			path:   "/api/tasks/123",
		},
		{
			name:   "health check logging",
			method: http.MethodGet,
			path:   "/health",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			// Execute wrapped handler
			wrappedHandler.ServeHTTP(w, req)

			// Verify the original handler was called
			if w.Code != http.StatusOK {
				t.Errorf("withLogging() status = %v, want %v", w.Code, http.StatusOK)
			}

			if w.Body.String() != "test response" {
				t.Errorf("withLogging() body = %v, want 'test response'", w.Body.String())
			}

			// Note: We can't easily test the actual logging output without
			// capturing logger output, but we can verify the handler chain works
		})
	}
}

func TestBroker_withCORS(t *testing.T) {
	broker, _, _ := setupTestBroker(t)

	// Create a test handler that we can wrap
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	// Wrap the handler with CORS middleware
	wrappedHandler := broker.withCORS(testHandler)

	tests := []struct {
		name                  string
		method                string
		expectedStatus        int
		expectedBody          string
		checkCORSHeaders      bool
		expectedAccessControl map[string]string
	}{
		{
			name:             "OPTIONS request",
			method:           http.MethodOptions,
			expectedStatus:   http.StatusOK,
			expectedBody:     "",
			checkCORSHeaders: true,
			expectedAccessControl: map[string]string{
				"Access-Control-Allow-Origin":  "*",
				"Access-Control-Allow-Methods": "GET, POST, PUT, DELETE, OPTIONS",
				"Access-Control-Allow-Headers": "Content-Type, Authorization",
			},
		},
		{
			name:             "GET request with CORS",
			method:           http.MethodGet,
			expectedStatus:   http.StatusOK,
			expectedBody:     "test response",
			checkCORSHeaders: true,
			expectedAccessControl: map[string]string{
				"Access-Control-Allow-Origin":  "*",
				"Access-Control-Allow-Methods": "GET, POST, PUT, DELETE, OPTIONS",
				"Access-Control-Allow-Headers": "Content-Type, Authorization",
			},
		},
		{
			name:             "POST request with CORS",
			method:           http.MethodPost,
			expectedStatus:   http.StatusOK,
			expectedBody:     "test response",
			checkCORSHeaders: true,
			expectedAccessControl: map[string]string{
				"Access-Control-Allow-Origin":  "*",
				"Access-Control-Allow-Methods": "GET, POST, PUT, DELETE, OPTIONS",
				"Access-Control-Allow-Headers": "Content-Type, Authorization",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request
			req := httptest.NewRequest(tt.method, "/api/tasks", nil)
			w := httptest.NewRecorder()

			// Execute wrapped handler
			wrappedHandler.ServeHTTP(w, req)

			// Verify status code
			if w.Code != tt.expectedStatus {
				t.Errorf("withCORS() status = %v, want %v", w.Code, tt.expectedStatus)
			}

			// Verify response body
			if w.Body.String() != tt.expectedBody {
				t.Errorf("withCORS() body = %v, want %v", w.Body.String(), tt.expectedBody)
			}

			// Check CORS headers
			if tt.checkCORSHeaders {
				for header, expectedValue := range tt.expectedAccessControl {
					actualValue := w.Header().Get(header)
					if actualValue != expectedValue {
						t.Errorf("withCORS() header %s = %v, want %v", header, actualValue, expectedValue)
					}
				}
			}
		})
	}
}

func TestBroker_middlewareChain(t *testing.T) {
	broker, _, _ := setupTestBroker(t)

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("chained response"))
	})

	// Apply both middlewares (as done in broker.Start())
	wrappedHandler := broker.withLogging(broker.withCORS(testHandler))

	tests := []struct {
		name           string
		method         string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "OPTIONS through middleware chain",
			method:         http.MethodOptions,
			expectedStatus: http.StatusOK,
			expectedBody:   "", // OPTIONS returns empty body
		},
		{
			name:           "GET through middleware chain",
			method:         http.MethodGet,
			expectedStatus: http.StatusCreated,
			expectedBody:   "chained response",
		},
		{
			name:           "POST through middleware chain",
			method:         http.MethodPost,
			expectedStatus: http.StatusCreated,
			expectedBody:   "chained response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/test", nil)
			w := httptest.NewRecorder()

			// Execute the middleware chain
			wrappedHandler.ServeHTTP(w, req)

			// Verify status
			if w.Code != tt.expectedStatus {
				t.Errorf("middleware chain status = %v, want %v", w.Code, tt.expectedStatus)
			}

			// Verify body
			if w.Body.String() != tt.expectedBody {
				t.Errorf("middleware chain body = %v, want %v", w.Body.String(), tt.expectedBody)
			}

			// Verify CORS headers are still present
			if w.Header().Get("Access-Control-Allow-Origin") != "*" {
				t.Error("middleware chain should preserve CORS headers")
			}
		})
	}
}

func TestBroker_middlewareWithNilHandler(t *testing.T) {
	broker, _, _ := setupTestBroker(t)

	// Test behavior when next handler is nil (edge case)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("middleware should handle nil handler gracefully, got panic: %v", r)
		}
	}()

	// This shouldn't be called in practice, but test defensive programming
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	// Create middlewares with nil handler - this will panic if not handled
	// In Go's http package, this is expected behavior
	corsHandler := broker.withCORS(nil)

	// This will panic, which is expected behavior in Go's http package
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when wrapping nil handler")
		}
	}()

	corsHandler.ServeHTTP(w, req)
}
