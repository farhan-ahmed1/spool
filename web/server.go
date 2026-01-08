package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/farhan-ahmed1/spool/internal/monitoring"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
)

// Server provides a real-time web dashboard for monitoring the task queue system
type Server struct {
	metrics  *monitoring.Metrics
	queue    queue.Queue
	storage  storage.Storage
	addr     string
	server   *http.Server
	
	// WebSocket connections
	mu          sync.RWMutex
	connections map[*connection]bool
	broadcast   chan MetricsUpdate
	
	// Shutdown
	done chan struct{}
}

// connection represents a WebSocket client connection
type connection struct {
	ws   http.ResponseWriter
	send chan MetricsUpdate
}

// Config holds server configuration
type Config struct {
	Addr    string // e.g., ":8080"
	Metrics *monitoring.Metrics
	Queue   queue.Queue
	Storage storage.Storage
}

// MetricsUpdate represents real-time metrics data sent to dashboard clients
type MetricsUpdate struct {
	Timestamp         string            `json:"timestamp"`
	QueueDepth        int64             `json:"queueDepth"`
	QueueDepthHigh    int64             `json:"queueDepthHigh"`
	QueueDepthLow     int64             `json:"queueDepthLow"`
	QueueTrend        []QueueDataPoint  `json:"queueTrend"`
	ActiveWorkers     int32             `json:"activeWorkers"`
	IdleWorkers       int32             `json:"idleWorkers"`
	BusyWorkers       int32             `json:"busyWorkers"`
	WorkerUtilization float64           `json:"workerUtilization"`
	TasksProcessed    int64             `json:"tasksProcessed"`
	TasksFailed       int64             `json:"tasksFailed"`
	TasksRetried      int64             `json:"tasksRetried"`
	TasksEnqueued     int64             `json:"tasksEnqueued"`
	TasksInProgress   int32             `json:"tasksInProgress"`
	CurrentThroughput float64           `json:"currentThroughput"`
	AvgThroughput     float64           `json:"avgThroughput"`
	AvgProcessingTime string            `json:"avgProcessingTime"`
	P95ProcessingTime string            `json:"p95ProcessingTime"`
	P99ProcessingTime string            `json:"p99ProcessingTime"`
	Uptime            string            `json:"uptime"`
	OldestIdleTime    string            `json:"oldestIdleTime"`
	DLQSize           int64             `json:"dlqSize"`
	QueueHealth       string            `json:"queueHealth"`
}

// QueueDataPoint represents a point in the queue depth time series
type QueueDataPoint struct {
	Timestamp string `json:"timestamp"`
	Depth     int64  `json:"depth"`
}

