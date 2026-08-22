package config

import "strconv"

// Default compaction settings
const DefaultCompactionRetainTokens = 20000

// CompactionConfig holds compaction settings for a job
type CompactionConfig struct {
	RetainTokens int `yaml:"retain_tokens"` // Tokens to retain (default: 20000)
}

// GetRetainTokens returns the token budget to retain, defaulting to 20000
func (c *CompactionConfig) GetRetainTokens() int {
	if c.RetainTokens <= 0 {
		return DefaultCompactionRetainTokens
	}
	return c.RetainTokens
}

// Job represents a job definition
type Job struct {
	Provider     string            `yaml:"provider"`
	Model        string            `yaml:"model"`
	Thinking     any               `yaml:"thinking"`          // Token budget (int) or effort level
	Preconditions []string         `yaml:"preconditions"`     // Shell commands to run before job
	Env          map[string]string `yaml:"env"`              // Environment variables
	Compaction   CompactionConfig  `yaml:"compaction"`       // Compaction settings
	SourceFile   string            `yaml:"-"`
	Name         string            `yaml:"-"`                // Derived from filename
	Body         string            `yaml:"-"`                // The task prompt
}

// GetEnv returns environment variables
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
