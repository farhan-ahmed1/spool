# Data Structures & Algorithms

## Priority Queue Implementation

Uses Redis sorted sets (ZSET) with score-based ordering. Each priority level maps to a separate sorted set.

**Structure:**

```text
ZADD Spool:queue:critical <timestamp> <task_id>
```

Score = Unix timestamp ensures FIFO within same priority. ZPOPMIN atomically removes lowest score (oldest task).

**Complexity:**

- Enqueue: O(log N) - Redis ZADD
- Dequeue: O(log N) - Redis ZPOPMIN
- Size check: O(1) - Redis ZCARD

Alternative considered: Single sorted set with priority encoded in score. Rejected because priority changes would require score recalculation and re-insertion.

## Task State Machine

```text
pending → processing → completed
                    ↓
                retrying → processing (up to MaxRetries)
                    ↓
                dead_letter
```

State transitions atomic via Redis transactions. Failed tasks increment retry counter before state change.

## Exponential Backoff

Retry delay calculation:

```text
delay = min(baseDelay * 2^retryCount, maxDelay)
delay = min(1s * 2^retryCount, 5m)
```

Examples:

- Retry 0: 1s
- Retry 1: 2s
- Retry 2: 4s
- Retry 3: 8s
- Retry 4+: 5m (capped)

Prevents thundering herd on transient failures. Tasks with same failure time spread out over exponential intervals.

## Worker State Tracking

In-memory map: `workerID → WorkerState`

Protected by sync.RWMutex. Read lock for state checks (scaling decisions), write lock for state updates (task start/complete).

States: idle, busy, shutting_down

Auto-scaler uses idle count to determine scale-down eligibility. Idle threshold: 2 minutes consecutive idle time.

## Metrics Aggregation

Lock-free counters using sync/atomic package. Each metric (completed, failed, retried) is atomic.AddInt64.

Throughput calculated as delta between samples:

```text
throughput = (currentCompleted - previousCompleted) / sampleInterval
```

Latency percentiles stored in circular buffer (size 1000). Sorted on-demand for percentile calculation. O(N log N) sort acceptable for dashboard refresh rate (1s).

## Dead Letter Queue

Separate Redis sorted set with metadata. Stores task ID, original payload, failure reason, and failure timestamp.

No automatic cleanup - requires manual inspection. Design choice: Failed tasks indicate bugs that need investigation.

## Handler Registry

Thread-safe map: `taskType → Handler function`

Uses sync.RWMutex. Registration (write) happens once at startup. Lookups (read) happen per task execution with read lock.

Pattern: Register during initialization, read-only during execution. Prevents handler registration race conditions.

## Connection Pool Algorithm

Redis client maintains:

- Min idle connections: poolSize / 2
- Max connections: poolSize
- Connection reuse via free list

On request:

1. Check free list
2. If empty and count < max, create new
3. If at max, wait for available (4s timeout)
4. Return connection to free list after use

Prevents connection exhaustion under burst load. Pool timeout ensures bounded wait time.

## JSON Serialization

Tasks serialized to JSON for Redis storage. Standard library encoding/json with struct tags.

Alternative considered: Protocol Buffers. Rejected because:

- JSON human-readable for debugging
- Schema evolution simpler
- Performance difference negligible compared to network I/O
- No code generation required
