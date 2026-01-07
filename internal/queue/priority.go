package queue

import (
	"fmt"

	"github.com/farhan-ahmed1/spool/internal/task"
)

// PriorityString returns the string representation of a priority level
func PriorityString(p task.Priority) string {
	switch p {
	case task.PriorityCritical:
		return "critical"
	case task.PriorityHigh:
		return "high"
	case task.PriorityNormal:
		return "normal"
	case task.PriorityLow:
		return "low"
	default:
		return "unknown"
	}
}

// ParsePriority converts a string to a Priority value
func ParsePriority(s string) (task.Priority, error) {
	switch s {
	case "critical":
		return task.PriorityCritical, nil
	case "high":
		return task.PriorityHigh, nil
	case "normal":
		return task.PriorityNormal, nil
	case "low":
		return task.PriorityLow, nil
	default:
		return task.PriorityNormal, fmt.Errorf("unknown priority: %s", s)
	}
}
