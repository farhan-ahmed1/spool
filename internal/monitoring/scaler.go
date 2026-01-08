package monitoring

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/farhan-ahmed1/spool/internal/config"
)

// ScalingDecision represents a decision to scale workers
type ScalingDecision struct {
	Action        ScalingAction
	CurrentCount  int32
	DesiredCount  int32
	Reason        string
	QueueDepth    int64
	IdleTime      time.Duration
	Throughput    float64
	DecisionTime  time.Time
}

// ScalingAction represents the type of scaling action
type ScalingAction string

const (
	ScaleUp   ScalingAction = "scale_up"
	ScaleDown ScalingAction = "scale_down"
	NoAction  ScalingAction = "no_action"
)

// WorkerController defines the interface for controlling workers
type WorkerController interface {
	// GetWorkerCount returns the current number of active workers
	GetWorkerCount() int32

	// AddWorker spawns a new worker and returns its ID
	AddWorker(ctx context.Context) (string, error)

	// RemoveWorker gracefully shuts down a worker
	RemoveWorker(ctx context.Context, workerID string) error

	// GetIdleWorker returns an idle worker ID if available
	GetIdleWorker() string
}

// AutoScaler manages dynamic worker scaling based on metrics
type AutoScaler struct {
	metrics    *Metrics
	controller WorkerController
	config     config.AutoScalingConfig

	mu               sync.RWMutex
	running          bool
	stopChan         chan struct{}
	decisions        []ScalingDecision
	lastScaleUp      time.Time
	lastScaleDown    time.Time
	consecutiveIdle  int
	scaleUpCount     int64
	scaleDownCount   int64

	// Cooldown prevents rapid scaling oscillations
	scaleUpCooldown   time.Duration
	scaleDownCooldown time.Duration
}

// NewAutoScaler creates a new auto-scaler
func NewAutoScaler(metrics *Metrics, controller WorkerController, cfg config.AutoScalingConfig) *AutoScaler {
	return &AutoScaler{
		metrics:           metrics,
		controller:        controller,
		config:            cfg,
		stopChan:          make(chan struct{}),
		decisions:         make([]ScalingDecision, 0, 100),
		scaleUpCooldown:   30 * time.Second,  // Wait 30s after scaling up
		scaleDownCooldown: 2 * time.Minute,   // Wait 2m after scaling down
	}
}

// Start begins the auto-scaling loop
func (s *AutoScaler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("auto-scaler is already running")
	}
	s.running = true
	s.mu.Unlock()

	log.Printf("[AutoScaler] Starting with config: min=%d, max=%d, threshold=%d, idle=%v, interval=%v",
		s.config.MinWorkers, s.config.MaxWorkers, s.config.ScaleUpThreshold,
		s.config.ScaleDownIdleTime, s.config.CheckInterval)

	go s.run(ctx)
	return nil
}

// Stop stops the auto-scaling loop
func (s *AutoScaler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	log.Println("[AutoScaler] Stopping...")
	close(s.stopChan)
	s.running = false
}

// run is the main auto-scaling loop
func (s *AutoScaler) run(ctx context.Context) {
	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[AutoScaler] Context cancelled")
			return
		case <-s.stopChan:
			log.Println("[AutoScaler] Stop signal received")
			return
		case <-ticker.C:
			if err := s.evaluate(ctx); err != nil {
				log.Printf("[AutoScaler] Error during evaluation: %v", err)
			}
		}
	}
}

// evaluate checks metrics and makes scaling decisions
func (s *AutoScaler) evaluate(ctx context.Context) error {
	// Update queue depth
	if err := s.metrics.UpdateQueueDepth(ctx); err != nil {
		return fmt.Errorf("failed to update queue depth: %w", err)
	}

	// Get current metrics
	snapshot := s.metrics.Snapshot()
	currentWorkers := s.controller.GetWorkerCount()

	// Make scaling decision
	decision := s.decide(snapshot, currentWorkers)

	// Record decision
	s.recordDecision(decision)

	// Execute decision
	if decision.Action != NoAction {
		log.Printf("[AutoScaler] Decision: %s from %d to %d workers. Reason: %s",
			decision.Action, decision.CurrentCount, decision.DesiredCount, decision.Reason)

		if err := s.execute(ctx, decision); err != nil {
			log.Printf("[AutoScaler] Failed to execute %s: %v", decision.Action, err)
			return err
		}
	}

	return nil
}

