package load

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/farhan-ahmed1/spool/internal/queue"
	"github.com/farhan-ahmed1/spool/internal/storage"
	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/farhan-ahmed1/spool/internal/worker"
	"github.com/redis/go-redis/v9"
)

// BenchmarkConfig holds configuration for benchmark runs
type BenchmarkConfig struct {
	WorkerCount     int
	TaskCount       int
	Duration        time.Duration
	TaskType        string
	Priority        task.Priority
	PollInterval    time.Duration
	RedisPoolSize   int
	PayloadSize     int // in bytes
	EnableProfiling bool
	CPUProfile      string
	MemProfile      string
}

// DefaultBenchmarkConfig returns reasonable defaults
func DefaultBenchmarkConfig() *BenchmarkConfig {
	return &BenchmarkConfig{
		WorkerCount:   4,
		TaskCount:     1000,
		Duration:      30 * time.Second,
		TaskType:      "benchmark_task",
		Priority:      task.PriorityNormal,
		PollInterval:  10 * time.Millisecond,
		RedisPoolSize: 20,
		PayloadSize:   1024, // 1KB
	}
}

// BenchmarkResults holds the results of a benchmark run
type BenchmarkResults struct {
	TotalTasks     int64
	CompletedTasks int64
	FailedTasks    int64
	Duration       time.Duration
	TasksPerSecond float64
	AvgLatency     time.Duration
	P50Latency     time.Duration
	P95Latency     time.Duration
	P99Latency     time.Duration
	MinLatency     time.Duration
	MaxLatency     time.Duration
	MemAllocated   uint64
	MemAllocTotal  uint64
	GoroutineCount int
	ErrorRate      float64
}

// String returns a formatted string of the results
func (br *BenchmarkResults) String() string {
	return fmt.Sprintf(`
Benchmark Results:
==================
Total Tasks:        %d
Completed:          %d
Failed:             %d
Duration:           %v
Tasks/Second:       %.2f
Error Rate:         %.2f%%

Latency:
  Average:          %v
  P50:              %v
  P95:              %v
  P99:              %v
  Min:              %v
  Max:              %v

Memory:
  Allocated:        %.2f MB
  Total Allocated:  %.2f MB
  Goroutines:       %d
`,
		br.TotalTasks,
		br.CompletedTasks,
		br.FailedTasks,
		br.Duration,
		br.TasksPerSecond,
		br.ErrorRate,
		br.AvgLatency,
		br.P50Latency,
		br.P95Latency,
		br.P99Latency,
		br.MinLatency,
		br.MaxLatency,
		float64(br.MemAllocated)/(1024*1024),
		float64(br.MemAllocTotal)/(1024*1024),
		br.GoroutineCount,
	)
}

// BenchmarkRunner orchestrates benchmark execution
type BenchmarkRunner struct {
	config   *BenchmarkConfig
	queue    *queue.RedisQueue
	storage  storage.Storage
	workers  []*worker.Worker
	registry *task.Registry
	client   *redis.Client

	// Metrics
	startTime   time.Time
	endTime     time.Time
	latencies   []time.Duration
	latenciesMu sync.Mutex
	tasksEnq    atomic.Int64
	tasksComp   atomic.Int64
	tasksFail   atomic.Int64
}

