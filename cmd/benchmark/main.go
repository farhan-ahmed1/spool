package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/farhan-ahmed1/spool/tests/load"
)

func main() {
	// Define command-line flags
	benchType := flag.String("type", "quick", "Benchmark type: quick, scalability, or profiled")
	workers := flag.Int("workers", 4, "Number of workers")
	tasks := flag.Int("tasks", 1000, "Number of tasks")
	cpuProfile := flag.String("cpuprofile", "", "Write CPU profile to file")
	memProfile := flag.String("memprofile", "", "Write memory profile to file")

	flag.Parse()

	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║ Spool Performance Benchmark Suite ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Println()

	var err error
	switch *benchType {
	case "quick":
		fmt.Println("Running quick benchmark...")
		err = load.RunQuickBenchmark()

	case "scalability":
		fmt.Println("Running scalability test...")
		err = load.RunScalabilityTest()

	case "profiled":
		fmt.Println("Running profiled benchmark...")
		err = load.RunProfiledBenchmark()

	case "custom":
		fmt.Printf("Running custom benchmark: workers=%d, tasks=%d\n",
			*workers, *tasks)
		err = runCustomBenchmark(*workers, *tasks, *cpuProfile, *memProfile)

	default:
		fmt.Printf("Unknown benchmark type: %s\n", *benchType)
		fmt.Println("Available types: quick, scalability, profiled, custom")
		os.Exit(1)
	}

	if err != nil {
		fmt.Printf("\n Benchmark failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n Benchmark completed successfully!")
}

func runCustomBenchmark(workers, tasks int, cpuProfile, memProfile string) error {
	config := load.DefaultBenchmarkConfig()
	config.WorkerCount = workers
	config.TaskCount = tasks
	config.Duration = 0 // Use task count instead

	if cpuProfile != "" || memProfile != "" {
		config.EnableProfiling = true
		config.CPUProfile = cpuProfile
		config.MemProfile = memProfile
	}

	runner, err := load.NewBenchmarkRunner(config)
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	results, err := runner.Run()
	if err != nil {
		return fmt.Errorf("benchmark failed: %w", err)
	}

	fmt.Println(results.String())

	if config.EnableProfiling {
		fmt.Println("\nProfile files created:")
		if cpuProfile != "" {
			fmt.Printf(" CPU: %s\n", cpuProfile)
		}
		if memProfile != "" {
			fmt.Printf(" Memory: %s\n", memProfile)
		}
	}

	return nil
}
