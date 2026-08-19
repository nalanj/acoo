package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nalanj/acoo/internal/config"
	"github.com/nalanj/acoo/internal/provider"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			MarginBottom(1)

	fieldStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))
)

// wizardModel holds the state for the agent creation wizard
type wizardModel struct {
	step          int
	totalSteps    int
	name          string
	provider      string
	model         string
	thinking      string
	systemPrompt  string
	jobName       string
	jobSchedule   string
	envVars       map[string]string // key -> value

	nameInput         textinput.Model
	promptInput       textinput.Model
	jobNameInput      textinput.Model
	jobScheduleInput  textinput.Model
	envKeyInput       textinput.Model
	envValueInput     textinput.Model

	providers      []string
	providerNames []string
	models        []string
	thinkingOpts  []string

	currentSelection int
	envVarKeys      []string // sorted keys for display
	showEnvValues   bool    // whether to show env values
	err             error
	done            bool
	quitting        bool
	resultFile      string
}

// initialModel creates the wizard with optional source agent
func initialModel(sourceAgent *config.Agent) *wizardModel {
	pf := provider.NewFactory()
	providerInfos := pf.ListProviders()

	providers := make([]string, len(providerInfos))
	providerNames := make([]string, len(providerInfos))
	for i, p := range providerInfos {
		providers[i] = p.ID
		providerNames[i] = p.Name
	}

	m := &wizardModel{
		totalSteps:    8,
		providers:     providers,
		providerNames: providerNames,
		models:        []string{},
		thinkingOpts:  []string{"disabled", "low", "medium", "high", "very_high", "max"},
		envVars:      make(map[string]string),
	}

	// Initialize text inputs
	m.nameInput = textinput.New()
	m.nameInput.Placeholder = "my-agent"
	m.nameInput.Focus()

	m.promptInput = textinput.New()
	m.promptInput.Placeholder = "You are a helpful AI assistant."

	m.jobNameInput = textinput.New()
	m.jobNameInput.Placeholder = "default"

	m.jobScheduleInput = textinput.New()
	m.jobScheduleInput.Placeholder = "@every 30s"

	m.envKeyInput = textinput.New()
	m.envKeyInput.Placeholder = "ENV_VAR_NAME"

	m.envValueInput = textinput.New()
	m.envValueInput.Placeholder = "value (will not be echoed)"
	m.envValueInput.EchoMode = textinput.EchoNone

	// Pre-fill from source agent if provided
	if sourceAgent != nil {
		m.name = sourceAgent.Name + "-copy"
		m.provider = sourceAgent.Provider
		m.model = sourceAgent.Model
		m.thinking = thinkingToOption(sourceAgent.GetThinkingBudget())
		m.systemPrompt = sourceAgent.Body
		m.nameInput.SetValue(m.name)
		m.promptInput.SetValue(m.systemPrompt)
		m.updateModelsForProvider()

		// Copy env vars but mark as hidden - they won't be shown by default
		// User must explicitly choose to include them
		for k, v := range sourceAgent.Env {
			m.envVars[k] = v
		}
		m.updateEnvVarKeys()
	}

	return m
}

func (m *wizardModel) updateEnvVarKeys() {
	m.envVarKeys = make([]string, 0, len(m.envVars))
	for k := range m.envVars {
		m.envVarKeys = append(m.envVarKeys, k)
	}
	sort.Strings(m.envVarKeys)
}

func thinkingToOption(budget int64) string {
	switch {
	case budget == 0:
		return "disabled"
	case budget <= 10000:
		return "low"
	case budget <= 16000:
		return "medium"
	case budget <= 32000:
		return "high"
	case budget <= 64000:
		return "very_high"
	default:
		return "max"
	}
}

func (m *wizardModel) updateModelsForProvider() {
	pf := provider.NewFactory()
	providerInfos := pf.ListProviders()

	for _, p := range providerInfos {
		if p.ID == m.provider {
			m.models = p.Models
			if len(m.models) > 15 {
				m.models = m.models[:15]
			}
			if m.model == "" || !contains(m.models, m.model) {
				m.model = m.models[0]
			}
			// Set current selection to match current model
			for i, mod := range m.models {
				if mod == m.model {
					m.currentSelection = i
					break
				}
			}
			break
		}
	}
}

