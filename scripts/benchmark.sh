#!/bin/bash

# Spool Performance Benchmark Script
# This script runs comprehensive performance tests and generates profiling data

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
RESULTS_DIR="benchmark_results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RUN_DIR="$RESULTS_DIR/run_$TIMESTAMP"

echo -e "${BLUE}╔══════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  Spool Performance Benchmark & Optimization Suite ║${NC}"
echo -e "${BLUE}╚══════════════════════════════════════════════════════╝${NC}"
echo ""

# Create results directory
mkdir -p "$RUN_DIR"
cd "$RUN_DIR"

echo -e "${YELLOW}Results will be saved to: ${NC}$RUN_DIR"
echo ""

# Function to run a benchmark
run_benchmark() {
  local name=$1
  local type=$2
  local additional_flags=$3
  
  echo -e "${GREEN}[1/5] Running $name benchmark...${NC}"
  go run ../../cmd/benchmark/main.go -type "$type" $additional_flags 2>&1 | tee "${name}_output.log"
  echo ""
}

# Function to run Go benchmarks
run_go_benchmarks() {
  echo -e "${GREEN}[2/5] Running Go benchmark tests...${NC}"
  cd ../..
  go test -bench=. -benchmem -benchtime=10s ./tests/load/ 2>&1 | tee "$RUN_DIR/gobench_output.log"
  cd "$RUN_DIR"
  echo ""
}

# Function to run profiled benchmark
run_profiled() {
  echo -e "${GREEN}[3/5] Running profiled benchmark (this will take ~2 minutes)...${NC}"
  go run ../../cmd/benchmark/main.go \
    -type custom \
    -workers 8 \
    -tasks 5000 \
    -duration 120 \
    -cpuprofile cpu.prof \
    -memprofile mem.prof \
    2>&1 | tee profiled_output.log
  echo ""
}

# Function to analyze profiles
analyze_profiles() {
  echo -e "${GREEN}[4/5] Analyzing CPU profile...${NC}"
  if [ -f "cpu.prof" ]; then
    echo "Top 20 CPU hotspots:" > cpu_analysis.txt
    go tool pprof -text -nodecount=20 cpu.prof >> cpu_analysis.txt 2>&1 || true
    echo "CPU profile analysis saved to cpu_analysis.txt"
  fi
  echo ""
  
  echo -e "${GREEN}[4/5] Analyzing Memory profile...${NC}"
  if [ -f "mem.prof" ]; then
    echo "Top 20 Memory allocations:" > mem_analysis.txt
    go tool pprof -text -nodecount=20 mem.prof >> mem_analysis.txt 2>&1 || true
    echo "Memory profile analysis saved to mem_analysis.txt"
  fi
  echo ""
}

# Function to generate summary
generate_summary() {
  echo -e "${GREEN}[5/5] Generating summary report...${NC}"
  
  cat > SUMMARY.md <<EOF
# Spool Performance Benchmark Results
**Run Date:** $(date)
**Run ID:** $TIMESTAMP

## Test Configuration
- **Workers:** 4-16 (variable)
- **Tasks:** 1000-5000
- **Duration:** 30-120 seconds
- **Redis:** localhost:6379
- **Go Version:** $(go version)

## Results

### Quick Benchmark (1000 tasks, 4 workers)
\`\`\`
$(grep -A 15 "Benchmark Results:" quick_output.log 2>/dev/null || echo "No results found")
\`\`\`

### Scalability Test
\`\`\`
$(grep -A 50 "=== Scalability Test ===" scalability_output.log 2>/dev/null || echo "No results found")
\`\`\`

### Go Benchmarks
\`\`\`
$(grep "Benchmark" gobench_output.log 2>/dev/null || echo "No results found")
\`\`\`

### Profiling Results

#### CPU Hotspots
\`\`\`
$(head -30 cpu_analysis.txt 2>/dev/null || echo "No CPU profile found")
\`\`\`

#### Memory Allocations
\`\`\`
$(head -30 mem_analysis.txt 2>/dev/null || echo "No memory profile found")
\`\`\`

## Files Generated
- quick_output.log - Quick benchmark output
- scalability_output.log - Scalability test output 
- gobench_output.log - Go benchmark tests
- profiled_output.log - Profiled benchmark output
- cpu.prof - CPU profile data
- mem.prof - Memory profile data
- cpu_analysis.txt - CPU hotspot analysis
- mem_analysis.txt - Memory allocation analysis
- SUMMARY.md - This file

## Analysis Commands

### Explore CPU Profile Interactively
\`\`\`bash
go tool pprof cpu.prof
# Commands: top, list <function>, web (requires graphviz)
\`\`\`

### Explore Memory Profile Interactively
\`\`\`bash
go tool pprof mem.prof
# Commands: top, list <function>, web (requires graphviz)
\`\`\`

### Generate Flame Graphs (requires go-torch)
\`\`\`bash
go-torch -f cpu_flame.svg cpu.prof
go-torch -alloc_space -f mem_flame.svg mem.prof
\`\`\`

## Optimization Opportunities

Based on the profiling data, focus optimization efforts on:
1. Functions appearing in top CPU hotspots
2. Functions with high memory allocation
3. Redis operations (Enqueue/Dequeue)
4. Worker polling loops
5. Task serialization/deserialization

EOF
  
  echo -e "${GREEN}Summary report saved to SUMMARY.md${NC}"
  echo ""
}

# Main execution
main() {
  echo -e "${YELLOW}Starting benchmark suite...${NC}"
  echo ""
  
  # Check if Redis is running
  if ! redis-cli ping > /dev/null 2>&1; then
    echo -e "${RED}ERROR: Redis is not running!${NC}"
    echo "Please start Redis with: make docker-up"
    exit 1
  fi
  
  # Run benchmarks
  run_benchmark "quick" "quick"
  run_benchmark "scalability" "scalability"
  run_go_benchmarks
  run_profiled
  analyze_profiles
  generate_summary
  
  echo -e "${GREEN} Benchmark suite completed successfully!${NC}"
  echo ""
  echo -e "${BLUE}Results location: ${NC}$RUN_DIR"
  echo -e "${BLUE}Summary report: ${NC}$RUN_DIR/SUMMARY.md"
  echo ""
  echo -e "${YELLOW}Next steps:${NC}"
  echo "1. Review SUMMARY.md for performance metrics"
  echo "2. Analyze cpu_analysis.txt and mem_analysis.txt for hotspots"
  echo "3. Explore profiles interactively with: go tool pprof cpu.prof"
  echo "4. Implement optimizations based on findings"
  echo "5. Re-run benchmarks to measure improvements"
  echo ""
}

# Run main function
main
