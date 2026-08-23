package agent

import (
	"fmt"
	"strings"

	"charm.land/fantasy"
)

// BuildSystemPrompt composes the full system prompt from agent body, tools, and skills
func BuildSystemPrompt(agentBody string, agentName string, tools []fantasy.AgentTool, skills []Skill, workspacePath string) string {
	var parts []string

	// Agent identity - at the top, prominent
	if agentName != "" {
		parts = append(parts, fmt.Sprintf("You are %s.", agentName))
	}

	// Agent body
	if agentBody != "" {
		parts = append(parts, strings.TrimSpace(agentBody))
	}

	// Workspace guidance
	if workspacePath != "" {
		parts = append(parts, "", fmt.Sprintf("Your workspace folder is %s. Write files there instead of elsewhere.", workspacePath))
	}

	// Tools section
	if len(tools) > 0 {
		parts = append(parts, "", "You have access to these tools:")
		for _, tool := range tools {
			info := tool.Info()
			parts = append(parts, "- "+info.Name+": "+info.Description)
		}
	}

	// Skills section - at the end
	if len(skills) > 0 {
		parts = append(parts, "", BuildSkillsSection(skills))
	}

	return strings.Join(parts, "\n")
}
