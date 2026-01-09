package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/farhan-ahmed1/spool/internal/logger"
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
	serverMu sync.RWMutex // Protects server field
	logger   *logger.Logger
	
	// WebSocket connections
	mu          sync.RWMutex
	connections map[*connection]bool
	broadcast   chan MetricsUpdate
	
	// Task events for live feed
	taskEvents     chan TaskEvent
	recentEvents   []TaskEvent
	eventsMu       sync.RWMutex
	maxRecentEvents int
	
	// Shutdown
	done chan struct{}
	ready chan struct{} // Signals when server is ready
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
	Timestamp         string              `json:"timestamp"`
	QueueDepth        int64               `json:"queueDepth"`
	QueueDepthHigh    int64               `json:"queueDepthHigh"`
	QueueDepthLow     int64               `json:"queueDepthLow"`
	QueueTrend        []QueueDataPoint    `json:"queueTrend"`
	ThroughputTrend   []ThroughputDataPoint `json:"throughputTrend"`
	ActiveWorkers     int32               `json:"activeWorkers"`
	IdleWorkers       int32               `json:"idleWorkers"`
	BusyWorkers       int32               `json:"busyWorkers"`
	Workers           []WorkerSummary     `json:"workers"`
	WorkerUtilization float64             `json:"workerUtilization"`
	TasksProcessed    int64               `json:"tasksProcessed"`
	TasksFailed       int64               `json:"tasksFailed"`
	TasksRetried      int64               `json:"tasksRetried"`
	TasksEnqueued     int64               `json:"tasksEnqueued"`
	TasksInProgress   int32               `json:"tasksInProgress"`
	CurrentThroughput float64             `json:"currentThroughput"`
	AvgThroughput     float64             `json:"avgThroughput"`
	AvgProcessingTime string              `json:"avgProcessingTime"`
	P95ProcessingTime string              `json:"p95ProcessingTime"`
	P99ProcessingTime string              `json:"p99ProcessingTime"`
	Uptime            string              `json:"uptime"`
	OldestIdleTime    string              `json:"oldestIdleTime"`
	DLQSize           int64               `json:"dlqSize"`
	QueueHealth       string              `json:"queueHealth"`
}

// QueueDataPoint represents a point in the queue depth time series
type QueueDataPoint struct {
	Timestamp string `json:"timestamp"`
	Depth     int64  `json:"depth"`
}

// ThroughputDataPoint represents a point in the throughput time series
type ThroughputDataPoint struct {
	Timestamp  string  `json:"timestamp"`
	Throughput float64 `json:"throughput"`
}