// decide determines if scaling is needed based on metrics
func (s *AutoScaler) decide(snapshot MetricsSnapshot, currentWorkers int32) ScalingDecision {
	decision := ScalingDecision{
		Action:       NoAction,
		CurrentCount: currentWorkers,
		DesiredCount: currentWorkers,
		QueueDepth:   snapshot.QueueDepth,
		IdleTime:     snapshot.OldestIdleTime,
		Throughput:   snapshot.CurrentThroughput,
		DecisionTime: time.Now(),
	}

	// Check if we should scale up
	if s.shouldScaleUp(snapshot, currentWorkers) {
		decision.Action = ScaleUp
		decision.DesiredCount = s.calculateScaleUpTarget(snapshot, currentWorkers)
		decision.Reason = fmt.Sprintf("Queue depth %d exceeds threshold %d",
			snapshot.QueueDepth, s.config.ScaleUpThreshold)
		return decision
	}

	// Check if we should scale down
	if s.shouldScaleDown(snapshot, currentWorkers) {
		decision.Action = ScaleDown
		decision.DesiredCount = s.calculateScaleDownTarget(snapshot, currentWorkers)
		decision.Reason = fmt.Sprintf("Workers idle for %v (threshold: %v), idle workers: %d/%d",
			snapshot.OldestIdleTime, s.config.ScaleDownIdleTime,
			snapshot.IdleWorkers, currentWorkers)
		return decision
	}

	return decision
}

// shouldScaleUp determines if we should add workers
func (s *AutoScaler) shouldScaleUp(snapshot MetricsSnapshot, currentWorkers int32) bool {
	// Already at max capacity
	if currentWorkers >= int32(s.config.MaxWorkers) {
		return false
	}

	// Cooldown period after last scale-up
	if time.Since(s.lastScaleUp) < s.scaleUpCooldown {
		return false
	}

	// Queue depth exceeds threshold
	if snapshot.QueueDepth > int64(s.config.ScaleUpThreshold) {
		return true
	}

	// All workers busy and queue is growing
	if snapshot.IdleWorkers == 0 && snapshot.QueueDepth > 0 {
		return true
	}

	// High task-to-worker ratio
	if snapshot.QueueDepth > int64(currentWorkers*10) {
		return true
	}

	return false
}

// shouldScaleDown determines if we should remove workers
func (s *AutoScaler) shouldScaleDown(snapshot MetricsSnapshot, currentWorkers int32) bool {
	// Already at min capacity
	if currentWorkers <= int32(s.config.MinWorkers) {
		return false
	}

	// No idle workers
	if snapshot.IdleWorkers == 0 {
		s.consecutiveIdle = 0
		return false
	}

	// Cooldown period after last scale-down
	if time.Since(s.lastScaleDown) < s.scaleDownCooldown {
		return false
	}

	// Workers have been idle for long enough
	if snapshot.OldestIdleTime > s.config.ScaleDownIdleTime {
		s.consecutiveIdle++
		// Require multiple consecutive checks to avoid flapping
		return s.consecutiveIdle >= 2
	}

	// Queue is empty and more than half workers are idle
	if snapshot.QueueDepth == 0 && snapshot.IdleWorkers >= currentWorkers/2 {
		return true
	}

	s.consecutiveIdle = 0
	return false
}

// calculateScaleUpTarget determines how many workers to add
func (s *AutoScaler) calculateScaleUpTarget(snapshot MetricsSnapshot, current int32) int32 {
	// Calculate desired workers based on queue depth
	// Add 1 worker per 50 tasks in queue (configurable ratio)
	desiredWorkers := current + int32(snapshot.QueueDepth/50)
	if desiredWorkers <= current {
		desiredWorkers = current + 1 // Add at least 1
	}

	// Cap at max workers
	if desiredWorkers > int32(s.config.MaxWorkers) {
		desiredWorkers = int32(s.config.MaxWorkers)
	}

	// Scale gradually: add at most 50% of current workers at once
	maxIncrease := current/2 + 1
	if desiredWorkers > current+maxIncrease {
		desiredWorkers = current + maxIncrease
	}

	return desiredWorkers
}

