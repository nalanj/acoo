package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"charm.land/fantasy"
)

func TestReadFileTool(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	os.WriteFile(testFile, []byte(content), 0644)

	tool := ReadFileTool()
	ctx := context.Background()

	input, _ := json.Marshal(map[string]any{"path": testFile})
	resp, err := tool.Run(ctx, fantasy.ToolCall{Input: string(input)})
	if err != nil {
		t.Fatalf("Tool run error: %v", err)
	}

	if resp.IsError {
		t.Error("Expected text response, got error")
	}

	if resp.Content != content {
		t.Errorf("Got %q, want %q", resp.Content, content)
	}
}

func TestReadFileToolNotFound(t *testing.T) {
	tool := ReadFileTool()
	ctx := context.Background()

	input, _ := json.Marshal(map[string]any{"path": "/nonexistent/file.txt"})
	resp, err := tool.Run(ctx, fantasy.ToolCall{Input: string(input)})
	if err != nil {
		t.Fatalf("Tool run error: %v", err)
	}

	if !resp.IsError {
		t.Error("Expected error response for missing file")
	}
}

func TestEditFileTool(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "output.txt")
	content := "Hello, World!"

	tool := EditFileTool()
	ctx := context.Background()

	input, _ := json.Marshal(map[string]any{
		"path":    testFile,
		"content": content,
	})
	_, err = tool.Run(ctx, fantasy.ToolCall{Input: string(input)})
	if err != nil {
		t.Fatalf("Tool run error: %v", err)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(data) != content {
		t.Errorf("File content = %q, want %q", string(data), content)
	}
}

func TestEditFileToolAppend(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "output.txt")
	os.WriteFile(testFile, []byte("Line 1\n"), 0644)

	tool := EditFileTool()
	ctx := context.Background()

	input, _ := json.Marshal(map[string]any{
		"path":    testFile,
		"content": "Line 2\n",
		"append":  true,
	})
	_, err = tool.Run(ctx, fantasy.ToolCall{Input: string(input)})
	if err != nil {
		t.Fatalf("Tool run error: %v", err)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(data) != "Line 1\nLine 2\n" {
		t.Errorf("File content = %q, want %q", string(data), "Line 1\nLine 2\n")
	}
}

func TestGlobTool(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("1"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("2"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file3.go"), []byte("3"), 0644)

	tool := GlobTool()
	ctx := context.Background()

	input, _ := json.Marshal(map[string]any{
		"pattern": "*.txt",
		"dir":     tmpDir,
	})
	resp, err := tool.Run(ctx, fantasy.ToolCall{Input: string(input)})
	if err != nil {
		t.Fatalf("Tool run error: %v", err)
	}

	if resp.Content == "No files matched" {
		t.Error("Expected to find txt files")
	}
}

func TestGlobToolNoMatches(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tool := GlobTool()
	ctx := context.Background()

	input, _ := json.Marshal(map[string]any{
		"pattern": "*.xyz",
		"dir":     tmpDir,
	})
	resp, err := tool.Run(ctx, fantasy.ToolCall{Input: string(input)})
	if err != nil {
		t.Fatalf("Tool run error: %v", err)
	}

	if resp.Content != "No files matched" {
		t.Errorf("Expected 'No files matched', got %q", resp.Content)
	}
}

func TestListDirTool(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte(""), 0644)

	tool := ListDirTool()
	ctx := context.Background()

	input, _ := json.Marshal(map[string]any{"path": tmpDir})
	resp, err := tool.Run(ctx, fantasy.ToolCall{Input: string(input)})
	if err != nil {
		t.Fatalf("Tool run error: %v", err)
	}

	if resp.Content == "" {
		t.Error("Expected directory contents")
	}
}

func TestListDirToolNotFound(t *testing.T) {
	tool := ListDirTool()
	ctx := context.Background()

	input, _ := json.Marshal(map[string]any{"path": "/nonexistent/dir"})
	resp, err := tool.Run(ctx, fantasy.ToolCall{Input: string(input)})
	if err != nil {
		t.Fatalf("Tool run error: %v", err)
	}

	if !resp.IsError {
		t.Error("Expected error response for missing directory")
	}
}

func TestTools(t *testing.T) {
	tools := Tools()
	if len(tools) != 10 {
		t.Errorf("Tools() returned %d tools, want 10", len(tools))
	}

	names := []string{}
	for _, tool := range tools {
		names = append(names, tool.Info().Name)
	}

	expected := []string{"read_file", "edit_file", "bash", "glob", "list_dir", "mail_inbox", "mail_send", "mail_read", "mail_reply", "mail_archive"}
	for _, exp := range expected {
		found := false
		for _, name := range names {
			if name == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected tool %q not found", exp)
		}
	}
}
