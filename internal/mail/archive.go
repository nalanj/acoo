package mail

import (
	"fmt"
	"os"
	"path/filepath"
)

// ArchiveManager handles moving messages between inbox and archive
type ArchiveManager struct {
	messagesDir string
	mailRoot    string
}

func NewArchiveManager(messagesDir, mailRoot string) *ArchiveManager {
	return &ArchiveManager{
		messagesDir: messagesDir,
		mailRoot:    mailRoot,
	}
}

// GetInboxDir returns the inbox directory for a recipient
func (m *ArchiveManager) GetInboxDir(recipient string) string {
	if recipient == "nalanj" {
		return filepath.Join(m.mailRoot, "inbox")
	}
	return filepath.Join(m.mailRoot, "agents", recipient, "inbox")
}

// GetArchiveDir returns the archive directory for a recipient
func (m *ArchiveManager) GetArchiveDir(recipient string) string {
	if recipient == "nalanj" {
		return filepath.Join(m.mailRoot, "archive")
	}
	return filepath.Join(m.mailRoot, "agents", recipient, "archive")
}

// Archive moves a message from inbox to archive
func (m *ArchiveManager) Archive(recipient, msgID string) error {
	inboxDir := m.GetInboxDir(recipient)
	archiveDir := m.GetArchiveDir(recipient)

	// Ensure archive directory exists
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return fmt.Errorf("creating archive directory: %w", err)
	}

	// Move symlink from inbox to archive
	inboxLink := filepath.Join(inboxDir, msgID+".md")
	archiveLink := filepath.Join(archiveDir, msgID+".md")

	if err := os.Rename(inboxLink, archiveLink); err != nil {
		return fmt.Errorf("moving message to archive: %w", err)
	}

	return nil
}

// IsArchived checks if a message has been archived
func (m *ArchiveManager) IsArchived(recipient, msgID string) bool {
	archiveLink := filepath.Join(m.GetArchiveDir(recipient), msgID+".md")
	_, err := os.Lstat(archiveLink)
	return err == nil
}