func (m *wizardModel) setCurrentSelection(i int) {
	if i < 0 {
		i = 0
	}
	m.currentSelection = i
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (m *wizardModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEnter:
			return m.handleEnter()
		case tea.KeyTab:
			return m.handleTab()
		case tea.KeyShiftTab:
			return m.handleShiftTab()
		case tea.KeyUp:
			return m.handleUp()
		case tea.KeyDown:
			return m.handleDown()
		case tea.KeyDelete:
			if m.step == 5 && len(m.envVarKeys) > 0 && m.currentSelection < len(m.envVarKeys) {
				key := m.envVarKeys[m.currentSelection]
				m.removeEnvVar(key)
				if m.currentSelection >= len(m.envVarKeys) && m.currentSelection > 0 {
					m.currentSelection = len(m.envVarKeys) - 1
				}
			}
		case tea.KeyRunes:
			if m.step == 5 && len(msg.Runes) > 0 {
				switch msg.Runes[0] {
				case 'd', 'D':
					m.showEnvValues = !m.showEnvValues
				}
			}
		}
	}
	return m, nil
}


func (m *wizardModel) handleEnter() (tea.Model, tea.Cmd) {
	switch m.step {
	case 0: // Name
		if m.nameInput.Value() != "" {
			m.name = m.nameInput.Value()
			m.step++
		}
	case 1: // Provider
		m.step++
		m.currentSelection = 0
	case 2: // Model
		m.step++
	case 3: // Thinking
		m.step++
	case 4: // System prompt
		m.systemPrompt = m.promptInput.Placeholder
		if m.promptInput.Value() != "" {
			m.systemPrompt = m.promptInput.Value()
		}
		m.step++
	case 5: // Env vars
		m.step++
	case 6: // Job name
		if m.jobNameInput.Value() != "" {
			m.jobName = m.jobNameInput.Value()
			m.step++
		}
	case 7: // Job schedule
		if m.jobScheduleInput.Value() != "" {
			m.jobSchedule = m.jobScheduleInput.Value()
			return m, m.saveAgent
		}
	}
	return m, nil
}

func (m *wizardModel) handleTab() (tea.Model, tea.Cmd) {
	switch m.step {
	case 1: // Provider
		m.setCurrentSelection((m.currentSelection + 1) % len(m.providers))
		m.provider = m.providers[m.currentSelection]
		m.updateModelsForProvider()
	case 2: // Model
		if len(m.models) > 0 {
			m.setCurrentSelection((m.currentSelection + 1) % len(m.models))
			m.model = m.models[m.currentSelection]
		}
	case 3: // Thinking
		m.setCurrentSelection((m.currentSelection + 1) % len(m.thinkingOpts))
		m.thinking = m.thinkingOpts[m.currentSelection]
	}
	return m, nil
}

func (m *wizardModel) handleShiftTab() (tea.Model, tea.Cmd) {
	switch m.step {
	case 1:
		m.currentSelection--
		if m.currentSelection < 0 {
			m.currentSelection = len(m.providers) - 1
		}
		m.provider = m.providers[m.currentSelection]
		m.updateModelsForProvider()
	case 2:
		if len(m.models) > 0 {
			m.currentSelection--
			if m.currentSelection < 0 {
				m.currentSelection = len(m.models) - 1
			}
			m.model = m.models[m.currentSelection]
		}
	case 3:
		m.currentSelection--
		if m.currentSelection < 0 {
			m.currentSelection = len(m.thinkingOpts) - 1
		}
		m.thinking = m.thinkingOpts[m.currentSelection]
	}
	return m, nil
}

func (m *wizardModel) handleUp() (tea.Model, tea.Cmd) {
	return m.handleShiftTab()
}

func (m *wizardModel) handleDown() (tea.Model, tea.Cmd) {
	return m.handleTab()
}

func (m *wizardModel) addEnvVar() {
	key := m.envKeyInput.Value()
	value := m.envValueInput.Value()
	if key != "" {
		m.envVars[key] = value
		m.updateEnvVarKeys()
	}
	m.envKeyInput.SetValue("")
	m.envValueInput.SetValue("")
}

func (m *wizardModel) removeEnvVar(key string) {
	delete(m.envVars, key)
	m.updateEnvVarKeys()
}

func (m *wizardModel) saveAgent() tea.Msg {
	agent := &config.Agent{
		Name:     m.name,
		Provider: m.provider,
		Model:    m.model,
		Body:     m.systemPrompt,
		Env:      m.envVars,
		Jobs:     map[string]string{m.jobName: m.jobSchedule},
	}

	if m.thinking != "disabled" {
		budget := config.ThinkingBudgets[m.thinking]
		agent.Thinking = budget
	}

	content := formatAgentMarkdown(agent)
	filename := filepath.Join(agentsDir, m.name+".md")

	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	m.resultFile = filename
	m.done = true
	return nil
}

