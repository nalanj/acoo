package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	agentsDir string
	jobsDir   string
	verbose   bool
)

// defaultStateDir returns the default state directory
func defaultStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.local/share/acoo"
	}
	return filepath.Join(home, ".local", "share", "acoo")
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

// BuildCommands creates all cobra commands
func BuildCommands() *cobra.Command {
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
		Short: "Test an agent job",
		Long: `Test an agent job to see what it would do.

Shows the system prompt, job prompt, and execution details.

Example:
  acoo test code-reviewer review-changes`,
		Args: cobra.ExactArgs(2),
		RunE:  testAgent,
	}

	startCmd := &cobra.Command{
		Use:   "start [agent-name]",
		Short: "Start agents",
		Long: `Start all agents or a specific agent. Without arguments, runs all agents.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return runAllAgents()
			}
			return runAgentOnce(args[0])
		},
	}

	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Run agent in subprocess",
		RunE:  runAgentSubprocess,
	}
	agentCmd.Flags().String("system-prompt", "", "System prompt")
	agentCmd.Flags().String("system-prompt-path", "", "Path to system prompt file")
	agentCmd.Flags().String("task-prompt", "", "Task prompt")
	agentCmd.Flags().String("task-prompt-path", "", "Path to task prompt file")
	agentCmd.Flags().String("model", "", "Model")
	agentCmd.Flags().String("provider", "", "Provider")
	agentCmd.Flags().Int64("thinking-budget", 0, "Thinking budget in tokens (0 = disabled)")
	agentCmd.Flags().String("agent-name", "", "Agent name (for session persistence)")
	agentCmd.Flags().String("job-name", "", "Job name (for logging)")
	agentCmd.Flags().String("state-dir", "~/.local/share/acoo", "State directory")

	providersCmd := &cobra.Command{
		Use:   "providers",
		Short: "List available LLM providers",
		RunE:  listProviders,
	}

	dbCmd := &cobra.Command{
		Use:   "db [agent-name]",
		Short: "Inspect session storage",
		RunE:  inspectDB,
	}
	dbCmd.Flags().String("state-dir", "~/.local/share/acoo", "State directory")

	root := &cobra.Command{
		Use:          "acoo",
		Short:        "Agent Command Orchestrator",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAllAgents()
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
	root.AddCommand(dbCmd)

	return root
}
