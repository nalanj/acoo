package agent

import (
	"fmt"
	"strings"

	"charm.land/fantasy"
)

// BuildSystemPrompt composes the full system prompt from agent body and available tools
func BuildSystemPrompt(agentBody string, agentName string, tools []fantasy.AgentTool, workspacePath string) string {
	var parts []string

	// Agent identity
	if agentName != "" {
		parts = append(parts, fmt.Sprintf("Your name is %s.", agentName))
	}

	// Agent body
	if agentBody != "" {
		if len(parts) > 0 {
			parts = append(parts, "")
		}
		parts = append(parts, strings.TrimSpace(agentBody))
	}

	// Workspace guidance
	if workspacePath != "" {
		parts = append(parts, "", fmt.Sprintf("You have a special workspace folder, located at %s, where you're able to add new files. Avoid writing to files anywhere else unless specifically asked to do so. It's your job to keep your workspace folder tidy, so feel free to take a moment at any time to do so.", workspacePath))
	}

	// Tools section
	if len(tools) > 0 {
		parts = append(parts, "", "You have access to the following tools:", "")
		for _, tool := range tools {
			info := tool.Info()
			parts = append(parts, info.Name+" - "+info.Description)
		}
		parts = append(parts, "", "Prefer direct tools over bash when possible. For example, use glob to find files by name rather than 'find' or 'ls' commands.")
	}

	return strings.Join(parts, "\n")
}