// calculateScaleDownTarget determines how many workers to remove
func (s *AutoScaler) calculateScaleDownTarget(snapshot MetricsSnapshot, current int32) int32 {
	// Remove idle workers, but keep at least min workers
	desiredWorkers := current - snapshot.IdleWorkers

	// Always maintain minimum workers
	if desiredWorkers < int32(s.config.MinWorkers) {
		desiredWorkers = int32(s.config.MinWorkers)
	}

	// Scale gradually: remove at most 25% of current workers at once
	maxDecrease := current / 4
	if maxDecrease < 1 {
		maxDecrease = 1
	}
	if current-desiredWorkers > maxDecrease {
		desiredWorkers = current - maxDecrease
	}

	return desiredWorkers
}

// execute carries out the scaling decision
func (s *AutoScaler) execute(ctx context.Context, decision ScalingDecision) error {
	switch decision.Action {
	case ScaleUp:
		return s.scaleUp(ctx, decision)
	case ScaleDown:
		return s.scaleDown(ctx, decision)
	default:
		return nil
	}
}

// scaleUp adds workers to reach the desired count
func (s *AutoScaler) scaleUp(ctx context.Context, decision ScalingDecision) error {
	workersToAdd := decision.DesiredCount - decision.CurrentCount

	log.Printf("[AutoScaler] Scaling up: adding %d workers (%d -> %d)",
		workersToAdd, decision.CurrentCount, decision.DesiredCount)

	var errors []error
	for i := int32(0); i < workersToAdd; i++ {
		workerID, err := s.controller.AddWorker(ctx)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to add worker: %w", err))
			log.Printf("[AutoScaler] Failed to add worker %d/%d: %v", i+1, workersToAdd, err)
			continue
		}
		log.Printf("[AutoScaler] Added worker: %s (%d/%d)", workerID, i+1, workersToAdd)
	}

	s.mu.Lock()
	s.lastScaleUp = time.Now()
	s.scaleUpCount++
	s.mu.Unlock()

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors while scaling up", len(errors))
	}

	return nil
}

// scaleDown removes workers to reach the desired count
func (s *AutoScaler) scaleDown(ctx context.Context, decision ScalingDecision) error {
	workersToRemove := decision.CurrentCount - decision.DesiredCount

	log.Printf("[AutoScaler] Scaling down: removing %d workers (%d -> %d)",
		workersToRemove, decision.CurrentCount, decision.DesiredCount)

	var errors []error
	for i := int32(0); i < workersToRemove; i++ {
		// Get an idle worker to remove
		workerID := s.controller.GetIdleWorker()
		if workerID == "" {
			log.Printf("[AutoScaler] No idle workers available to remove")
			break
		}

		if err := s.controller.RemoveWorker(ctx, workerID); err != nil {
			errors = append(errors, fmt.Errorf("failed to remove worker %s: %w", workerID, err))
			log.Printf("[AutoScaler] Failed to remove worker %s: %v", workerID, err)
			continue
		}
		log.Printf("[AutoScaler] Removed worker: %s (%d/%d)", workerID, i+1, workersToRemove)
	}

	s.mu.Lock()
	s.lastScaleDown = time.Now()
	s.scaleDownCount++
	s.consecutiveIdle = 0 // Reset counter after scaling down
	s.mu.Unlock()

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors while scaling down", len(errors))
	}

	return nil
}

// recordDecision stores the decision for history/analysis
func (s *AutoScaler) recordDecision(decision ScalingDecision) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.decisions = append(s.decisions, decision)

	// Keep only last 100 decisions
	if len(s.decisions) > 100 {
		s.decisions = s.decisions[1:]
	}
}

// GetDecisionHistory returns recent scaling decisions
func (s *AutoScaler) GetDecisionHistory(limit int) []ScalingDecision {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.decisions) {
		limit = len(s.decisions)
	}

	start := len(s.decisions) - limit
	history := make([]ScalingDecision, limit)
	copy(history, s.decisions[start:])
	return history
}

// GetStats returns auto-scaler statistics
func (s *AutoScaler) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"running":           s.running,
		"scale_up_count":    s.scaleUpCount,
		"scale_down_count":  s.scaleDownCount,
		"last_scale_up":     s.lastScaleUp,
		"last_scale_down":   s.lastScaleDown,
		"decisions_count":   len(s.decisions),
		"consecutive_idle":  s.consecutiveIdle,
	}
}
