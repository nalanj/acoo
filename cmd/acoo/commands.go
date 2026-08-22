package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/nalanj/acoo/internal/agent"
	"github.com/nalanj/acoo/internal/config"
	"github.com/nalanj/acoo/internal/log"
	"github.com/nalanj/acoo/internal/provider"
)

var (
	styleOK   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleErr  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleBold = lipgloss.NewStyle().Bold(true)
	styleDim  = lipgloss.NewStyle().Faint(true)
	styleCyan = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
)

// runAllAgents is the default command that runs all agents
func runAllAgents() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logger := log.System()
	manager := NewAgentManager(agentsDir, jobsDir, logger)

	if err := manager.Start(ctx); err != nil {
		return fmt.Errorf("starting agent manager: %w", err)
	}

	logger.Info("started", log.F("agents", manager.AgentCount()))

	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	<-ctx.Done()
	manager.Stop()

	return nil
}

func runAgentOnce(name string) error {
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

	logger := log.Agent(target.Name)
	runner := agent.NewRunner(target, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	runner.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	runner.Stop()

	return nil
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

func validateAgents(cmd *cobra.Command, args []string) error {
	agents, jobs, err := config.LoadAll(agentsDir, jobsDir)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	validationErrors := 0

	for _, a := range agents {
		if a.Name == "" {
			fmt.Fprintf(out, "✗ %s: missing name\n", a.SourceFile)
			validationErrors++
		}
		if len(a.Jobs) == 0 {
			fmt.Fprintf(out, "✗ %s: no jobs defined\n", a.Name)
			validationErrors++
		}

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
	agentName, jobName := args[0], args[1]

	path := filepath.Join(agentsDir, agentName+".md")
	agentConfig, err := config.LoadAgentFile(path)
	if err != nil {
		return fmt.Errorf("loading agent %s: %w", agentName, err)
	}

	jobs, err := config.LoadJobs(jobsDir)
	if err != nil {
		return fmt.Errorf("loading jobs: %w", err)
	}
	job, ok := jobs[jobName]
	if !ok {
		return fmt.Errorf("job '%s' not found", jobName)
	}

	schedule, configured := agentConfig.Jobs[jobName]

	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "%s Running %s/%s\n", styleCyan.Render("→"), styleBold.Render(agentName), styleBold.Render(jobName))

	if configured {
		fmt.Fprintf(out, "%s %s · %s\n\n", styleDim.Render(schedule), job.Provider, job.Model)
	} else {
		fmt.Fprintf(out, "%s · %s %s\n\n", job.Provider, job.Model, styleDim.Render("(not configured)"))
	}

	// Run preconditions
	if len(job.Preconditions) > 0 {
		fmt.Fprintf(out, "%s\n", styleDim.Render("Checking preconditions..."))
		for _, prec := range job.Preconditions {
			if _, err := exec.Command("sh", "-c", prec).CombinedOutput(); err != nil {
				fmt.Fprintf(out, "%s %s\n", styleErr.Render("✗"), prec)
				return fmt.Errorf("precondition failed: %s", prec)
			}
		}
		fmt.Fprintf(out, "%s\n", styleOK.Render("✓ Preconditions passed"))
	}

	// Build environment
	env := os.Environ()
	for k, v := range agentConfig.Env {
		env = append(env, k+"="+v)
	}
	for k, v := range job.Env {
		env = append(env, k+"="+v)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	systemPrompt := agentConfig.Body
	if systemPrompt == "" {
		systemPrompt = "You are a helpful AI assistant."
	}

	cmdArgs := []string{"agent",
		"--system-prompt", systemPrompt,
		"--task-prompt", job.Body,
		"--model", job.Model,
		"--provider", job.Provider,
		"--agent-name", agentName,
	}
	if thinkingBudget := job.GetThinkingBudget(); thinkingBudget > 0 {
		cmdArgs = append(cmdArgs, "--thinking-budget", fmt.Sprintf("%d", thinkingBudget))
	}

	fmt.Fprintf(out, "\n%s\n\n", styleDim.Render("─────────────────────────────────────────────────────"))

	proc := exec.Command(execPath, cmdArgs...)
	proc.Env = env
	stdout, err := proc.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderr, err := proc.StderrPipe()
	if err != nil {
		return fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := proc.Start(); err != nil {
		return fmt.Errorf("starting process: %w", err)
	}

	// Copy stdout, filtering out DONE marker
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == DoneMarker {
				continue
			}
			fmt.Fprintln(out, line)
		}
	}()

	// Copy stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Fprintln(out, scanner.Text())
		}
	}()

	if err := proc.Wait(); err != nil {
		return fmt.Errorf("job execution: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s\n", styleOK.Render("✓ Done"))

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
