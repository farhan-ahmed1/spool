# System Architecture

## Component Overview

**Client → Broker → Redis Queue → Worker Pool → Result Storage**

The system uses a staged pipeline where each component has a single responsibility.

### Broker

HTTP API server handling task submission and management. Routes tasks to priority-specific Redis queues based on priority level.
Implements connection pooling with configurable timeouts (15s read/write, 60s idle).

Key design: Stateless. All state lives in Redis, allowing horizontal scaling.

### Queue Layer

Redis-backed priority queue system with 5 levels (critical, high, normal, low, default). Each priority maps to a separate Redis sorted set for O(log N) operations.

```text
Spool:queue:critical
Spool:queue:high
Spool:queue:normal
Spool:queue:low
```

Tasks in higher priority queues always dequeue first. Within same priority, FIFO ordering.

### Worker Pool

Dynamic pool with auto-scaling between min/max bounds. Workers poll queues in priority order using Redis ZPOPMIN for atomic dequeue operations.

Each worker runs in a goroutine with its own context for cancellation. Pool maintains worker state tracking (idle, busy, shutting_down) for scaling decisions.

### Storage

Redis for both queueing and result storage. Results use TTL (default 24h) for automatic cleanup. Dead letter queue stores permanently failed tasks.

Key namespaces:

- `Spool:task:{id}` - task metadata
- `Spool:result:{id}` - execution results
- `Spool:dlq:{id}` - dead letter queue

## Data Flow

1. Client submits task via HTTP POST
2. Broker validates and serializes to JSON
3. Task written to Redis (task metadata + queue entry)
4. Worker dequeues atomically via ZPOPMIN
5. Handler execution with timeout context
6. Result stored in Redis with TTL
7. Metrics updated via channels

Critical path latency: ~10-15ms for high priority tasks under normal load.

## Scaling Strategy

Broker scales horizontally (stateless design). Workers scale automatically based on queue depth and throughput metrics.
Redis is the single point of coordination - production deployments should use Redis Sentinel or Cluster.

Single Redis instance handles ~3,000 tasks/sec with default configuration (pool size 10, 3 max retries).