// TaskDetail represents detailed information about a task
type TaskDetail struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	State       string                 `json:"state"`
	Priority    int                    `json:"priority"`
	RetryCount  int                    `json:"retryCount"`
	MaxRetries  int                    `json:"maxRetries"`
	CreatedAt   string                 `json:"createdAt"`
	StartedAt   string                 `json:"startedAt,omitempty"`
	CompletedAt string                 `json:"completedAt,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Payload     json.RawMessage        `json:"payload,omitempty"`
}

// NewServer creates a new dashboard server
func NewServer(cfg Config) *Server {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}

	return &Server{
		metrics:     cfg.Metrics,
		queue:       cfg.Queue,
		storage:     cfg.Storage,
		addr:        cfg.Addr,
		connections: make(map[*connection]bool),
		broadcast:   make(chan MetricsUpdate, 100),
		done:        make(chan struct{}),
	}
}

// Start starts the HTTP server and WebSocket broadcaster
func (s *Server) Start() error {
	mux := http.NewServeMux()
	
	// Serve dashboard HTML
	mux.HandleFunc("/", s.handleDashboard)
	
	// API endpoints
	mux.HandleFunc("/api/metrics", s.handleMetrics)
	mux.HandleFunc("/api/ws", s.handleWebSocket)
	mux.HandleFunc("/api/tasks", s.handleTasks)
	mux.HandleFunc("/api/tasks/", s.handleTaskDetail)
	mux.HandleFunc("/api/dlq", s.handleDLQ)
	
	// Health check
	mux.HandleFunc("/health", s.handleHealth)
	
	// Serve static files
	fs := http.FileServer(http.Dir("web/dashboard/static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))
	
	s.server = &http.Server{
		Addr:         s.addr,
		Handler:      s.withLogging(s.withCORS(mux)),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	
	// Start WebSocket broadcaster
	go s.broadcastMetrics()
	
	// Start metrics update loop
	go s.updateMetricsLoop()
	
	log.Printf("[Dashboard] Starting server on %s", s.addr)
	return s.server.ListenAndServe()
}

// Stop gracefully shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	close(s.done)
	
	// Close all WebSocket connections
	s.mu.Lock()
	for conn := range s.connections {
		close(conn.send)
	}
	s.connections = make(map[*connection]bool)
	s.mu.Unlock()
	
	return s.server.Shutdown(ctx)
}

// handleDashboard serves the main dashboard HTML page
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	
	tmplPath := filepath.Join("web", "dashboard", "templates", "index.html")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		log.Printf("[Dashboard] Error parsing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	
	data := map[string]interface{}{
		"Title": "Spool Dashboard",
	}
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("[Dashboard] Error executing template: %v", err)
	}
}

// handleMetrics returns current metrics as JSON
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	update := s.collectMetrics()
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(update); err != nil {
		log.Printf("[Dashboard] Failed to encode metrics: %v", err)
		http.Error(w, "Failed to encode metrics", http.StatusInternalServerError)
	}
}

// handleWebSocket handles WebSocket connections for real-time updates
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade to WebSocket using Server-Sent Events (SSE) for simplicity
	// In production, use gorilla/websocket for full WebSocket support
	
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	
	// Create connection
	conn := &connection{
		ws:   w,
		send: make(chan MetricsUpdate, 10),
	}
	
	// Register connection
	s.mu.Lock()
	s.connections[conn] = true
	s.mu.Unlock()
	
	// Send initial metrics
	initialMetrics := s.collectMetrics()
	data, _ := json.Marshal(initialMetrics)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
	
	// Listen for updates or client disconnect
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			s.mu.Lock()
			delete(s.connections, conn)
			close(conn.send)
			s.mu.Unlock()
			return
		case update := <-conn.send:
			data, err := json.Marshal(update)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// handleTasks returns a list of recent tasks
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	if s.storage == nil {
		// If storage not configured, return empty list
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": []TaskDetail{},
			"total": 0,
		})
		return
	}
	
	ctx := r.Context()
	
	// Fetch recent tasks from different states
	limit := 50 // Show last 50 tasks
	
	allTasks := make([]*task.Task, 0, limit)
	
	// Fetch completed tasks
	completedTasks, err := s.storage.GetTasksByState(ctx, task.StateCompleted, limit/3)
	if err == nil {
		allTasks = append(allTasks, completedTasks...)
	}
	
	// Fetch failed tasks
	failedTasks, err := s.storage.GetTasksByState(ctx, task.StateFailed, limit/3)
	if err == nil {
		allTasks = append(allTasks, failedTasks...)
	}
	
	// Fetch processing tasks
	processingTasks, err := s.storage.GetTasksByState(ctx, task.StateProcessing, limit/3)
	if err == nil {
		allTasks = append(allTasks, processingTasks...)
	}
	
	// Convert to TaskDetail
	details := make([]TaskDetail, 0, len(allTasks))
	for _, t := range allTasks {
		detail := TaskDetail{
			ID:         t.ID,
			Type:       t.Type,
			State:      string(t.State),
			Priority:   int(t.Priority),
			RetryCount: t.RetryCount,
			MaxRetries: t.MaxRetries,
			CreatedAt:  t.CreatedAt.Format(time.RFC3339),
			Error:      t.Error,
			Metadata:   t.Metadata,
		}
		
		if t.StartedAt != nil {
			detail.StartedAt = t.StartedAt.Format(time.RFC3339)
		}
		if t.CompletedAt != nil {
			detail.CompletedAt = t.CompletedAt.Format(time.RFC3339)
		}
		
		details = append(details, detail)
	}
	
	// Sort by creation time (most recent first)
	// Simple bubble sort for small datasets
	for i := 0; i < len(details)-1; i++ {
		for j := i + 1; j < len(details); j++ {
			if details[i].CreatedAt < details[j].CreatedAt {
				details[i], details[j] = details[j], details[i]
			}
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks": details,
		"total": len(details),
	})
}

// handleTaskDetail returns detailed information about a specific task
func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	taskID := r.URL.Path[len("/api/tasks/"):]
	if taskID == "" {
		http.Error(w, "Task ID required", http.StatusBadRequest)
		return
	}
	
	// Fetch task from storage
	if s.storage == nil {
		http.Error(w, "Storage not configured", http.StatusServiceUnavailable)
		return
	}
	
	ctx := r.Context()
	t, err := s.storage.GetTask(ctx, taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	
	detail := TaskDetail{
		ID:         t.ID,
		Type:       t.Type,
		State:      string(t.State),
		Priority:   int(t.Priority),
		RetryCount: t.RetryCount,
		MaxRetries: t.MaxRetries,
		CreatedAt:  t.CreatedAt.Format(time.RFC3339),
		Error:      t.Error,
		Metadata:   t.Metadata,
		Payload:    t.Payload,
	}
	
	if t.StartedAt != nil {
		detail.StartedAt = t.StartedAt.Format(time.RFC3339)
	}
	if t.CompletedAt != nil {
		detail.CompletedAt = t.CompletedAt.Format(time.RFC3339)
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(detail); err != nil {
		log.Printf("[Dashboard] Failed to encode task detail: %v", err)
		http.Error(w, "Failed to encode task detail", http.StatusInternalServerError)
	}
}

// handleDLQ returns dead letter queue tasks
func (s *Server) handleDLQ(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	ctx := r.Context()
	tasks, err := s.queue.GetDLQTasks(ctx, 100)
	if err != nil {
		http.Error(w, "Failed to fetch DLQ tasks", http.StatusInternalServerError)
		return
	}
	
	details := make([]TaskDetail, 0, len(tasks))
	for _, t := range tasks {
		detail := TaskDetail{
			ID:         t.ID,
			Type:       t.Type,
			State:      string(t.State),
			Priority:   int(t.Priority),
			RetryCount: t.RetryCount,
			MaxRetries: t.MaxRetries,
			CreatedAt:  t.CreatedAt.Format(time.RFC3339),
			Error:      t.Error,
		}
		if t.StartedAt != nil {
			detail.StartedAt = t.StartedAt.Format(time.RFC3339)
		}
		if t.CompletedAt != nil {
			detail.CompletedAt = t.CompletedAt.Format(time.RFC3339)
		}
		details = append(details, detail)
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks": details,
		"total": len(details),
	})
}

// handleHealth returns health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	health := map[string]interface{}{
		"status": "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
	}
	
	// Check queue health
	if err := s.queue.Health(ctx); err != nil {
		health["status"] = "unhealthy"
		health["queue_error"] = err.Error()
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(health); err != nil {
		log.Printf("[Dashboard] Failed to encode health response: %v", err)
	}
}

// collectMetrics gathers current metrics for dashboard
func (s *Server) collectMetrics() MetricsUpdate {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Update queue depth
	if err := s.metrics.UpdateQueueDepth(ctx); err != nil {
		log.Printf("[Dashboard] Failed to update queue depth: %v", err)
	}
	
	snapshot := s.metrics.Snapshot()
	
	// Get queue depth trend (last 5 minutes)
	trend := s.metrics.GetQueueDepthTrend(5 * time.Minute)
	queueTrend := make([]QueueDataPoint, 0, len(trend))
	for _, point := range trend {
		queueTrend = append(queueTrend, QueueDataPoint{
			Timestamp: point.Timestamp.Format(time.RFC3339),
			Depth:     point.Depth,
		})
	}
	
	// Get DLQ size
	dlqSize, _ := s.queue.GetDLQSize(ctx)
	
	// Calculate worker utilization
	var utilization float64
	if snapshot.ActiveWorkers > 0 {
		utilization = float64(snapshot.BusyWorkers) / float64(snapshot.ActiveWorkers) * 100
	}
	
	// Check queue health
	queueHealth := "healthy"
	if err := s.queue.Health(ctx); err != nil {
		queueHealth = "unhealthy"
	}
	
	return MetricsUpdate{
		Timestamp:         time.Now().Format(time.RFC3339),
		QueueDepth:        snapshot.QueueDepth,
		QueueDepthHigh:    snapshot.QueueDepthHigh,
		QueueDepthLow:     snapshot.QueueDepthLow,
		QueueTrend:        queueTrend,
		ActiveWorkers:     snapshot.ActiveWorkers,
		IdleWorkers:       snapshot.IdleWorkers,
		BusyWorkers:       snapshot.BusyWorkers,
		WorkerUtilization: utilization,
		TasksProcessed:    snapshot.TasksProcessed,
		TasksFailed:       snapshot.TasksFailed,
		TasksRetried:      snapshot.TasksRetried,
		TasksEnqueued:     snapshot.TasksEnqueued,
		TasksInProgress:   snapshot.TasksInProgress,
		CurrentThroughput: snapshot.CurrentThroughput,
		AvgThroughput:     snapshot.AvgThroughput,
		AvgProcessingTime: snapshot.AvgProcessingTime.Round(time.Millisecond).String(),
		P95ProcessingTime: snapshot.P95ProcessingTime.Round(time.Millisecond).String(),
		P99ProcessingTime: snapshot.P99ProcessingTime.Round(time.Millisecond).String(),
		Uptime:            snapshot.Uptime.Round(time.Second).String(),
		OldestIdleTime:    snapshot.OldestIdleTime.Round(time.Second).String(),
		DLQSize:           dlqSize,
		QueueHealth:       queueHealth,
	}
}

// broadcastMetrics sends metrics updates to all connected clients
func (s *Server) broadcastMetrics() {
	for {
		select {
		case <-s.done:
			return
		case update := <-s.broadcast:
			s.mu.RLock()
			for conn := range s.connections {
				select {
				case conn.send <- update:
				default:
					// Buffer full, skip this update for this connection
				}
			}
			s.mu.RUnlock()
		}
	}
}

// updateMetricsLoop periodically collects and broadcasts metrics
func (s *Server) updateMetricsLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			update := s.collectMetrics()
			select {
			case s.broadcast <- update:
			default:
				// Broadcast channel full, skip this update
			}
		}
	}
}

// Middleware: withLogging logs all HTTP requests
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[Dashboard] %s %s - %v", r.Method, r.URL.Path, time.Since(start))
	})
}

// Middleware: withCORS adds CORS headers
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}
