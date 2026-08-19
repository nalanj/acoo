package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/nalanj/acoo/internal/config"
	"github.com/nalanj/acoo/internal/scheduler"
)

// Runner manages the execution of a single agent
type Runner struct {
	Agent *config.Agent
	Logger *log.Logger

	schedule map[string]*scheduler.Schedule // job name -> schedule
	running map[string]bool // job name -> is running
	jobCancelFuncs map[string]context.CancelFunc // job name -> cancel
	wg sync.WaitGroup
	mu sync.Mutex
	started bool
}

// NewRunner creates a new agent runner
func NewRunner(agent *config.Agent, logger *log.Logger) *Runner {
	return &Runner{
		Agent:          agent,
		Logger:         logger,
		schedule:      make(map[string]*scheduler.Schedule),
		running:       make(map[string]bool),
		jobCancelFuncs: make(map[string]context.CancelFunc),
	}
}

// Start begins the agent's job loops in goroutines
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.mu.Unlock()

	r.Logger.Printf("Agent started with %d jobs", len(r.Agent.Jobs))

	// Parse schedules for each job
	for jobName, scheduleSpec := range r.Agent.Jobs {
		sched, err := scheduler.Parse(scheduleSpec)
		if err != nil {
			r.Logger.Printf("Invalid schedule for %s: %v", jobName, err)
			continue
		}
		r.schedule[jobName] = sched
	}

	// Start each job in its own goroutine
	for jobName := range r.schedule {
		jobCtx, cancel := context.WithCancel(ctx)
		r.jobCancelFuncs[jobName] = cancel
		r.wg.Add(1)
		go r.runJob(jobCtx, jobName, cancel)
	}
}

// Stop gracefully stops the agent
func (r *Runner) Stop() {
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	// Cancel all job contexts
	r.mu.Lock()
	for _, cancel := range r.jobCancelFuncs {
		cancel()
	}
	r.mu.Unlock()

	// Wait with timeout to avoid hanging forever
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished
	case <-time.After(5 * time.Second):
		r.Logger.Printf("Warning: graceful shutdown timed out")
	}
}

// runJob runs a single job on its schedule
func (r *Runner) runJob(ctx context.Context, jobName string, cancel context.CancelFunc) {
	defer r.wg.Done()

	sched := r.schedule[jobName]
	job := r.Agent.JobsMap[jobName]

	r.Logger.Printf("[%s] Job started, schedule: %s", jobName, sched.Spec)

	// Run immediately for @once
	if sched.IsOneShot() {
		r.executeJob(jobName, job)
		return
	}

	for {
		wait := sched.SleepUntil()
		if wait > 0 {
			r.Logger.Printf("[%s] Next run in %s", jobName, scheduler.FormatInterval(wait))

			// Sleep in small chunks to respond to cancellation faster
			for wait > 0 {
				select {
				case <-ctx.Done():
					r.Logger.Printf("[%s] Job stopped", jobName)
					return
				default:
				}
				sleepTime := 100 * time.Millisecond
				if sleepTime > wait {
					sleepTime = wait
				}
				time.Sleep(sleepTime)
				wait -= sleepTime
			}
		}

		// Check context before starting job
		select {
		case <-ctx.Done():
			r.Logger.Printf("[%s] Job stopped", jobName)
			return
		default:
		}

		// Check if already running, skip if so
		r.mu.Lock()
		if r.running[jobName] {
			r.Logger.Printf("[%s] Skipped (already running)", jobName)
			r.mu.Unlock()
			sched.NextRun()
			continue
		}
		r.running[jobName] = true
		r.mu.Unlock()

		// Check preconditions
		if !r.checkPreconditions(jobName, job) {
			r.mu.Lock()
			r.running[jobName] = false
			r.mu.Unlock()
			sched.NextRun()
			continue
		}

		r.executeJob(jobName, job)

		r.mu.Lock()
		r.running[jobName] = false
		r.mu.Unlock()

		sched.NextRun()
	}
}

// checkPreconditions runs preconditions and returns true if all pass
func (r *Runner) checkPreconditions(jobName string, job *config.Job) bool {
	if len(job.Preconditions) == 0 {
		return true
	}

	for _, cmd := range job.Preconditions {
		r.Logger.Printf("[%s] Running precondition: %s", jobName, cmd)
		output, err := exec.Command("sh", "-c", cmd).CombinedOutput()
		if err != nil {
			r.Logger.Printf("[%s] Precondition failed (skipping job): %s", jobName, cmd)
			r.Logger.Printf("[%s] Precondition output: %s", jobName, string(output))
			return false
		}
	}
	return true
}

// executeJob runs a single job execution in a subprocess for environment isolation
func (r *Runner) executeJob(jobName string, job *config.Job) {
	r.Logger.Printf("[%s] Starting job", jobName)

	// Build system prompt from agent body
	systemPrompt := r.Agent.Body
	if systemPrompt == "" {
		systemPrompt = "You are a helpful AI assistant."
	}

	// Build task prompt from job body
	taskPrompt := job.Body

	// Find the acoo binary path
	execPath, err := os.Executable()
	if err != nil {
		r.Logger.Printf("[%s] Failed to find executable: %v", jobName, err)
		return
	}

	// Build environment - start with current env, then add agent-specific vars
	env := os.Environ()
	for k, v := range r.Agent.GetEnv() {
		env = append(env, k+"="+v)
	}

	// Build command with thinking budget if set
	cmdArgs := []string{"agent",
		"--system-prompt", systemPrompt,
		"--task-prompt", taskPrompt,
		"--model", r.Agent.Model,
		"--provider", r.Agent.Provider,
	}
	if thinkingBudget := r.Agent.GetThinkingBudget(); thinkingBudget > 0 {
		cmdArgs = append(cmdArgs, "--thinking-budget", fmt.Sprintf("%d", thinkingBudget))
	}

	// Run in subprocess for environment isolation
	cmd := exec.Command(execPath, cmdArgs...)
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		r.Logger.Printf("[%s] Subprocess error: %v", jobName, err)
		r.Logger.Printf("[%s] Output: %s", jobName, string(output))
		return
	}

	r.Logger.Printf("[%s] Result: %s", jobName, truncate(string(output), 200))
}

// truncate truncates a string to maxLen, adding ellipsis if truncated
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
