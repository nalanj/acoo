package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"

	"github.com/nalanj/acoo/internal/agent"
	"github.com/nalanj/acoo/internal/provider"
	"github.com/nalanj/acoo/internal/storage"
)

const maxMessagesBeforeCompaction = 20 // Trigger compaction after this many messages

// runAgentSubprocess handles the "agent" subcommand for subprocess mode
func runAgentSubprocess(cmd *cobra.Command, args []string) error {
	systemPrompt, _ := cmd.Flags().GetString("system-prompt")
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

	// Check if session needs compaction at startup
	meta, _ := store.GetMetadata()
	if meta != nil && meta.MessageCount >= maxMessagesBeforeCompaction {
		log.Printf("[compaction] Session has %d messages, compacting before new work", meta.MessageCount)
		
		// Generate summary using the LLM
		summary := generateSummary(ctx, lm, systemPrompt, meta.MessageCount)
		
		_, err := store.CompactStart(summary)
		if err != nil {
			log.Printf("[compaction] Warning: %v", err)
		}
	}

	// Save system prompt only if different from current (and current is not empty)
	existingPrompt, _ := store.GetSystemPrompt()
	if existingPrompt != "" && existingPrompt != systemPrompt {
		store.SaveSystemPrompt(systemPrompt)
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
	sessionMsgCount := 0

	for iteration < maxIterations {
		iteration++

		// Save user message
		store.AddMessage(storage.Message{
			Role:      "user",
			Content:   currentPrompt,
			Timestamp: time.Now(),
		})
		sessionMsgCount++

		result, err := agentInstance.Generate(ctx, fantasy.AgentCall{
			Prompt:   currentPrompt,
			Messages: messages,
		})
		if err != nil {
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
				sessionMsgCount++
			}
		}

		// Check for compaction
		messages, err = checkCompaction(ctx, store, agentInstance, messages, sessionMsgCount)
		if err != nil {
			log.Printf("[compaction] Warning: %v", err)
		}
		sessionMsgCount = len(messages)

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

// checkCompaction checks if we need to compact and does so if necessary
func checkCompaction(ctx context.Context, store *storage.Store, agentInstance fantasy.Agent, messages []fantasy.Message, sessionMsgCount int) ([]fantasy.Message, error) {
	if sessionMsgCount < maxMessagesBeforeCompaction {
		return messages, nil
	}

	log.Printf("[compaction] Starting compaction (session has %d messages)", sessionMsgCount)

	// Build conversation text for summarization
	var conversationText strings.Builder
	for _, msg := range messages {
		role := strings.ToUpper(string(msg.Role))
		content := serializeMessageContent(msg.Content)
		conversationText.WriteString(fmt.Sprintf("[%s] %s\n", role, content))
	}

	// Generate summary using the agent
	summaryPrompt := fmt.Sprintf(`Summarize the following conversation concisely. Capture the key topics discussed, any conclusions reached, and important context needed to continue naturally.

Conversation:
%s

Provide a 1-2 sentence summary:`, conversationText.String())

	result, err := agentInstance.Generate(ctx, fantasy.AgentCall{
		Prompt:   summaryPrompt,
		Messages: []fantasy.Message{messages[0]}, // Just system prompt
	})
	if err != nil {
		log.Printf("[compaction] Failed to generate summary: %v", err)
		return messages, nil // Don't compact on error
	}

	summary := strings.TrimSpace(result.Response.Content.Text())
	log.Printf("[compaction] Summary: %s", truncate(summary, 100))

	// Start new session with summary
	_, err = store.CompactStart(summary)
	if err != nil {
		log.Printf("[compaction] Failed to start new session: %v", err)
		return messages, nil
	}

	// Return only system + last few messages
	if len(messages) > 4 {
		return messages[len(messages)-4:], nil
	}
	return messages, nil
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
		log.Printf("[compaction] Failed to generate summary: %v", err)
		return "Conversation summary unavailable"
	}

	summary := strings.TrimSpace(response.Content.Text())
	if len(summary) > 200 {
		summary = summary[:200] + "..."
	}
	return summary
}
