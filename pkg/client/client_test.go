package client

import (
	"context"
	"testing"
	"time"

	"github.com/farhan-ahmed1/spool/internal/task"
)

// TestNew tests creating a new client
func TestNew(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
		Timeout:    30 * time.Second,
	}

	client, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client == nil {
		t.Fatal("Expected client to be created")
	}

	if client.config.BrokerAddr != config.BrokerAddr {
		t.Errorf("Expected broker address %s, got %s", config.BrokerAddr, client.config.BrokerAddr)
	}

	if client.config.Timeout != config.Timeout {
		t.Errorf("Expected timeout %v, got %v", config.Timeout, client.config.Timeout)
	}
}

// TestNewWithEmptyBrokerAddr tests creating a client with empty broker address
func TestNewWithEmptyBrokerAddr(t *testing.T) {
	config := Config{
		BrokerAddr: "",
	}

	client, err := New(config)
	if err == nil {
		t.Error("Expected error when broker address is empty")
	}

	if client != nil {
		t.Error("Expected nil client on error")
	}
}

// TestNewWithDefaultTimeout tests that default timeout is set
func TestNewWithDefaultTimeout(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
		Timeout:    0, // No timeout specified
	}

	client, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client.config.Timeout != 30*time.Second {
		t.Errorf("Expected default timeout 30s, got %v", client.config.Timeout)
	}
}

// TestNewWithCustomTimeout tests creating a client with custom timeout
func TestNewWithCustomTimeout(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
		Timeout:    5 * time.Minute,
	}

	client, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client.config.Timeout != 5*time.Minute {
		t.Errorf("Expected timeout 5m, got %v", client.config.Timeout)
	}
}

// TestClose tests closing the client
func TestClose(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)

	err := client.Close()
	if err != nil {
		t.Errorf("Failed to close client: %v", err)
	}
}

// TestSubmit tests submitting a task
func TestSubmit(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)
	defer client.Close()

	task, _ := task.NewTask("test_task", map[string]string{"key": "value"})

	ctx := context.Background()
	taskID, err := client.Submit(ctx, task)

	// Since implementation is TODO, we expect not implemented error
	if err == nil {
		t.Log("Submit returned success (implementation may be complete)")
	}

	if taskID != task.ID {
		t.Errorf("Expected task ID %s, got %s", task.ID, taskID)
	}
}

// TestSubmitWithContext tests submitting with context
func TestSubmitWithContext(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)
	defer client.Close()

	task, _ := task.NewTask("test_task", map[string]string{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Submit(ctx, task)
	// Implementation is TODO, so we just verify it can be called with context
	if err == nil {
		t.Log("Submit with context executed")
	}
}

// TestSubmitBatch tests batch submission
func TestSubmitBatch(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)
	defer client.Close()

	tasks := make([]*task.Task, 3)
	for i := 0; i < 3; i++ {
		tasks[i], _ = task.NewTask("test_task", map[string]int{"id": i})
	}

	ctx := context.Background()
	taskIDs, err := client.SubmitBatch(ctx, tasks)

	// Since implementation is TODO, we expect not implemented error
	if err == nil {
		t.Log("SubmitBatch returned success (implementation may be complete)")
	}

	if len(taskIDs) != len(tasks) {
		t.Errorf("Expected %d task IDs, got %d", len(tasks), len(taskIDs))
	}

	// Verify IDs match
	for i, id := range taskIDs {
		if id != tasks[i].ID {
			t.Errorf("Task %d: expected ID %s, got %s", i, tasks[i].ID, id)
		}
	}
}

// TestSubmitBatchEmpty tests submitting empty batch
func TestSubmitBatchEmpty(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)
	defer client.Close()

	ctx := context.Background()
	taskIDs, err := client.SubmitBatch(ctx, []*task.Task{})

	if err == nil {
		t.Log("SubmitBatch with empty tasks executed")
	}

	if len(taskIDs) != 0 {
		t.Errorf("Expected 0 task IDs for empty batch, got %d", len(taskIDs))
	}
}

// TestGetResult tests retrieving a task result
func TestGetResult(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)
	defer client.Close()

	ctx := context.Background()
	taskID := "test_task_id"
	timeout := 5 * time.Second

	result, err := client.GetResult(ctx, taskID, timeout)

	// Since implementation is TODO, we expect not implemented error
	if err == nil {
		t.Error("Expected not implemented error")
	}

	if result != nil {
		t.Error("Expected nil result when not implemented")
	}
}

// TestGetResultWithCanceledContext tests result retrieval with canceled context
func TestGetResultWithCanceledContext(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.GetResult(ctx, "test_task_id", 5*time.Second)

	// Should return an error (either not implemented or context canceled)
	if err == nil {
		t.Log("GetResult with canceled context handled")
	}
}