func (m *wizardModel) View() string {
	if m.quitting {
		return "Cancelled.\n"
	}

	var s strings.Builder

	s.WriteString(titleStyle.Render("Create New Agent"))
	s.WriteString(fmt.Sprintf("\nStep %d of %d\n\n", m.step+1, m.totalSteps))

	switch m.step {
	case 0:
		s.WriteString(fieldStyle.Render("Agent name:") + "\n")
		s.WriteString(m.nameInput.View())

	case 1:
		s.WriteString(fieldStyle.Render("Provider (Tab/Shift+Tab to change):") + "\n")
		for i, p := range m.providers {
			prefix := "  "
			name := m.providerNames[i]
			if p == m.provider {
				prefix = selectedStyle.Render("▶ ")
			}
			s.WriteString(fmt.Sprintf("%s%s (%s)\n", prefix, name, p))
		}

	case 2:
		s.WriteString(fieldStyle.Render("Model (Tab/Shift+Tab to change):") + "\n")
		for _, mod := range m.models {
			prefix := "  "
			if mod == m.model {
				prefix = selectedStyle.Render("▶ ")
			}
			s.WriteString(fmt.Sprintf("%s%s\n", prefix, mod))
		}

	case 3:
		s.WriteString(fieldStyle.Render("Thinking effort (Tab/Shift+Tab to change):") + "\n")
		for _, opt := range m.thinkingOpts {
			prefix := "  "
			if opt == m.thinking {
				prefix = selectedStyle.Render("▶ ")
			}
			s.WriteString(fmt.Sprintf("%s%s\n", prefix, opt))
		}

	case 4:
		s.WriteString(fieldStyle.Render("System prompt (press Enter for default):") + "\n")
		s.WriteString(m.promptInput.View())

	case 5:
		s.WriteString(fieldStyle.Render("Environment variables:") + "\n\n")
		if len(m.envVars) == 0 {
			s.WriteString(fieldStyle.Render("  (no env vars defined)") + "\n")
		} else {
			for _, key := range m.envVarKeys {
				value := m.envVars[key]
				if !m.showEnvValues {
					value = "********"
				}
				s.WriteString(fmt.Sprintf("  %s=%s", key, value))
				s.WriteString(helpStyle.Render(" [del]"))
				s.WriteString("\n")
			}
		}
		s.WriteString("\n")
		s.WriteString(fieldStyle.Render("Add env var:") + "\n")
		s.WriteString("Key: ")
		s.WriteString(m.envKeyInput.View())
		s.WriteString("\n")
		s.WriteString("Value: ")
		s.WriteString(m.envValueInput.View())
		s.WriteString("\n")
		s.WriteString(helpStyle.Render("(Press Enter to add, d to toggle value visibility, Del to remove selected)"))

	case 6:
		s.WriteString(fieldStyle.Render("First job name:") + "\n")
		s.WriteString(m.jobNameInput.View())

	case 7:
		s.WriteString(fieldStyle.Render("First job schedule:") + "\n")
		s.WriteString(m.jobScheduleInput.View())
		s.WriteString("\n")
		s.WriteString(helpStyle.Render("Examples: @every 30s, @once, 0 0 * * * * (cron)"))
	}

	s.WriteString("\n\n")
	s.WriteString(helpStyle.Render("Tab/Shift+Tab: change selection | Enter: next | Ctrl+C: cancel"))

	return s.String()
}

func formatAgentMarkdown(a *config.Agent) string {
	var s strings.Builder
	s.WriteString("---\n")
	s.WriteString(fmt.Sprintf("name: %s\n", a.Name))
	s.WriteString(fmt.Sprintf("provider: %s\n", a.Provider))
	s.WriteString(fmt.Sprintf("model: %s\n", a.Model))
	if a.GetThinkingBudget() > 0 {
		s.WriteString(fmt.Sprintf("thinking: %d\n", a.GetThinkingBudget()))
	}
	if len(a.Env) > 0 {
		s.WriteString("env:\n")
		keys := make([]string, 0, len(a.Env))
		for k := range a.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s.WriteString(fmt.Sprintf("  %s: \"%s\"\n", k, a.Env[k]))
		}
	}
	s.WriteString("jobs:\n")
	for job, schedule := range a.Jobs {
		s.WriteString(fmt.Sprintf("  %s: \"%s\"\n", job, schedule))
	}
	s.WriteString("---\n\n")
	s.WriteString(a.Body)
	return s.String()
}
