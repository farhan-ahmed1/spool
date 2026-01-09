# Spool

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Code Coverage](https://img.shields.io/badge/coverage-75%25-brightgreen.svg)](docs/04_COVERAGE_REPORT.md)
[![Performance](https://img.shields.io/badge/throughput-3000%20tasks%2Fs-orange.svg)](docs/05_BENCHMARKS.md)
[![Go Report Card](https://img.shields.io/badge/go%20report-A+-brightgreen.svg)](https://goreportcard.com/)

A distributed task queue system built in Go with auto-scaling workers and real-time monitoring.

## Overview

Spool is a production-grade task queue I built to demonstrate distributed systems patterns and Go concurrency. It's built on Redis and handles
around 3,000 tasks/second with sub-50ms p99 latency. Workers scale automatically based on queue depth, and there's a live dashboard for monitoring.

**Key Features:**

- Priority queues (5 levels)
- Auto-retry with exponential backoff
- Result storage with TTL
- Dead letter queue for failed tasks
- WebSocket dashboard for real-time metrics
- Dynamic worker pool auto-scaling

## Architecture

Client → Broker (HTTP) → Redis Queue → Workers → Redis Storage → Dashboard

Tasks flow through a priority queue system (5 levels), get distributed to workers, and results are stored in Redis. The auto-scaler watches queue depth
and dynamically adjusts the worker pool. Dashboard shows everything in real-time via WebSocket connections.

## Demo

> 🎬 **Demo video and screenshots coming soon!** 

**Want to see it in action?** Run locally:

```bash
make docker-up
go run cmd/broker/main.go &
go run cmd/worker/main.go &
go run examples/simple/main.go
```

Open `http://localhost:8080` to see the real-time dashboard.

## Running Locally

Requires Go 1.21+ and Docker.

```bash
make docker-up  # starts Redis
make setup
go run examples/simple/main.go
```

Dashboard runs at `http://localhost:8080` with `go run cmd/broker/main.go`.

## Technical Highlights

**Performance**: Benchmarked at ~3,000 tasks/second with p99 latency under 50ms. Built a custom priority queue implementation and optimized Redis operations
to minimize round-trips.

**Concurrency**: Worker pool uses Go's concurrency patterns (goroutines, channels) with graceful shutdown. Auto-scaler adjusts pool size based on queue depth
metrics collected every second.

**Monitoring**: Real-time dashboard with WebSocket updates showing throughput, latency percentiles, queue depths, and worker states. Built the frontend with vanilla JS.

**Reliability**: Exponential backoff retry logic, dead letter queue for failed tasks, and Redis-backed result storage with configurable TTL.

See [docs/](docs/) for detailed architecture, benchmarks, and implementation notes.

## vs Others

**vs Celery**: Faster (3x throughput), uses less memory, simpler setup. But Celery has more features if you're already in Python land.

**vs RabbitMQ**: Easier to use. RabbitMQ is better for complex routing or if you need a general message broker instead of a task queue.

Use Spool when you want something simple that works out of the box. Check [COMPARISON.md](docs/COMPARISON.md) for details.

## Development

```bash
make test              # unit tests
make test-integration  # integration tests
make coverage          # coverage report
make lint              # run linter
```

Run benchmarks: `./scripts/benchmark.sh`

## Design Decisions

**Why Redis over RabbitMQ**: Simpler operations model and better performance for task queue use cases. Redis sorted sets provide O(log N) priority queue operations.

**Why Go**: Needed true concurrency for worker pools and wanted to learn Go's patterns. The lightweight goroutines made the auto-scaler implementation straightforward.

**Architecture trade-offs**: Chose Redis for both queuing and storage to reduce infrastructure complexity, but this creates a single point of failure.
In production, you'd want Redis Sentinel or Cluster. See [COMPARISON.md](docs/COMPARISON.md) for more.

## Testing & Benchmarks

```bash
make test              # unit tests
make test-integration  # integration tests
make coverage          # ~75% coverage
```

Run benchmarks: `./scripts/benchmark.sh`

Includes load tests simulating up to 10,000 concurrent clients. See [docs/05_BENCHMARKS.md](docs/05_BENCHMARKS.md) for results.

---

Built by Farhan Ahmed - [@farhan-ahmed1](https://github.com/farhan-ahmed1)
