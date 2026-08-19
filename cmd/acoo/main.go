package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/nalanj/acoo/internal/agent"
	"github.com/nalanj/acoo/internal/config"
	"github.com/nalanj/acoo/internal/provider"
)

var (
	agentsDir string
	jobsDir   string
	verbose   bool
)

func main() {
	// Build all commands
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all agents and their jobs",
		RunE:  listAgents,
	}

	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate agent and job configs",
		RunE:  validateAgents,
	}

	testCmd := &cobra.Command{
		Use:   "test <agent-name> <job-name>",
		Short: "Test an agent job (dry run)",
		Long: `Test an agent job to see what it would do.

Shows the system prompt, job prompt, and execution details without
actually calling the LLM.

Example:
  acoo test code-reviewer review-changes`,
		Args: cobra.ExactArgs(2),
		RunE: testAgent,
	}

	startCmd := &cobra.Command{
		Use:   "start [agent-name]",
		Short: "Start agents",
		Long:  `Start all agents or a specific agent. Without arguments, runs all agents.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return runAllAgents(cmd, args)
			}
			return runAgentOnce(cmd, args[0])
		},
	}

	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Run agent in subprocess",
		RunE:  runAgentSubprocess,
	}
	agentCmd.Flags().String("system-prompt", "", "System prompt")
	agentCmd.Flags().String("task-prompt", "", "Task prompt")
	agentCmd.Flags().String("model", "", "Model")
	agentCmd.Flags().String("provider", "", "Provider")
	agentCmd.Flags().Int64("thinking-budget", 0, "Thinking budget in tokens (0 = disabled)")

	providersCmd := &cobra.Command{
		Use:   "providers",
		Short: "List available LLM providers",
		RunE:  listProviders,
	}

	// Root command - use RunE instead of Run
	root := &cobra.Command{
		Use:          "acoo",
		Short:        "Agent Command Orchestrator",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check if this is being called directly (no subcommand)
			if len(args) == 0 {
				return runAllAgents(cmd, args)
			}
			// If we get here with args, show help
			cmd.Help()
			return nil
		},
	}

	root.PersistentFlags().StringVar(&agentsDir, "agents-dir", defaultAgentsDir(), "Directory containing agent configs")
	root.PersistentFlags().StringVar(&jobsDir, "jobs-dir", defaultJobsDir(), "Directory containing job configs")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	root.AddCommand(listCmd)
	root.AddCommand(validateCmd)
	root.AddCommand(testCmd)
	root.AddCommand(startCmd)
	root.AddCommand(agentCmd)
	root.AddCommand(providersCmd)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// defaultAgentsDir returns the default agents directory
func defaultAgentsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.config/acoo/agents"
	}
	return filepath.Join(home, ".config", "acoo", "agents")
}

// defaultJobsDir returns the default jobs directory
func defaultJobsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.config/acoo/jobs"
	}
	return filepath.Join(home, ".config", "acoo", "jobs")
}

// runAllAgents is the default command that runs all agents
func runAllAgents(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create logger
	logger := log.New(os.Stdout, "", log.LstdFlags)

	// Create agent manager
	manager := NewAgentManager(agentsDir, jobsDir, logger)

	// Start manager
	if err := manager.Start(ctx); err != nil {
		return fmt.Errorf("starting agent manager: %w", err)
	}

	logger.Printf("Started with %d agents", manager.AgentCount())

	// Handle signals
	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Stop manager
	manager.Stop()

	return nil
}

// AgentManager manages all agent runners
type AgentManager struct {
	agentsDir string
	jobsDir   string
	logger    *log.Logger
	watcher   *config.Watcher
	runners   map[string]*agent.Runner // agent name -> runner

	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	changed   chan string
}

// NewAgentManager creates a new agent manager
func NewAgentManager(agentsDir, jobsDir string, logger *log.Logger) *AgentManager {
	return &AgentManager{
		agentsDir: agentsDir,
		jobsDir:   jobsDir,
		logger:    logger,
		runners:   make(map[string]*agent.Runner),
	}
}

// Start starts the agent manager
func (m *AgentManager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()

	// Load and start all agents
	if err := m.reloadAgents(); err != nil {
		return err
	}

	// Set up file watcher for hot reloading
	watcher, err := config.NewWatcher(m.agentsDir, m.jobsDir)
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	m.watcher = watcher

	// Handle file changes
	watcher.OnChange(func(changed []string) {
		m.mu.Lock()
		defer m.mu.Unlock()

		for _, c := range changed {
			parts := strings.SplitN(c, ":", 2)
			if len(parts) != 2 {
				continue
			}
			action := parts[0]
			path := parts[1]

			dir := filepath.Dir(path)
			name := filepath.Base(dir)

			switch action {
			case "added", "modified":
				m.logger.Printf("Reloading agent: %s", name)
				m.reloadAgent(name)
			case "removed":
				m.logger.Printf("Stopping agent: %s", name)
				m.stopAgent(name)
			}
		}
	})

	watcher.Watch(m.logger)

	return nil
}

// reloadAgents loads and starts all agents
func (m *AgentManager) reloadAgents() error {
	agents, _, err := config.LoadAll(m.agentsDir, m.jobsDir)
	if err != nil {
		return fmt.Errorf("loading agents: %w", err)
	}

	for _, a := range agents {
		m.startAgent(a)
	}

	return nil
}

// reloadAgent reloads a single agent
func (m *AgentManager) reloadAgent(name string) {
	// Stop existing runner if any
	m.stopAgent(name)

	// Load the agent
	path := filepath.Join(m.agentsDir, name+".md")
	agentConfig, err := config.LoadAgentFile(path)
	if err != nil {
		m.logger.Printf("Failed to reload agent %s: %v", name, err)
		return
	}

	// Load jobs
	jobs, err := config.LoadJobs(m.jobsDir)
	if err != nil {
		m.logger.Printf("Failed to load jobs: %v", err)
		return
	}

	// Link jobs
	agentConfig.JobsMap = make(map[string]*config.Job)
	for _, jobName := range agentConfig.Jobs {
		if job, ok := jobs[jobName]; ok {
			agentConfig.JobsMap[jobName] = job
		}
	}

	m.startAgent(agentConfig)
}

// startAgent starts an agent runner
func (m *AgentManager) startAgent(a *config.Agent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.runners[a.Name]; exists {
		return // Already running
	}

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", a.Name), 0)
	runner := agent.NewRunner(a, logger)

	m.runners[a.Name] = runner
	runner.Start(m.ctx)
}

// stopAgent stops an agent runner
func (m *AgentManager) stopAgent(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	runner, exists := m.runners[name]
	if !exists {
		return
	}

	runner.Stop()
	delete(m.runners, name)
}

// Stop stops the agent manager
func (m *AgentManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.watcher != nil {
		m.watcher.Close()
	}
	if m.cancel != nil {
		m.cancel()
	}

	for name, runner := range m.runners {
		runner.Stop()
		delete(m.runners, name)
	}
}

// AgentCount returns the number of running agents
func (m *AgentManager) AgentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runners)
}

func listAgents(cmd *cobra.Command, args []string) error {
	infos, err := config.ListAgents(agentsDir, jobsDir)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	if len(infos) == 0 {
		fmt.Fprintln(out, "No agents found")
		return nil
	}

	for _, info := range infos {
		fmt.Fprintf(out, "Agent: %s\n", info.Name)
		fmt.Fprintf(out, "  Source: %s\n", info.Source)
		if len(info.Jobs) > 0 {
			fmt.Fprintln(out, "  Jobs:")
			for _, job := range info.Jobs {
				fmt.Fprintf(out, "    - %s\n", job)
			}
		}
		fmt.Fprintln(out)
	}

	return nil
}

func runAgentOnce(cmd *cobra.Command, name string) error {
	agents, _, err := config.LoadAll(agentsDir, jobsDir)
	if err != nil {
		return err
	}

	var target *config.Agent
	for _, a := range agents {
		if a.Name == name {
			target = a
			break
		}
	}

	if target == nil {
		return fmt.Errorf("agent not found: %s", name)
	}

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", target.Name), 0)
	runner := agent.NewRunner(target, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	runner.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	runner.Stop()

	return nil
}

func validateAgents(cmd *cobra.Command, args []string) error {
	agents, jobs, err := config.LoadAll(agentsDir, jobsDir)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	validationErrors := 0

	// Validate agents
	for _, a := range agents {
		if a.Name == "" {
			fmt.Fprintf(out, "✗ %s: missing name\n", a.SourceFile)
			validationErrors++
		}
		if len(a.Jobs) == 0 {
			fmt.Fprintf(out, "✗ %s: no jobs defined\n", a.Name)
			validationErrors++
		}

		// Validate job references exist and have required fields
		for jobName, schedule := range a.Jobs {
			job, ok := jobs[jobName]
			if !ok {
				fmt.Fprintf(out, "✗ %s: references unknown job %s\n", a.Name, jobName)
				validationErrors++
				continue
			}
			if schedule == "" {
				fmt.Fprintf(out, "✗ %s.%s: missing schedule\n", a.Name, jobName)
				validationErrors++
			}
			if job.Provider == "" {
				fmt.Fprintf(out, "✗ %s.%s: missing provider\n", a.Name, jobName)
				validationErrors++
			}
			if job.Model == "" {
				fmt.Fprintf(out, "✗ %s.%s: missing model\n", a.Name, jobName)
				validationErrors++
			}
		}

		if validationErrors == 0 {
			fmt.Fprintf(out, "✓ %s\n", a.Name)
		}
	}

	if validationErrors > 0 {
		return fmt.Errorf("%d validation error(s)", validationErrors)
	}

	fmt.Fprintf(out, "\n%d agent(s) valid\n", len(agents))
	return nil
}

func testAgent(cmd *cobra.Command, args []string) error {
	agentName := args[0]
	jobName := args[1]

	// Load agent
	path := filepath.Join(agentsDir, agentName+".md")
	agentConfig, err := config.LoadAgentFile(path)
	if err != nil {
		return fmt.Errorf("loading agent %s: %w", agentName, err)
	}

	// Check if agent references this job
	schedule, found := agentConfig.Jobs[jobName]
	if !found {
		return fmt.Errorf("agent '%s' does not reference job '%s'", agentName, jobName)
	}

	// Load the job
	jobs, err := config.LoadJobs(jobsDir)
	if err != nil {
		return fmt.Errorf("loading jobs: %w", err)
	}
	job, ok := jobs[jobName]
	if !ok {
		return fmt.Errorf("job '%s' not found", jobName)
	}

	out := cmd.OutOrStdout()
	sep := strings.Repeat("=", 60)
	dash := strings.Repeat("-", 30)

	// Run preconditions
	if len(job.Preconditions) > 0 {
		fmt.Fprintln(out, sep)
		fmt.Fprintln(out, "# PRECONDITIONS")
		fmt.Fprintln(out, dash)
		for _, prec := range job.Preconditions {
			fmt.Fprintf(out, "Running: %s\n", prec)
			execOut, err := exec.Command("sh", "-c", prec).CombinedOutput()
			if err != nil {
				fmt.Fprintf(out, "FAILED: %s\n", string(execOut))
				return fmt.Errorf("precondition failed: %s", prec)
			}
			fmt.Fprintf(out, "OK\n")
		}
		fmt.Fprintln(out)
	}

	// Show config summary
	fmt.Fprintln(out, sep)
	fmt.Fprintf(out, "Agent: %s\n", agentConfig.Name)
	fmt.Fprintf(out, "Job: %s\n", job.Name)
	fmt.Fprintf(out, "Schedule: %s\n", schedule)
	fmt.Fprintf(out, "Provider: %s, Model: %s\n", job.Provider, job.Model)
	fmt.Fprintln(out, sep)

	// Build environment
	env := os.Environ()
	for k, v := range agentConfig.Env {
		env = append(env, k+"="+v)
	}
	for k, v := range job.Env {
		env = append(env, k+"="+v)
	}

	// Find the acoo binary path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	// Build command
	systemPrompt := agentConfig.Body
	if systemPrompt == "" {
		systemPrompt = "You are a helpful AI assistant."
	}

	cmdArgs := []string{"agent",
		"--system-prompt", systemPrompt,
		"--task-prompt", job.Body,
		"--model", job.Model,
		"--provider", job.Provider,
	}
	if thinkingBudget := job.GetThinkingBudget(); thinkingBudget > 0 {
		cmdArgs = append(cmdArgs, "--thinking-budget", fmt.Sprintf("%d", thinkingBudget))
	}

	fmt.Fprintln(out, "# RUNNING")
	fmt.Fprintln(out, dash)

	// Execute
	proc := exec.Command(execPath, cmdArgs...)
	proc.Env = env
	proc.Stdout = out
	proc.Stderr = out
	if err := proc.Run(); err != nil {
		return fmt.Errorf("job execution: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, sep)
	fmt.Fprintln(out, "Done.")

	return nil
}

func listProviders(cmd *cobra.Command, args []string) error {
	pf := provider.NewFactory()
	infos := pf.ListProviders()

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Available providers:")
	fmt.Fprintln(out)

	for _, info := range infos {
		fmt.Fprintf(out, "%-15s %s (type: %s)\n", info.ID, info.Name, info.Type)
		if len(info.Models) > 0 {
			fmt.Fprintf(out, "  Models: %s\n", strings.Join(info.Models, ", "))
		}
		fmt.Fprintln(out)
	}

	return nil
}