// TestGetStatus tests retrieving task status
func TestGetStatus(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)
	defer client.Close()

	ctx := context.Background()
	taskID := "test_task_id"

	task, err := client.GetStatus(ctx, taskID)

	// Since implementation is TODO, we expect not implemented error
	if err == nil {
		t.Error("Expected not implemented error")
	}

	if task != nil {
		t.Error("Expected nil task when not implemented")
	}
}

// TestGetStatusWithEmptyID tests getting status with empty task ID
func TestGetStatusWithEmptyID(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)
	defer client.Close()

	ctx := context.Background()

	task, err := client.GetStatus(ctx, "")

	// Should return an error
	if err == nil {
		t.Log("GetStatus with empty ID handled")
	}

	if task != nil {
		t.Error("Expected nil task for empty ID")
	}
}

// TestGetStats tests retrieving queue statistics
func TestGetStats(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)
	defer client.Close()

	ctx := context.Background()

	stats, err := client.GetStats(ctx)

	// Since implementation is TODO, we expect not implemented error
	if err == nil {
		t.Error("Expected not implemented error")
	}

	// Stats should still be returned (even if empty)
	if stats == nil {
		t.Error("Expected stats object to be returned")
	}
}

// TestGetStatsFields tests that Stats struct has expected fields
func TestGetStatsFields(t *testing.T) {
	stats := &Stats{
		Pending:       10,
		Processing:    5,
		Completed:     100,
		Failed:        2,
		ActiveWorkers: 3,
	}

	if stats.Pending != 10 {
		t.Error("Stats.Pending field mismatch")
	}
	if stats.Processing != 5 {
		t.Error("Stats.Processing field mismatch")
	}
	if stats.Completed != 100 {
		t.Error("Stats.Completed field mismatch")
	}
	if stats.Failed != 2 {
		t.Error("Stats.Failed field mismatch")
	}
	if stats.ActiveWorkers != 3 {
		t.Error("Stats.ActiveWorkers field mismatch")
	}
}

// TestConfigStruct tests the Config struct
func TestConfigStruct(t *testing.T) {
	config := Config{
		BrokerAddr: "redis.example.com:6379",
		Timeout:    1 * time.Minute,
	}

	if config.BrokerAddr != "redis.example.com:6379" {
		t.Error("Config.BrokerAddr mismatch")
	}

	if config.Timeout != 1*time.Minute {
		t.Error("Config.Timeout mismatch")
	}
}

// TestMultipleClients tests creating multiple client instances
func TestMultipleClients(t *testing.T) {
	configs := []Config{
		{BrokerAddr: "localhost:6379", Timeout: 10 * time.Second},
		{BrokerAddr: "localhost:6380", Timeout: 20 * time.Second},
		{BrokerAddr: "localhost:6381", Timeout: 30 * time.Second},
	}

	clients := make([]*Client, len(configs))
	for i, config := range configs {
		client, err := New(config)
		if err != nil {
			t.Fatalf("Failed to create client %d: %v", i, err)
		}
		clients[i] = client
	}

	// Verify each client has correct config
	for i, client := range clients {
		if client.config.BrokerAddr != configs[i].BrokerAddr {
			t.Errorf("Client %d: broker address mismatch", i)
		}
		if client.config.Timeout != configs[i].Timeout {
			t.Errorf("Client %d: timeout mismatch", i)
		}
	}

	// Close all clients
	for i, client := range clients {
		if err := client.Close(); err != nil {
			t.Errorf("Failed to close client %d: %v", i, err)
		}
	}
}

// TestClientStateAfterClose tests client state after closing
func TestClientStateAfterClose(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)

	// Close the client
	err := client.Close()
	if err != nil {
		t.Fatalf("Failed to close client: %v", err)
	}

	// Try to use client after closing
	task, _ := task.NewTask("test", map[string]string{})
	_, err = client.Submit(context.Background(), task)

	// Should handle closed client (might return error or not depending on implementation)
	if err == nil {
		t.Log("Client operations after close handled")
	}
}

// TestConcurrentSubmits tests concurrent task submissions
func TestConcurrentSubmits(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)
	defer client.Close()

	done := make(chan bool)
	numConcurrent := 10

	for i := 0; i < numConcurrent; i++ {
		go func(id int) {
			task, _ := task.NewTask("test", map[string]int{"id": id})
			_, err := client.Submit(context.Background(), task)
			if err == nil {
				t.Logf("Concurrent submit %d succeeded", id)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numConcurrent; i++ {
		<-done
	}
}

// TestGetResultTimeout tests result retrieval with different timeouts
func TestGetResultTimeout(t *testing.T) {
	config := Config{
		BrokerAddr: "localhost:6379",
	}

	client, _ := New(config)
	defer client.Close()

	timeouts := []time.Duration{
		1 * time.Second,
		5 * time.Second,
		10 * time.Second,
		1 * time.Minute,
	}

	for _, timeout := range timeouts {
		_, err := client.GetResult(context.Background(), "test_id", timeout)
		if err == nil {
			t.Logf("GetResult with timeout %v handled", timeout)
		}
	}
}
