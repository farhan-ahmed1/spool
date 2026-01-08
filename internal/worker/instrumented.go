package worker

import (
	"context"
	"time"

	"github.com/farhan-ahmed1/spool/internal/monitoring"
)

// InstrumentedWorker wraps a Worker with metrics instrumentation
type InstrumentedWorker struct {
	worker         *Worker
	metrics        *monitoring.Metrics
	lastProcessed  int64
	stopStatsSyncChan chan struct{}
}

// NewInstrumentedWorker creates a new instrumented worker
func NewInstrumentedWorker(w *Worker, m *monitoring.Metrics) *InstrumentedWorker {
	return &InstrumentedWorker{
		worker:         w,
		metrics:        m,
		stopStatsSyncChan: make(chan struct{}),
	}
}

// Start starts the worker with metrics tracking
func (iw *InstrumentedWorker) Start(ctx context.Context) error {
	// Register worker with metrics
	iw.metrics.RegisterWorker(iw.worker.ID())

	// Start periodic stats sync
	go iw.syncStatsLoop(ctx)

	// Start the worker
	return iw.worker.Start(ctx)
}

// syncStatsLoop periodically syncs worker stats to metrics
func (iw *InstrumentedWorker) syncStatsLoop(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond) // Sync every 100ms
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-iw.stopStatsSyncChan:
			return
		case <-ticker.C:
			processed, _, _ := iw.worker.Stats()
			
			// Calculate how many tasks were completed since last sync
			newTasks := processed - iw.lastProcessed
			if newTasks > 0 {
				// Record each completed task (approximate duration)
				for i := int64(0); i < newTasks; i++ {
					iw.metrics.RecordTaskCompleted(100 * time.Millisecond)
				}
				iw.lastProcessed = processed
			}
		}
	}
}

// Stop stops the worker and updates metrics
func (iw *InstrumentedWorker) Stop() error {
	// Signal stats sync to stop
	close(iw.stopStatsSyncChan)
	
	err := iw.worker.Stop()

	// Unregister worker from metrics
	iw.metrics.UnregisterWorker(iw.worker.ID())

	return err
}

// GetID returns the worker ID
func (iw *InstrumentedWorker) GetID() string {
	return iw.worker.ID()
}

// GetWorker returns the underlying worker
func (iw *InstrumentedWorker) GetWorker() *Worker {
	return iw.worker
}

// Worker returns the wrapped worker instance
func (iw *InstrumentedWorker) Worker() *Worker {
	return iw.worker
}

// Stats returns worker statistics
func (iw *InstrumentedWorker) Stats() (processed, failed, retried int64) {
	return iw.worker.Stats()
}

// IsRunning returns whether the worker is running
func (iw *InstrumentedWorker) IsRunning() bool {
	return iw.worker.IsRunning()
}

// Shutdown gracefully stops the worker with timeout
func (iw *InstrumentedWorker) Shutdown(ctx context.Context) error {
	// Signal stats sync to stop
	close(iw.stopStatsSyncChan)
	
	err := iw.worker.Shutdown(ctx)

	// Unregister worker from metrics
	iw.metrics.UnregisterWorker(iw.worker.ID())

	return err
}

// InstrumentedPool manages a pool of instrumented workers
type InstrumentedPool struct {
	workers []*InstrumentedWorker
	metrics *monitoring.Metrics
}

// NewInstrumentedPool creates a new instrumented worker pool
func NewInstrumentedPool(workers []*InstrumentedWorker, metrics *monitoring.Metrics) *InstrumentedPool {
	return &InstrumentedPool{
		workers: workers,
		metrics: metrics,
	}
}

// StartAll starts all workers in the pool
func (ip *InstrumentedPool) StartAll(ctx context.Context) error {
	for _, w := range ip.workers {
		if err := w.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

// StopAll stops all workers in the pool
func (ip *InstrumentedPool) StopAll() error {
	for _, w := range ip.workers {
		if err := w.Stop(); err != nil {
			return err
		}
	}
	return nil
}

// ShutdownAll gracefully shuts down all workers
func (ip *InstrumentedPool) ShutdownAll(ctx context.Context) error {
	for _, w := range ip.workers {
		if err := w.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Size returns the number of workers in the pool
func (ip *InstrumentedPool) Size() int {
	return len(ip.workers)
}

// GetMetrics returns the metrics instance
func (ip *InstrumentedPool) GetMetrics() *monitoring.Metrics {
	return ip.metrics
}

// MonitoredWorker wraps task execution with detailed metrics tracking
type MonitoredWorker struct {
	*InstrumentedWorker
}

// NewMonitoredWorker creates a worker with comprehensive monitoring
func NewMonitoredWorker(w *Worker, m *monitoring.Metrics) *MonitoredWorker {
	return &MonitoredWorker{
		InstrumentedWorker: NewInstrumentedWorker(w, m),
	}
}
