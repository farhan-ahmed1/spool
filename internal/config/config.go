package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all configuration for the Spool system
type Config struct {
	Redis     RedisConfig
	Worker    WorkerConfig
	Broker    BrokerConfig
	Dashboard DashboardConfig
	Logging   LoggingConfig
}

// RedisConfig holds Redis connection settings
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	PoolSize int
}

// WorkerConfig holds worker-specific settings
type WorkerConfig struct {
	// Number of concurrent task executors
	Concurrency int

	// Worker ID (auto-generated if empty)
	ID string

	// Graceful shutdown timeout
	ShutdownTimeout time.Duration

	// Task execution timeout (default)
	DefaultTimeout time.Duration

	// Health check interval
	HealthCheckInterval time.Duration

	// Enable auto-scaling
	AutoScaling AutoScalingConfig
}

// AutoScalingConfig holds auto-scaling settings
type AutoScalingConfig struct {
	Enabled bool

	// Minimum number of workers
	MinWorkers int

	// Maximum number of workers
	MaxWorkers int

	// Scale up when queue depth exceeds this threshold
	ScaleUpThreshold int

	// Scale down when idle for this duration
	ScaleDownIdleTime time.Duration

	// Check interval for scaling decisions
	CheckInterval time.Duration
}

// BrokerConfig holds broker-specific settings
type BrokerConfig struct {
	// Broker server address
	ListenAddr string

	// Task retention period
	RetentionPeriod time.Duration

	// Dead letter queue settings
	DLQEnabled      bool
	DLQRetentionDay int
}

// DashboardConfig holds dashboard settings
type DashboardConfig struct {
	Enabled    bool
	ListenAddr string
	RefreshMS  int // Metrics refresh interval in milliseconds
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level  string // debug, info, warn, error
	Format string // json, text
}

// Default returns a configuration with sensible defaults
func Default() *Config {
	return &Config{
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     6379,
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       0,
			PoolSize: 10,
		},
		Worker: WorkerConfig{
			Concurrency:         5,
			ShutdownTimeout:     30 * time.Second,
			DefaultTimeout:      30 * time.Second,
			HealthCheckInterval: 10 * time.Second,
			AutoScaling: AutoScalingConfig{
				Enabled:           false, // Disable by default, enable in Week 2
				MinWorkers:        1,
				MaxWorkers:        10,
				ScaleUpThreshold:  100,
				ScaleDownIdleTime: 5 * time.Minute,
				CheckInterval:     30 * time.Second,
			},
		},
		Broker: BrokerConfig{
			ListenAddr:      ":8080",
			RetentionPeriod: 24 * time.Hour,
			DLQEnabled:      true,
			DLQRetentionDay: 7,
		},
		Dashboard: DashboardConfig{
			Enabled:    true,
			ListenAddr: ":8080",
			RefreshMS:  1000,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// RedisAddr returns the full Redis address
func (c *RedisConfig) RedisAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Redis.Host == "" {
		return fmt.Errorf("redis host cannot be empty")
	}
	if c.Worker.Concurrency < 1 {
		return fmt.Errorf("worker concurrency must be at least 1")
	}
	if c.Worker.AutoScaling.Enabled {
		if c.Worker.AutoScaling.MinWorkers < 1 {
			return fmt.Errorf("min workers must be at least 1")
		}
		if c.Worker.AutoScaling.MaxWorkers < c.Worker.AutoScaling.MinWorkers {
			return fmt.Errorf("max workers must be >= min workers")
		}
	}
	return nil
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
