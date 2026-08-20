package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultDirs(t *testing.T) {
	home, _ := os.UserHomeDir()
	expectedAgents := filepath.Join(home, ".config", "acoo", "agents")
	expectedJobs := filepath.Join(home, ".config", "acoo", "jobs")

	if got := defaultAgentsDir(); got != expectedAgents {
		t.Errorf("defaultAgentsDir() = %q, want %q", got, expectedAgents)
	}

	if got := defaultJobsDir(); got != expectedJobs {
		t.Errorf("defaultJobsDir() = %q, want %q", got, expectedJobs)
	}
}

func TestBuildCommands(t *testing.T) {
	root := BuildCommands()

	if root == nil {
		t.Fatal("BuildCommands() returned nil")
	}

	// Check that all commands are registered
	cmds := root.Commands()
	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name()] = true
	}

	expected := []string{"list", "validate", "test", "start", "agent", "providers"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("Command %q not registered", name)
		}
	}
}

func TestCLIIntegration(t *testing.T) {
	// Build the binary first
	cmd := exec.Command("go", "build", "-o", "acoo-test", "./cmd/acoo")
	cmd.Dir = "../.."
	if err := cmd.Run(); err != nil {
		t.Skipf("Skipping integration test: failed to build: %v", err)
	}
	defer os.Remove("../../acoo-test")

	// Test --help
	cmd = exec.Command("./acoo-test", "--help")
	cmd.Dir = "../.."
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "Agent Command Orchestrator") {
		t.Errorf("--help output doesn't contain expected text")
	}
}

func TestSignalHandler(t *testing.T) {
	// Just verify it returns a channel
	ch := SignalHandler()
	if ch == nil {
		t.Error("SignalHandler() returned nil")
	}
}

func TestConstants(t *testing.T) {
	if DoneMarker != "<<<<<DONE>>>>>" {
		t.Errorf("DoneMarker = %q, want %q", DoneMarker, "<<<<<DONE>>>>>")
	}
}
