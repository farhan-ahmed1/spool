package config

import (
	"os"
	"testing"
	"time"
)

// =============================================================================
// Default() Tests
// =============================================================================

func TestDefault(t *testing.T) {
	// Clear environment variables that might affect defaults
	originalHost := os.Getenv("REDIS_HOST")
	originalPassword := os.Getenv("REDIS_PASSWORD")
	os.Unsetenv("REDIS_HOST")
	os.Unsetenv("REDIS_PASSWORD")
	defer func() {
		if originalHost != "" {
			os.Setenv("REDIS_HOST", originalHost)
		}
		if originalPassword != "" {
			os.Setenv("REDIS_PASSWORD", originalPassword)
		}
	}()

	cfg := Default()

	t.Run("Redis defaults", func(t *testing.T) {
		if cfg.Redis.Host != "localhost" {
			t.Errorf("expected Redis.Host = 'localhost', got '%s'", cfg.Redis.Host)
		}
		if cfg.Redis.Port != 6379 {
			t.Errorf("expected Redis.Port = 6379, got %d", cfg.Redis.Port)
		}
		if cfg.Redis.Password != "" {
			t.Errorf("expected Redis.Password = '', got '%s'", cfg.Redis.Password)
		}
		if cfg.Redis.DB != 0 {
			t.Errorf("expected Redis.DB = 0, got %d", cfg.Redis.DB)
		}
		if cfg.Redis.PoolSize != 10 {
			t.Errorf("expected Redis.PoolSize = 10, got %d", cfg.Redis.PoolSize)
		}
	})

	t.Run("Worker defaults", func(t *testing.T) {
		if cfg.Worker.Concurrency != 5 {
			t.Errorf("expected Worker.Concurrency = 5, got %d", cfg.Worker.Concurrency)
		}
		if cfg.Worker.ShutdownTimeout != 30*time.Second {
			t.Errorf("expected Worker.ShutdownTimeout = 30s, got %v", cfg.Worker.ShutdownTimeout)
		}
		if cfg.Worker.DefaultTimeout != 30*time.Second {
			t.Errorf("expected Worker.DefaultTimeout = 30s, got %v", cfg.Worker.DefaultTimeout)
		}
		if cfg.Worker.HealthCheckInterval != 10*time.Second {
			t.Errorf("expected Worker.HealthCheckInterval = 10s, got %v", cfg.Worker.HealthCheckInterval)
		}
	})

	t.Run("AutoScaling defaults", func(t *testing.T) {
		as := cfg.Worker.AutoScaling
		if as.Enabled != false {
			t.Errorf("expected AutoScaling.Enabled = false, got %v", as.Enabled)
		}
		if as.MinWorkers != 1 {
			t.Errorf("expected AutoScaling.MinWorkers = 1, got %d", as.MinWorkers)
		}
		if as.MaxWorkers != 10 {
			t.Errorf("expected AutoScaling.MaxWorkers = 10, got %d", as.MaxWorkers)
		}
		if as.ScaleUpThreshold != 100 {
			t.Errorf("expected AutoScaling.ScaleUpThreshold = 100, got %d", as.ScaleUpThreshold)
		}
		if as.ScaleDownIdleTime != 5*time.Minute {
			t.Errorf("expected AutoScaling.ScaleDownIdleTime = 5m, got %v", as.ScaleDownIdleTime)
		}
		if as.CheckInterval != 30*time.Second {
			t.Errorf("expected AutoScaling.CheckInterval = 30s, got %v", as.CheckInterval)
		}
	})

	t.Run("Broker defaults", func(t *testing.T) {
		if cfg.Broker.ListenAddr != ":8080" {
			t.Errorf("expected Broker.ListenAddr = ':8080', got '%s'", cfg.Broker.ListenAddr)
		}
		if cfg.Broker.RetentionPeriod != 24*time.Hour {
			t.Errorf("expected Broker.RetentionPeriod = 24h, got %v", cfg.Broker.RetentionPeriod)
		}
		if cfg.Broker.DLQEnabled != true {
			t.Errorf("expected Broker.DLQEnabled = true, got %v", cfg.Broker.DLQEnabled)
		}
		if cfg.Broker.DLQRetentionDay != 7 {
			t.Errorf("expected Broker.DLQRetentionDay = 7, got %d", cfg.Broker.DLQRetentionDay)
		}
	})

	t.Run("Dashboard defaults", func(t *testing.T) {
		if cfg.Dashboard.Enabled != true {
			t.Errorf("expected Dashboard.Enabled = true, got %v", cfg.Dashboard.Enabled)
		}
		if cfg.Dashboard.ListenAddr != ":8080" {
			t.Errorf("expected Dashboard.ListenAddr = ':8080', got '%s'", cfg.Dashboard.ListenAddr)
		}
		if cfg.Dashboard.RefreshMS != 1000 {
			t.Errorf("expected Dashboard.RefreshMS = 1000, got %d", cfg.Dashboard.RefreshMS)
		}
	})

	t.Run("Logging defaults", func(t *testing.T) {
		if cfg.Logging.Level != "info" {
			t.Errorf("expected Logging.Level = 'info', got '%s'", cfg.Logging.Level)
		}
		if cfg.Logging.Format != "text" {
			t.Errorf("expected Logging.Format = 'text', got '%s'", cfg.Logging.Format)
		}
	})
}

func TestDefaultWithEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name         string
		envVars      map[string]string
		expectedHost string
		expectedPass string
	}{
		{
			name: "custom Redis host from env",
			envVars: map[string]string{
				"REDIS_HOST": "redis.example.com",
			},
			expectedHost: "redis.example.com",
			expectedPass: "",
		},
		{
			name: "custom Redis password from env",
			envVars: map[string]string{
				"REDIS_PASSWORD": "secretpassword",
			},
			expectedHost: "localhost",
			expectedPass: "secretpassword",
		},
		{
			name: "both Redis host and password from env",
			envVars: map[string]string{
				"REDIS_HOST":     "redis-cluster.internal",
				"REDIS_PASSWORD": "clusterpass123",
			},
			expectedHost: "redis-cluster.internal",
			expectedPass: "clusterpass123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and clear existing env vars
			originalHost := os.Getenv("REDIS_HOST")
			originalPassword := os.Getenv("REDIS_PASSWORD")
			os.Unsetenv("REDIS_HOST")
			os.Unsetenv("REDIS_PASSWORD")

			// Set test env vars
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			// Restore after test
			defer func() {
				os.Unsetenv("REDIS_HOST")
				os.Unsetenv("REDIS_PASSWORD")
				if originalHost != "" {
					os.Setenv("REDIS_HOST", originalHost)
				}
				if originalPassword != "" {
					os.Setenv("REDIS_PASSWORD", originalPassword)
				}
			}()

			cfg := Default()

			if cfg.Redis.Host != tt.expectedHost {
				t.Errorf("expected Redis.Host = '%s', got '%s'", tt.expectedHost, cfg.Redis.Host)
			}
			if cfg.Redis.Password != tt.expectedPass {
				t.Errorf("expected Redis.Password = '%s', got '%s'", tt.expectedPass, cfg.Redis.Password)
			}
		})
	}
}

// =============================================================================
// RedisAddr() Tests
// =============================================================================

