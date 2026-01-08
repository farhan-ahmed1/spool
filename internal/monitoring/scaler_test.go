package monitoring

import (
	"context"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/config"
)

// mockController implements WorkerController interface for testing
type mockController struct {
	workers     map[string]bool // worker ID -> is idle
	workerCount int32
	addCalls    int
	removeCalls int
	addError    error
	removeError error
}

func newMockController(initialWorkers int32) *mockController {
	workers := make(map[string]bool)
	for i := int32(0); i < initialWorkers; i++ {
		workers[string(rune('A'+i))] = true
	}
	return &mockController{
		workers:     workers,
		workerCount: initialWorkers,
	}
}

func (m *mockController) GetWorkerCount() int32 {
	return m.workerCount
}

func (m *mockController) AddWorker(ctx context.Context) (string, error) {
	if m.addError != nil {
		return "", m.addError
	}
	m.addCalls++
	workerID := string(rune('A' + int(m.workerCount)))
	m.workers[workerID] = true // starts as idle
	m.workerCount++
	return workerID, nil
}

func (m *mockController) RemoveWorker(ctx context.Context, workerID string) error {
	if m.removeError != nil {
		return m.removeError
	}
	m.removeCalls++
	delete(m.workers, workerID)
	m.workerCount--
	return nil
}

func (m *mockController) GetIdleWorker() string {
	for id, idle := range m.workers {
		if idle {
			return id
		}
	}
	return ""
}

func TestAutoScaler_ScaleUp_QueueDepth(t *testing.T) {
	q := newMockQueue(150) // Above threshold of 100
	metrics := NewMetrics(q)
	controller := newMockController(2)

	cfg := config.AutoScalingConfig{
		Enabled:           true,
		MinWorkers:        1,
		MaxWorkers:        10,
		ScaleUpThreshold:  100,
		ScaleDownIdleTime: 5 * time.Minute,
		CheckInterval:     100 * time.Millisecond,
	}

	scaler := NewAutoScaler(metrics, controller, cfg)

	// Update metrics
	ctx := context.Background()
	metrics.UpdateQueueDepth(ctx)

	// Evaluate should trigger scale up
	if err := scaler.evaluate(ctx); err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}

	// Verify workers were added
	if controller.addCalls == 0 {
		t.Error("Expected workers to be added")
	}

	newCount := controller.GetWorkerCount()
	if newCount <= 2 {
		t.Errorf("Expected worker count > 2, got %d", newCount)
	}
}

func TestAutoScaler_ScaleDown_Idle(t *testing.T) {
	q := newMockQueue(0) // Empty queue
	metrics := NewMetrics(q)
	controller := newMockController(5)

	// Register all workers as idle for a long time
	for id := range controller.workers {
		metrics.RegisterWorker(id)
	}

	// Set idle time to past threshold
	time.Sleep(10 * time.Millisecond)

	cfg := config.AutoScalingConfig{
		Enabled:           true,
		MinWorkers:        2,
		MaxWorkers:        10,
		ScaleUpThreshold:  100,
		ScaleDownIdleTime: 5 * time.Millisecond, // Very short for testing
		CheckInterval:     100 * time.Millisecond,
	}

	scaler := NewAutoScaler(metrics, controller, cfg)

	// Update metrics
	ctx := context.Background()
	if err := metrics.UpdateQueueDepth(ctx); err != nil {
		t.Fatalf("UpdateQueueDepth failed: %v", err)
	}

	// First evaluation - should increase consecutive idle counter
	if err := scaler.evaluate(ctx); err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}

	// Second evaluation - should trigger scale down
	if err := scaler.evaluate(ctx); err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}

	// Verify workers were removed
	if controller.removeCalls == 0 {
		t.Error("Expected workers to be removed")
	}

	newCount := controller.GetWorkerCount()
	if newCount >= 5 {
		t.Errorf("Expected worker count < 5, got %d", newCount)
	}
	if newCount < 2 {
		t.Errorf("Expected worker count >= min (2), got %d", newCount)
	}
}

func TestAutoScaler_RespectMinWorkers(t *testing.T) {
	q := newMockQueue(0)
	metrics := NewMetrics(q)
	controller := newMockController(2)

	cfg := config.AutoScalingConfig{
		Enabled:           true,
		MinWorkers:        2,
		MaxWorkers:        10,
		ScaleUpThreshold:  100,
		ScaleDownIdleTime: 1 * time.Millisecond,
		CheckInterval:     100 * time.Millisecond,
	}

	scaler := NewAutoScaler(metrics, controller, cfg)

	ctx := context.Background()
	metrics.UpdateQueueDepth(ctx)

	// Try to scale down when at minimum
	decision := scaler.decide(metrics.Snapshot(), 2)

	if decision.Action != NoAction {
		t.Errorf("Expected no action when at minimum workers, got %v", decision.Action)
	}
}

func TestAutoScaler_RespectMaxWorkers(t *testing.T) {
	q := newMockQueue(1000) // Very high queue depth
	metrics := NewMetrics(q)
	controller := newMockController(10)

	cfg := config.AutoScalingConfig{
		Enabled:           true,
		MinWorkers:        1,
		MaxWorkers:        10,
		ScaleUpThreshold:  100,
		ScaleDownIdleTime: 5 * time.Minute,
		CheckInterval:     100 * time.Millisecond,
	}

	scaler := NewAutoScaler(metrics, controller, cfg)

	ctx := context.Background()
	metrics.UpdateQueueDepth(ctx)

	// Try to scale up when at maximum
	decision := scaler.decide(metrics.Snapshot(), 10)

	if decision.Action != NoAction {
		t.Errorf("Expected no action when at maximum workers, got %v", decision.Action)
	}
}

