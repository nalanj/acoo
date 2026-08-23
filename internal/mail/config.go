package mail

import (
	"os"
	"path/filepath"
)

// Config holds mail configuration paths
type Config struct {
	MailRoot    string // Root of mail spool (~/.local/share/acoo/mail)
	MessagesDir string // Messages directory
	agentName   string
}

// Default returns the default mail config
func Default() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	agentName := os.Getenv("AGENT_NAME")
	if agentName == "" {
		agentName = "nalanj"
	}

	mailRoot := filepath.Join(home, ".local", "share", "acoo", "mail")
	return &Config{
		MailRoot:    mailRoot,
		MessagesDir: filepath.Join(mailRoot, "messages"),
		agentName:   agentName,
	}, nil
}

// AgentName returns the current agent name
func (c *Config) AgentName() string {
	return c.agentName
}

// EnsureDirs creates necessary directories
func (c *Config) EnsureDirs() error {
	dirs := []string{
		filepath.Join(c.MailRoot, "inbox"),
		filepath.Join(c.MailRoot, "archive"),
		filepath.Join(c.MailRoot, "messages"),
		filepath.Join(c.MailRoot, "agents"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}
