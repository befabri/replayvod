package scheduler

import (
	"context"
	"fmt"
	"math"
)

// RunFunc runs a task until completion or context cancellation.
type RunFunc func(ctx context.Context) error

// Task defines a handler and its startup configuration. An interval of zero
// permits only explicitly requested runs.
type Task struct {
	Name            string
	Description     string
	IntervalSeconds int64
	Run             RunFunc
}

// Registry is an immutable set of handlers this process can execute. Its zero
// value is an empty registry. Dashboard preferences belong in the database and
// never change registry membership.
type Registry struct {
	tasks map[string]Task
}

// NewRegistry validates and copies definitions before any database changes.
// Task values are owned by the registry; handlers retain their dependencies.
func NewRegistry(tasks []Task) (Registry, error) {
	r := Registry{tasks: make(map[string]Task, len(tasks))}
	for _, task := range tasks {
		if task.Name == "" {
			return Registry{}, fmt.Errorf("scheduler: task name required")
		}
		if task.Run == nil {
			return Registry{}, fmt.Errorf("scheduler: task %q has no Run func", task.Name)
		}
		// Both repository adapters represent interval_seconds as int32.
		if task.IntervalSeconds < 0 || task.IntervalSeconds > math.MaxInt32 {
			return Registry{}, fmt.Errorf("scheduler: task %q interval must be between 0 and %d seconds", task.Name, math.MaxInt32)
		}
		if _, exists := r.tasks[task.Name]; exists {
			return Registry{}, fmt.Errorf("scheduler: duplicate task %q", task.Name)
		}
		r.tasks[task.Name] = task
	}
	return r, nil
}

// Len returns the number of configured handlers.
func (r Registry) Len() int { return len(r.tasks) }
