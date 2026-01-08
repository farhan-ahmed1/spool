package monitoring

import (
	"context"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/task"
)

// mockQueue implements queue.Queue interface for testing
type mockQueue struct {
	size     int64
	sizeChan chan int64 // for dynamic size changes
}

func newMockQueue(size int64) *mockQueue {
	return &mockQueue{
		size:     size,
		sizeChan: make(chan int64, 10),
	}
}

func (m *mockQueue) Enqueue(ctx context.Context, t *task.Task) error           { return nil }
func (m *mockQueue) Dequeue(ctx context.Context) (*task.Task, error)           { return nil, queue.ErrNoTask }
func (m *mockQueue) Peek(ctx context.Context) (*task.Task, error)              { return nil, nil }
func (m *mockQueue) Ack(ctx context.Context, taskID string) error              { return nil }
func (m *mockQueue) Nack(ctx context.Context, taskID string, requeue bool) error { return nil }
func (m *mockQueue) Size(ctx context.Context) (int64, error) {
	select {
	case newSize := <-m.sizeChan:
		m.size = newSize
	default:
	}
	return m.size, nil
}
func (m *mockQueue) SizeByPriority(ctx context.Context) (map[task.Priority]int64, error) {
	return nil, nil
}
func (m *mockQueue) Purge(ctx context.Context) error                                          { return nil }
func (m *mockQueue) EnqueueDLQ(ctx context.Context, t *task.Task, reason string) error        { return nil }
func (m *mockQueue) GetDLQSize(ctx context.Context) (int64, error)                            { return 0, nil }
func (m *mockQueue) GetDLQTasks(ctx context.Context, limit int) ([]*task.Task, error)         { return nil, nil }
func (m *mockQueue) Close() error                                                             { return nil }
func (m *mockQueue) Health(ctx context.Context) error                                         { return nil }

func (m *mockQueue) setSize(size int64) {
	m.sizeChan <- size
}

func TestMetrics_QueueDepth(t *testing.T) {
	q := newMockQueue(42)
	metrics := NewMetrics(q)

	ctx := context.Background()
	err := metrics.UpdateQueueDepth(ctx)
	if err != nil {
		t.Fatalf("UpdateQueueDepth failed: %v", err)
	}

	depth := metrics.GetQueueDepth()
	if depth != 42 {
		t.Errorf("Expected queue depth 42, got %d", depth)
	}

	// Test high water mark
	q.setSize(100)
	if err := metrics.UpdateQueueDepth(ctx); err != nil {
		t.Fatalf("UpdateQueueDepth failed: %v", err)
	}

	snapshot := metrics.Snapshot()
	if snapshot.QueueDepthHigh != 100 {
		t.Errorf("Expected high water mark 100, got %d", snapshot.QueueDepthHigh)
	}

	// Test low water mark
	q.setSize(5)
	if err := metrics.UpdateQueueDepth(ctx); err != nil {
		t.Fatalf("UpdateQueueDepth failed: %v", err)
	}

	snapshot = metrics.Snapshot()
	if snapshot.QueueDepthLow != 5 {
		t.Errorf("Expected low water mark 5, got %d", snapshot.QueueDepthLow)
	}
}

func TestMetrics_WorkerTracking(t *testing.T) {
	q := newMockQueue(0)
	metrics := NewMetrics(q)

	// Register workers
	metrics.RegisterWorker("worker-1")
	metrics.RegisterWorker("worker-2")
	metrics.RegisterWorker("worker-3")

	active, idle, busy := metrics.GetWorkerCounts()
	if active != 3 {
		t.Errorf("Expected 3 active workers, got %d", active)
	}
	if idle != 3 {
		t.Errorf("Expected 3 idle workers, got %d", idle)
	}
	if busy != 0 {
		t.Errorf("Expected 0 busy workers, got %d", busy)
	}

	// Mark worker as busy
	metrics.MarkWorkerBusy("worker-1")
	_, idle, busy = metrics.GetWorkerCounts()
	if idle != 2 {
		t.Errorf("Expected 2 idle workers, got %d", idle)
	}
	if busy != 1 {
		t.Errorf("Expected 1 busy worker, got %d", busy)
	}

	// Mark worker as idle again
	metrics.MarkWorkerIdle("worker-1")
	_, idle, busy = metrics.GetWorkerCounts()
	if idle != 3 {
		t.Errorf("Expected 3 idle workers, got %d", idle)
	}
	if busy != 0 {
		t.Errorf("Expected 0 busy workers, got %d", busy)
	}

	// Unregister worker
	metrics.UnregisterWorker("worker-1")
	active, _, _ = metrics.GetWorkerCounts()
	if active != 2 {
		t.Errorf("Expected 2 active workers, got %d", active)
	}
}

func TestMetrics_OldestIdleTime(t *testing.T) {
	q := newMockQueue(0)
	metrics := NewMetrics(q)

	metrics.RegisterWorker("worker-1")
	time.Sleep(50 * time.Millisecond)
	metrics.RegisterWorker("worker-2")

	idleTime := metrics.GetOldestIdleTime()
	if idleTime < 50*time.Millisecond {
		t.Errorf("Expected oldest idle time >= 50ms, got %v", idleTime)
	}

	// Mark oldest worker as busy
	metrics.MarkWorkerBusy("worker-1")
	idleTime = metrics.GetOldestIdleTime()
	if idleTime >= 50*time.Millisecond {
		t.Errorf("Expected oldest idle time < 50ms after marking worker-1 busy, got %v", idleTime)
	}
}

