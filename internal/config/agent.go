package config

import (
	"strconv"
	"time"
)

// Thinking effort levels mapped to token budgets
var ThinkingBudgets = map[string]int64{
	"low":       10000,
	"medium":    16000,
	"high":      32000,
	"very_high": 64000,
	"veryhigh":  64000,
	"max":       100000,
}

// Agent represents an agent configuration
type Agent struct {
	Name     string            `yaml:"name"`
	Model    string            `yaml:"model"`
	Provider string            `yaml:"provider"`
	Thinking any               `yaml:"thinking"` // Token budget (int) or effort level (string: low, medium, high, very_high, max)
	Env      map[string]string `yaml:"env"`
	Jobs     map[string]string `yaml:"jobs"` // job name -> schedule

	SourceFile string `yaml:"-"`
	Body      string `yaml:"-"` // The system prompt

	JobsMap map[string]*Job `yaml:"-"` // Resolved job objects
}

// GetThinkingBudget returns the thinking budget in tokens
func (a *Agent) GetThinkingBudget() int64 {
	if a.Thinking == nil {
		return 0
	}

	switch v := a.Thinking.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		if budget, ok := ThinkingBudgets[v]; ok {
			return budget
		}
		// Try parsing as number
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 0
}

// GetEnv returns the environment variables for this agent
func (a *Agent) GetEnv() map[string]string {
	return a.Env
}

// Tool represents a tool in the agent config
type Tool struct {
	Type      ToolType
	Name      string
	Content   string
	RawBlock  string
	LineNum   int
}

// ToolType defines the type of tool
type ToolType string

const (
	ToolTypePickup  ToolType = "pickup"
	ToolTypeProcess ToolType = "process"
	ToolTypeBash    ToolType = "bash"
	ToolTypeGeneric ToolType = "tool"
)

// CommandTimeout is the default timeout for agent commands
const CommandTimeout = 5 * time.Minute