// NewBenchmarkRunner creates a new benchmark runner
func NewBenchmarkRunner(config *BenchmarkConfig) (*BenchmarkRunner, error) {
	if config == nil {
		config = DefaultBenchmarkConfig()
	}

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:         "localhost:6379",
		Password:     "",
		DB:           0,
		PoolSize:     config.RedisPoolSize,
		MinIdleConns: config.RedisPoolSize / 2,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	// Create queue
	q, err := queue.NewRedisQueue("localhost:6379", "", 0, config.RedisPoolSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create queue: %w", err)
	}

	// Create storage
	store := storage.NewRedisStorage(client)

	// Create registry with benchmark handler
	registry := task.NewRegistry()
	err = registry.Register(config.TaskType, func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
		// Simple processing - unmarshal and return
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"processed": true,
			"timestamp": time.Now().Unix(),
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to register handler: %w", err)
	}

	// Create workers
	workers := make([]*worker.Worker, config.WorkerCount)
	for i := 0; i < config.WorkerCount; i++ {
		workers[i] = worker.NewWorker(q, store, registry, worker.Config{
			ID:           fmt.Sprintf("bench-worker-%d", i),
			PollInterval: config.PollInterval,
		})
	}

	return &BenchmarkRunner{
		config:    config,
		queue:     q,
		storage:   store,
		workers:   workers,
		registry:  registry,
		client:    client,
		latencies: make([]time.Duration, 0, config.TaskCount),
	}, nil
}

// Run executes the benchmark
func (br *BenchmarkRunner) Run() (*BenchmarkResults, error) {
	log.Printf("Starting benchmark: workers=%d, tasks=%d, duration=%v",
		br.config.WorkerCount, br.config.TaskCount, br.config.Duration)

	// Clear any existing tasks
	ctx := context.Background()
	if err := br.queue.Purge(ctx); err != nil {
		return nil, fmt.Errorf("failed to purge queue: %w", err)
	}

	// Start profiling if enabled
	if br.config.EnableProfiling {
		if err := br.startProfiling(); err != nil {
			log.Printf("Warning: failed to start profiling: %v", err)
		}
	}

	// Start workers
	for _, w := range br.workers {
		if err := w.Start(ctx); err != nil {
			return nil, fmt.Errorf("failed to start worker: %v", err)
		}
	}

	// Wait for workers to be ready
	time.Sleep(100 * time.Millisecond)

	// Record start time
	br.startTime = time.Now()

	// Enqueue tasks
	log.Println("Enqueueing tasks...")
	if err := br.enqueueTasks(ctx); err != nil {
		return nil, fmt.Errorf("failed to enqueue tasks: %w", err)
	}

	// Wait for completion or timeout
	log.Println("Waiting for task completion...")
	if err := br.waitForCompletion(ctx); err != nil {
		return nil, fmt.Errorf("benchmark failed: %w", err)
	}

	// Record end time
	br.endTime = time.Now()

	// Stop profiling if enabled
	if br.config.EnableProfiling {
		if err := br.stopProfiling(); err != nil {
			log.Printf("Warning: failed to stop profiling: %v", err)
		}
	}

	// Collect results
	results := br.collectResults()

	// Cleanup
	br.cleanup()

	return results, nil
}

// enqueueTasks enqueues all benchmark tasks
func (br *BenchmarkRunner) enqueueTasks(ctx context.Context) error {
	payload := make(map[string]interface{})
	payload["data"] = generatePayload(br.config.PayloadSize)

	var wg sync.WaitGroup
	errChan := make(chan error, br.config.TaskCount)
	semaphore := make(chan struct{}, 100) // Limit concurrent enqueues

	for i := 0; i < br.config.TaskCount; i++ {
		wg.Add(1)
		semaphore <- struct{}{} // Acquire

		go func(taskNum int) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release

			// Create a copy of payload for this task
			taskPayload := make(map[string]interface{})
			for k, v := range payload {
				taskPayload[k] = v
			}
			taskPayload["task_id"] = taskNum

			t, err := task.NewTask(br.config.TaskType, taskPayload)
			if err != nil {
				errChan <- fmt.Errorf("failed to create task %d: %w", taskNum, err)
				return
			}

			t.Priority = br.config.Priority
			t.Metadata = map[string]interface{}{
				"enqueue_time": time.Now().Format(time.RFC3339Nano),
			}

			if err := br.queue.Enqueue(ctx, t); err != nil {
				errChan <- fmt.Errorf("failed to enqueue task %d: %w", taskNum, err)
				return
			}

			br.tasksEnq.Add(1)
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		return err
	}

	log.Printf("Enqueued %d tasks", br.tasksEnq.Load())
	return nil
}

