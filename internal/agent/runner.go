package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/nalanj/acoo/internal/config"
	"github.com/nalanj/acoo/internal/log"
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

	r.Logger.Info("started", log.F("jobs", len(r.Agent.JobsMap)))

	// Parse schedules for each job (schedule is on agent)
	for jobName, schedule := range r.Agent.Jobs {
		sched, err := scheduler.Parse(schedule)
		if err != nil {
			r.Logger.Warn("invalid_schedule", log.F("job", jobName), log.F("error", err))
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
}

// Wait waits for all goroutines to finish (call after Stop)
func (r *Runner) Wait() {
	r.wg.Wait()
}

// runJob runs a single job on its schedule
func (r *Runner) runJob(ctx context.Context, jobName string, cancel context.CancelFunc) {
	defer r.wg.Done()

	sched := r.schedule[jobName]
	job := r.Agent.JobsMap[jobName]

	r.Logger.Info("job_started", log.F("job", jobName), log.F("schedule", sched.Spec))

	// Run immediately for @once
	if sched.IsOneShot() {
		r.executeJob(jobName, job)
		return
	}

	for {
		wait := sched.SleepUntil()
		if wait > 0 {
			r.Logger.Info("next_run", log.F("job", jobName), log.F("in", scheduler.FormatInterval(wait)))

			// Sleep in small chunks to respond to cancellation faster
			for wait > 0 {
				select {
				case <-ctx.Done():
					r.Logger.Info("job_stopped", log.F("job", jobName))
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
			r.Logger.Info("job_stopped", log.F("job", jobName))
			return
		default:
		}

		// Check if already running, skip if so
		r.mu.Lock()
		if r.running[jobName] {
			r.Logger.Info("job_skipped", log.F("job", jobName), log.F("reason", "already_running"))
			r.mu.Unlock()
			sched.NextRun()
			continue
		}
		r.running[jobName] = true
		r.mu.Unlock()

		// Check preconditions
		if !r.checkPreconditions(job) {
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
func (r *Runner) checkPreconditions(job *config.Job) bool {
	if len(job.Preconditions) == 0 {
		return true
	}

	for _, cmd := range job.Preconditions {
		r.Logger.Info("running_precondition", log.F("job", job.Name), log.F("command", cmd))
		output, err := exec.Command("sh", "-c", cmd).CombinedOutput()
		if err != nil {
			outputStr := strings.TrimSpace(string(output))
			if outputStr != "" {
				r.Logger.Info("precondition_failed", log.F("job", job.Name), log.F("command", cmd), log.F("output", outputStr))
			} else {
				r.Logger.Info("precondition_failed", log.F("job", job.Name), log.F("command", cmd))
			}
			return false
		}
	}
	return true
}

// executeJob runs a single job execution in a subprocess for environment isolation
func (r *Runner) executeJob(jobName string, job *config.Job) {
	r.Logger.Info("job_starting", log.F("job", job.Name))

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
		r.Logger.Error("find_executable_failed", log.F("job", job.Name), log.F("error", err))
		return
	}

	// Build environment - start with current env, then add agent vars, then job vars
	env := os.Environ()
	for k, v := range r.Agent.GetEnv() {
		env = append(env, k+"="+v)
	}
	for k, v := range job.GetEnv() {
		env = append(env, k+"="+v)
	}

	// Build command with thinking budget if set
	cmdArgs := []string{"agent",
		"--system-prompt", systemPrompt,
		"--task-prompt", taskPrompt,
		"--model", job.Model,
		"--provider", job.Provider,
		"--agent-name", r.Agent.Name,
	}
	if thinkingBudget := job.GetThinkingBudget(); thinkingBudget > 0 {
		cmdArgs = append(cmdArgs, "--thinking-budget", fmt.Sprintf("%d", thinkingBudget))
	}

	// Run in subprocess for environment isolation
	cmd := exec.Command(execPath, cmdArgs...)
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		r.Logger.Error("subprocess_failed", log.F("job", jobName), log.F("error", err))
		r.Logger.Info("subprocess_output", log.F("job", jobName), log.F("output", string(output)))
		return
	}

	r.Logger.Info("job_complete", log.F("job", jobName), log.F("result", truncate(string(output), 200)))
}

// truncate truncates a string to maxLen, adding ellipsis if truncated
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}