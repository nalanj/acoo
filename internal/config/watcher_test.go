package config

import (
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestWatcherDebounce(t *testing.T) {
	agentsDir, err := os.MkdirTemp("", "acoo-test-agents-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(agentsDir)

	jobsDir, err := os.MkdirTemp("", "acoo-test-jobs-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(jobsDir)

	watcher, err := NewWatcher(agentsDir, jobsDir)
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}
	defer watcher.Close()

	changes := []string{}
	watcher.OnChange(func(c []string) {
		changes = append(changes, c...)
	})

	logger := log.New(os.Stdout, "", 0)

	// Simulate multiple rapid events
	testFile := filepath.Join(agentsDir, "test.md")
	go func() {
		watcher.handleEvent(fsnotify.Event{Op: fsnotify.Create, Name: testFile}, logger)
		watcher.handleEvent(fsnotify.Event{Op: fsnotify.Write, Name: testFile}, logger)
		watcher.handleEvent(fsnotify.Event{Op: fsnotify.Chmod, Name: testFile}, logger)
	}()

	// Wait for debounce
	time.Sleep(200 * time.Millisecond)

	// Should have received one change (debounced)
	if len(changes) != 1 {
		t.Errorf("Expected 1 change after debounce, got %d", len(changes))
	}
}

func TestWatcherNewFile(t *testing.T) {
	agentsDir, err := os.MkdirTemp("", "acoo-test-agents-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(agentsDir)

	jobsDir, err := os.MkdirTemp("", "acoo-test-jobs-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(jobsDir)

	watcher, err := NewWatcher(agentsDir, jobsDir)
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}
	defer watcher.Close()

	changes := []string{}
	watcher.OnChange(func(c []string) {
		changes = append(changes, c...)
	})

	logger := log.New(os.Stdout, "", 0)

	newFile := filepath.Join(agentsDir, "newagent.md")
	go func() {
		watcher.handleEvent(fsnotify.Event{Op: fsnotify.Create, Name: newFile}, logger)
	}()

	time.Sleep(200 * time.Millisecond)

	if len(changes) != 1 {
		t.Errorf("Expected 1 change, got %d", len(changes))
	}
	if len(changes) > 0 && changes[0] != "added:"+newFile {
		t.Errorf("Expected 'added' action, got %s", changes[0])
	}
}

func TestWatcherRemoveFile(t *testing.T) {
	agentsDir, err := os.MkdirTemp("", "acoo-test-agents-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(agentsDir)

	jobsDir, err := os.MkdirTemp("", "acoo-test-jobs-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(jobsDir)

	watcher, err := NewWatcher(agentsDir, jobsDir)
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}
	defer watcher.Close()

	changes := []string{}
	watcher.OnChange(func(c []string) {
		changes = append(changes, c...)
	})

	logger := log.New(os.Stdout, "", 0)

	// First add the file and wait for it to be processed
	testFile := filepath.Join(agentsDir, "test.md")
	go func() {
		watcher.handleEvent(fsnotify.Event{Op: fsnotify.Create, Name: testFile}, logger)
	}()
	time.Sleep(200 * time.Millisecond) // Wait for debounce

	// Reset changes to only track the remove
	changes = []string{}

	// Then remove it
	go func() {
		watcher.handleEvent(fsnotify.Event{Op: fsnotify.Remove, Name: testFile}, logger)
	}()

	time.Sleep(200 * time.Millisecond)

	if len(changes) != 1 {
		t.Errorf("Expected 1 change (remove), got %d", len(changes))
	}
}

func TestWatcherNonMarkdownFile(t *testing.T) {
	agentsDir, err := os.MkdirTemp("", "acoo-test-agents-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(agentsDir)

	jobsDir, err := os.MkdirTemp("", "acoo-test-jobs-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(jobsDir)

	watcher, err := NewWatcher(agentsDir, jobsDir)
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}
	defer watcher.Close()

	changes := []string{}
	watcher.OnChange(func(c []string) {
		changes = append(changes, c...)
	})

	logger := log.New(os.Stdout, "", 0)

	// Try to add non-markdown file
	go func() {
		watcher.handleEvent(fsnotify.Event{Op: fsnotify.Create, Name: filepath.Join(agentsDir, "test.txt")}, logger)
	}()

	time.Sleep(200 * time.Millisecond)

	if len(changes) != 0 {
		t.Errorf("Expected no changes for non-markdown file, got %d", len(changes))
	}
}
