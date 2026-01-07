// Package scheduler provides a generic task scheduler for periodic and cron-based jobs.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/logging"
)

// TaskFunc is a function that performs a scheduled task.
// It receives a context that will be cancelled if the scheduler stops.
type TaskFunc func(ctx context.Context) error

// Schedule defines when a task should run.
type Schedule interface {
	// Next returns the next time the task should run after the given time.
	Next(after time.Time) time.Time
}

// Task represents a scheduled task.
type Task struct {
	ID          string
	Name        string
	Description string
	Schedule    Schedule
	Func        TaskFunc
	Enabled     bool
	RunOnStart  bool // Run immediately when scheduler starts
	Timeout     time.Duration
}

// TaskStatus represents the current status of a task.
type TaskStatus struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Enabled      bool          `json:"enabled"`
	LastRun      time.Time     `json:"last_run,omitempty"`
	LastDuration time.Duration `json:"last_duration,omitempty"`
	LastError    string        `json:"last_error,omitempty"`
	NextRun      time.Time     `json:"next_run,omitempty"`
	RunCount     int64         `json:"run_count"`
	ErrorCount   int64         `json:"error_count"`
}

// Scheduler manages and runs scheduled tasks.
type Scheduler struct {
	tasks   map[string]*taskEntry
	mu      sync.RWMutex
	logger  *slog.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	running bool
	wg      sync.WaitGroup
}

type taskEntry struct {
	task       *Task
	status     TaskStatus
	nextRun    time.Time
	cancelFunc context.CancelFunc
}

// New creates a new scheduler.
func New(logger *logging.Logger) *Scheduler {
	var l *slog.Logger
	if logger == nil {
		l = slog.Default()
	} else {
		// Use the embedded slog.Logger
		l = logger.Logger
	}

	return &Scheduler{
		tasks:  make(map[string]*taskEntry),
		logger: l.With("component", "scheduler"),
	}
}

// AddTask adds a task to the scheduler.
func (s *Scheduler) AddTask(task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if task.ID == "" {
		return fmt.Errorf("task ID is required")
	}
	if task.Schedule == nil {
		return fmt.Errorf("task schedule is required")
	}
	if task.Func == nil {
		return fmt.Errorf("task function is required")
	}

	if _, exists := s.tasks[task.ID]; exists {
		return fmt.Errorf("task %s already exists", task.ID)
	}

	entry := &taskEntry{
		task: task,
		status: TaskStatus{
			ID:          task.ID,
			Name:        task.Name,
			Description: task.Description,
			Enabled:     task.Enabled,
		},
	}

	if task.Enabled {
		entry.nextRun = task.Schedule.Next(clock.Now())
		entry.status.NextRun = entry.nextRun
	}

	s.tasks[task.ID] = entry
	s.logger.Info("task added", "id", task.ID, "name", task.Name)

	return nil
}

// RemoveTask removes a task from the scheduler.
func (s *Scheduler) RemoveTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.tasks[id]
	if !exists {
		return fmt.Errorf("task %s not found", id)
	}

	// Cancel if running
	if entry.cancelFunc != nil {
		entry.cancelFunc()
	}

	delete(s.tasks, id)
	s.logger.Info("task removed", "id", id)
	return nil
}

// EnableTask enables or disables a task.
func (s *Scheduler) EnableTask(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.tasks[id]
	if !exists {
		return fmt.Errorf("task %s not found", id)
	}

	entry.task.Enabled = enabled
	entry.status.Enabled = enabled

	if enabled {
		entry.nextRun = entry.task.Schedule.Next(clock.Now())
		entry.status.NextRun = entry.nextRun
	} else {
		entry.nextRun = time.Time{}
		entry.status.NextRun = time.Time{}
	}

	return nil
}

// RunTask runs a task immediately, regardless of schedule.
func (s *Scheduler) RunTask(id string) error {
	s.mu.RLock()
	entry, exists := s.tasks[id]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("task %s not found", id)
	}

	go s.executeTask(entry)
	return nil
}

