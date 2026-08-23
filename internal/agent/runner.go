package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nalanj/acoo/internal/config"
	"github.com/nalanj/acoo/internal/log"
	"github.com/nalanj/acoo/internal/scheduler"
	"github.com/nalanj/acoo/internal/storage"
)

// Runner manages the execution of a single agent
type Runner struct {
	Agent *config.Agent
	Logger *log.Logger
	store      *storage.Store

	schedule      map[string]*scheduler.Schedule      // job name -> schedule
	running       map[string]bool                    // job name -> is running
	jobCancelFuncs map[string]context.CancelFunc     // job name -> cancel
	workspace     string                             // working directory for agent
	wg            sync.WaitGroup
	mu            sync.Mutex
	started       bool
}

// NewRunner creates a new agent runner
func NewRunner(agent *config.Agent, logger *log.Logger) (*Runner, error) {
	// Workspace at ~/.local/share/acoo/{agent}/workspace
	home, _ := os.UserHomeDir()
	shareDir := filepath.Join(home, ".local", "share", "acoo")
	workspace := filepath.Join(shareDir, agent.Name, "workspace")

	// Create store for job history
	store, err := storage.NewStore(shareDir, agent.Name)
	if err != nil {
		return nil, fmt.Errorf("creating store: %w", err)
	}

	return &Runner{
		Agent:          agent,
		Logger:         logger,
		store:         store,
		schedule:      make(map[string]*scheduler.Schedule),
		running:       make(map[string]bool),
		jobCancelFuncs: make(map[string]context.CancelFunc),
		workspace:     workspace,
	}, nil
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

	// Load job history to get last run times
	jobHistory, err := r.store.GetJobHistory()
	if err != nil {
		r.Logger.Warn("failed_to_load_job_history", log.F("error", err))
	}

	// Build map of last run time per job
	lastRuns := make(map[string]time.Time)
	for _, run := range jobHistory {
		if _, exists := lastRuns[run.JobName]; !exists || run.StartedAt.After(lastRuns[run.JobName]) {
			lastRuns[run.JobName] = run.StartedAt
		}
	}

	// Parse schedules for each job (schedule is on agent), using last run time
	for jobName, schedule := range r.Agent.Jobs {
		lastRun := lastRuns[jobName]
		sched, err := scheduler.ParseWithLastRun(schedule, lastRun)
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

	r.Logger.Info("job_loaded", log.F("job", jobName), log.F("schedule", sched.Spec))

	// Initialize schedule - calculates first run time (immediately if never run)
	sched.NextRun()

	// Run immediately for @once
	if sched.IsOneShot() {
		r.executeJob(ctx, jobName, job)
		return
	}

	for {
		wait := sched.SleepUntil()
		if wait > 0 {
			nextRun := time.Now().Add(wait).Format(time.RFC3339)
			r.Logger.Info("next_run", log.F("job", jobName), log.F("at", nextRun))

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
			// Record precondition failure in job history so we don't loop infinitely
			now := time.Now()
			jobRun := storage.JobRun{
				ID:         uuid.New().String(),
				JobName:    jobName,
				StartedAt:  now,
				FinishedAt: now,
				Success:    false,
				ExitCode:   -1,
			}
			r.store.SaveJobRun(jobRun)
			sched.LastRun = now // Update schedule so next run is at interval

			r.mu.Lock()
			r.running[jobName] = false
			r.mu.Unlock()
			sched.NextRun() // Advance to next interval even on precondition failure
			// Sleep briefly before retrying
			time.Sleep(time.Second)
			continue
		}

		r.executeJob(ctx, jobName, job)

		r.mu.Lock()
		r.running[jobName] = false
		r.mu.Unlock()

		sched.LastRun = time.Now() // Update after successful run
		sched.NextRun()
	}
}

// checkPreconditions runs preconditions and returns true if all pass
func (r *Runner) checkPreconditions(job *config.Job) bool {
	if job == nil || len(job.Preconditions) == 0 {
		return true
	}

	for _, cmd := range job.Preconditions {
		r.Logger.Info("running_precondition", log.F("job", job.Name), log.F("command", cmd))
		output, err := exec.Command("sh", "-c", cmd).CombinedOutput()
		if err != nil {
			outputStr := strings.TrimSpace(string(output))
			exitCode := -1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			fields := []log.Field{log.F("job", job.Name), log.F("command", cmd), log.F("exit", exitCode)}
			if outputStr != "" {
				fields = append(fields, log.F("output", outputStr))
			}
			r.Logger.Info("precondition_failed", fields...)
			return false
		}
	}
	return true
}

// executeJob runs a single job execution in a subprocess for environment isolation
func (r *Runner) executeJob(ctx context.Context, jobName string, job *config.Job) {
	startTime := time.Now()
	r.Logger.Info("launching_subprocess", log.F("job", job.Name))

	// Ensure workspace exists
	if err := os.MkdirAll(r.workspace, 0755); err != nil {
		r.Logger.Error("create_workspace_failed", log.F("job", job.Name), log.F("workspace", r.workspace), log.F("error", err))
		return
	}

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
	// Set AGENT_NAME so mail tools know who we are
	env = append(env, "AGENT_NAME="+r.Agent.Name)

	// Build full system prompt and write to file (avoids command line limits)
	tools := Tools()
	home, _ := os.UserHomeDir()
	shareDir := filepath.Join(home, ".local", "share", "acoo")
	configDir := filepath.Join(home, ".config", "acoo")
	skillsDir := filepath.Join(configDir, "skills")
	skills := Skills(skillsDir)
	fullSystemPrompt := BuildSystemPrompt(systemPrompt, r.Agent.Name, tools, skills, r.workspace)
	systemPromptPath := filepath.Join(shareDir, r.Agent.Name, "system_prompt")
	if err := os.WriteFile(systemPromptPath, []byte(fullSystemPrompt), 0644); err != nil {
		r.Logger.Error("write_system_prompt_failed", log.F("job", job.Name), log.F("error", err))
		return
	}

	// Write task prompt to file
	taskPromptPath := filepath.Join(shareDir, r.Agent.Name, "task_prompt")
	if err := os.WriteFile(taskPromptPath, []byte(taskPrompt), 0644); err != nil {
		r.Logger.Error("write_task_prompt_failed", log.F("job", job.Name), log.F("error", err))
		return
	}

	// Build command with thinking budget if set
	cmdArgs := []string{"agent",
		"--system-prompt-path", systemPromptPath,
		"--task-prompt-path", taskPromptPath,
		"--model", job.Model,
		"--provider", job.Provider,
		"--agent-name", r.Agent.Name,
		"--job-name", jobName,
	}
	if thinkingBudget := job.GetThinkingBudget(); thinkingBudget > 0 {
		cmdArgs = append(cmdArgs, "--thinking-budget", fmt.Sprintf("%d", thinkingBudget))
	}

	// Run in subprocess for environment isolation with context support
	cmd := exec.Command(execPath, cmdArgs...)
	cmd.Env = env
	cmd.Dir = r.workspace
	// Don't pipe stdout/stderr - agent output is too verbose for server logs

	r.Logger.Info("subprocess_started", log.F("job", job.Name), log.F("executable", execPath), log.F("dir", r.workspace))

	// Start the process
	if err := cmd.Start(); err != nil {
		r.Logger.Error("subprocess_start_failed", log.F("job", job.Name), log.F("error", err))
		return
	}

	// Wait for process with context cancellation support
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var exitErr error
	select {
	case <-ctx.Done():
		r.Logger.Info("job_cancelled", log.F("job", job.Name))
		cmd.Process.Kill()
		cmd.Wait()
		return
	case exitErr = <-done:
		// Process completed
	}

	// Record job run
	finishTime := time.Now()
	exitCode := 0
	success := true
	if exitErr != nil {
		success = false
		if ee, ok := exitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
		r.Logger.Error("subprocess_failed", log.F("job", jobName), log.F("exit_code", exitCode))
	}

	// Save to history (truncate output to avoid huge files)
	// Note: output goes to stdout/stderr directly, so we just record success/failure
	jobRun := storage.JobRun{
		ID:         uuid.New().String(),
		JobName:    jobName,
		StartedAt:  startTime,
		FinishedAt: finishTime,
		Success:    success,
		ExitCode:   exitCode,
	}
	if err := r.store.SaveJobRun(jobRun); err != nil {
		r.Logger.Warn("failed_to_save_job_run", log.F("job", jobName), log.F("error", err))
	}

	if success {
		r.Logger.Info("job_complete", log.F("job", jobName))
	}
}

// truncate truncates a string to maxLen, adding ellipsis if truncated
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}