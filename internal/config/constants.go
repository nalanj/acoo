package config

import "time"

const (
	// DefaultCommandTimeout is the default timeout for agent commands
	DefaultCommandTimeout = 5 * time.Minute
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
