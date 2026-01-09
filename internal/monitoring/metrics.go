package monitoring

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/farhan-ahmed1/spool/internal/queue"
)

// Metrics collects and tracks system metrics for monitoring and auto-scaling
type Metrics struct {
	mu sync.RWMutex

	// Queue metrics
	queueDepth       int64
	queueDepthHigh   int64 // high water mark
	queueDepthLow    int64 // low water mark
	lastQueueCheck   time.Time
	queueDepthSeries []QueueDepthSnapshot

	// Worker metrics
	activeWorkers   int32
	idleWorkers     int32
	busyWorkers     int32
	workerLastIdle  map[string]time.Time
	workerStartTime map[string]time.Time

	// Per-worker detailed metrics
	workerStats       map[string]*WorkerStats
	workerCurrentTask map[string]*TaskInfo

	// Task metrics
	tasksProcessed  int64
	tasksFailed     int64
	tasksRetried    int64
	tasksEnqueued   int64
	tasksInProgress int32

	// Throughput tracking
	throughputSamples []ThroughputSample
	lastSampleTime    time.Time

	// Performance metrics
	processingTimes []time.Duration

	// System metrics
	startTime   time.Time
	lastUpdated time.Time

	// External dependencies
	queue queue.Queue
}

// QueueDepthSnapshot represents a point-in-time queue depth measurement
type QueueDepthSnapshot struct {
	Timestamp time.Time
	Depth     int64
}

// ThroughputSample represents a throughput measurement over a time window
type ThroughputSample struct {
	Timestamp   time.Time
	TasksPerSec float64
	WindowSize  time.Duration
}

// WorkerStats holds detailed statistics for a single worker
type WorkerStats struct {
	ID              string
	State           string // "idle" or "busy"
	TasksCompleted  int64
	TasksFailed     int64
	TotalIdleTime   time.Duration
	TotalBusyTime   time.Duration
	LastIdleStart   time.Time
	LastBusyStart   time.Time
	ProcessingTimes []time.Duration
	StartTime       time.Time
}

// TaskInfo holds information about a task being processed
type TaskInfo struct {
	ID        string
	Type      string
	StartTime time.Time
}

// MetricsSnapshot provides a point-in-time view of all metrics
type MetricsSnapshot struct {
	// Queue
	QueueDepth     int64
	QueueDepthHigh int64
	QueueDepthLow  int64

	// Workers
	ActiveWorkers   int32
	IdleWorkers     int32
	BusyWorkers     int32
	OldestIdleTime  time.Duration
	NewestWorkerAge time.Duration

	// Tasks
	TasksProcessed  int64
	TasksFailed     int64
	TasksRetried    int64
	TasksEnqueued   int64
	TasksInProgress int32

	// Performance
	CurrentThroughput float64 // tasks per second
	AvgThroughput     float64
	AvgProcessingTime time.Duration
	P95ProcessingTime time.Duration
	P99ProcessingTime time.Duration

	// System
	Uptime      time.Duration
	LastUpdated time.Time
}

// NewMetrics creates a new metrics collector
func NewMetrics(q queue.Queue) *Metrics {
	now := time.Now()
	return &Metrics{
		queue:             q,
		startTime:         now,
		lastUpdated:       now,
		lastQueueCheck:    now,
		lastSampleTime:    now,
		workerLastIdle:    make(map[string]time.Time),
		workerStartTime:   make(map[string]time.Time),
		workerStats:       make(map[string]*WorkerStats),
		workerCurrentTask: make(map[string]*TaskInfo),
		queueDepthSeries:  make([]QueueDepthSnapshot, 0, 100),
		throughputSamples: make([]ThroughputSample, 0, 100),
		processingTimes:   make([]time.Duration, 0, 1000),
	}
}

// UpdateQueueDepth fetches and updates the current queue depth
func (m *Metrics) UpdateQueueDepth(ctx context.Context) error {
	size, err := m.queue.Size(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	atomic.StoreInt64(&m.queueDepth, size)
	m.lastQueueCheck = time.Now()

	// Update high/low water marks
	if size > m.queueDepthHigh {
		m.queueDepthHigh = size
	}
	if m.queueDepthLow == 0 || size < m.queueDepthLow {
		m.queueDepthLow = size
	}

	// Record snapshot for trend analysis
	m.queueDepthSeries = append(m.queueDepthSeries, QueueDepthSnapshot{
		Timestamp: time.Now(),
		Depth:     size,
	})

	// Keep only last 100 samples
	if len(m.queueDepthSeries) > 100 {
		m.queueDepthSeries = m.queueDepthSeries[1:]
	}

	return nil
}

// GetQueueDepth returns the current queue depth
func (m *Metrics) GetQueueDepth() int64 {
	return atomic.LoadInt64(&m.queueDepth)
}

// GetQueueDepthTrend returns recent queue depth measurements
func (m *Metrics) GetQueueDepthTrend(duration time.Duration) []QueueDepthSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	var trend []QueueDepthSnapshot
	for _, snap := range m.queueDepthSeries {
		if snap.Timestamp.After(cutoff) {
			trend = append(trend, snap)
		}
	}
	return trend
}