// GetStatus returns the status of all tasks.
func (s *Scheduler) GetStatus() []TaskStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statuses := make([]TaskStatus, 0, len(s.tasks))
	for _, entry := range s.tasks {
		statuses = append(statuses, entry.status)
	}

	// Sort by name
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Name < statuses[j].Name
	})

	return statuses
}

// GetTaskStatus returns the status of a specific task.
func (s *Scheduler) GetTaskStatus(id string) (TaskStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.tasks[id]
	if !exists {
		return TaskStatus{}, false
	}
	return entry.status, true
}

// GetState returns the current state of all tasks for persistence.
func (s *Scheduler) GetState() []TaskStatus {
	return s.GetStatus()
}

// RestoreState restores task state from a previous run.
func (s *Scheduler) RestoreState(statuses []TaskStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, status := range statuses {
		if entry, exists := s.tasks[status.ID]; exists {
			// Restore historical data only
			entry.status.LastRun = status.LastRun
			entry.status.LastDuration = status.LastDuration
			entry.status.LastError = status.LastError
			entry.status.RunCount = status.RunCount
			entry.status.ErrorCount = status.ErrorCount

			// Keep runtime config (Enabled) from current config,
			// unless we explicitly want to restore enable/disable state?
			// Generally upgrade should respect the new config file's enabled/disabled state,
			// but we want to preserve HISTORY (RunCount, LastRun).
			s.logger.Debug("restored task state", "id", status.ID, "run_count", status.RunCount)
		}
	}
}

// Start starts the scheduler.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.running = true
	s.mu.Unlock()

	s.logger.Info("scheduler started")

	// Run tasks that should run on start
	s.mu.RLock()
	for _, entry := range s.tasks {
		if entry.task.Enabled && entry.task.RunOnStart {
			go s.executeTask(entry)
		}
	}
	s.mu.RUnlock()

	// Start the main scheduler loop
	go s.run()
}

// Stop stops the scheduler and waits for running tasks to complete.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.cancel()
	s.running = false
	s.mu.Unlock()

	// Wait for running tasks
	s.wg.Wait()
	s.logger.Info("scheduler stopped")
}

// IsRunning returns whether the scheduler is running.
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// run is the main scheduler loop.
func (s *Scheduler) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-ticker.C:
			s.checkAndRunTasks(now)
		}
	}
}

// checkAndRunTasks checks all tasks and runs those that are due.
func (s *Scheduler) checkAndRunTasks(now time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, entry := range s.tasks {
		if !entry.task.Enabled {
			continue
		}
		if entry.nextRun.IsZero() {
			continue
		}
		if now.After(entry.nextRun) || now.Equal(entry.nextRun) {
			go s.executeTask(entry)
		}
	}
}

// executeTask runs a single task.
func (s *Scheduler) executeTask(entry *taskEntry) {
	s.wg.Add(1)
	defer s.wg.Done()

	task := entry.task
	s.logger.Debug("executing task", "id", task.ID, "name", task.Name)

	// Create task context with timeout
	ctx := s.ctx
	var cancel context.CancelFunc
	if task.Timeout > 0 {
		ctx, cancel = context.WithTimeout(s.ctx, task.Timeout)
	} else {
		ctx, cancel = context.WithCancel(s.ctx)
	}

	// Store cancel func so task can be cancelled
	s.mu.Lock()
	entry.cancelFunc = cancel
	s.mu.Unlock()

	defer func() {
		cancel()
		s.mu.Lock()
		entry.cancelFunc = nil
		s.mu.Unlock()
	}()

	start := clock.Now()
	err := task.Func(ctx)
	duration := time.Since(start)

	// Update status
	s.mu.Lock()
	entry.status.LastRun = start
	entry.status.LastDuration = duration
	entry.status.RunCount++
	if err != nil {
		entry.status.LastError = err.Error()
		entry.status.ErrorCount++
		s.logger.Warn("task failed", "id", task.ID, "error", err, "duration", duration)
	} else {
		entry.status.LastError = ""
		s.logger.Debug("task completed", "id", task.ID, "duration", duration)
	}

	// Schedule next run
	if task.Enabled {
		entry.nextRun = task.Schedule.Next(clock.Now())
		entry.status.NextRun = entry.nextRun
	}
	s.mu.Unlock()
}