// TaskEvent represents a task completion event for the activity feed
type TaskEvent struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Status    string `json:"status"` // "success" or "failed"
	Duration  string `json:"duration"`
	Timestamp string `json:"timestamp"`
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

	// Create logger for dashboard server
	dashboardLogger := logger.GetDefault()
	if dashboardLogger == nil {
		dashboardLogger = logger.New("info", "text", "dashboard")
	} else {
		dashboardLogger = dashboardLogger.WithComponent("dashboard")
	}

	return &Server{
		metrics:        cfg.Metrics,
		queue:          cfg.Queue,
		storage:        cfg.Storage,
		addr:           cfg.Addr,
		logger:         dashboardLogger,
		connections:    make(map[*connection]bool),
		broadcast:      make(chan MetricsUpdate, 100),
		taskEvents:     make(chan TaskEvent, 100),
		recentEvents:   make([]TaskEvent, 0, 20),
		maxRecentEvents: 20,
		done:           make(chan struct{}),
		ready:          make(chan struct{}),
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
	mux.HandleFunc("/api/workers", s.handleWorkers)
	mux.HandleFunc("/api/workers/", s.handleWorkerDetail)
	
	// Health check
	mux.HandleFunc("/health", s.handleHealth)
	
	// Serve static files
	fs := http.FileServer(http.Dir("web/dashboard/static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))
	
	server := &http.Server{
		Addr:         s.addr,
		Handler:      s.withLogging(s.withCORS(mux)),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	
	// Set server with mutex protection
	s.serverMu.Lock()
	s.server = server
	s.serverMu.Unlock()
	
	// Start WebSocket broadcaster
	go s.broadcastMetrics()
	
	// Start metrics update loop
	go s.updateMetricsLoop()
	
	// Signal that server is ready
	close(s.ready)
	
	s.logger.Info("Starting dashboard server", logger.Fields{
		"address": s.addr,
	})
	return server.ListenAndServe()
}

// Ready returns a channel that is closed when the server is ready to accept requests
func (s *Server) Ready() <-chan struct{} {
	return s.ready
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
	
	// Safely access server with mutex
	s.serverMu.RLock()
	server := s.server
	s.serverMu.RUnlock()
	
	if server == nil {
		return nil
	}
	
	return server.Shutdown(ctx)
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
		s.logger.Error("Failed to parse template", logger.Fields{
			"error": err.Error(),
			"path":  tmplPath,
		})
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	
	data := map[string]interface{}{
		"Title": "Spool Dashboard",
	}
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		s.logger.Error("Failed to execute template", logger.Fields{
			"error": err.Error(),
		})
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
		s.logger.Error("Failed to encode metrics", logger.Fields{
			"error": err.Error(),
		})
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
	fmt.Fprintf(w, "event: metrics\ndata: %s\n\n", data)
	flusher.Flush()
	
	// Send recent task events
	recentEvents := s.getRecentEvents()
	for _, event := range recentEvents {
		eventData, _ := json.Marshal(event)
		fmt.Fprintf(w, "event: task\ndata: %s\n\n", eventData)
	}
	flusher.Flush()
	
	// Listen for updates or client disconnect
	ctx := r.Context()
	taskEventCh := make(chan TaskEvent, 10)
	
	// Subscribe to task events
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-s.taskEvents:
				select {
				case taskEventCh <- event:
				default:
					// Buffer full, skip
				}
			}
		}
	}()
	
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
			fmt.Fprintf(w, "event: metrics\ndata: %s\n\n", data)
			flusher.Flush()
		case event := <-taskEventCh:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: task\ndata: %s\n\n", data)
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
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": []TaskDetail{},
			"total": 0,
		}); err != nil {
			s.logger.Error("Failed to encode response", logger.Fields{
				"error": err.Error(),
			})
		}
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
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks": details,
		"total": len(details),
	}); err != nil {
		s.logger.Error("Failed to encode response", logger.Fields{
			"error": err.Error(),
		})
	}
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
		s.logger.Error("Failed to encode task detail", logger.Fields{
			"error":   err.Error(),
			"task_id": taskID,
		})
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
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks": details,
		"total": len(details),
	}); err != nil {
		s.logger.Error("Failed to encode response", logger.Fields{
			"error": err.Error(),
		})
	}
}

// WorkerDetailResponse represents the response for worker details
type WorkerDetailResponse struct {
	ID                string  `json:"id"`
	State             string  `json:"state"`
	TasksCompleted    int64   `json:"tasksCompleted"`
	TasksFailed       int64   `json:"tasksFailed"`
	TotalIdleTime     string  `json:"totalIdleTime"`
	TotalBusyTime     string  `json:"totalBusyTime"`
	AvgProcessingTime string  `json:"avgProcessingTime"`
	StartTime         string  `json:"startTime"`
	Uptime            string  `json:"uptime"`
	CurrentTask       *struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		StartTime   string `json:"startTime"`
		ElapsedTime string `json:"elapsedTime"`
	} `json:"currentTask,omitempty"`
}

// WorkerSummary represents a summary of a worker for the workers list
type WorkerSummary struct {
	ID             string `json:"id"`
	State          string `json:"state"`
	TasksCompleted int64  `json:"tasksCompleted"`
}

// handleWorkers returns a list of all active workers
func (s *Server) handleWorkers(w http.ResponseWriter, r *http.Request) {
	workers := s.metrics.GetAllWorkerDetails()
	
	summaries := make([]WorkerSummary, 0, len(workers))
	for _, worker := range workers {
		summaries = append(summaries, WorkerSummary{
			ID:             worker.ID,
			State:          worker.State,
			TasksCompleted: worker.TasksCompleted,
		})
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"workers": summaries,
		"total":   len(summaries),
	}); err != nil {
		s.logger.Error("Failed to encode response", logger.Fields{
			"error": err.Error(),
		})
	}
}

