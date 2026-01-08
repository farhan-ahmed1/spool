package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/farhan-ahmed1/spool/internal/monitoring"
	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
)

// Pool manages a dynamic pool of workers
type Pool struct {
	queue    queue.Queue
	storage  storage.Storage
	registry *task.Registry
	metrics  *monitoring.Metrics

	mu           sync.RWMutex
	workers      map[string]*Worker
	workerStates map[string]WorkerState
	nextWorkerID int

	ctx        context.Context
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup

	// Configuration
	minWorkers      int
	maxWorkers      int
	pollInterval    time.Duration
	shutdownTimeout time.Duration
}

// WorkerState represents the current state of a worker
type WorkerState string

const (
	WorkerStateIdle       WorkerState = "idle"
	WorkerStateBusy       WorkerState = "busy"
	WorkerStateShuttingDown WorkerState = "shutting_down"
)

// PoolConfig holds configuration for the worker pool
type PoolConfig struct {
	MinWorkers      int
	MaxWorkers      int
	PollInterval    time.Duration
	ShutdownTimeout time.Duration
}

// NewPool creates a new worker pool
func NewPool(q queue.Queue, storage storage.Storage, registry *task.Registry, metrics *monitoring.Metrics, cfg PoolConfig) *Pool {
	if cfg.MinWorkers < 1 {
		cfg.MinWorkers = 1
	}
	if cfg.MaxWorkers < cfg.MinWorkers {
		cfg.MaxWorkers = cfg.MinWorkers
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 100 * time.Millisecond
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Pool{
		queue:           q,
		storage:         storage,
		registry:        registry,
		metrics:         metrics,
		workers:         make(map[string]*Worker),
		workerStates:    make(map[string]WorkerState),
		minWorkers:      cfg.MinWorkers,
		maxWorkers:      cfg.MaxWorkers,
		pollInterval:    cfg.PollInterval,
		shutdownTimeout: cfg.ShutdownTimeout,
		ctx:             ctx,
		cancelFunc:      cancel,
		nextWorkerID:    1,
	}
}

// Start initializes the pool with minimum workers
func (p *Pool) Start() error {
	log.Printf("[Pool] Starting with min=%d, max=%d workers", p.minWorkers, p.maxWorkers)

	// Start minimum workers
	for i := 0; i < p.minWorkers; i++ {
		if _, err := p.AddWorker(p.ctx); err != nil {
			log.Printf("[Pool] Failed to start initial worker %d: %v", i+1, err)
			// Continue starting other workers
		}
	}

	currentCount := p.GetWorkerCount()
	log.Printf("[Pool] Started with %d/%d workers", currentCount, p.minWorkers)

	if currentCount == 0 {
		return fmt.Errorf("failed to start any workers")
	}

	return nil
}

// AddWorker spawns a new worker and adds it to the pool
func (p *Pool) AddWorker(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if we're at max capacity
	if len(p.workers) >= p.maxWorkers {
		return "", fmt.Errorf("pool at maximum capacity (%d workers)", p.maxWorkers)
	}

	// Generate worker ID
	workerID := fmt.Sprintf("worker-%d", p.nextWorkerID)
	p.nextWorkerID++

	// Create worker with metrics tracking
	worker := p.createWorkerWithMetrics(workerID)

	// Start worker
	if err := worker.Start(p.ctx); err != nil {
		return "", fmt.Errorf("failed to start worker: %w", err)
	}

	// Add to pool
	p.workers[workerID] = worker
	p.workerStates[workerID] = WorkerStateIdle

	// Register with metrics
	if p.metrics != nil {
		p.metrics.RegisterWorker(workerID)
	}

	log.Printf("[Pool] Added worker %s (total: %d)", workerID, len(p.workers))

	return workerID, nil
}

// RemoveWorker gracefully shuts down and removes a worker from the pool
func (p *Pool) RemoveWorker(ctx context.Context, workerID string) error {
	p.mu.Lock()
	worker, exists := p.workers[workerID]
	if !exists {
		p.mu.Unlock()
		return fmt.Errorf("worker %s not found", workerID)
	}

	// Check if we're at minimum capacity
	if len(p.workers) <= p.minWorkers {
		p.mu.Unlock()
		return fmt.Errorf("pool at minimum capacity (%d workers)", p.minWorkers)
	}

	// Mark as shutting down
	p.workerStates[workerID] = WorkerStateShuttingDown
	p.mu.Unlock()

	// Shutdown worker with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, p.shutdownTimeout)
	defer cancel()

	if err := worker.Shutdown(shutdownCtx); err != nil {
		log.Printf("[Pool] Worker %s shutdown error: %v", workerID, err)
	}

	// Remove from pool
	p.mu.Lock()
	delete(p.workers, workerID)
	delete(p.workerStates, workerID)
	totalWorkers := len(p.workers)
	p.mu.Unlock()

	// Unregister from metrics
	if p.metrics != nil {
		p.metrics.UnregisterWorker(workerID)
	}

	log.Printf("[Pool] Removed worker %s (total: %d)", workerID, totalWorkers)

	return nil
}

// GetWorkerCount returns the current number of active workers
func (p *Pool) GetWorkerCount() int32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return int32(len(p.workers))
}

// GetIdleWorker returns an idle worker ID, or empty string if none available
func (p *Pool) GetIdleWorker() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for workerID, state := range p.workerStates {
		if state == WorkerStateIdle {
			return workerID
		}
	}
	return ""
}

// GetWorkerStates returns a snapshot of all worker states
func (p *Pool) GetWorkerStates() map[string]WorkerState {
	p.mu.RLock()
	defer p.mu.RUnlock()

	states := make(map[string]WorkerState, len(p.workerStates))
	for id, state := range p.workerStates {
		states[id] = state
	}
	return states
}

// Shutdown gracefully stops all workers in the pool
func (p *Pool) Shutdown(ctx context.Context) error {
	log.Printf("[Pool] Shutting down pool with %d workers...", p.GetWorkerCount())

	// Cancel context to signal all workers
	p.cancelFunc()

	// Wait for all workers to finish with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("[Pool] All workers shut down gracefully")
	case <-ctx.Done():
		log.Println("[Pool] Shutdown timeout exceeded")
		return ctx.Err()
	}

	return nil
}

// createWorkerWithMetrics creates a worker with metrics tracking
func (p *Pool) createWorkerWithMetrics(workerID string) *Worker {
	worker := NewWorker(p.queue, p.storage, p.registry, Config{
		ID:           workerID,
		PollInterval: p.pollInterval,
	})

	// We'll track worker state changes through periodic monitoring
	// rather than wrapping internal methods
	return worker
}

// monitorWorkerStates periodically updates worker states based on activity
// This would be called from a background goroutine if needed
func (p *Pool) monitorWorkerStates() {
	// For now, we'll update states based on metrics
	// In a more sophisticated implementation, we could track
	// each worker's activity more granularly
}

// updateWorkerState updates the state of a worker
func (p *Pool) updateWorkerState(workerID string, state WorkerState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.workerStates[workerID]; exists {
		p.workerStates[workerID] = state
	}
}

// GetStats returns pool statistics
func (p *Pool) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	idleCount := 0
	busyCount := 0
	for _, state := range p.workerStates {
		switch state {
		case WorkerStateIdle:
			idleCount++
		case WorkerStateBusy:
			busyCount++
		}
	}

	return map[string]interface{}{
		"total_workers": len(p.workers),
		"idle_workers":  idleCount,
		"busy_workers":  busyCount,
		"min_workers":   p.minWorkers,
		"max_workers":   p.maxWorkers,
	}
}

