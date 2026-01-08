package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogger_Levels(t *testing.T) {
	tests := []struct {
		name       string
		level      string
		logLevel   Level
		shouldLog  bool
	}{
		{"Debug logs at DEBUG level", "debug", DEBUG, true},
		{"Info logs at DEBUG level", "debug", INFO, true},
		{"Debug doesn't log at INFO level", "info", DEBUG, false},
		{"Info logs at INFO level", "info", INFO, true},
		{"Warn logs at INFO level", "info", WARN, true},
		{"Error logs at WARN level", "warn", ERROR, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := New(tt.level, "text", "test")
			logger.output = &buf
			logger.colorOutput = false

			switch tt.logLevel {
			case DEBUG:
				logger.Debug("test message")
			case INFO:
				logger.Info("test message")
			case WARN:
				logger.Warn("test message")
			case ERROR:
				logger.Error("test message")
			}

			output := buf.String()
			hasOutput := len(output) > 0

			if hasOutput != tt.shouldLog {
				t.Errorf("Expected shouldLog=%v, got output: %q", tt.shouldLog, output)
			}
		})
	}
}

func TestLogger_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", "text", "worker")
	logger.output = &buf
	logger.colorOutput = false

	logger.Info("task completed", Fields{
		"task_id":  "123",
		"duration": 42,
		"success":  true,
	})

	output := buf.String()

	// Check format contains all elements
	if !strings.Contains(output, "INFO") {
		t.Error("Output should contain INFO level")
	}
	if !strings.Contains(output, "[worker]") {
		t.Error("Output should contain component name")
	}
	if !strings.Contains(output, "task completed") {
		t.Error("Output should contain message")
	}
	if !strings.Contains(output, "task_id=123") {
		t.Error("Output should contain task_id field")
	}
	if !strings.Contains(output, "duration=42") {
		t.Error("Output should contain duration field")
	}
}

func TestLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", "json", "queue")
	logger.output = &buf

	logger.Info("task enqueued", Fields{
		"task_id":  "abc-123",
		"priority": "high",
		"retries":  0,
	})

	output := buf.String()

	// Check JSON contains all fields
	expectedFields := []string{
		`"level":"INFO"`,
		`"component":"queue"`,
		`"message":"task enqueued"`,
		`"task_id":"abc-123"`,
		`"priority":"high"`,
		`"retries":0`,
	}

	for _, field := range expectedFields {
		if !strings.Contains(output, field) {
			t.Errorf("JSON output should contain %q, got: %s", field, output)
		}
	}
}

func TestLogger_WithComponent(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", "text", "main")
	logger.output = &buf
	logger.colorOutput = false

	workerLogger := logger.WithComponent("worker-1")
	workerLogger.output = &buf

	workerLogger.Info("worker started")

	output := buf.String()
	if !strings.Contains(output, "[worker-1]") {
		t.Errorf("Expected component [worker-1], got: %s", output)
	}
}

func TestLogger_ErrorFieldTypes(t *testing.T) {
	var buf bytes.Buffer
	logger := New("error", "json", "test")
	logger.output = &buf

	logger.Error("test error", Fields{
		"string_val": "hello",
		"int_val":    42,
		"bool_val":   true,
		"float_val":  3.14,
	})

	output := buf.String()

	expectedFields := []string{
		`"string_val":"hello"`,
		`"int_val":42`,
		`"bool_val":true`,
		`"float_val":3.14`,
	}

	for _, field := range expectedFields {
		if !strings.Contains(output, field) {
			t.Errorf("JSON should contain %q, got: %s", field, output)
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"debug", DEBUG},
		{"DEBUG", DEBUG},
		{"info", INFO},
		{"INFO", INFO},
		{"warn", WARN},
		{"WARN", WARN},
		{"error", ERROR},
		{"ERROR", ERROR},
		{"fatal", FATAL},
		{"FATAL", FATAL},
		{"invalid", INFO}, // default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMergeFields(t *testing.T) {
	f1 := Fields{"a": 1, "b": 2}
	f2 := Fields{"c": 3, "b": 4} // b should be overwritten
	
	result := mergeFields(f1, f2)
	
	if result["a"] != 1 {
		t.Errorf("Expected a=1, got %v", result["a"])
	}
	if result["b"] != 4 { // f2 should overwrite f1
		t.Errorf("Expected b=4, got %v", result["b"])
	}
	if result["c"] != 3 {
		t.Errorf("Expected c=3, got %v", result["c"])
	}
}

func TestEscapeJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`hello"world`, `hello\"world`},
		{"line1\nline2", "line1\\nline2"},
		{"tab\there", "tab\\there"},
		{`back\slash`, `back\\slash`},
	}

	for _, tt := range tests {
		result := escapeJSON(tt.input)
		if result != tt.expected {
			t.Errorf("escapeJSON(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}