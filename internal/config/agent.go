package config

import (
	"time"
)

// Agent represents an agent configuration
type Agent struct {
	Name     string            `yaml:"name"`
	Model    string            `yaml:"model"`
	Provider string            `yaml:"provider"`
	Env      map[string]string `yaml:"env"`
	Jobs     map[string]string `yaml:"jobs"` // job name -> schedule

	SourceFile string `yaml:"-"`
	Body      string `yaml:"-"` // The system prompt

	JobsMap map[string]*Job `yaml:"-"` // Resolved job objects
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