// handleWorkerDetail returns detailed information about a specific worker
func (s *Server) handleWorkerDetail(w http.ResponseWriter, r *http.Request) {
	// Extract worker ID from URL path
	workerID := r.URL.Path[len("/api/workers/"):]
	if workerID == "" {
		http.Error(w, "Worker ID required", http.StatusBadRequest)
		return
	}
	
	stats, currentTask, exists := s.metrics.GetWorkerDetails(workerID)
	if !exists {
		http.Error(w, "Worker not found", http.StatusNotFound)
		return
	}
	
	// Calculate average processing time
	var avgProcessingTime time.Duration
	if len(stats.ProcessingTimes) > 0 {
		var sum time.Duration
		for _, t := range stats.ProcessingTimes {
			sum += t
		}
		avgProcessingTime = sum / time.Duration(len(stats.ProcessingTimes))
	}
	
	// Build response
	response := WorkerDetailResponse{
		ID:                stats.ID,
		State:             stats.State,
		TasksCompleted:    stats.TasksCompleted,
		TasksFailed:       stats.TasksFailed,
		TotalIdleTime:     stats.TotalIdleTime.Round(time.Second).String(),
		TotalBusyTime:     stats.TotalBusyTime.Round(time.Second).String(),
		AvgProcessingTime: avgProcessingTime.Round(time.Millisecond).String(),
		StartTime:         stats.StartTime.Format(time.RFC3339),
		Uptime:            time.Since(stats.StartTime).Round(time.Second).String(),
	}
	
	// Add current task if processing one
	if currentTask != nil {
		response.CurrentTask = &struct {
			ID          string `json:"id"`
			Type        string `json:"type"`
			StartTime   string `json:"startTime"`
			ElapsedTime string `json:"elapsedTime"`
		}{
			ID:          currentTask.ID,
			Type:        currentTask.Type,
			StartTime:   currentTask.StartTime.Format(time.RFC3339),
			ElapsedTime: time.Since(currentTask.StartTime).Round(time.Millisecond).String(),
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode response", logger.Fields{
			"error": err.Error(),
		})
	}
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
		s.logger.Error("Failed to encode health response", logger.Fields{
			"error": err.Error(),
		})
	}
}

// BroadcastTaskEvent broadcasts a task completion event to all connected clients
// This should be called by the worker when a task completes or fails
func (s *Server) BroadcastTaskEvent(taskID, taskType, status string, duration time.Duration) {
	event := TaskEvent{
		ID:        taskID,
		Type:      taskType,
		Status:    status,
		Duration:  duration.Round(time.Millisecond).String(),
		Timestamp: time.Now().Format(time.RFC3339),
	}
	
	// Add to recent events
	s.eventsMu.Lock()
	s.recentEvents = append(s.recentEvents, event)
	if len(s.recentEvents) > s.maxRecentEvents {
		s.recentEvents = s.recentEvents[1:]
	}
	s.eventsMu.Unlock()
	
	// Broadcast to all connections
	select {
	case s.taskEvents <- event:
	default:
		// Channel full, skip this event
	}
}

// getRecentEvents returns the list of recent task events
func (s *Server) getRecentEvents() []TaskEvent {
	s.eventsMu.RLock()
	defer s.eventsMu.RUnlock()
	
	// Return a copy
	events := make([]TaskEvent, len(s.recentEvents))
	copy(events, s.recentEvents)
	return events
}

// collectMetrics gathers current metrics for dashboard
func (s *Server) collectMetrics() MetricsUpdate {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Update queue depth
	if err := s.metrics.UpdateQueueDepth(ctx); err != nil {
		s.logger.Error("Failed to update queue depth", logger.Fields{
			"error": err.Error(),
		})
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
	
	// Get throughput trend (last 5 minutes)
	throughputHistory := s.metrics.GetThroughputTrend(5 * time.Minute)
	throughputTrend := make([]ThroughputDataPoint, 0, len(throughputHistory))
	for _, point := range throughputHistory {
		throughputTrend = append(throughputTrend, ThroughputDataPoint{
			Timestamp:  point.Timestamp.Format(time.RFC3339),
			Throughput: point.TasksPerSec,
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
	
	// Get workers list
	workers := s.metrics.GetAllWorkerDetails()
	workersList := make([]WorkerSummary, 0, len(workers))
	for _, worker := range workers {
		workersList = append(workersList, WorkerSummary{
			ID:             worker.ID,
			State:          worker.State,
			TasksCompleted: worker.TasksCompleted,
		})
	}
	
	return MetricsUpdate{
		Timestamp:         time.Now().Format(time.RFC3339),
		QueueDepth:        snapshot.QueueDepth,
		QueueDepthHigh:    snapshot.QueueDepthHigh,
		QueueDepthLow:     snapshot.QueueDepthLow,
		QueueTrend:        queueTrend,
		ThroughputTrend:   throughputTrend,
		ActiveWorkers:     snapshot.ActiveWorkers,
		IdleWorkers:       snapshot.IdleWorkers,
		BusyWorkers:       snapshot.BusyWorkers,
		Workers:           workersList,
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
		s.logger.Debug("HTTP request", logger.Fields{
			"method":   r.Method,
			"path":     r.URL.Path,
			"duration": time.Since(start).String(),
		})
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