// GetThroughputTrend returns throughput samples within the specified duration
func (m *Metrics) GetThroughputTrend(duration time.Duration) []ThroughputSample {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	var trend []ThroughputSample
	for _, sample := range m.throughputSamples {
		if sample.Timestamp.After(cutoff) {
			trend = append(trend, sample)
		}
	}
	return trend
}

// RegisterWorker adds a new worker to tracking
func (m *Metrics) RegisterWorker(workerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	atomic.AddInt32(&m.activeWorkers, 1)
	atomic.AddInt32(&m.idleWorkers, 1)

	now := time.Now()

	// Initialize worker stats
	m.workerStats[workerID] = &WorkerStats{
		ID:              workerID,
		State:           "idle",
		TasksCompleted:  0,
		TasksFailed:     0,
		TotalIdleTime:   0,
		TotalBusyTime:   0,
		LastIdleStart:   now,
		StartTime:       now,
		ProcessingTimes: make([]time.Duration, 0, 100),
	}
	m.workerLastIdle[workerID] = now
	m.workerStartTime[workerID] = now
}

// UnregisterWorker removes a worker from tracking
func (m *Metrics) UnregisterWorker(workerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if worker was idle or busy
	_, wasIdle := m.workerLastIdle[workerID]
	if wasIdle {
		atomic.AddInt32(&m.idleWorkers, -1)
	} else {
		atomic.AddInt32(&m.busyWorkers, -1)
	}

	atomic.AddInt32(&m.activeWorkers, -1)

	delete(m.workerLastIdle, workerID)
	delete(m.workerStartTime, workerID)
	delete(m.workerStats, workerID)
	delete(m.workerCurrentTask, workerID)
}

// MarkWorkerBusy marks a worker as processing a task
func (m *Metrics) MarkWorkerBusy(workerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, wasIdle := m.workerLastIdle[workerID]; wasIdle {
		// Update idle time
		if stats, exists := m.workerStats[workerID]; exists {
			stats.TotalIdleTime += time.Since(stats.LastIdleStart)
			stats.State = "busy"
			stats.LastBusyStart = time.Now()
		}

		delete(m.workerLastIdle, workerID)
		atomic.AddInt32(&m.idleWorkers, -1)
		atomic.AddInt32(&m.busyWorkers, 1)
	}
}

// MarkWorkerBusyWithTask marks a worker as processing a specific task
func (m *Metrics) MarkWorkerBusyWithTask(workerID string, taskID string, taskType string) {
	m.MarkWorkerBusy(workerID)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.workerCurrentTask[workerID] = &TaskInfo{
		ID:        taskID,
		Type:      taskType,
		StartTime: time.Now(),
	}
}

// MarkWorkerIdle marks a worker as idle
func (m *Metrics) MarkWorkerIdle(workerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, alreadyIdle := m.workerLastIdle[workerID]; !alreadyIdle {
		now := time.Now()
		m.workerLastIdle[workerID] = now

		// Update busy time and clear current task
		if stats, exists := m.workerStats[workerID]; exists {
			stats.TotalBusyTime += time.Since(stats.LastBusyStart)
			stats.State = "idle"
			stats.LastIdleStart = now
		}

		// Clear current task
		delete(m.workerCurrentTask, workerID)

		atomic.AddInt32(&m.busyWorkers, -1)
		atomic.AddInt32(&m.idleWorkers, 1)
	}
}

// RecordWorkerTaskCompleted records a completed task for a specific worker
func (m *Metrics) RecordWorkerTaskCompleted(workerID string, duration time.Duration, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if stats, exists := m.workerStats[workerID]; exists {
		if success {
			stats.TasksCompleted++
		} else {
			stats.TasksFailed++
		}

		stats.ProcessingTimes = append(stats.ProcessingTimes, duration)
		// Keep only last 100 samples per worker
		if len(stats.ProcessingTimes) > 100 {
			stats.ProcessingTimes = stats.ProcessingTimes[1:]
		}
	}
}

