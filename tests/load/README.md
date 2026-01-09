# Spool Benchmark Tools

Production-grade performance benchmarking and optimization tools for the Spool distributed task queue.

## Quick Start

```bash
# Run a quick benchmark (30 seconds, 1000 tasks)
go run cmd/benchmark/main.go -type quick

# Run full benchmark suite (recommended)
./scripts/benchmark.sh
```

## Tools Overview

### 1. Benchmark Library (`tests/load/benchmark.go`)

Core benchmarking engine with:

- Configurable test parameters
- Multiple test scenarios
- Comprehensive metrics collection
- CPU and memory profiling support

### 2. CLI Tool (`cmd/benchmark/main.go`)

Command-line interface for running benchmarks:

```bash
# Quick test
go run cmd/benchmark/main.go -type quick

# Scalability test (1, 2, 4, 8, 16 workers)
go run cmd/benchmark/main.go -type scalability

# Profiled test (generates cpu.prof and mem.prof)
go run cmd/benchmark/main.go -type profiled

# Custom test
go run cmd/benchmark/main.go -type custom \
    -workers 8 \
    -tasks 10000 \
    -duration 60 \
    -cpuprofile cpu.prof \
    -memprofile mem.prof
```

### 3. Automation Script (`scripts/benchmark.sh`)

Runs complete benchmark suite and generates reports:

```bash
./scripts/benchmark.sh
```

**Output Location**: `benchmark_results/run_YYYYMMDD_HHMMSS/`

**Generated Files**:

- `SUMMARY.md` - Executive summary with all metrics
- `*_output.log` - Detailed benchmark outputs
- `cpu.prof` / `mem.prof` - Profile data
- `*_analysis.txt` - Analyzed hotspots

### 4. Go Test Benchmarks (`tests/load/benchmark_test.go`)

Standard Go benchmarks for component testing:

```bash
# Run all benchmarks
go test -bench=. -benchmem ./tests/load/

# Run specific benchmark
go test -bench=BenchmarkTaskThroughput -benchmem ./tests/load/

# With profiling
go test -bench=. -benchmem \
    -cpuprofile=cpu.prof \
    -memprofile=mem.prof \
    ./tests/load/
```

## Benchmark Types

### Quick Benchmark

- **Duration**: ~30 seconds
- **Tasks**: 1,000
- **Workers**: 4
- **Purpose**: Fast validation during development

### Scalability Test

- **Duration**: ~5 minutes
- **Worker Counts**: 1, 2, 4, 8, 16
- **Tasks per test**: 2,000
- **Purpose**: Measure scaling characteristics

### Profiled Benchmark

- **Duration**: ~2 minutes
- **Tasks**: 5,000
- **Workers**: 8
- **Profiling**: CPU and Memory
- **Purpose**: Identify performance bottlenecks

### Custom Benchmark

- **Configurable**: All parameters
- **Purpose**: Specific testing scenarios

## Metrics Collected

### Throughput

- **Tasks/Second**: Completed tasks per second
- **Total Tasks**: Total tasks processed
- **Completed**: Successfully completed tasks
- **Failed**: Failed tasks

### Latency

- **Average**: Mean task completion time
- **P50**: Median latency
- **P95**: 95th percentile
- **P99**: 99th percentile
- **Min/Max**: Minimum and maximum latencies

### Resource Usage

- **Memory Allocated**: Current memory usage
- **Memory Total**: Total allocations
- **Goroutines**: Active goroutine count

### Reliability

- **Error Rate**: Percentage of failed tasks
- **Success Rate**: Percentage of successful tasks

## Profiling

### Generate Profiles

```bash
# Using CLI tool
go run cmd/benchmark/main.go -type profiled

# Using Go tests
go test -bench=. -cpuprofile=cpu.prof -memprofile=mem.prof ./tests/load/
```

### Analyze Profiles

```bash
# View top CPU consumers
go tool pprof -text -nodecount=20 cpu.prof

# View top memory allocators
go tool pprof -alloc_space -text -nodecount=20 mem.prof

# Interactive analysis
go tool pprof cpu.prof
> top      # Show top functions
> list <function>  # Show function code
> web      # Visualize (requires graphviz)

# Web UI (recommended)
go tool pprof -http=:8080 cpu.prof
```

## Interpreting Results

### Good Performance ✅

- TPS scales linearly with worker count
- P99 latency < 50ms
- Error rate < 0.1%
- Stable memory usage

### Performance Issues ⚠️

- TPS plateaus despite adding workers
- P99 latency > 100ms
- Error rate > 1%
- Memory usage growing over time

### Common Bottlenecks