// waitForCompletion waits for all tasks to complete or timeout
func (br *BenchmarkRunner) waitForCompletion(ctx context.Context) error {
	deadline := time.Now().Add(br.config.Duration)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			completed, err := br.storage.GetTasksByState(ctx, task.StateCompleted, 0)
			if err != nil {
				log.Printf("Warning: failed to check completion: %v", err)
				continue
			}

			failed, err := br.storage.GetTasksByState(ctx, task.StateFailed, 0)
			if err != nil {
				log.Printf("Warning: failed to check failures: %v", err)
				continue
			}

			completedCount := int64(len(completed))
			failedCount := int64(len(failed))
			totalDone := completedCount + failedCount

			br.tasksComp.Store(completedCount)
			br.tasksFail.Store(failedCount)

			log.Printf("Progress: %d/%d tasks (completed: %d, failed: %d)",
				totalDone, br.tasksEnq.Load(), completedCount, failedCount)

			// Check if all tasks are done
			if totalDone >= br.tasksEnq.Load() {
				log.Println("All tasks completed")
				return nil
			}

			// Check timeout
			if time.Now().After(deadline) {
				return fmt.Errorf("benchmark timeout: completed %d/%d tasks",
					totalDone, br.tasksEnq.Load())
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// collectResults gathers benchmark metrics
func (br *BenchmarkRunner) collectResults() *BenchmarkResults {
	duration := br.endTime.Sub(br.startTime)
	completed := br.tasksComp.Load()
	failed := br.tasksFail.Load()
	total := br.tasksEnq.Load()

	var tps float64
	if duration.Seconds() > 0 {
		tps = float64(completed) / duration.Seconds()
	}

	var errorRate float64
	if total > 0 {
		errorRate = (float64(failed) / float64(total)) * 100
	}

	// Memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	results := &BenchmarkResults{
		TotalTasks:     total,
		CompletedTasks: completed,
		FailedTasks:    failed,
		Duration:       duration,
		TasksPerSecond: tps,
		MemAllocated:   m.Alloc,
		MemAllocTotal:  m.TotalAlloc,
		GoroutineCount: runtime.NumGoroutine(),
		ErrorRate:      errorRate,
	}

	// Calculate latency metrics if we have data
	if len(br.latencies) > 0 {
		results.AvgLatency = calculateAvgLatency(br.latencies)
		results.P50Latency = calculatePercentile(br.latencies, 0.50)
		results.P95Latency = calculatePercentile(br.latencies, 0.95)
		results.P99Latency = calculatePercentile(br.latencies, 0.99)
		results.MinLatency = findMinLatency(br.latencies)
		results.MaxLatency = findMaxLatency(br.latencies)
	}

	return results
}

// startProfiling starts CPU profiling
func (br *BenchmarkRunner) startProfiling() error {
	if br.config.CPUProfile != "" {
		f, err := os.Create(br.config.CPUProfile)
		if err != nil {
			return fmt.Errorf("failed to create CPU profile: %w", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			f.Close()
			return fmt.Errorf("failed to start CPU profile: %w", err)
		}
		log.Printf("CPU profiling enabled: %s", br.config.CPUProfile)
	}
	return nil
}

// stopProfiling stops CPU and memory profiling
func (br *BenchmarkRunner) stopProfiling() error {
	if br.config.CPUProfile != "" {
		pprof.StopCPUProfile()
		log.Printf("CPU profile saved to: %s", br.config.CPUProfile)
	}

	if br.config.MemProfile != "" {
		f, err := os.Create(br.config.MemProfile)
		if err != nil {
			return fmt.Errorf("failed to create memory profile: %w", err)
		}
		defer f.Close()

		runtime.GC() // Get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			return fmt.Errorf("failed to write memory profile: %w", err)
		}
		log.Printf("Memory profile saved to: %s", br.config.MemProfile)
	}

	return nil
}

// cleanup stops workers and closes connections
func (br *BenchmarkRunner) cleanup() {
	log.Println("Cleaning up...")

	// Stop workers
	var wg sync.WaitGroup
	for _, w := range br.workers {
		wg.Add(1)
		go func(worker *worker.Worker) {
			defer wg.Done()
			if err := worker.Stop(); err != nil {
				log.Printf("Warning: failed to stop worker: %v", err)
			}
		}(w)
	}
	wg.Wait()

	// Close queue
	if br.queue != nil {
		br.queue.Close()
	}

	// Close client
	if br.client != nil {
		br.client.Close()
	}
}

// Helper functions

func generatePayload(size int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, size)
	for i := range b {
		b[i] = charset[i%len(charset)]
	}
	return string(b)
}