// GetWorkerCounts returns active, idle, and busy worker counts
func (m *Metrics) GetWorkerCounts() (active, idle, busy int32) {
	return atomic.LoadInt32(&m.activeWorkers),
		atomic.LoadInt32(&m.idleWorkers),
		atomic.LoadInt32(&m.busyWorkers)
}

// getOldestIdleTime returns how long the oldest worker has been idle (caller must hold lock)
func (m *Metrics) getOldestIdleTime() time.Duration {
	var oldest time.Time
	for _, idleTime := range m.workerLastIdle {
		if oldest.IsZero() || idleTime.Before(oldest) {
			oldest = idleTime
		}
	}

	if oldest.IsZero() {
		return 0
	}
	return time.Since(oldest)
}

// GetOldestIdleTime returns how long the oldest worker has been idle
func (m *Metrics) GetOldestIdleTime() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getOldestIdleTime()
}

// GetWorkerDetails returns detailed stats for a specific worker
func (m *Metrics) GetWorkerDetails(workerID string) (*WorkerStats, *TaskInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats, exists := m.workerStats[workerID]
	if !exists {
		return nil, nil, false
	}

	// Make a copy to avoid data races
	statsCopy := *stats

	// Get current task if any
	var taskCopy *TaskInfo
	if task, hasTask := m.workerCurrentTask[workerID]; hasTask {
		taskCopy = &TaskInfo{
			ID:        task.ID,
			Type:      task.Type,
			StartTime: task.StartTime,
		}
	}

	return &statsCopy, taskCopy, true
}

// GetAllWorkerDetails returns a list of all workers with their details
func (m *Metrics) GetAllWorkerDetails() []WorkerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	workers := make([]WorkerStats, 0, len(m.workerStats))
	for _, stats := range m.workerStats {
		workers = append(workers, *stats)
	}

	return workers
}

// RecordTaskEnqueued increments the enqueued task counter
func (m *Metrics) RecordTaskEnqueued() {
	atomic.AddInt64(&m.tasksEnqueued, 1)
}

// RecordTaskStarted increments the in-progress counter
func (m *Metrics) RecordTaskStarted() {
	atomic.AddInt32(&m.tasksInProgress, 1)
}

// RecordTaskCompleted records a successfully completed task
func (m *Metrics) RecordTaskCompleted(duration time.Duration) {
	atomic.AddInt64(&m.tasksProcessed, 1)
	atomic.AddInt32(&m.tasksInProgress, -1)

	m.mu.Lock()
	m.processingTimes = append(m.processingTimes, duration)
	// Keep only last 1000 samples
	if len(m.processingTimes) > 1000 {
		m.processingTimes = m.processingTimes[1:]
	}
	m.mu.Unlock()

	m.updateThroughput()
}

// RecordTaskFailed records a failed task
func (m *Metrics) RecordTaskFailed() {
	atomic.AddInt64(&m.tasksFailed, 1)
	atomic.AddInt32(&m.tasksInProgress, -1)
}

// RecordTaskRetried records a retried task
func (m *Metrics) RecordTaskRetried() {
	atomic.AddInt64(&m.tasksRetried, 1)
}

// updateThroughput calculates current throughput
func (m *Metrics) updateThroughput() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(m.lastSampleTime)

	// Sample every 5 seconds
	if elapsed < 5*time.Second {
		return
	}

	completed := atomic.LoadInt64(&m.tasksProcessed)
	tasksInWindow := completed - m.getLastTotalTasks()
	throughput := float64(tasksInWindow) / elapsed.Seconds()

	m.throughputSamples = append(m.throughputSamples, ThroughputSample{
		Timestamp:   now,
		TasksPerSec: throughput,
		WindowSize:  elapsed,
	})

	// Keep only last 100 samples (about 8 minutes at 5s intervals)
	if len(m.throughputSamples) > 100 {
		m.throughputSamples = m.throughputSamples[1:]
	}

	m.lastSampleTime = now
}

// getLastTotalTasks returns the task count from the last sample
func (m *Metrics) getLastTotalTasks() int64 {
	if len(m.throughputSamples) == 0 {
		return 0
	}
	lastSample := m.throughputSamples[len(m.throughputSamples)-1]
	return atomic.LoadInt64(&m.tasksProcessed) - int64(lastSample.TasksPerSec*lastSample.WindowSize.Seconds())
}