1. **Redis operations** - High latency on Enqueue/Dequeue
2. **JSON serialization** - High CPU in marshal/unmarshal
3. **Worker polling** - Excessive CPU with low throughput
4. **Connection pool** - Timeout errors
5. **Lock contention** - High CPU in sync primitives

## Optimization Workflow

### 1. Baseline

```bash
./scripts/benchmark.sh
mv benchmark_results/run_* benchmark_results/baseline
```

### 2. Analyze

```bash
cd benchmark_results/baseline
cat SUMMARY.md
cat cpu_analysis.txt
cat mem_analysis.txt
```

### 3. Optimize

Implement optimizations based on analysis:

- See `docs/PERFORMANCE_OPTIMIZATION.md` for strategies
- Focus on top CPU and memory hotspots
- One optimization at a time

### 4. Verify

```bash
./scripts/benchmark.sh
mv benchmark_results/run_* benchmark_results/optimized
```

### 5. Compare

```bash
diff benchmark_results/baseline/SUMMARY.md \
     benchmark_results/optimized/SUMMARY.md
```

### 6. Iterate

Repeat until reaching performance targets

## Performance Targets

| Metric | Current | Target | Status |
| -------- | --------- | -------- | -------- |
| Throughput | ~147 TPS | 2,500+ TPS | ⚠️ In progress |
| P99 Latency | TBD | < 50ms | ⚠️ To measure |
| Error Rate | 0% | < 0.1% | ✅ Good |
| Memory | TBD | < 500MB | ⚠️ To measure |

## Examples

### Development Testing

```bash
# Quick validation after code changes
go run cmd/benchmark/main.go -type quick

# Test specific worker configuration
go run cmd/benchmark/main.go -type custom -workers 8 -tasks 2000
```

### Performance Investigation

```bash
# Generate profiles
go run cmd/benchmark/main.go -type profiled

# Find CPU hotspots
go tool pprof -top cpu.prof | head -20

# Detailed investigation
go tool pprof -http=:8080 cpu.prof
```

### CI/CD Integration

```bash
# Run benchmarks in CI
./scripts/benchmark.sh

# Parse results
grep "Tasks/Second" benchmark_results/run_*/SUMMARY.md

# Fail if below threshold
tps=$(grep "Tasks/Second" benchmark_results/run_*/SUMMARY.md | awk '{print $2}')
if (( $(echo "$tps < 100" | bc -l) )); then
    echo "Performance regression detected!"
    exit 1
fi
```

### Portfolio/Demo

```bash
# Run comprehensive suite
./scripts/benchmark.sh

# Generate flame graphs (if go-torch installed)
go-torch -f cpu_flame.svg cpu.prof

# Create visualization
go tool pprof -http=:8080 cpu.prof
# Take screenshots of flame graph
```

## Requirements

### Software

- Go 1.21+
- Redis 6.0+
- Docker (for Redis)

### Optional Tools

- `graphviz` - For visual graph generation
- `go-torch` - For flame graph generation
- `bc` - For numerical comparisons in scripts

### System Resources

- **CPU**: 4+ cores recommended
- **Memory**: 2GB+ available
- **Disk**: 1GB for benchmark results

## Troubleshooting

### Redis Connection Failed

```bash
# Start Redis
make docker-up

# Verify connection
redis-cli ping
```

### Out of Memory

```bash
# Reduce task count or workers
go run cmd/benchmark/main.go -type custom -workers 2 -tasks 500
```

### Slow Benchmarks

```bash
# Use quick benchmark for faster feedback
go run cmd/benchmark/main.go -type quick

# Reduce duration
go run cmd/benchmark/main.go -type custom -duration 10
```

### Profile Analysis Fails

```bash
# Install required tools
go install github.com/google/pprof@latest

# Try text output instead of web
go tool pprof -text cpu.prof
```

## Documentation

- **Quick Reference**: [docs/BENCHMARK_QUICK_REF.md](../docs/BENCHMARK_QUICK_REF.md)
- **Optimization Guide**: [docs/PERFORMANCE_OPTIMIZATION.md](../docs/PERFORMANCE_OPTIMIZATION.md)
- **Implementation Summary**: [docs/15_PERFORMANCE_SUMMARY.md](../docs/15_PERFORMANCE_SUMMARY.md)

## Resources

- [Go Profiling Guide](https://go.dev/blog/pprof)
- [pprof Documentation](https://github.com/google/pprof)
- [Go Benchmarking](https://pkg.go.dev/testing#hdr-Benchmarks)
- [Redis Performance](https://redis.io/docs/management/optimization/)

## Contributing

When adding new benchmarks:

1. Add benchmark function to `tests/load/benchmark.go`
2. Update CLI tool if needed
3. Document in this README
4. Add example usage

## License

Same as main project.