func TestRedisConfig_RedisAddr(t *testing.T) {
	tests := []struct {
		name     string
		config   RedisConfig
		expected string
	}{
		{
			name: "default localhost",
			config: RedisConfig{
				Host: "localhost",
				Port: 6379,
			},
			expected: "localhost:6379",
		},
		{
			name: "custom host and port",
			config: RedisConfig{
				Host: "redis.example.com",
				Port: 6380,
			},
			expected: "redis.example.com:6380",
		},
		{
			name: "IP address",
			config: RedisConfig{
				Host: "192.168.1.100",
				Port: 6379,
			},
			expected: "192.168.1.100:6379",
		},
		{
			name: "IPv6 address",
			config: RedisConfig{
				Host: "::1",
				Port: 6379,
			},
			expected: "::1:6379",
		},
		{
			name: "port zero",
			config: RedisConfig{
				Host: "localhost",
				Port: 0,
			},
			expected: "localhost:0",
		},
		{
			name: "high port number",
			config: RedisConfig{
				Host: "localhost",
				Port: 65535,
			},
			expected: "localhost:65535",
		},
		{
			name: "empty host",
			config: RedisConfig{
				Host: "",
				Port: 6379,
			},
			expected: ":6379",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.RedisAddr()
			if result != tt.expected {
				t.Errorf("expected RedisAddr() = '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// =============================================================================
// Validate() Tests
// =============================================================================

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid default config",
			config:      Default(),
			expectError: false,
		},
		{
			name: "valid custom config",
			config: &Config{
				Redis: RedisConfig{
					Host:     "redis.example.com",
					Port:     6379,
					PoolSize: 20,
				},
				Worker: WorkerConfig{
					Concurrency: 10,
				},
			},
			expectError: false,
		},
		{
			name: "empty redis host",
			config: &Config{
				Redis: RedisConfig{
					Host: "",
					Port: 6379,
				},
				Worker: WorkerConfig{
					Concurrency: 5,
				},
			},
			expectError: true,
			errorMsg:    "redis host cannot be empty",
		},
		{
			name: "zero worker concurrency",
			config: &Config{
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				Worker: WorkerConfig{
					Concurrency: 0,
				},
			},
			expectError: true,
			errorMsg:    "worker concurrency must be at least 1",
		},
		{
			name: "negative worker concurrency",
			config: &Config{
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				Worker: WorkerConfig{
					Concurrency: -1,
				},
			},
			expectError: true,
			errorMsg:    "worker concurrency must be at least 1",
		},
		{
			name: "valid autoscaling enabled",
			config: &Config{
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				Worker: WorkerConfig{
					Concurrency: 5,
					AutoScaling: AutoScalingConfig{
						Enabled:    true,
						MinWorkers: 2,
						MaxWorkers: 10,
					},
				},
			},
			expectError: false,
		},
		{
			name: "autoscaling with zero min workers",
			config: &Config{
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				Worker: WorkerConfig{
					Concurrency: 5,
					AutoScaling: AutoScalingConfig{
						Enabled:    true,
						MinWorkers: 0,
						MaxWorkers: 10,
					},
				},
			},
			expectError: true,
			errorMsg:    "min workers must be at least 1",
		},
		{
			name: "autoscaling with negative min workers",
			config: &Config{
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				Worker: WorkerConfig{
					Concurrency: 5,
					AutoScaling: AutoScalingConfig{
						Enabled:    true,
						MinWorkers: -1,
						MaxWorkers: 10,
					},
				},
			},
			expectError: true,
			errorMsg:    "min workers must be at least 1",
		},
		{
			name: "autoscaling with max < min workers",
			config: &Config{
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				Worker: WorkerConfig{
					Concurrency: 5,
					AutoScaling: AutoScalingConfig{
						Enabled:    true,
						MinWorkers: 10,
						MaxWorkers: 5,
					},
				},
			},
			expectError: true,
			errorMsg:    "max workers must be >= min workers",
		},
		{
			name: "autoscaling with max = min workers",
			config: &Config{
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				Worker: WorkerConfig{
					Concurrency: 5,
					AutoScaling: AutoScalingConfig{
						Enabled:    true,
						MinWorkers: 5,
						MaxWorkers: 5,
					},
				},
			},
			expectError: false,
		},
		{
			name: "autoscaling disabled skips validation",
			config: &Config{
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				Worker: WorkerConfig{
					Concurrency: 5,
					AutoScaling: AutoScalingConfig{
						Enabled:    false,
						MinWorkers: 0,  // Would be invalid if enabled
						MaxWorkers: -1, // Would be invalid if enabled
					},
				},
			},
			expectError: false,
		},
		{
			name: "minimal valid config",
			config: &Config{
				Redis: RedisConfig{
					Host: "localhost",
				},
				Worker: WorkerConfig{
					Concurrency: 1,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
					return
				}
				if err.Error() != tt.errorMsg {
					t.Errorf("expected error message '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

// =============================================================================
// getEnv() Tests (through Default())
// =============================================================================

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name         string
		envKey       string
		envValue     string
		setEnv       bool
		checkDefault func(*Config) bool
		description  string
	}{
		{
			name:     "env var set returns env value",
			envKey:   "REDIS_HOST",
			envValue: "custom-redis-host",
			setEnv:   true,
			checkDefault: func(cfg *Config) bool {
				return cfg.Redis.Host == "custom-redis-host"
			},
			description: "Redis.Host should be 'custom-redis-host'",
		},
		{
			name:     "env var not set returns default",
			envKey:   "REDIS_HOST",
			envValue: "",
			setEnv:   false,
			checkDefault: func(cfg *Config) bool {
				return cfg.Redis.Host == "localhost"
			},
			description: "Redis.Host should be 'localhost'",
		},
		{
			name:     "empty env var returns default",
			envKey:   "REDIS_HOST",
			envValue: "",
			setEnv:   true,
			checkDefault: func(cfg *Config) bool {
				// Empty string is set but treated as not set due to condition
				return cfg.Redis.Host == "localhost"
			},
			description: "Redis.Host should be 'localhost' when env is empty string",
		},
		{
			name:     "password env var set",
			envKey:   "REDIS_PASSWORD",
			envValue: "mypassword",
			setEnv:   true,
			checkDefault: func(cfg *Config) bool {
				return cfg.Redis.Password == "mypassword"
			},
			description: "Redis.Password should be 'mypassword'",
		},
		{
			name:     "password env var with special characters",
			envKey:   "REDIS_PASSWORD",
			envValue: "p@ssw0rd!#$%",
			setEnv:   true,
			checkDefault: func(cfg *Config) bool {
				return cfg.Redis.Password == "p@ssw0rd!#$%"
			},
			description: "Redis.Password should handle special characters",
		},
		{
			name:     "env var with spaces",
			envKey:   "REDIS_HOST",
			envValue: "  redis.example.com  ",
			setEnv:   true,
			checkDefault: func(cfg *Config) bool {
				return cfg.Redis.Host == "  redis.example.com  "
			},
			description: "Redis.Host should preserve spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original value
			originalValue := os.Getenv(tt.envKey)
			os.Unsetenv(tt.envKey)

			// Set or unset based on test case
			if tt.setEnv {
				os.Setenv(tt.envKey, tt.envValue)
			}

			// Restore after test
			defer func() {
				os.Unsetenv(tt.envKey)
				if originalValue != "" {
					os.Setenv(tt.envKey, originalValue)
				}
			}()

			cfg := Default()

			if !tt.checkDefault(cfg) {
				t.Errorf("check failed: %s", tt.description)
			}
		})
	}
}

// =============================================================================
// Config Struct Tests
// =============================================================================

func TestRedisConfig_Fields(t *testing.T) {
	config := RedisConfig{
		Host:     "redis.test.com",
		Port:     6380,
		Password: "testpass",
		DB:       1,
		PoolSize: 15,
	}

	if config.Host != "redis.test.com" {
		t.Errorf("expected Host = 'redis.test.com', got '%s'", config.Host)
	}
	if config.Port != 6380 {
		t.Errorf("expected Port = 6380, got %d", config.Port)
	}
	if config.Password != "testpass" {
		t.Errorf("expected Password = 'testpass', got '%s'", config.Password)
	}
	if config.DB != 1 {
		t.Errorf("expected DB = 1, got %d", config.DB)
	}
	if config.PoolSize != 15 {
		t.Errorf("expected PoolSize = 15, got %d", config.PoolSize)
	}
}

func TestWorkerConfig_Fields(t *testing.T) {
	config := WorkerConfig{
		Concurrency:         10,
		ID:                  "worker-1",
		ShutdownTimeout:     60 * time.Second,
		DefaultTimeout:      45 * time.Second,
		HealthCheckInterval: 15 * time.Second,
		AutoScaling: AutoScalingConfig{
			Enabled:    true,
			MinWorkers: 2,
			MaxWorkers: 20,
		},
	}

	if config.Concurrency != 10 {
		t.Errorf("expected Concurrency = 10, got %d", config.Concurrency)
	}
	if config.ID != "worker-1" {
		t.Errorf("expected ID = 'worker-1', got '%s'", config.ID)
	}
	if config.ShutdownTimeout != 60*time.Second {
		t.Errorf("expected ShutdownTimeout = 60s, got %v", config.ShutdownTimeout)
	}
	if config.DefaultTimeout != 45*time.Second {
		t.Errorf("expected DefaultTimeout = 45s, got %v", config.DefaultTimeout)
	}
	if config.HealthCheckInterval != 15*time.Second {
		t.Errorf("expected HealthCheckInterval = 15s, got %v", config.HealthCheckInterval)
	}
	if !config.AutoScaling.Enabled {
		t.Errorf("expected AutoScaling.Enabled = true")
	}
}

func TestBrokerConfig_Fields(t *testing.T) {
	config := BrokerConfig{
		ListenAddr:      ":9090",
		RetentionPeriod: 48 * time.Hour,
		DLQEnabled:      false,
		DLQRetentionDay: 14,
	}

	if config.ListenAddr != ":9090" {
		t.Errorf("expected ListenAddr = ':9090', got '%s'", config.ListenAddr)
	}
	if config.RetentionPeriod != 48*time.Hour {
		t.Errorf("expected RetentionPeriod = 48h, got %v", config.RetentionPeriod)
	}
	if config.DLQEnabled != false {
		t.Errorf("expected DLQEnabled = false, got %v", config.DLQEnabled)
	}
	if config.DLQRetentionDay != 14 {
		t.Errorf("expected DLQRetentionDay = 14, got %d", config.DLQRetentionDay)
	}
}

func TestDashboardConfig_Fields(t *testing.T) {
	config := DashboardConfig{
		Enabled:    true,
		ListenAddr: ":3000",
		RefreshMS:  500,
	}

	if config.Enabled != true {
		t.Errorf("expected Enabled = true, got %v", config.Enabled)
	}
	if config.ListenAddr != ":3000" {
		t.Errorf("expected ListenAddr = ':3000', got '%s'", config.ListenAddr)
	}
	if config.RefreshMS != 500 {
		t.Errorf("expected RefreshMS = 500, got %d", config.RefreshMS)
	}
}

func TestAutoScalingConfig_Fields(t *testing.T) {
	config := AutoScalingConfig{
		Enabled:           true,
		MinWorkers:        3,
		MaxWorkers:        15,
		ScaleUpThreshold:  200,
		ScaleDownIdleTime: 10 * time.Minute,
		CheckInterval:     1 * time.Minute,
	}

	if config.Enabled != true {
		t.Errorf("expected Enabled = true, got %v", config.Enabled)
	}
	if config.MinWorkers != 3 {
		t.Errorf("expected MinWorkers = 3, got %d", config.MinWorkers)
	}
	if config.MaxWorkers != 15 {
		t.Errorf("expected MaxWorkers = 15, got %d", config.MaxWorkers)
	}
	if config.ScaleUpThreshold != 200 {
		t.Errorf("expected ScaleUpThreshold = 200, got %d", config.ScaleUpThreshold)
	}
	if config.ScaleDownIdleTime != 10*time.Minute {
		t.Errorf("expected ScaleDownIdleTime = 10m, got %v", config.ScaleDownIdleTime)
	}
	if config.CheckInterval != 1*time.Minute {
		t.Errorf("expected CheckInterval = 1m, got %v", config.CheckInterval)
	}
}

func TestLoggingConfig_Fields(t *testing.T) {
	config := LoggingConfig{
		Level:  "debug",
		Format: "json",
	}

	if config.Level != "debug" {
		t.Errorf("expected Level = 'debug', got '%s'", config.Level)
	}
	if config.Format != "json" {
		t.Errorf("expected Format = 'json', got '%s'", config.Format)
	}
}

// =============================================================================
// Edge Cases and Error Conditions
// =============================================================================

func TestValidate_MultipleErrors(t *testing.T) {
	// Test that validation returns the first error encountered
	config := &Config{
		Redis: RedisConfig{
			Host: "", // First error: empty host
		},
		Worker: WorkerConfig{
			Concurrency: 0, // Would be second error
		},
	}

	err := config.Validate()
	if err == nil {
		t.Error("expected error but got nil")
		return
	}

	// Should get the first validation error (redis host)
	expectedMsg := "redis host cannot be empty"
	if err.Error() != expectedMsg {
		t.Errorf("expected error '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestDefaultReturnsNewInstance(t *testing.T) {
	// Ensure Default() returns a new instance each time
	cfg1 := Default()
	cfg2 := Default()

	if cfg1 == cfg2 {
		t.Error("Default() should return new instance each time")
	}

	// Modify one and ensure the other is unaffected
	cfg1.Redis.Host = "modified-host"
	if cfg2.Redis.Host == "modified-host" {
		t.Error("modifying one config should not affect another")
	}
}

func TestConfigWithZeroValues(t *testing.T) {
	// Test config with all zero/empty values
	config := &Config{}

	err := config.Validate()
	if err == nil {
		t.Error("expected error for empty config")
		return
	}

	if err.Error() != "redis host cannot be empty" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAutoScalingBoundaryConditions(t *testing.T) {
	tests := []struct {
		name        string
		minWorkers  int
		maxWorkers  int
		expectError bool
	}{
		{
			name:        "min=1, max=1 (single worker)",
			minWorkers:  1,
			maxWorkers:  1,
			expectError: false,
		},
		{
			name:        "min=1, max=2",
			minWorkers:  1,
			maxWorkers:  2,
			expectError: false,
		},
		{
			name:        "min=100, max=1000 (large scale)",
			minWorkers:  100,
			maxWorkers:  1000,
			expectError: false,
		},
		{
			name:        "min=2, max=1 (invalid)",
			minWorkers:  2,
			maxWorkers:  1,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Redis: RedisConfig{Host: "localhost"},
				Worker: WorkerConfig{
					Concurrency: 1,
					AutoScaling: AutoScalingConfig{
						Enabled:    true,
						MinWorkers: tt.minWorkers,
						MaxWorkers: tt.maxWorkers,
					},
				},
			}

			err := config.Validate()

			if tt.expectError && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got: %v", err)
			}
		})
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkDefault(b *testing.B) {
	// Clear env vars for consistent benchmarking
	os.Unsetenv("REDIS_HOST")
	os.Unsetenv("REDIS_PASSWORD")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Default()
	}
}

func BenchmarkRedisAddr(b *testing.B) {
	config := RedisConfig{
		Host: "localhost",
		Port: 6379,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = config.RedisAddr()
	}
}

func BenchmarkValidate(b *testing.B) {
	config := Default()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = config.Validate()
	}
}

func BenchmarkValidateWithAutoScaling(b *testing.B) {
	config := Default()
	config.Worker.AutoScaling.Enabled = true

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = config.Validate()
	}
}