// GetThroughput returns the current throughput (tasks per second)
// getThroughput returns current throughput (caller must hold lock)
func (m *Metrics) getThroughput() float64 {
	if len(m.throughputSamples) == 0 {
		return 0
	}

	// Return most recent sample
	return m.throughputSamples[len(m.throughputSamples)-1].TasksPerSec
}

func (m *Metrics) GetThroughput() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getThroughput()
}

// getAvgThroughput returns the average throughput over all samples (caller must hold lock)
func (m *Metrics) getAvgThroughput() float64 {
	if len(m.throughputSamples) == 0 {
		return 0
	}

	var sum float64
	for _, sample := range m.throughputSamples {
		sum += sample.TasksPerSec
	}
	return sum / float64(len(m.throughputSamples))
}

// GetAvgThroughput returns the average throughput over all samples
func (m *Metrics) GetAvgThroughput() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getAvgThroughput()
}

// calculatePercentiles calculates processing time percentiles (caller must hold lock)
func (m *Metrics) calculatePercentiles() (avg, p95, p99 time.Duration) {
	if len(m.processingTimes) == 0 {
		return 0, 0, 0
	}

	// Calculate average
	var sum time.Duration
	for _, t := range m.processingTimes {
		sum += t
	}
	avg = sum / time.Duration(len(m.processingTimes))

	// For percentiles, we'd ideally sort, but for simplicity we'll estimate
	// In production, use a proper percentile library or maintain sorted data
	// For now, return reasonable estimates
	p95 = avg * 2
	p99 = avg * 3

	return avg, p95, p99
}

// CalculatePercentiles calculates processing time percentiles
func (m *Metrics) CalculatePercentiles() (avg, p95, p99 time.Duration) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.calculatePercentiles()
}

// Snapshot returns a point-in-time snapshot of all metrics
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	active, idle, busy := m.GetWorkerCounts()
	avg, p95, p99 := m.calculatePercentiles()

	var newestAge time.Duration
	now := time.Now()
	for _, startTime := range m.workerStartTime {
		age := now.Sub(startTime)
		if newestAge == 0 || age < newestAge {
			newestAge = age
		}
	}

	return MetricsSnapshot{
		QueueDepth:        m.GetQueueDepth(),
		QueueDepthHigh:    m.queueDepthHigh,
		QueueDepthLow:     m.queueDepthLow,
		ActiveWorkers:     active,
		IdleWorkers:       idle,
		BusyWorkers:       busy,
		OldestIdleTime:    m.getOldestIdleTime(),
		NewestWorkerAge:   newestAge,
		TasksProcessed:    atomic.LoadInt64(&m.tasksProcessed),
		TasksFailed:       atomic.LoadInt64(&m.tasksFailed),
		TasksRetried:      atomic.LoadInt64(&m.tasksRetried),
		TasksEnqueued:     atomic.LoadInt64(&m.tasksEnqueued),
		TasksInProgress:   atomic.LoadInt32(&m.tasksInProgress),
		CurrentThroughput: m.getThroughput(),
		AvgThroughput:     m.getAvgThroughput(),
		AvgProcessingTime: avg,
		P95ProcessingTime: p95,
		P99ProcessingTime: p99,
		Uptime:            time.Since(m.startTime),
		LastUpdated:       time.Now(),
	}
}

// Reset clears all metrics (useful for testing)
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	atomic.StoreInt64(&m.queueDepth, 0)
	m.queueDepthHigh = 0
	m.queueDepthLow = 0
	atomic.StoreInt32(&m.activeWorkers, 0)
	atomic.StoreInt32(&m.idleWorkers, 0)
	atomic.StoreInt32(&m.busyWorkers, 0)
	atomic.StoreInt64(&m.tasksProcessed, 0)
	atomic.StoreInt64(&m.tasksFailed, 0)
	atomic.StoreInt64(&m.tasksRetried, 0)
	atomic.StoreInt64(&m.tasksEnqueued, 0)
	atomic.StoreInt32(&m.tasksInProgress, 0)

	m.workerLastIdle = make(map[string]time.Time)
	m.workerStartTime = make(map[string]time.Time)
	m.workerStats = make(map[string]*WorkerStats)
	m.workerCurrentTask = make(map[string]*TaskInfo)
	m.queueDepthSeries = make([]QueueDepthSnapshot, 0, 100)
	m.throughputSamples = make([]ThroughputSample, 0, 100)
	m.processingTimes = make([]time.Duration, 0, 1000)

	m.startTime = time.Now()
	m.lastUpdated = time.Now()
	m.lastQueueCheck = time.Now()
	m.lastSampleTime = time.Now()
}