func TestMetrics_TaskTracking(t *testing.T) {
	q := newMockQueue(0)
	metrics := NewMetrics(q)

	// Test task lifecycle
	metrics.RecordTaskEnqueued()
	metrics.RecordTaskStarted()

	snapshot := metrics.Snapshot()
	if snapshot.TasksEnqueued != 1 {
		t.Errorf("Expected 1 enqueued task, got %d", snapshot.TasksEnqueued)
	}
	if snapshot.TasksInProgress != 1 {
		t.Errorf("Expected 1 in-progress task, got %d", snapshot.TasksInProgress)
	}

	metrics.RecordTaskCompleted(100 * time.Millisecond)

	snapshot = metrics.Snapshot()
	if snapshot.TasksProcessed != 1 {
		t.Errorf("Expected 1 processed task, got %d", snapshot.TasksProcessed)
	}
	if snapshot.TasksInProgress != 0 {
		t.Errorf("Expected 0 in-progress tasks, got %d", snapshot.TasksInProgress)
	}

	// Test failure tracking
	metrics.RecordTaskStarted()
	metrics.RecordTaskFailed()

	snapshot = metrics.Snapshot()
	if snapshot.TasksFailed != 1 {
		t.Errorf("Expected 1 failed task, got %d", snapshot.TasksFailed)
	}

	// Test retry tracking
	metrics.RecordTaskRetried()
	snapshot = metrics.Snapshot()
	if snapshot.TasksRetried != 1 {
		t.Errorf("Expected 1 retried task, got %d", snapshot.TasksRetried)
	}
}

func TestMetrics_QueueDepthTrend(t *testing.T) {
	q := newMockQueue(10)
	metrics := NewMetrics(q)
	ctx := context.Background()

	// Record several snapshots
	for i := 0; i < 5; i++ {
		q.setSize(int64(10 + i*10))
		if err := metrics.UpdateQueueDepth(ctx); err != nil {
			t.Fatalf("UpdateQueueDepth failed: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Get trend for last 100ms
	trend := metrics.GetQueueDepthTrend(100 * time.Millisecond)

	if len(trend) < 4 {
		t.Errorf("Expected at least 4 trend samples, got %d", len(trend))
	}

	// Verify trend is increasing
	for i := 1; i < len(trend); i++ {
		if trend[i].Depth <= trend[i-1].Depth {
			t.Errorf("Expected increasing trend, but got %d after %d", trend[i].Depth, trend[i-1].Depth)
		}
	}
}

func TestMetrics_Snapshot(t *testing.T) {
	q := newMockQueue(25)
	metrics := NewMetrics(q)
	ctx := context.Background()

	// Setup some state
	metrics.UpdateQueueDepth(ctx)
	metrics.RegisterWorker("worker-1")
	metrics.RegisterWorker("worker-2")
	metrics.MarkWorkerBusy("worker-1")
	metrics.RecordTaskEnqueued()
	metrics.RecordTaskStarted()
	metrics.RecordTaskCompleted(50 * time.Millisecond)

	// Get snapshot
	snapshot := metrics.Snapshot()

	// Verify snapshot
	if snapshot.QueueDepth != 25 {
		t.Errorf("Expected queue depth 25, got %d", snapshot.QueueDepth)
	}
	if snapshot.ActiveWorkers != 2 {
		t.Errorf("Expected 2 active workers, got %d", snapshot.ActiveWorkers)
	}
	if snapshot.BusyWorkers != 1 {
		t.Errorf("Expected 1 busy worker, got %d", snapshot.BusyWorkers)
	}
	if snapshot.IdleWorkers != 1 {
		t.Errorf("Expected 1 idle worker, got %d", snapshot.IdleWorkers)
	}
	if snapshot.TasksProcessed != 1 {
		t.Errorf("Expected 1 processed task, got %d", snapshot.TasksProcessed)
	}
	if snapshot.Uptime <= 0 {
		t.Errorf("Expected positive uptime, got %v", snapshot.Uptime)
	}
}

func TestMetrics_Reset(t *testing.T) {
	q := newMockQueue(100)
	metrics := NewMetrics(q)
	ctx := context.Background()

	// Add some data
	metrics.UpdateQueueDepth(ctx)
	metrics.RegisterWorker("worker-1")
	metrics.RecordTaskEnqueued()
	metrics.RecordTaskStarted()
	metrics.RecordTaskCompleted(100 * time.Millisecond)

	// Reset
	metrics.Reset()

	// Verify everything is cleared
	snapshot := metrics.Snapshot()
	if snapshot.QueueDepth != 0 {
		t.Errorf("Expected queue depth 0 after reset, got %d", snapshot.QueueDepth)
	}
	if snapshot.ActiveWorkers != 0 {
		t.Errorf("Expected 0 active workers after reset, got %d", snapshot.ActiveWorkers)
	}
	if snapshot.TasksProcessed != 0 {
		t.Errorf("Expected 0 processed tasks after reset, got %d", snapshot.TasksProcessed)
	}
}

func BenchmarkMetrics_RecordTaskCompleted(b *testing.B) {
	q := newMockQueue(0)
	metrics := NewMetrics(q)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.RecordTaskCompleted(100 * time.Millisecond)
	}
}

func BenchmarkMetrics_Snapshot(b *testing.B) {
	q := newMockQueue(50)
	metrics := NewMetrics(q)
	ctx := context.Background()

	// Setup some state
	metrics.UpdateQueueDepth(ctx)
	for i := 0; i < 10; i++ {
		metrics.RegisterWorker(string(rune('A' + i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = metrics.Snapshot()
	}
}

