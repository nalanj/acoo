package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAgentContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		filename string
		wantName string
		wantEnv  string
		wantBody string
		wantErr  bool
	}{
		{
			name: "basic agent",
			content: `---
env:
  API_KEY: secret123
jobs:
  test: "@every 30s"
---

You are a helpful assistant.`,
			filename: "myagent.md",
			wantName: "myagent",
			wantEnv:  "secret123",
			wantBody: "You are a helpful assistant.",
		},
		{
			name: "no env",
			content: `---
jobs:
  test: "@every 30s"
---

System prompt here.`,
			filename: "agent2.md",
			wantName: "agent2",
			wantBody: "System prompt here.",
		},
		{
			name:    "missing front matter",
			content: "Just a body.",
			filename: "bad.md",
			wantErr:  true,
		},
		{
			name: "multiple jobs",
			content: `---
jobs:
  job1: "@every 30s"
  job2: "0 9 * * *"
---

Prompt.`,
			filename: "multi.md",
			wantName: "multi",
			wantBody: "Prompt.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, err := ParseAgentContent(tt.content, filepath.Join("/fake", tt.filename))
			if tt.wantErr {
				if err == nil {
					t.Error("ParseAgentContent() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("ParseAgentContent() error: %v", err)
				return
			}

			if agent.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", agent.Name, tt.wantName)
			}

			if tt.wantEnv != "" {
				if agent.Env["API_KEY"] != tt.wantEnv {
					t.Errorf("Env[API_KEY] = %q, want %q", agent.Env["API_KEY"], tt.wantEnv)
				}
			}

			if agent.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", agent.Body, tt.wantBody)
			}
		})
	}
}

func TestParseJobContent(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		filename     string
		wantProvider string
		wantModel    string
		wantBody     string
		wantErr      bool
	}{
		{
			name: "full job",
			content: `---
provider: openai
model: gpt-4o
thinking: high
preconditions:
  - "test -f /tmp/data"
env:
  DATA_PATH: /tmp/data
---

Read the data and process it.`,
			filename:     "processor.md",
			wantProvider: "openai",
			wantModel:    "gpt-4o",
			wantBody:     "Read the data and process it.",
		},
		{
			name: "minimal job",
			content: `---
provider: anthropic
model: claude-3
---

Simple task.`,
			filename:     "simple.md",
			wantProvider: "anthropic",
			wantModel:    "claude-3",
			wantBody:     "Simple task.",
		},
		{
			name:     "no front matter",
			content:  "Just body content.",
			filename: "nofm.md",
			wantBody: "Just body content.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, err := ParseJobContent(tt.content, filepath.Join("/fake", tt.filename))
			if tt.wantErr {
				if err == nil {
					t.Error("ParseJobContent() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("ParseJobContent() error: %v", err)
				return
			}

			if tt.wantProvider != "" && job.Provider != tt.wantProvider {
				t.Errorf("Provider = %q, want %q", job.Provider, tt.wantProvider)
			}

			if tt.wantModel != "" && job.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", job.Model, tt.wantModel)
			}

			if job.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", job.Body, tt.wantBody)
			}
		})
	}
}

func TestJobGetThinkingBudget(t *testing.T) {
	tests := []struct {
		name     string
		thinking any
		want     int64
	}{
		{"nil", nil, 0},
		{"disabled", "disabled", 0},
		{"low", "low", 10000},
		{"medium", "medium", 16000},
		{"high", "high", 32000},
		{"very_high", "very_high", 64000},
		{"veryhigh", "veryhigh", 64000},
		{"max", "max", 100000},
		{"numeric int", 50000, 50000},
		{"numeric float", 25000.0, 25000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Thinking: tt.thinking}
			got := job.GetThinkingBudget()
			if got != tt.want {
				t.Errorf("GetThinkingBudget() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestLoadJobs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-test-jobs-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	job1 := `---
provider: openai
model: gpt-4o
---

Task 1.`
	os.WriteFile(filepath.Join(tmpDir, "job1.md"), []byte(job1), 0644)

	job2 := `---
provider: anthropic
model: claude-3
---

Task 2.`
	os.WriteFile(filepath.Join(tmpDir, "job2.md"), []byte(job2), 0644)

	jobs, err := LoadJobs(tmpDir)
	if err != nil {
		t.Fatalf("LoadJobs() error: %v", err)
	}

	if len(jobs) != 2 {
		t.Errorf("LoadJobs() returned %d jobs, want 2", len(jobs))
	}

	if jobs["job1"].Provider != "openai" {
		t.Errorf("job1.Provider = %q, want openai", jobs["job1"].Provider)
	}

	if jobs["job2"].Model != "claude-3" {
		t.Errorf("job2.Model = %q, want claude-3", jobs["job2"].Model)
	}
}

func TestLoadJobsEmptyDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acoo-test-empty-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	jobs, err := LoadJobs(tmpDir)
	if err != nil {
		t.Fatalf("LoadJobs() error: %v", err)
	}

	if len(jobs) != 0 {
		t.Errorf("LoadJobs() returned %d jobs, want 0", len(jobs))
	}
}

func TestLoadJobsNonexistent(t *testing.T) {
	jobs, err := LoadJobs("/nonexistent/path")
	if err != nil {
		t.Fatalf("LoadJobs() error: %v", err)
	}

	if len(jobs) != 0 {
		t.Errorf("LoadJobs() returned %d jobs, want 0", len(jobs))
	}
}

func TestCompactionConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     CompactionConfig
		wantRetain int
	}{
		{
			name:       "zero value defaults to 20k",
			config:     CompactionConfig{},
			wantRetain: 20000,
		},
		{
			name:       "custom retain tokens",
			config:     CompactionConfig{RetainTokens: 15000},
			wantRetain: 15000,
		},
		{
			name:       "zero retain tokens uses default",
			config:     CompactionConfig{RetainTokens: 0},
			wantRetain: 20000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetRetainTokens(); got != tt.wantRetain {
				t.Errorf("GetRetainTokens() = %v, want %v", got, tt.wantRetain)
			}
		})
	}
}

func TestJobCompactionConfig(t *testing.T) {
	content := `---
provider: openai
model: gpt-4o
compaction:
  retain_tokens: 30000
---

Task with custom compaction.`

	job, err := ParseJobContent(content, "test.md")
	if err != nil {
		t.Fatalf("ParseJobContent() error: %v", err)
	}

	if job.Compaction.GetRetainTokens() != 30000 {
		t.Errorf("Compaction.RetainTokens = %d, want 30000", job.Compaction.GetRetainTokens())
	}
}
