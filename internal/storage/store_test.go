package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreBasics(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-store-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir, "agent1")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Test empty get
	messages, err := store.GetMessages()
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(messages))
	}

	// Test add and get
	err = store.AddMessage(Message{Role: "user", Content: "Hello"})
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}
	err = store.AddMessage(Message{Role: "assistant", Content: "Hi there"})
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	messages, err = store.GetMessages()
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}
}

func TestSystemPrompt(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-store-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir, "agent1")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Save system prompt
	err = store.SaveSystemPrompt("You are a helpful assistant.")
	if err != nil {
		t.Fatalf("SaveSystemPrompt failed: %v", err)
	}

	// Get system prompt
	prompt, err := store.GetSystemPrompt()
	if err != nil {
		t.Fatalf("GetSystemPrompt failed: %v", err)
	}
	if prompt != "You are a helpful assistant." {
		t.Errorf("Expected 'You are a helpful assistant.', got '%s'", prompt)
	}
}

func TestMultipleAgents(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-store-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store1, err := NewStore(tmpDir, "agent1")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store1.Close()

	store2, err := NewStore(tmpDir, "agent2")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store2.Close()

	store1.AddMessage(Message{Role: "user", Content: "Hello from agent1"})
	store2.AddMessage(Message{Role: "user", Content: "Hello from agent2"})

	messages1, _ := store1.GetMessages()
	messages2, _ := store2.GetMessages()

	if len(messages1) != 1 {
		t.Errorf("Expected agent1 to have 1 message, got %d", len(messages1))
	}
	if len(messages2) != 1 {
		t.Errorf("Expected agent2 to have 1 message, got %d", len(messages2))
	}
}

func TestListSessions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-store-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, name := range []string{"agent1", "agent2", "agent3"} {
		store, _ := NewStore(tmpDir, name)
		store.AddMessage(Message{Role: "user", Content: "test"})
		store.Close()
	}

	sessions, err := ListSessions(tmpDir)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("Expected 3 sessions, got %d", len(sessions))
	}
}

func TestCompaction(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-store-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir, "agent1")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Add some messages
	store.AddMessage(Message{Role: "user", Content: "Hello"})
	store.AddMessage(Message{Role: "assistant", Content: "Hi there"})
	store.AddMessage(Message{Role: "user", Content: "How are you?"})
	store.AddMessage(Message{Role: "assistant", Content: "I'm good"})

	// Verify we have 4 messages
	messages, _ := store.GetMessages()
	if len(messages) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(messages))
	}

	// Start compaction - should create session_002.jsonl
	newNum, err := store.CompactStart("User greeted, assistant responded")
	if err != nil {
		t.Fatalf("CompactStart failed: %v", err)
	}
	if newNum != 2 {
		t.Errorf("Expected new session number 2, got %d", newNum)
	}

	// Should now have fewer messages in current session
	messages, _ = store.GetMessages()
	// Session had 4 messages, we keep last 6 (or all if fewer), plus summary
	// So we expect 4 + 1 = 5 messages in new session
	if len(messages) != 5 {
		t.Errorf("Expected 5 messages after compaction, got %d", len(messages))
	}

	// Check old session still exists
	oldPath := filepath.Join(tmpDir, "agent1", "session_001.jsonl")
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		t.Error("Old session file should still exist")
	}

	// Check new session exists
	newPath := filepath.Join(tmpDir, "agent1", "session_002.jsonl")
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Error("New session file should exist")
	}
}

func TestPrevLink(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-store-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir, "agent1")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	store.AddMessage(Message{Role: "user", Content: "First"})
	store.AddMessage(Message{Role: "assistant", Content: "Second"})
	store.AddMessage(Message{Role: "user", Content: "Third"})

	messages, _ := store.GetMessages()
	if len(messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(messages))
	}

	// Check prev links are set
	for i, msg := range messages {
		if i > 0 && msg.Prev == "" {
			t.Errorf("Message %d missing prev link", i)
		}
	}
}
