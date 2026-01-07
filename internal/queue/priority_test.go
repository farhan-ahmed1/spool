package queue

import (
	"testing"

	"github.com/farhan-ahmed1/spool/internal/task"
	"github.com/stretchr/testify/assert"
)

func TestPriorityString(t *testing.T) {
	tests := []struct {
		name     string
		priority task.Priority
		expected string
	}{
		{"critical priority", task.PriorityCritical, "critical"},
		{"high priority", task.PriorityHigh, "high"},
		{"normal priority", task.PriorityNormal, "normal"},
		{"low priority", task.PriorityLow, "low"},
		{"unknown priority", task.Priority(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PriorityString(tt.priority)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParsePriority(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  task.Priority
		wantError bool
	}{
		{"parse critical", "critical", task.PriorityCritical, false},
		{"parse high", "high", task.PriorityHigh, false},
		{"parse normal", "normal", task.PriorityNormal, false},
		{"parse low", "low", task.PriorityLow, false},
		{"invalid priority", "invalid", task.PriorityNormal, true},
		{"empty string", "", task.PriorityNormal, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParsePriority(tt.input)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
