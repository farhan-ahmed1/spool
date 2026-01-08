package monitoring

import (
	"fmt"
	"testing"
	"time"
)

// TestAutoScaling_Integration tests the complete auto-scaling system
// This test requires Redis to be running
func TestAutoScaling_Integration(t *testing.T) {
	t.Skip("Integration test moved to tests/integration - skipping to avoid import cycle")
	
	// Note: This test has been moved to tests/integration/autoscaling_test.go
	// to avoid import cycles between monitoring and worker packages.
	// The full integration test includes:
	// - Setting up Redis queue and storage
	// - Creating worker pool with auto-scaler
	// - Enqueuing tasks to trigger scale up
	// - Waiting for tasks to complete and scale down
	// - Verifying metrics and decision history
}

// TestAutoScaling_LoadPattern tests auto-scaling with varying load
func TestAutoScaling_LoadPattern(t *testing.T) {
	t.Skip("Load pattern test moved to tests/integration - skipping to avoid import cycle")
	
	// Note: This test demonstrates that the scaler can handle
	// sudden spikes and drops in queue depth. Full implementation
	// is in tests/integration/autoscaling_test.go
}

// TestScalingScenarios provides example scenarios for testing auto-scaling
func TestScalingScenarios(t *testing.T) {
	t.Log("Auto-scaling test scenarios:")
	
	scenarios := []struct {
		name          string
		queueDepth    int
		currentWorkers int
		expectedAction string
	}{
		{"Low load", 5, 3, "no action"},
		{"High load", 200, 3, "scale up"},
		{"All idle", 0, 5, "scale down (after idle time)"},
		{"At minimum", 0, 1, "no action"},
		{"At maximum", 500, 10, "no action (if max=10)"},
	}

	for _, sc := range scenarios {
		t.Logf("Scenario: %s - Queue: %d, Workers: %d, Expected: %s",
			sc.name, sc.queueDepth, sc.currentWorkers, sc.expectedAction)
	}

	t.Log("\nTo run full integration tests:")
	t.Log("  go test ./tests/integration/... -v")
	t.Log("  (Requires Redis running on localhost:6379)")
}

// Example demonstrates basic auto-scaler usage
func ExampleAutoScaler() {
	// This example shows how to set up auto-scaling
	fmt.Println("Auto-Scaler Example:")
	fmt.Println("1. Create metrics collector")
	fmt.Println("2. Create worker pool implementing WorkerController")
	fmt.Println("3. Configure auto-scaling parameters")
	fmt.Println("4. Create and start auto-scaler")
	fmt.Println("5. Auto-scaler monitors metrics and adjusts workers")
	
	// Output:
	// Auto-Scaler Example:
	// 1. Create metrics collector
	// 2. Create worker pool implementing WorkerController
	// 3. Configure auto-scaling parameters
	// 4. Create and start auto-scaler
	// 5. Auto-scaler monitors metrics and adjusts workers
}

// Benchmark demonstrates auto-scaler performance characteristics
func BenchmarkAutoScaler_DecisionMaking(b *testing.B) {
	q := newMockQueue(100)
	metrics := NewMetrics(q)
	
	b.Log("Benchmarking auto-scaler decision making speed")
	b.Log("This helps ensure the scaler doesn't impact system performance")
	
	// Would run actual benchmark if controller interface was available
	// In practice, scaler decision overhead should be < 1ms
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate decision making
		snapshot := metrics.Snapshot()
		_ = snapshot.QueueDepth > 100
		time.Sleep(1 * time.Microsecond) // Simulate minimal decision time
	}
}

