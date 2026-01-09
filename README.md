# Spool

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Code Coverage](https://img.shields.io/badge/coverage-75%25-brightgreen.svg)](docs/04_COVERAGE_REPORT.md)
[![Performance](https://img.shields.io/badge/throughput-3000%20tasks%2Fs-orange.svg)](docs/05_BENCHMARKS.md)
[![Go Report Card](https://img.shields.io/badge/go%20report-A+-brightgreen.svg)](https://goreportcard.com/)

A distributed task queue built in Go with Redis, featuring priority queuing, adaptive auto-scaling, and real-time monitoring with sub-50ms P99 latency at 3,000+ tasks/second.

## Features

- **Priority Queues** — 5-level priority system with O(log N) operations via Redis sorted sets
- **Auto-Scaling Workers** — Dynamic pool sizing based on queue depth metrics
- **Retry with Backoff** — Exponential backoff with configurable max attempts
- **Dead Letter Queue** — Automatic capture of permanently failed tasks
- **Result Storage** — Redis-backed results with configurable TTL
- **Real-Time Dashboard** — WebSocket-powered metrics visualization

## Architecture

```text
┌─────────────┐       ┌─────────────┐       ┌─────────────────────┐
│   Client    │──────▶│   Broker    │──────▶│        Redis        │
│  (HTTP/SDK) │       │   (HTTP)    │       │   (Priority Queue)  │
└─────────────┘       └─────────────┘       └──────────┬──────────┘
                                                       │
                            ┌──────────────────────────┼──────────────────────────┐
                            │                          │                          │
                       ┌────▼────┐                ┌────▼────┐                ┌────▼────┐
                       │ Worker 1│                │ Worker 2│       ...      │ Worker N│
                       └────┬────┘                └────┬────┘                └────┬────┘
                            │                          │                          │
                            └──────────────────────────┼──────────────────────────┘
                                                       │
                                                       ▼
                                            ┌─────────────────────┐
                                            │   Redis (Storage)   │
                                            └──────────┬──────────┘
                                                       │
                                                       ▼
                                            ┌─────────────────────┐
                                            │     Dashboard       │
                                            │    (WebSocket)      │
                                            └─────────────────────┘
```

**Data Flow:**

1. Clients submit tasks via HTTP API or Go SDK
2. Broker validates and enqueues tasks into Redis priority queue
3. Workers poll queue, execute tasks, and store results in Redis
4. Auto-scaler monitors queue depth and adjusts worker pool size
5. Dashboard receives real-time updates via WebSocket

## Demo

### Live Dashboard

![Spool Dashboard - Real-time Metrics](tech-docs/images/dashboard-view.png)
*Real-time metrics dashboard showing throughput, latency, queue depths, and worker states*

### Auto-Scaling in Action

![Auto-scaling Demo](tech-docs/videos/auto-scaling.gif)
*Workers automatically scale from 1→3 workers as queue depth increases, then scale back down during idle periods*

## Quick Start

**Prerequisites:** Go 1.21+ and Docker

```bash
# Start Redis
make docker-up

# Terminal 1: Start the complete dashboard with auto-scaling demo
go run examples/dashboard/main.go

# Terminal 2: For auto-scaling demonstration
go run examples/autoscaling/main.go
```

Open `http://localhost:8080` to view the real-time metrics dashboard.

**Expected behavior:** Dashboard shows real-time task processing. For auto-scaling demo, workers scale from 2→10 based on queue depth, then scale back down to 2 during idle periods.

## Performance

Benchmark results on Apple M1 Pro (8-core, 16GB RAM):

| Metric | Value |
| -------- | ------- |
| **Throughput** | 3,178 tasks/sec |
| **P50 Latency** | 12ms |
| **P99 Latency** | 47ms |
| **Concurrent Clients** | 10 goroutines |
| **Test Duration** | 10 seconds |

### Benchmark Methodology

All benchmarks executed using Go's testing framework with the following configuration:

- **Hardware:** Apple M1 Pro, 8-core CPU, 16GB unified memory
- **Redis:** v7.0, running in Docker (default configuration)
- **Worker Count:** 4 workers (configurable)
- **Poll Interval:** 10ms
- **Task Type:** Counter increment with simulated 10ms processing time
- **Connection Pool:** 10 connections

**Reproducibility:**

```bash
# Run full benchmark suite
make benchmark

# Run specific throughput test
go test -v ./tests/load/ -run TestThroughputRequirement -timeout 2m

# Run concurrent submission test
go test -v ./tests/load/ -run TestConcurrentTaskSubmission -timeout 2m

# Run Go benchmarks with memory profiling
go test -bench=BenchmarkTaskThroughput -benchmem ./tests/load/
```

Detailed results and methodology available in [docs/05_BENCHMARKS.md](docs/05_BENCHMARKS.md).

## Comparison

| Feature | Spool | Celery | RabbitMQ |
| --------- | ------- | -------- | ---------- |
| **Throughput** | ~3,000/sec | ~1,000/sec | ~5,000/sec |
| **Setup Complexity** | Low | Medium | High |
| **Auto-scaling** | ✓ Built-in | ✗ External | ✗ External |
| **Language** | Go | Python | Erlang |
| **Priority Queues** | ✓ 5 levels | ✓ Limited | ✓ Full |
| **Best For** | Go microservices | Python ecosystems | Complex routing |

*Benchmarks performed under equivalent conditions. See [COMPARISON.md](docs/COMPARISON.md) for detailed analysis.*

## Technical Highlights

**Concurrency Model** — Worker pool leverages Go's goroutines and channels with graceful shutdown
via `context.Context`. The auto-scaler samples queue depth every second and adjusts pool size within configurable bounds.

**Priority Queue Implementation** — Redis sorted sets provide O(log N) insertion and O(1) retrieval for
highest-priority tasks. Five priority levels (critical, high, normal, low, background) ensure important tasks execute first.

**Monitoring Stack** — Real-time dashboard built with vanilla JavaScript and WebSocket connections.
Displays throughput graphs, latency percentiles (P50, P95, P99), queue depths per priority level, and worker states.

**Reliability Features** — Exponential backoff retry (configurable attempts), dead letter queue for forensic analysis, and Redis-backed result storage with TTL-based cleanup.

## Development

```bash
make test              # Run unit tests
make test-integration  # Run integration tests
make coverage          # Generate coverage report
make lint              # Run golangci-lint
make benchmark         # Run performance benchmarks
```

## Design Decisions

| Decision | Rationale |
| ---------- | ----------- |
| **Redis over RabbitMQ** | Simpler operational model; sorted sets provide efficient priority queue semantics |
| **Go** | Native concurrency primitives ideal for worker pools; minimal memory footprint per goroutine |
| **Single Redis instance** | Reduces infrastructure complexity; production deployments should use Redis Sentinel or Cluster |

Detailed architecture documentation available in [docs/](docs/).

---

Built by Farhan Ahmed — [GitHub](https://github.com/farhan-ahmed1)
