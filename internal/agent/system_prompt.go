package agent

import (
	"strings"

	"charm.land/fantasy"
)

// BuildSystemPrompt composes the full system prompt from agent body and available tools
func BuildSystemPrompt(agentBody string, tools []fantasy.AgentTool) string {
	var parts []string

	// Agent body
	if agentBody != "" {
		parts = append(parts, strings.TrimSpace(agentBody))
	} else {
		parts = append(parts, "You are a helpful AI assistant.")
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
