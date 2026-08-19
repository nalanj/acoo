package config

import "strconv"

// Job represents a job definition
type Job struct {
	Name          string            `yaml:"name"`
	Schedule     string            `yaml:"schedule"`
	Model        string            `yaml:"model"`
	Thinking      any               `yaml:"thinking"` // Token budget (int) or effort level
	Preconditions []string         `yaml:"preconditions"` // Shell commands to run before job
	Env           map[string]string `yaml:"env"` // Environment variables
	SourceFile    string            `yaml:"-"`
	Body          string            `yaml:"-"` // The task prompt
}

// GetEnv returns merged environment variables (job takes precedence over agent)
func (j *Job) GetEnv() map[string]string {
	return j.Env
}

// GetThinkingBudget returns the thinking budget in tokens
func (j *Job) GetThinkingBudget() int64 {
	if j.Thinking == nil {
		return 0
	}

	switch v := j.Thinking.(type) {
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
