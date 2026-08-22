package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nalanj/acoo/internal/storage"
)

func inspectDB(cmd *cobra.Command, args []string) error {
	stateDir, _ := cmd.Flags().GetString("state-dir")
	if strings.HasPrefix(stateDir, "~/") {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, stateDir[2:])
	}

	out := cmd.OutOrStdout()

	if len(args) == 0 {
		// List all agents
		agents, err := storage.ListSessions(stateDir)
		if err != nil {
			return fmt.Errorf("listing sessions: %w", err)
		}
		if len(agents) == 0 {
			fmt.Fprintln(out, "No sessions found")
			return nil
		}
		fmt.Fprintf(out, "Sessions for %d agents:\n\n", len(agents))
		for _, agent := range agents {
			agentDir := filepath.Join(stateDir, agent)
			files, _ := storage.ListSessionFiles(agentDir)
			meta, _ := getMeta(stateDir, agent)
			count := 0
			if meta != nil {
				count = meta.SessionNumber
			}
			fmt.Fprintf(out, "  %-20s %d sessions (current: %d)\n", agent, len(files), count)
		}
		return nil
	}

	// Show details for specific agent
	agentName := args[0]
	agentDir := filepath.Join(stateDir, agentName)
	
	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		fmt.Fprintf(out, "No session found for agent: %s\n", agentName)
		return nil
	}

	fmt.Fprintf(out, "=== Agent: %s ===\n\n", agentName)

	// List session files
	files, err := storage.ListSessionFiles(agentDir)
	if err != nil {
		return fmt.Errorf("listing session files: %w", err)
	}

	fmt.Fprintf(out, "Sessions (%d):\n", len(files))
	for i, file := range files {
		fmt.Fprintf(out, "  Session %d: %s\n", i+1, filepath.Base(file))
	}
	fmt.Fprintln(out)

	// Show current session details
	store, err := storage.NewStore(stateDir, agentName)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer store.Close()

	// System prompt
	prompt, _ := store.GetSystemPrompt()
	if prompt != "" {
		content := prompt
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		fmt.Fprintf(out, "Current System Prompt:\n  %s\n\n", content)
	}

	// Messages from current session
	messages, _ := store.GetMessages()
	fmt.Fprintf(out, "Messages in current session (%d):\n", len(messages))
	for i, msg := range messages {
		content := msg.Content
		if len(content) > 80 {
			content = content[:80] + "..."
		}
		content = strings.ReplaceAll(content, "\n", " ")
		fmt.Fprintf(out, "  %3d. [%s] %s\n", i+1, msg.Role, content)
	}
	fmt.Fprintln(out)

	return nil
}

func getMeta(stateDir, agentName string) (*storage.Metadata, error) {
	store, err := storage.NewStore(stateDir, agentName)
	if err != nil {
		return nil, err
	}
	return store.GetMetadata()
}
