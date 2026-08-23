package mail

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type InboxManager struct {
	messagesDir string
}

func NewInboxManager(messagesDir string) *InboxManager {
	return &InboxManager{
		messagesDir: messagesDir,
	}
}

func (m *InboxManager) AddToInboxes(msg *Message) error {
	for _, recipient := range msg.To {
		inboxPath, err := m.inboxPath(recipient)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(inboxPath, 0755); err != nil {
			return fmt.Errorf("creating inbox directory for %s: %w", recipient, err)
		}

		linkPath := filepath.Join(inboxPath, msg.ID+".md")
		
		// Calculate relative path from inbox to messages
		var targetPath string
		if recipient == "nalanj" {
			targetPath = filepath.Join("..", "messages", msg.ID+".md")
		} else {
			targetPath = filepath.Join("..", "..", "..", "messages", msg.ID+".md")
		}

		if err := os.Symlink(targetPath, linkPath); err != nil {
			if os.IsExist(err) {
				continue
			}
			return fmt.Errorf("creating symlink in %s inbox: %w", recipient, err)
		}
	}

	return nil
}

func (m *InboxManager) RemoveFromInbox(msgID string, recipient string) error {
	inboxPath, err := m.inboxPath(recipient)
	if err != nil {
		return err
	}

	linkPath := filepath.Join(inboxPath, msgID+".md")
	if err := os.Remove(linkPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("removing symlink from %s inbox: %w", recipient, err)
	}

	return nil
}

func (m *InboxManager) inboxPath(recipient string) (string, error) {
	if recipient == "nalanj" {
		return filepath.Join(m.messagesDir, "..", "inbox"), nil
	}

	return filepath.Join(m.messagesDir, "..", "agents", recipient, "inbox"), nil
}

func (m *InboxManager) ListAgents() ([]string, error) {
	agentsPath := filepath.Join(m.messagesDir, "..", "agents")

	entries, err := os.ReadDir(agentsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("reading agents directory: %w", err)
	}

	var agents []string
	for _, entry := range entries {
		if entry.IsDir() {
			agents = append(agents, entry.Name())
		}
	}

	return agents, nil
}

// ListInboxMessages returns IDs of messages in the recipient's inbox
func (m *InboxManager) ListInboxMessages(recipient string) ([]string, error) {
	inboxPath, err := m.inboxPath(recipient)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(inboxPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("reading inbox directory: %w", err)
	}

	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			ids = append(ids, strings.TrimSuffix(entry.Name(), ".md"))
		}
	}

	return ids, nil
}