func TestAutoScaler_Cooldown(t *testing.T) {
	q := newMockQueue(150)
	metrics := NewMetrics(q)
	controller := newMockController(2)

	cfg := config.AutoScalingConfig{
		Enabled:           true,
		MinWorkers:        1,
		MaxWorkers:        10,
		ScaleUpThreshold:  100,
		ScaleDownIdleTime: 5 * time.Minute,
		CheckInterval:     100 * time.Millisecond,
	}

	scaler := NewAutoScaler(metrics, controller, cfg)
	scaler.scaleUpCooldown = 1 * time.Second // Set cooldown

	ctx := context.Background()
	if err := metrics.UpdateQueueDepth(ctx); err != nil {
		t.Fatalf("UpdateQueueDepth failed: %v", err)
	}

	// First scale up
	if err := scaler.evaluate(ctx); err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	firstAddCalls := controller.addCalls

	// Immediate second evaluation should not scale due to cooldown
	if err := scaler.evaluate(ctx); err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if controller.addCalls != firstAddCalls {
		t.Error("Expected cooldown to prevent immediate scale up")
	}

	// After cooldown, should scale again
	scaler.lastScaleUp = time.Now().Add(-2 * time.Second)
	if err := scaler.evaluate(ctx); err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if controller.addCalls == firstAddCalls {
		t.Error("Expected scale up after cooldown period")
	}
}

func TestAutoScaler_DecisionHistory(t *testing.T) {
	q := newMockQueue(150)
	metrics := NewMetrics(q)
	controller := newMockController(2)

	cfg := config.AutoScalingConfig{
		Enabled:           true,
		MinWorkers:        1,
		MaxWorkers:        10,
		ScaleUpThreshold:  100,
		ScaleDownIdleTime: 5 * time.Minute,
		CheckInterval:     100 * time.Millisecond,
	}

	scaler := NewAutoScaler(metrics, controller, cfg)

	ctx := context.Background()
	if err := metrics.UpdateQueueDepth(ctx); err != nil {
		t.Fatalf("UpdateQueueDepth failed: %v", err)
	}

	// Trigger some scaling decisions
	if err := scaler.evaluate(ctx); err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}

	history := scaler.GetDecisionHistory(10)
	if len(history) == 0 {
		t.Error("Expected decision history to be recorded")
	}

	decision := history[len(history)-1]
	if decision.Action == NoAction {
		t.Error("Expected a scaling action in history")
	}
	if decision.QueueDepth != 150 {
		t.Errorf("Expected queue depth 150 in decision, got %d", decision.QueueDepth)
	}
}

func TestAutoScaler_Stats(t *testing.T) {
	q := newMockQueue(150)
	metrics := NewMetrics(q)
	controller := newMockController(2)

	cfg := config.AutoScalingConfig{
		Enabled:           true,
		MinWorkers:        1,
		MaxWorkers:        10,
		ScaleUpThreshold:  100,
		ScaleDownIdleTime: 5 * time.Minute,
		CheckInterval:     100 * time.Millisecond,
	}

	scaler := NewAutoScaler(metrics, controller, cfg)

	stats := scaler.GetStats()
	if stats["running"] != false {
		t.Error("Expected scaler to not be running initially")
	}

	// Start scaler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := scaler.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // Let it run briefly

	stats = scaler.GetStats()
	if stats["running"] != true {
		t.Error("Expected scaler to be running after start")
	}

	scaler.Stop()
	time.Sleep(50 * time.Millisecond)

	stats = scaler.GetStats()
	if stats["running"] != false {
		t.Error("Expected scaler to be stopped after stop")
	}
}

func TestAutoScaler_GradualScaling(t *testing.T) {
	q := newMockQueue(500) // High queue depth
	metrics := NewMetrics(q)
	controller := newMockController(2)

	cfg := config.AutoScalingConfig{
		Enabled:           true,
		MinWorkers:        1,
		MaxWorkers:        20,
		ScaleUpThreshold:  100,
		ScaleDownIdleTime: 5 * time.Minute,
		CheckInterval:     100 * time.Millisecond,
	}

	scaler := NewAutoScaler(metrics, controller, cfg)

	ctx := context.Background()
	metrics.UpdateQueueDepth(ctx)

	decision := scaler.decide(metrics.Snapshot(), 2)

	// Should scale gradually, not jump to max
	targetIncrease := decision.DesiredCount - decision.CurrentCount
	maxAllowedIncrease := decision.CurrentCount/2 + 1

	if targetIncrease > maxAllowedIncrease {
		t.Errorf("Expected gradual scaling (max %d workers), but decision wants to add %d",
			maxAllowedIncrease, targetIncrease)
	}
}

func BenchmarkAutoScaler_Evaluate(b *testing.B) {
	q := newMockQueue(100)
	metrics := NewMetrics(q)
	controller := newMockController(5)

	cfg := config.AutoScalingConfig{
		Enabled:           true,
		MinWorkers:        1,
		MaxWorkers:        10,
		ScaleUpThreshold:  100,
		ScaleDownIdleTime: 5 * time.Minute,
		CheckInterval:     1 * time.Second,
	}

	scaler := NewAutoScaler(metrics, controller, cfg)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scaler.evaluate(ctx)
	}
}
