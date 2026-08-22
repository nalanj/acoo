package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"

	"github.com/nalanj/acoo/internal/agent"
	"github.com/nalanj/acoo/internal/log"
	"github.com/nalanj/acoo/internal/provider"
	"github.com/nalanj/acoo/internal/storage"
)

const maxCompactionRetries = 1 // Max compaction attempts before giving up

// runAgentSubprocess handles the "agent" subcommand for subprocess mode
func runAgentSubprocess(cmd *cobra.Command, args []string) error {
	systemPromptPath, _ := cmd.Flags().GetString("system-prompt-path")
	systemPrompt := ""
	if systemPromptPath != "" {
		// Read system prompt from file
		data, err := os.ReadFile(systemPromptPath)
		if err != nil {
			return fmt.Errorf("reading system prompt from %s: %w", systemPromptPath, err)
		}
		systemPrompt = string(data)
	} else {
		// Fall back to command line flag
		systemPrompt, _ = cmd.Flags().GetString("system-prompt")
	}

	taskPrompt, _ := cmd.Flags().GetString("task-prompt")
	model, _ := cmd.Flags().GetString("model")
	providerName, _ := cmd.Flags().GetString("provider")
	thinkingBudget, _ := cmd.Flags().GetInt64("thinking-budget")
	agentName, _ := cmd.Flags().GetString("agent-name")
	stateDir, _ := cmd.Flags().GetString("state-dir")

	// Expand ~ in state dir
	if strings.HasPrefix(stateDir, "~/") {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, stateDir[2:])
	}

	// Create agent-specific store
	store, err := storage.NewStore(stateDir, agentName)
	if err != nil {
		return fmt.Errorf("opening storage: %w", err)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Create provider first (needed for compaction)
	pf := provider.NewFactory()
	lm, err := pf.CreateProvider(providerName, model, "")
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}
	_ = lm // Used for compaction later

	// systemPrompt is already the full prompt (read from file)
	// Save system prompt only if different from current (and current is not empty)
	existingPrompt, _ := store.GetSystemPrompt()
	if existingPrompt != "" && existingPrompt != systemPrompt {
		store.SaveSystemPrompt(systemPrompt)
	}

	// Build agent options (tools are still needed for execution)
	tools := agent.Tools()
	agentOptions := []fantasy.AgentOption{
		fantasy.WithSystemPrompt(systemPrompt),
		fantasy.WithTools(tools...),
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
	currentPrompt := taskPrompt + "\n\nWhen finished, respond with '<<<<<DONE>>>>>' on its own line."
	compactionRetries := 0

	for iteration < maxIterations {
		iteration++
		if iteration == 1 {
			jobName, _ := cmd.Flags().GetString("job-name")
			log.System().Info("job_started", log.F("job", jobName), log.F("model", model))
		}

		// Save user message
		store.AddMessage(storage.Message{
			Role:      "user",
			Content:   currentPrompt,
			Timestamp: time.Now(),
		})

		result, err := agentInstance.Generate(ctx, fantasy.AgentCall{
			Prompt:   currentPrompt,
			Messages: messages,
		})
		if err != nil {
			// Check for context too large error
			var provErr *fantasy.ProviderError
			if errors.As(err, &provErr) && provErr.IsContextTooLarge() {
				if compactionRetries < maxCompactionRetries {
					slog := log.System()
					slog.Info("compaction_start", log.F("reason", "context_too_large"))

					// Get message count for summary
					meta, _ := store.GetMetadata()
					msgCount := 0
					if meta != nil {
						msgCount = meta.MessageCount
					}

					// Generate summary and compact
					summary := generateSummary(ctx, lm, systemPrompt, msgCount)
					_, err := store.CompactStart(summary)
					if err != nil {
						slog.Error("compaction_failed", log.F("error", err))
						return fmt.Errorf("context overflow after compaction: %w", err)
					}

					compactionRetries++
					messages = nil // Clear messages, start fresh
					continue // Retry with empty context
				}
				return fmt.Errorf("context overflow: %w", err)
			}
			return fmt.Errorf("agent error: %w", err)
		}

		response := strings.TrimSpace(result.Response.Content.Text())
		fmt.Println(response)

		// Save messages from steps (includes tool calls and results)
		for _, step := range result.Steps {
			for _, msg := range step.Messages {
				var role string
				var content string
				var toolName string

				switch msg.Role {
				case fantasy.MessageRoleUser:
					role = "user"
					content = serializeMessageContent(msg.Content)
				case fantasy.MessageRoleAssistant:
					role = "assistant"
					content = serializeMessageContent(msg.Content)
				case fantasy.MessageRoleTool:
					role = "tool"
					content = serializeMessageContent(msg.Content)
					for _, part := range msg.Content {
						if tc, ok := fantasy.AsMessagePart[fantasy.ToolCallPart](part); ok {
							toolName = tc.ToolName
							break
						}
					}
				default:
					continue
				}

				store.AddMessage(storage.Message{
					Role:      role,
					ToolName:  toolName,
					Content:   content,
					Timestamp: time.Now(),
				})
			}
		}

		// Check for done
		if isDone(response) {
			return nil
		}

		// Add to messages
		messages = append(messages, fantasy.Message{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				&fantasy.TextPart{Text: response},
			},
		})

		currentPrompt = "Continue. When finished, respond with '<<<<<DONE>>>>>' on its own line."
	}

	return fmt.Errorf("max iterations reached")
}

// serializeMessageContent serializes message content to a string
func serializeMessageContent(parts []fantasy.MessagePart) string {
	var result strings.Builder
	for _, part := range parts {
		if tp, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok {
			result.WriteString(tp.Text)
			continue
		}
		if tc, ok := fantasy.AsMessagePart[fantasy.ToolCallPart](part); ok {
			result.WriteString("\n[TOOL_CALL: ")
			result.WriteString(tc.ToolName)
			result.WriteString("]\n")
			result.WriteString(tc.Input)
			result.WriteString("\n")
			continue
		}
		if tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part); ok {
			result.WriteString("\n[TOOL_RESULT: ")
			result.WriteString(tr.ToolCallID)
			result.WriteString("]\n")
			result.WriteString(fmt.Sprintf("%v", tr.Output))
			result.WriteString("\n")
			continue
		}
	}
	return strings.TrimSpace(result.String())
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// generateSummary uses the LLM to summarize prior conversation
func generateSummary(ctx context.Context, lm fantasy.LanguageModel, systemPrompt string, messageCount int) string {
	summaryPrompt := fmt.Sprintf(`You are summarizing a conversation. The conversation had %d messages. The system prompt is: %s. Provide a brief 1-2 sentence summary of what this conversation covered.`, messageCount, systemPrompt)

	response, err := lm.Generate(ctx, fantasy.Call{
		Prompt: []fantasy.Message{
			{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{&fantasy.TextPart{Text: summaryPrompt}}},
		},
	})
	if err != nil {
		log.System().Error("summary_failed", log.F("error", err))
		return "Conversation summary unavailable"
	}

	summary := strings.TrimSpace(response.Content.Text())
	if len(summary) > 200 {
		summary = summary[:200] + "..."
	}
	return summary
}
