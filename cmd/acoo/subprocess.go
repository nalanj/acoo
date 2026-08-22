package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// jsonlog is a JSON logger for subprocess output to stdout
var jsonlog = &jsonLogger{}

type jsonLogger struct {
	mu sync.Mutex
}

func (j *jsonLogger) log(level, message string, fields []log.Field) {
	j.mu.Lock()
	defer j.mu.Unlock()

	entry := struct {
		Timestamp string      `json:"timestamp"`
		Scope     string      `json:"scope"`
		Level     string      `json:"level"`
		Message   string      `json:"message"`
		Fields    []log.Field `json:"fields,omitempty"`
	}{
		Timestamp: time.Now().Format(time.RFC3339),
		Scope:     "system",
		Level:     level,
		Message:   message,
		Fields:    fields,
	}
	data, _ := json.Marshal(entry)
	fmt.Fprintln(os.Stdout, string(data))
}

func (j *jsonLogger) Info(message string, fields ...log.Field) { j.log("info", message, fields) }
func (j *jsonLogger) Warn(message string, fields ...log.Field)  { j.log("warn", message, fields) }
func (j *jsonLogger) Error(message string, fields ...log.Field) { j.log("error", message, fields) }

// runAgentSubprocess handles the "agent" subcommand for subprocess mode
func runAgentSubprocess(cmd *cobra.Command, args []string) error {
	systemPromptPath, _ := cmd.Flags().GetString("system-prompt-path")
	systemPrompt := ""
	if systemPromptPath != "" {
		data, err := os.ReadFile(systemPromptPath)
		if err != nil {
			jsonlog.Error("read_system_prompt_failed", log.F("path", systemPromptPath), log.F("error", err))
			return fmt.Errorf("reading system prompt from %s: %w", systemPromptPath, err)
		}
		systemPrompt = string(data)
	} else {
		systemPrompt, _ = cmd.Flags().GetString("system-prompt")
	}

	taskPromptPath, _ := cmd.Flags().GetString("task-prompt-path")
	taskPrompt := ""
	if taskPromptPath != "" {
		data, err := os.ReadFile(taskPromptPath)
		if err != nil {
			jsonlog.Error("read_task_prompt_failed", log.F("path", taskPromptPath), log.F("error", err))
			return fmt.Errorf("reading task prompt from %s: %w", taskPromptPath, err)
		}
		taskPrompt = string(data)
	} else {
		taskPrompt, _ = cmd.Flags().GetString("task-prompt")
	}

	model, _ := cmd.Flags().GetString("model")
	providerName, _ := cmd.Flags().GetString("provider")
	thinkingBudget, _ := cmd.Flags().GetInt64("thinking-budget")
	agentName, _ := cmd.Flags().GetString("agent-name")
	jobName, _ := cmd.Flags().GetString("job-name")
	stateDir, _ := cmd.Flags().GetString("state-dir")

	if strings.HasPrefix(stateDir, "~/") {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, stateDir[2:])
	}

	store, err := storage.NewStore(stateDir, agentName)
	if err != nil {
		jsonlog.Error("create_store_failed", log.F("error", err))
		return fmt.Errorf("opening storage: %w", err)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	pf := provider.NewFactory()
	lm, err := pf.CreateProvider(providerName, model, "")
	if err != nil {
		jsonlog.Error("create_provider_failed", log.F("error", err))
		return fmt.Errorf("creating provider: %w", err)
	}

	tools := agent.Tools()
	agentOptions := []fantasy.AgentOption{
		fantasy.WithSystemPrompt(systemPrompt),
		fantasy.WithTools(tools...),
	}

	if thinkingBudget > 0 {
		opts := anthropic.NewProviderOptions(&anthropic.ProviderOptions{
			Thinking: &anthropic.ThinkingProviderOption{
				BudgetTokens: thinkingBudget,
			},
		})
		agentOptions = append(agentOptions, fantasy.WithProviderOptions(opts))
	}

	agentInstance := fantasy.NewAgent(lm, agentOptions...)

	jsonlog.Info("job_started", log.F("job", jobName), log.F("model", model))

	messages := []fantasy.Message{}
	iteration := 0
	maxIterations := 100
	currentPrompt := taskPrompt + "\n\nWhen finished, respond with '<<<<<DONE>>>>>' on its own line."
	compactionRetries := 0

	for iteration < maxIterations {
		iteration++

		store.AddMessage(storage.Message{
			Role:      "user",
			Content:   currentPrompt,
			Timestamp: time.Now(),
		})

		// Track text for this turn
		var textBuilder strings.Builder

		result, err := agentInstance.Stream(ctx, fantasy.AgentStreamCall{
			Prompt:   currentPrompt,
			Messages: messages,
			OnTextDelta: func(id, text string) error {
				fmt.Print(text)
				textBuilder.WriteString(text)
				return nil
			},
			OnReasoningDelta: func(id, text string) error {
				// Could print thinking with a different marker if wanted
				return nil
			},
			OnStepFinish: func(step fantasy.StepResult) error {
				// Save assistant message
				text := textBuilder.String()
				if text != "" {
					store.AddMessage(storage.Message{
						Role:      "assistant",
						Content:   text,
						Timestamp: time.Now(),
					})
				}

				// Save tool messages from step
				for _, msg := range step.Messages {
					var role, content string
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
				return nil
			},
		})

		if err != nil {
			var provErr *fantasy.ProviderError
			if errors.As(err, &provErr) && provErr.IsContextTooLarge() {
				if compactionRetries < maxCompactionRetries {
					jsonlog.Info("compaction_start", log.F("reason", "context_too_large"))

					meta, _ := store.GetMetadata()
					msgCount := 0
					if meta != nil {
						msgCount = meta.MessageCount
					}

					summary := generateSummary(ctx, lm, systemPrompt, msgCount)
					_, err := store.CompactStart(summary)
					if err != nil {
						jsonlog.Error("compaction_failed", log.F("error", err))
						return fmt.Errorf("context overflow after compaction: %w", err)
					}

					compactionRetries++
					messages = nil
					continue
				}
				return fmt.Errorf("context overflow: %w", err)
			}
			return fmt.Errorf("agent error: %w", err)
	}

		// Check for done using the last response
		response := textBuilder.String()
		if result != nil && result.Response.Content.Text() != "" {
			response = result.Response.Content.Text()
		}

		if isDone(response) {
			jsonlog.Info("job_complete", log.F("job", jobName))
			return nil
		}

		// Build messages for next iteration
		messages = append(messages, fantasy.Message{
			Role: fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{
				&fantasy.TextPart{Text: currentPrompt},
			},
		})
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

func isDone(response string) bool {
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if line == "<<<<<DONE>>>>>" {
			return true
		}
	}
	return false
}

func generateSummary(ctx context.Context, lm fantasy.LanguageModel, systemPrompt string, messageCount int) string {
	summaryPrompt := fmt.Sprintf(`You are summarizing a conversation. The conversation had %d messages. The system prompt is: %s. Provide a brief 1-2 sentence summary of what this conversation covered.`, messageCount, systemPrompt)

	response, err := lm.Generate(ctx, fantasy.Call{
		Prompt: []fantasy.Message{
			{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{&fantasy.TextPart{Text: summaryPrompt}}},
		},
	})
	if err != nil {
		jsonlog.Error("summary_failed", log.F("error", err))
		return "Conversation summary unavailable"
	}

	summary := strings.TrimSpace(response.Content.Text())
	if len(summary) > 200 {
		summary = summary[:200] + "..."
	}
	return summary
}
