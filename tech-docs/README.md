# Technical Documentation Index

Focused technical documentation for the Spool distributed task queue system.

## Documents

### [architecture.md](architecture.md)

System components, data flow, and scaling strategy. Covers broker, queue layer, worker pool, and storage design.

### [concurrency.md](concurrency.md)

Go concurrency patterns: worker pool implementation, goroutine lifecycle, channel usage, auto-scaling algorithm, and context hierarchy.

### [performance.md](performance.md)

Throughput characteristics (~3,000 tasks/sec), latency profile (p99 < 50ms), Redis optimizations, memory usage, and load test results.

### [data-structures.md](data-structures.md)

Priority queue implementation, task state machine, exponential backoff algorithm, worker state tracking, and metrics aggregation.

### [reliability.md](reliability.md)

Retry strategy, graceful shutdown, task timeouts, dead letter queue, Redis resilience, and failure modes.

## Quick Reference

**Throughput:** 3,000 tasks/sec sustained  
**Latency:** p99 < 50ms under normal load  
**Concurrency:** Goroutine-based worker pool with dynamic scaling  
**Queue:** Redis sorted sets with O(log N) operations  
**Retry:** Exponential backoff (1s → 5m cap)  
**Storage:** Redis with 24h TTL for results
