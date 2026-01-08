package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level represents logging level
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

var levelNames = map[Level]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
	FATAL: "FATAL",
}

var levelColors = map[Level]string{
	DEBUG: "\033[36m", // Cyan
	INFO:  "\033[32m", // Green
	WARN:  "\033[33m", // Yellow
	ERROR: "\033[31m", // Red
	FATAL: "\033[35m", // Magenta
}

const colorReset = "\033[0m"

// Logger provides structured logging capabilities
type Logger struct {
	mu          sync.Mutex
	level       Level
	output      io.Writer
	component   string
	format      string // "text" or "json"
	colorOutput bool
}

// Fields represents structured logging fields
type Fields map[string]interface{}

var (
	defaultLogger *Logger
	once          sync.Once
)

// Init initializes the default logger
func Init(level, format string, component string) {
	once.Do(func() {
		defaultLogger = New(level, format, component)
	})
}

// New creates a new logger instance
func New(levelStr, format, component string) *Logger {
	level := parseLevel(levelStr)
	
	// Enable color output if writing to terminal
	colorOutput := format == "text" && isTerminal(os.Stdout)
	
	return &Logger{
		level:       level,
		output:      os.Stdout,
		component:   component,
		format:      format,
		colorOutput: colorOutput,
	}
}

// WithComponent creates a new logger with a specific component name
func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{
		level:       l.level,
		output:      l.output,
		component:   component,
		format:      l.format,
		colorOutput: l.colorOutput,
	}
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields ...Fields) {
	l.log(DEBUG, msg, mergeFields(fields...))
}

// Info logs an info message
func (l *Logger) Info(msg string, fields ...Fields) {
	l.log(INFO, msg, mergeFields(fields...))
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields ...Fields) {
	l.log(WARN, msg, mergeFields(fields...))
}

// Error logs an error message
func (l *Logger) Error(msg string, fields ...Fields) {
	l.log(ERROR, msg, mergeFields(fields...))
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(msg string, fields ...Fields) {
	l.log(FATAL, msg, mergeFields(fields...))
	os.Exit(1)
}

// log performs the actual logging
func (l *Logger) log(level Level, msg string, fields Fields) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")

	if l.format == "json" {
		l.logJSON(timestamp, level, msg, fields)
	} else {
		l.logText(timestamp, level, msg, fields)
	}
}

// logText logs in human-readable text format
func (l *Logger) logText(timestamp string, level Level, msg string, fields Fields) {
	var output strings.Builder

	// Add color if enabled
	if l.colorOutput {
		output.WriteString(levelColors[level])
	}

	// Format: [TIMESTAMP] LEVEL [COMPONENT] message key=value key=value
	output.WriteString(fmt.Sprintf("[%s] %-5s", timestamp, levelNames[level]))

	if l.colorOutput {
		output.WriteString(colorReset)
	}

	if l.component != "" {
		output.WriteString(fmt.Sprintf(" [%s]", l.component))
	}

	output.WriteString(fmt.Sprintf(" %s", msg))

	// Add fields
	if len(fields) > 0 {
		for k, v := range fields {
			output.WriteString(fmt.Sprintf(" %s=%v", k, v))
		}
	}

	output.WriteString("\n")
	fmt.Fprint(l.output, output.String())
}

// logJSON logs in JSON format
func (l *Logger) logJSON(timestamp string, level Level, msg string, fields Fields) {
	var output strings.Builder
	output.WriteString("{")
	output.WriteString(fmt.Sprintf(`"timestamp":"%s"`, timestamp))
	output.WriteString(fmt.Sprintf(`,"level":"%s"`, levelNames[level]))
	
	if l.component != "" {
		output.WriteString(fmt.Sprintf(`,"component":"%s"`, l.component))
	}
	
	output.WriteString(fmt.Sprintf(`,"message":"%s"`, escapeJSON(msg)))

	// Add fields
	for k, v := range fields {
		switch val := v.(type) {
		case string:
			output.WriteString(fmt.Sprintf(`,"%s":"%s"`, k, escapeJSON(val)))
		case int, int32, int64, float32, float64, bool:
			output.WriteString(fmt.Sprintf(`,"%s":%v`, k, val))
		case error:
			output.WriteString(fmt.Sprintf(`,"%s":"%s"`, k, escapeJSON(val.Error())))
		default:
			output.WriteString(fmt.Sprintf(`,"%s":"%v"`, k, escapeJSON(fmt.Sprintf("%v", val))))
		}
	}

	// Add caller info for errors and above
	if level >= ERROR {
		_, file, line, ok := runtime.Caller(3)
		if ok {
			output.WriteString(fmt.Sprintf(`,"caller":"%s:%d"`, file, line))
		}
	}

	output.WriteString("}\n")
	fmt.Fprint(l.output, output.String())
}

// parseLevel converts string to Level
func parseLevel(levelStr string) Level {
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN", "WARNING":
		return WARN
	case "ERROR":
		return ERROR
	case "FATAL":
		return FATAL
	default:
		return INFO
	}
}

// mergeFields combines multiple Fields maps
func mergeFields(fields ...Fields) Fields {
	if len(fields) == 0 {
		return Fields{}
	}
	
	result := Fields{}
	for _, f := range fields {
		for k, v := range f {
			result[k] = v
		}
	}
	return result
}

// escapeJSON escapes special characters for JSON
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

// isTerminal checks if output is a terminal
func isTerminal(w io.Writer) bool {
	if w == os.Stdout || w == os.Stderr {
		return true // Simplified - in production, use a terminal detection library
	}
	return false
}

// Default logger convenience functions
func Debug(msg string, fields ...Fields) {
	if defaultLogger != nil {
		defaultLogger.Debug(msg, fields...)
	} else {
		log.Printf("[DEBUG] %s", msg)
	}
}

func Info(msg string, fields ...Fields) {
	if defaultLogger != nil {
		defaultLogger.Info(msg, fields...)
	} else {
		log.Printf("[INFO] %s", msg)
	}
}

func Warn(msg string, fields ...Fields) {
	if defaultLogger != nil {
		defaultLogger.Warn(msg, fields...)
	} else {
		log.Printf("[WARN] %s", msg)
	}
}

func Error(msg string, fields ...Fields) {
	if defaultLogger != nil {
		defaultLogger.Error(msg, fields...)
	} else {
		log.Printf("[ERROR] %s", msg)
	}
}

func Fatal(msg string, fields ...Fields) {
	if defaultLogger != nil {
		defaultLogger.Fatal(msg, fields...)
	} else {
		log.Fatalf("[FATAL] %s", msg)
	}
}

// GetDefault returns the default logger
func GetDefault() *Logger {
	return defaultLogger
}