# Reliability & Fault Tolerance

## Retry Strategy

Tasks automatically retry on failure up to MaxRetries (default 3). Each retry uses exponential backoff to avoid overwhelming failing services.

**Retry Logic:**

1. Handler returns error
2. Check if RetryCount < MaxRetries
3. If yes: State → retrying, increment counter, requeue with delay
4. If no: State → dead_letter, store in DLQ

Retry delays: 1s, 2s, 4s, 8s (capped at 5m). Tasks with transient failures recover automatically. Persistent failures move to DLQ after 4 attempts.

## Idempotency Requirements

Handlers must be idempotent since retries may execute multiple times for the same task. System provides task ID for deduplication.

Handler pattern:

```go
handler := func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
    // Check if already processed using task ID
    // Perform operation
    // Store result with task ID as key
}
```

No at-most-once delivery guarantee. Network failures between execution and result storage may cause duplicate execution.

## Graceful Shutdown

Shutdown sequence prevents data loss:

1. Stop accepting new tasks (broker closes listener)
2. Cancel pool context
3. Workers finish in-flight tasks (30s timeout)
4. WaitGroup ensures all goroutines complete
5. Close Redis connections

In-flight tasks either complete or return to queue. Workers don't start new tasks after context cancellation.

Timeout handling: After 30s, force shutdown. Tasks in execution state marked as pending for reprocessing.

## Task Timeout

Each task executes with a timeout context (default 30s). Prevents hanging on unresponsive handlers.

On timeout:

- Context canceled
- Handler should clean up and return error
- Task marked failed and enters retry logic
- If MaxRetries exceeded, moves to DLQ

Handlers must respect context cancellation for clean timeout behavior.

## Dead Letter Queue

Permanent storage for tasks that exhausted retries. Contains:

- Original task payload
- Failure reason (last error message)
- Failure timestamp
- Retry count

DLQ enables:

- Manual inspection of failures
- Replay after fixing bugs
- Alerting on failure patterns

No automatic DLQ processing. Design assumes failures indicate code issues requiring human intervention.

## Redis Connection Resilience

Client configured with:

- Max 3 automatic retries
- 5s dial timeout
- 3s read/write timeout
- Connection pooling with health checks

Transient Redis failures retry automatically. Persistent failures bubble up as task failures (enter retry logic).

No circuit breaker. Design assumes Redis high availability via Sentinel/Cluster in production.

## Result Storage Durability

Results stored with 24h TTL. After TTL, results deleted automatically.

Trade-off: Ephemeral results reduce storage costs but require clients to poll within retention window.

For permanent storage, handler should write to external database. Task system only guarantees execution, not long-term result persistence.

## State Consistency

Task state transitions use Redis transactions (MULTI/EXEC) for atomicity:

```text
MULTI
  SET task:{id} {updated_state}
  ZADD queue:retrying {timestamp} {task_id}
EXEC
```

Prevents partial updates if Redis connection drops mid-operation. Either full state change commits or none.

## Monitoring for Reliability

System tracks:

- Failed task count
- DLQ size
- Retry rate
- Task timeout rate

Alert thresholds:

- DLQ growth: Indicates persistent failures
- High retry rate: Suggests transient issues
- Timeout spike: Handler performance degradation

Dashboard exposes these metrics for real-time monitoring.

## Failure Modes

**Redis down:** All operations fail. Workers wait for reconnection (with backoff). No data loss if Redis recovers.

**Handler panic:** Recovered by worker. Task marked failed and enters retry logic. Worker continues processing.

**Network partition:** Tasks in-flight may timeout. Retry logic handles recovery when network restored.

**Worker crash:** In-flight task remains in processing state. Requires manual intervention to reset state. No automatic orphan detection.

Design assumes stable infrastructure. Production deployments should use health checks and orchestration (k8s) for worker restart.