func calculateAvgLatency(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	var total time.Duration
	for _, l := range latencies {
		total += l
	}
	return total / time.Duration(len(latencies))
}

func calculatePercentile(latencies []time.Duration, percentile float64) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	// Sort the latencies
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	index := int(float64(len(sorted)-1) * percentile)
	return sorted[index]
}

func findMinLatency(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	min := latencies[0]
	for _, l := range latencies {
		if l < min {
			min = l
		}
	}
	return min
}

func findMaxLatency(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	max := latencies[0]
	for _, l := range latencies {
		if l > max {
			max = l
		}
	}
	return max
}

// RunQuickBenchmark runs a quick performance test
func RunQuickBenchmark() error {
	config := DefaultBenchmarkConfig()
	config.TaskCount = 1000
	config.WorkerCount = 4
	config.Duration = 30 * time.Second

	runner, err := NewBenchmarkRunner(config)
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	results, err := runner.Run()
	if err != nil {
		return fmt.Errorf("benchmark failed: %w", err)
	}

	fmt.Println(results.String())
	return nil
}

// RunScalabilityTest tests performance with different worker counts
func RunScalabilityTest() error {
	workerCounts := []int{1, 2, 4, 8, 16}
	taskCount := 2000

	fmt.Println("=== Scalability Test ===")
	fmt.Println()

	for _, workers := range workerCounts {
		config := DefaultBenchmarkConfig()
		config.WorkerCount = workers
		config.TaskCount = taskCount
		config.Duration = 60 * time.Second

		runner, err := NewBenchmarkRunner(config)
		if err != nil {
			return fmt.Errorf("failed to create runner: %w", err)
		}

		log.Printf("\nTesting with %d workers...", workers)
		results, err := runner.Run()
		if err != nil {
			log.Printf("Failed with %d workers: %v", workers, err)
			continue
		}

		fmt.Printf("\nWorkers: %d\n", workers)
		fmt.Printf("  TPS: %.2f\n", results.TasksPerSecond)
		fmt.Printf("  Avg Latency: %v\n", results.AvgLatency)
		fmt.Printf("  P99 Latency: %v\n", results.P99Latency)
		fmt.Printf("  Error Rate: %.2f%%\n", results.ErrorRate)
	}

	return nil
}

// RunProfiledBenchmark runs a benchmark with profiling enabled
func RunProfiledBenchmark() error {
	config := DefaultBenchmarkConfig()
	config.TaskCount = 5000
	config.WorkerCount = 8
	config.Duration = 120 * time.Second
	config.EnableProfiling = true
	config.CPUProfile = "cpu.prof"
	config.MemProfile = "mem.prof"

	runner, err := NewBenchmarkRunner(config)
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	results, err := runner.Run()
	if err != nil {
		return fmt.Errorf("benchmark failed: %w", err)
	}

	fmt.Println(results.String())
	fmt.Println("\nProfile files created:")
	fmt.Println("  CPU: cpu.prof")
	fmt.Println("  Memory: mem.prof")
	fmt.Println("\nAnalyze with: go tool pprof cpu.prof")
	return nil
}
