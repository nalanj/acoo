package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nalanj/acoo/internal/agent"
	"github.com/nalanj/acoo/internal/config"
	"github.com/nalanj/acoo/internal/log"
)

// AgentManager manages all agent runners
type AgentManager struct {
	agentsDir string
	jobsDir   string
	logger    *log.Logger
	watcher   *config.Watcher
	runners   map[string]*agent.Runner

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewAgentManager creates a new agent manager
func NewAgentManager(agentsDir, jobsDir string, logger *log.Logger) *AgentManager {
	return &AgentManager{
		agentsDir: agentsDir,
		jobsDir:   jobsDir,
		logger:    logger,
		runners:   make(map[string]*agent.Runner),
	}
}

// Start starts the agent manager
func (m *AgentManager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()

	if err := m.reloadAll(); err != nil {
		return err
	}

	watcher, err := config.NewWatcher(m.agentsDir, m.jobsDir)
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	m.watcher = watcher

	watcher.OnChange(func(changed []string) {
		m.handleChanges(changed)
	})

	watcher.Watch()

	return nil
}

// handleChanges processes file change events
func (m *AgentManager) handleChanges(changed []string) {
	var agentChanges []string
	var jobChanges []string

	for _, c := range changed {
		parts := strings.SplitN(c, ":", 2)
		if len(parts) != 2 {
			continue
		}
		action, path := parts[0], parts[1]

		dir := filepath.Dir(path)
		name := strings.TrimSuffix(filepath.Base(path), ".md")

		if dir == m.jobsDir {
			jobChanges = append(jobChanges, name)
		} else if action == "removed" {
			m.stopAgent(name)
		} else {
			agentChanges = append(agentChanges, name)
		}
	}

	// Reload agents asynchronously
	for _, name := range agentChanges {
		go m.reloadAgentAsync(name)
	}

	// Reload agents that reference changed jobs
	for _, name := range jobChanges {
		m.reloadAgentsWithJobAsync(name)
	}
}

// reloadAll loads and starts all agents
func (m *AgentManager) reloadAll() error {
	agents, _, err := config.LoadAll(m.agentsDir, m.jobsDir)
	if err != nil {
		return fmt.Errorf("loading agents: %w", err)
	}

	for _, a := range agents {
		m.startAgent(a)
	}

	m.logger.Info("loaded", log.F("agents", len(agents)))
	return nil
}

// reloadAgentAsync reloads an agent asynchronously
func (m *AgentManager) reloadAgentAsync(name string) {
	m.logger.Info("reloading_agent", log.F("agent", name))

	// Stop existing runner
	m.stopAgent(name)

	// Load and start new config
	path := filepath.Join(m.agentsDir, name+".md")
	agentConfig, err := config.LoadAgentFile(path)
	if err != nil {
		m.logger.Error("reload_failed", log.F("agent", name), log.F("error", err))
		return
	}

	jobs, err := config.LoadJobs(m.jobsDir)
	if err != nil {
		m.logger.Error("load_jobs_failed", log.F("error", err))
		return
	}

	agentConfig.JobsMap = make(map[string]*config.Job)
	for jobName := range agentConfig.Jobs {
		if job, ok := jobs[jobName]; ok {
			agentConfig.JobsMap[jobName] = job
		}
	}

	m.startAgent(agentConfig)
}

// reloadAgentsWithJobAsync reloads all agents that reference the given job
func (m *AgentManager) reloadAgentsWithJobAsync(jobName string) {
	agents, err := config.LoadAgents(m.agentsDir)
	if err != nil {
		m.logger.Error("load_agents_failed", log.F("error", err))
		return
	}

	for _, a := range agents {
		if _, refsJob := a.Jobs[jobName]; refsJob {
			go m.reloadAgentAsync(a.Name)
		}
	}
}

// startAgent starts an agent runner
func (m *AgentManager) startAgent(a *config.Agent) {
	m.mu.Lock()

	// Wait for existing runner to finish if needed
	var existing *agent.Runner
	if e, ok := m.runners[a.Name]; ok {
		existing = e
	}
	m.mu.Unlock()

	if existing != nil {
		existing.Stop()
		existing.Wait()
	}

	logger := log.Agent(a.Name)
	runner := agent.NewRunner(a, logger)

	m.mu.Lock()
	m.runners[a.Name] = runner
	m.mu.Unlock()

	runner.Start(m.ctx)
}

// stopAgent stops an agent runner
func (m *AgentManager) stopAgent(name string) {
	m.mu.Lock()
	runner, exists := m.runners[name]
	if exists {
		delete(m.runners, name)
	}
	m.mu.Unlock()

	if !exists {
		return
	}

	m.logger.Info("stopping_agent", log.F("agent", name))
	runner.Stop()
	runner.Wait()
	m.logger.Info("stopped_agent", log.F("agent", name))
}

// Stop stops the agent manager
func (m *AgentManager) Stop() {
	m.mu.Lock()
	if m.watcher != nil {
		m.watcher.Close()
	}
	if m.cancel != nil {
		m.cancel()
	}

	runners := m.runners
	m.runners = make(map[string]*agent.Runner)
	m.mu.Unlock()

	for _, runner := range runners {
		runner.Stop()
		runner.Wait()
	}
}

// AgentCount returns the number of running agents
func (m *AgentManager) AgentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runners)
}