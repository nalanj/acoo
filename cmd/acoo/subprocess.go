package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"

	"github.com/nalanj/acoo/internal/agent"
	"github.com/nalanj/acoo/internal/provider"
)

// SubprocessRequest is the input for an agent subprocess
type SubprocessRequest struct {
	SystemPrompt string            `json:"system_prompt"`
	TaskPrompt   string            `json:"task_prompt"`
	Model        string            `json:"model"`
	Provider     string            `json:"provider"`
	Env          map[string]string `json:"env"`
}

// SubprocessResponse is the output from an agent subprocess
type SubprocessResponse struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// runAgentSubprocess handles the "agent" subcommand for subprocess mode
func runAgentSubprocess(cmd *cobra.Command, args []string) error {
	systemPrompt, _ := cmd.Flags().GetString("system-prompt")
	taskPrompt, _ := cmd.Flags().GetString("task-prompt")
	model, _ := cmd.Flags().GetString("model")
	providerName, _ := cmd.Flags().GetString("provider")
	thinkingBudget, _ := cmd.Flags().GetInt64("thinking-budget")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Create provider
	pf := provider.NewFactory()
	lm, err := pf.CreateProvider(providerName, model, "")
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	// Build agent options
	agentOptions := []fantasy.AgentOption{
		fantasy.WithSystemPrompt(systemPrompt),
		fantasy.WithTools(agent.Tools()...),
	}

	// Add thinking option if configured (for anthropic-compatible providers)
	if thinkingBudget > 0 {
		opts := anthropic.NewProviderOptions(&anthropic.ProviderOptions{
			Thinking: &anthropic.ThinkingProviderOption{
				BudgetTokens: thinkingBudget,
			},
		})
		agentOptions = append(agentOptions, fantasy.WithProviderOptions(opts))
	}

	// Create agent with tools
	agentInstance := fantasy.NewAgent(
		lm,
		agentOptions...,
	)

	// Build conversation
	messages := []fantasy.Message{}
	iteration := 0
	maxIterations := 100
	currentPrompt := taskPrompt

	for iteration < maxIterations {
		iteration++

		result, err := agentInstance.Generate(ctx, fantasy.AgentCall{
			Prompt:   currentPrompt,
			Messages: messages,
		})
		if err != nil {
			return fmt.Errorf("agent error: %w", err)
		}

		response := strings.TrimSpace(result.Response.Content.Text())
		fmt.Println(response)

		// Check for done
		if isDone(response) {
			return nil
		}

		// Add to messages
		messages = append(messages, fantasy.Message{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: response},
			},
		})

		currentPrompt = "Continue. When finished, include '<<<<<DONE>>>>>' on its own line."
	}

	return fmt.Errorf("max iterations reached")
}

// isDone checks if the response indicates the agent is done
func isDone(response string) bool {
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if line == "<<<<<DONE>>>>>" {
			return true
		}
	}
	return false
}
