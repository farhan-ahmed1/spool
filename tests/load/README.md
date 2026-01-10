# Spool Benchmark Tools

Performance benchmarking tools for the Spool distributed task queue.

## Quick Start

```bash
# Quick benchmark (~10 seconds)
go run cmd/benchmark/main.go -type quick

# Full benchmark suite with profiling
go run cmd/benchmark/main.go -type profiled

# Go benchmarks
go test -bench=. -benchmem ./tests/load/
```

## Current Performance

| Metric | Result | Target | Status |
| -------- | -------- | -------- | -------- |
| **Throughput** | **7,146 TPS** | 2,500+ TPS | ✅ **2.9x target** |
| **Error Rate** | **0%** | < 0.1% | ✅ Perfect |
| **Latency P99** | **< 1ms** | < 50ms | ✅ Excellent |
| **Memory** | **~50 MB** | < 500MB | ✅ Efficient |

*Measured with 4 workers processing 1,000 tasks in 9.4 seconds*

## Benchmark Types

**Quick** - Fast validation (1,000 tasks, 4 workers, ~10s)

```bash
go run cmd/benchmark/main.go -type quick
```

**Scalability** - Test worker scaling (1-16 workers, ~5min)

```bash
go run cmd/benchmark/main.go -type scalability
```

**Profiled** - Generate CPU/memory profiles (5,000 tasks, ~2min)

```bash
go run cmd/benchmark/main.go -type profiled
```

**Custom** - Configure all parameters

```bash
go run cmd/benchmark/main.go -type custom -workers 8 -tasks 5000
```

## Profiling

**Generate profiles:**

```bash
go run cmd/benchmark/main.go -type profiled
```

**Analyze bottlenecks:**

```bash
# CPU hotspots
go tool pprof -top cpu.prof

# Memory allocations  
go tool pprof -alloc_space -top mem.prof

# Interactive web UI
go tool pprof -http=:8080 cpu.prof
```

## Common Use Cases

**Development workflow:**

```bash
# 1. Baseline before changes
go run cmd/benchmark/main.go -type quick > before.txt

# 2. Make code changes
# ...

# 3. Verify performance maintained
go run cmd/benchmark/main.go -type quick > after.txt
diff before.txt after.txt
```

**Find performance issues:**

```bash
go run cmd/benchmark/main.go -type profiled
go tool pprof -http=:8080 cpu.prof
# Look for: Redis ops, JSON marshal/unmarshal, lock contention
```

## Requirements

- Go 1.21+
- Redis (start with `make docker-up`)

## Documentation

See [docs/performance.md](../../docs/performance.md) for optimization strategies.
