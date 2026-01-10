# Concurrency Implementation

## Worker Pool Design

Pool manages goroutines dynamically without pre-allocating workers. Each worker is a lightweight goroutine (~2KB stack) that polls Redis queues.

### Core Pattern

```text
Pool
├── Context tree for cancellation propagation
├── WaitGroup for coordinated shutdown
├── Worker map (ID → *Worker)
└── State tracking (ID → WorkerState)
```

Workers share queue and storage references but maintain independent execution contexts. No shared mutable state between workers except through thread-safe registry.

### Goroutine Lifecycle

Each worker runs in a loop:

1. Acquire task from Redis (blocking ZPOPMIN with 1s timeout)
2. Mark worker state as busy
3. Execute handler with task-specific context
4. Store result or requeue on failure
5. Mark worker state as idle
6. Repeat or shutdown on context cancellation

Shutdown sequence: Context cancel → Drain in-flight tasks → WaitGroup.Wait() → Close resources

Timeout: 30s graceful shutdown before forced termination.

## Channel Usage

### Metrics Collection

Non-blocking sends to buffered channel (size 1000) for metrics aggregation. Metrics goroutine consumes from channel and updates atomic counters.

Pattern: Fire-and-forget with select/default to prevent worker blocking.

```go
select {
case metricsChan <- event:
default:
    // Drop metric if channel full
}
```

Trade-off: Lose some metrics under extreme load rather than slow down workers.

### Shutdown Coordination

Unbuffered channel signals shutdown readiness:

```go
done := make(chan struct{})
close(done)  // Broadcast to all goroutines
```

All workers select on context.Done() and this channel. Closing broadcasts shutdown to entire pool simultaneously.

## Auto-Scaling Algorithm

Runs in separate goroutine with 1-second poll interval.

**Scale Up Conditions:**

- Queue depth > threshold (default 100)
- Current workers < max
- Not in cooldown period (30s)

**Scale Down Conditions:**

- Workers idle > 2 minutes
- Current workers > min
- Not in cooldown period (2m)

Decision logged with reason, queue depth, and timestamp for debugging.

### Race Prevention

All worker count modifications protected by sync.RWMutex. Read lock for count checks, write lock for add/remove operations.

Cooldown periods prevent oscillation between scale up/down decisions. Longer cooldown for scale-down ensures stability under bursty load.

## Context Hierarchy

```text
Root Context (pool)
├── Worker Context 1
│   └── Task Context 1 (with timeout)
├── Worker Context 2
│   └── Task Context 2 (with timeout)
└── Scaler Context
```

Canceling root context cascades to all children. Individual task timeouts don't affect worker or pool.

Handlers receive task-specific context with timeout (default 30s). Handler must respect context cancellation for clean shutdown.
