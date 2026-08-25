package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wrap"

	"github.com/nalanj/acoo/internal/mail"
)

// Styles
var (
	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626882")).
			Background(lipgloss.Color("#1a1b26"))

	itemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c0caf5"))

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#bb9af7")).
				Background(lipgloss.Color("#24283b"))

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7aa2f7"))

	borderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#414868"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#565f89"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#565f89"))

	statusOKStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9ece6a"))

	statusErrStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f7768e"))

	modalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7aa2f7")).
			Background(lipgloss.Color("#1a1b26")).
			Foreground(lipgloss.Color("#c0caf5")).
			Padding(1, 2)
)

// View constants
const (
	viewInbox    = "inbox"
	viewArchive  = "archive"
	viewMessage  = "message"
	viewCompose  = "compose"
	viewHelp     = "help"
	viewGoto     = "goto"
)

const refreshInterval = 5 * time.Second

// MailItem for list
type MailItem struct {
	id      string
	from    string
	subject string
	date    string
}

func (m MailItem) Title() string       { return m.subject }
func (m MailItem) Description() string { return fmt.Sprintf("%s | %s", m.from, m.date) }
func (m MailItem) FilterValue() string { return m.subject + " " + m.from }

type model struct {
	cfg       *mail.Config
	store     *mail.Store
	inboxMgr  *mail.InboxManager
	archiveMgr *mail.ArchiveManager

	view       string
	list       list.Model
	viewport   viewport.Model
	messages   []*mail.Message
	selectedID string
	refreshTick int // Counter to trigger list refresh
	prevView    string // Previous view, used when canceling from goto modal

	// Compose state
	composeTo       textinput.Model
	composeSubject  textinput.Model
	composeBody     textarea.Model
	composeError    string
	composeThread   string  // Thread ID for replies
	composeParent   string  // Parent message ID for replies
	composeAgents   []string  // Available agents for autocomplete
	composeMatchIdx int      // Index of currently selected autocomplete match

	quitting bool
	width    int
	height   int
}

func newModel() (*model, error) {
	cfg, err := mail.Default()
	if err != nil {
		return nil, err
	}

	if err := cfg.EnsureDirs(); err != nil {
		return nil, err
	}

	composeTo := textinput.New()
	composeTo.Placeholder = "recipient"
	composeTo.Prompt = ""
	composeTo.Focus()

	composeSubject := textinput.New()
	composeSubject.Placeholder = "subject"
	composeSubject.Prompt = ""

	composeBody := textarea.New()
	composeBody.Placeholder = "message body..."
	composeBody.ShowLineNumbers = false

	return &model{
		cfg:             cfg,
		store:           mail.NewStore(cfg.MessagesDir),
		inboxMgr:        mail.NewInboxManager(cfg.MessagesDir),
		archiveMgr:      mail.NewArchiveManager(cfg.MessagesDir, cfg.MailRoot),
		view:            viewInbox,
		composeTo:       composeTo,
		composeSubject:  composeSubject,
		composeBody:     composeBody,
	}, nil
}

func (m *model) Init() tea.Cmd {
	// Start a ticker to refresh lists periodically
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg{t}
	})
}

type tickMsg struct {
	time time.Time
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle compose mode keys before text inputs see them
	if m.view == viewCompose {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			keyStr := keyMsg.String()
			if keyStr == "tab" || keyStr == "enter" || keyStr == "ctrl+enter" ||
				keyStr == "ctrl+j" || keyStr == "right" || keyStr == "esc" || keyStr == "q" {
				return m.handleComposeKey(keyMsg)
			}
		}
		// Update text inputs
		m.composeTo, _ = m.composeTo.Update(msg)
		m.composeSubject, _ = m.composeSubject.Update(msg)
		m.composeBody, _ = m.composeBody.Update(msg)
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport = viewport.New(msg.Width, msg.Height-3)
		m.composeBody.SetWidth(msg.Width - 4)
		m.composeBody.SetHeight(msg.Height - 15)
		m.composeTo.Width = msg.Width - 10
		m.composeSubject.Width = msg.Width - 12
		// Load inbox now that we have dimensions
		if m.view == viewInbox {
			m.loadInbox()
		}
		return m, nil

	case tickMsg:
		// Refresh lists periodically but preserve selection
		selectedID := ""
		if m.list.SelectedItem() != nil {
			selectedID = m.list.SelectedItem().(MailItem).id
		}

		switch m.view {
		case viewInbox:
			m.loadInbox()
		case viewArchive:
			m.loadArchive()
		}

		// Restore selection if the item still exists
		if selectedID != "" {
			for i, msg := range m.messages {
				if msg.ID == selectedID {
					m.list.Select(i)
					break
				}
			}
		}

		// Continue ticking
		return m, tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
			return tickMsg{t}
		})

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.QuitMsg:
		m.quitting = true
		return m, nil
	}

	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// View-specific keys
	switch m.view {
	case viewInbox:
		return m.handleInboxKey(msg)

	case viewArchive:
		return m.handleArchiveKey(msg)

	case viewMessage:
		return m.handleMessageKey(msg)

	case viewCompose:
		return m.handleComposeKey(msg)

	case viewHelp:
		return m.handleHelpKey(msg)

	case viewGoto:
		return m.handleGotoKey(msg)
	}

	return m, nil
}

func (m *model) handleInboxKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.list.CursorDown()
	case "k", "up":
		m.list.CursorUp()
	case "g":
		m.prevView = viewInbox
		m.view = viewGoto
	case "G":
		m.list.GoToEnd()
	case "enter":
		if m.list.SelectedItem() != nil {
			item := m.list.SelectedItem().(MailItem)
			m.selectedID = item.id
			m.loadMessage(item.id)
			m.view = viewMessage
		}
	case "a", "d":
		if m.list.SelectedItem() != nil {
			item := m.list.SelectedItem().(MailItem)
			m.archiveMessage(item.id)
			m.loadInbox()
		}
	case "r":
		m.view = viewArchive
		m.loadArchive()
	case "i", "u":
		// Stay in inbox (refresh)
		m.loadInbox()
	case "c":
		m.startCompose("", "", "", "", "")
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.view = viewHelp
	}
	return m, nil
}

func (m *model) handleArchiveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.list.CursorDown()
	case "k", "up":
		m.list.CursorUp()
	case "g":
		m.prevView = viewArchive
		m.view = viewGoto
	case "G":
		m.list.GoToEnd()
	case "enter":
		if m.list.SelectedItem() != nil {
			item := m.list.SelectedItem().(MailItem)
			m.selectedID = item.id
			m.loadMessage(item.id)
			m.view = viewMessage
		}
	case "a", "d":
		if m.list.SelectedItem() != nil {
			item := m.list.SelectedItem().(MailItem)
			m.archiveMessage(item.id)
			m.loadArchive()
		}
	case "r":
		// Stay in archive (refresh)
		m.loadArchive()
	case "i", "u":
		// Go to inbox
		m.view = viewInbox
		m.loadInbox()
	case "c":
		m.startCompose("", "", "", "", "")
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.view = viewHelp
	}
	return m, nil
}

func (m *model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "?", "ctrl+c":
		m.view = viewInbox
	}
	return m, nil
}

func (m *model) handleMessageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		m.startReply(m.selectedID)
	case "a":
		m.archiveMessage(m.selectedID)
		m.view = viewInbox
		m.loadInbox()
	case "q":
		m.view = viewInbox
	case "?":
		m.view = viewHelp
	}
	return m, nil
}

func (m *model) handleGotoKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "i", "u":
		m.view = viewInbox
		m.loadInbox()
	case "a":
		m.view = viewArchive
		m.loadArchive()
	case "q", "esc", "ctrl+c":
		// Cancel, go back to previous view
		m.view = m.prevView
	}
	return m, nil
}

func (m *model) handleComposeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "right":
		// Forward: To -> Subject -> Body
		if m.composeTo.Focused() {
			input := m.composeTo.Value()
			if input != "" {
				prefix := strings.ToLower(input)
				for _, agent := range m.composeAgents {
					if strings.HasPrefix(strings.ToLower(agent), prefix) {
						m.composeTo.SetValue(agent)
						break
					}
				}
			}
			m.composeTo.Blur()
			m.composeSubject.Focus()
		} else if m.composeSubject.Focused() {
			m.composeSubject.Blur()
			m.composeBody.Focus()
		} else {
			m.composeBody.Blur()
			m.composeTo.Focus()
		}
	case "enter", "ctrl+enter", "ctrl+j":
		// Send the message
		return m, m.sendCompose
	case "esc", "q":
		m.view = viewInbox
		m.composeTo.Blur()
		m.composeSubject.Blur()
		m.composeBody.Blur()
		m.composeTo.Focus()
	}
	return m, nil
}

func (m *model) loadInbox() {
	messages, _ := m.loadMessagesForRecipient("")
	m.messages = messages
	m.updateList()
}

func (m *model) loadArchive() {
	messages, _ := m.loadArchivedMessages()
	m.messages = messages
	m.updateList()
}

func (m *model) loadMessagesForRecipient(folder string) ([]*mail.Message, error) {
	var ids []string
	var err error

	if folder == "archive" {
		archivePath := m.cfg.MailRoot + "/archive"
		entries, err := os.ReadDir(archivePath)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				ids = append(ids, strings.TrimSuffix(e.Name(), ".md"))
			}
		}
	} else {
		ids, err = m.inboxMgr.ListInboxMessages(m.cfg.AgentName())
		if err != nil {
			return nil, err
		}
	}

	var messages []*mail.Message
	for _, id := range ids {
		msg, err := m.store.Load(id)
		if err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func (m *model) loadArchivedMessages() ([]*mail.Message, error) {
	archivePath := m.cfg.MailRoot + "/archive"
	entries, err := os.ReadDir(archivePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var messages []*mail.Message
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			id := strings.TrimSuffix(e.Name(), ".md")
			msg, err := m.store.Load(id)
			if err != nil {
				continue
			}
			messages = append(messages, msg)
		}
	}
	return messages, nil
}

func (m *model) updateList() {
	items := make([]list.Item, len(m.messages))
	for i, msg := range m.messages {
		items[i] = MailItem{
			id:      msg.ID,
			from:    msg.From,
			subject: msg.Subject,
			date:    msg.Timestamp.Format("Jan 02 15:04"),
		}
	}
	delegate := list.NewDefaultDelegate()
	delegate.Styles = list.NewDefaultItemStyles()
	delegate.Styles.NormalTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5"))
	delegate.Styles.NormalDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("#bb9af7")).Background(lipgloss.Color("#24283b"))
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("#7dcfff")).Background(lipgloss.Color("#24283b"))
	delegate.ShowDescription = true

	// Set list title based on current view
	listTitle := "INBOX"
	if m.view == viewArchive {
		listTitle = "ARCHIVE"
	}

	m.list = list.New(items, delegate, m.width, m.height-3)
	m.list.Title = listTitle
	m.list.SetFilteringEnabled(false)
}

func (m *model) loadMessage(id string) {
	// Load full thread for context
	thread, err := m.store.GetThreadByMessageID(id)
	if err != nil {
		// Fallback to single message if thread load fails
		msg, err := m.store.Load(id)
		if err != nil {
			return
		}
		m.selectedID = id
		m.updateViewportForThread(&mail.Thread{
			ID:           id,
			Subject:      msg.Subject,
			MessageCount: 1,
			Messages:     []*mail.Message{msg},
		})
		return
	}
	m.selectedID = id
	m.updateViewportForThread(thread)
}

func (m *model) updateViewportForThread(thread *mail.Thread) {
	var b strings.Builder
	b.WriteString(titleStyle.Render(thread.Subject))
	b.WriteString(fmt.Sprintf(" (%d messages)\n", thread.MessageCount))
	b.WriteString(statusOKStyle.Render(strings.Repeat("─", m.width-2)))
	b.WriteString("\n\n")

	for i, msg := range thread.Messages {
		if i > 0 {
			b.WriteString("\n")
			b.WriteString(helpStyle.Render(strings.Repeat("─", 40)))
			b.WriteString("\n\n")
		}
		b.WriteString(fmt.Sprintf("%s %s | %s\n", statusOKStyle.Render("▎"), msg.From, msg.Timestamp.Format("2006-01-02 15:04")))
		b.WriteString(wrap.String(msg.Body, m.width-4))
	}

	m.viewport.SetContent(b.String())
}

func (m *model) newStyledDelegate() list.DefaultDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.Styles = list.NewDefaultItemStyles()
	delegate.Styles.NormalTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5"))
	delegate.Styles.NormalDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("#bb9af7")).Background(lipgloss.Color("#24283b"))
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("#7dcfff")).Background(lipgloss.Color("#24283b"))
	delegate.ShowDescription = true
	return delegate
}

func (m *model) startCompose(to, subject, body string, threadID string, parentID string) {
	m.composeTo.SetValue(to)
	m.composeSubject.SetValue(subject)
	m.composeBody.SetValue(body)
	m.composeError = ""
	m.composeThread = threadID
	m.composeParent = parentID
	m.composeTo.Focus()
	m.view = viewCompose

	// Load available agents for autocomplete
	m.composeAgents, _ = m.inboxMgr.ListAgents()
}

func (m *model) startReply(id string) {
	msg, err := m.store.Load(id)
	if err != nil {
		return
	}

	subject := msg.Subject
	if !strings.HasPrefix(subject, "Re: ") {
		subject = "Re: " + subject
	}

	// Use the parent message's thread ID
	threadID := msg.Thread
	if threadID == "" {
		threadID = msg.ID
	}

	m.startCompose(msg.From, subject, "", threadID, msg.ID)
}

func (m *model) sendCompose() tea.Msg {
	to := m.composeTo.Value()
	subject := m.composeSubject.Value()
	body := m.composeBody.Value()

	if to == "" || subject == "" || body == "" {
		m.composeError = "All fields required"
		return nil
	}

	// Parse comma-separated recipients
	recipients := strings.Split(to, ",")
	for i := range recipients {
		recipients[i] = strings.TrimSpace(recipients[i])
	}

	// Use existing thread ID if replying, otherwise generate new thread
	threadID := m.composeThread
	if threadID == "" {
		threadID = mail.GenerateID()
	}

	msg := &mail.Message{
		ID:        mail.GenerateID(),
		From:      m.cfg.AgentName(),
		To:        recipients,
		Subject:   subject,
		Timestamp: time.Now().UTC(),
		Body:      body,
		Thread:    threadID,
		Parent:    m.composeParent,
	}

	if err := m.store.Save(msg); err != nil {
		m.composeError = err.Error()
		return nil
	}

	if err := m.inboxMgr.AddToInboxes(msg); err != nil {
		// Clean up the saved message file since inbox creation failed
		m.store.Delete(msg.ID)
		m.composeError = err.Error()
		return nil
	}

	m.view = viewInbox
	m.loadInbox()
	// Return a tick to force re-render
	return tickMsg{time.Now()}
}

func (m *model) archiveMessage(id string) {
	m.archiveMgr.Archive(m.cfg.AgentName(), id)
}

func (m *model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Header bar
	m.renderHeader(&b)

	// Content
	switch m.view {
	case viewInbox:
		m.renderInbox(&b)
	case viewArchive:
		m.renderArchive(&b)
	case viewMessage:
		m.renderMessage(&b)
	case viewCompose:
		m.renderCompose(&b)
	case viewHelp:
		m.renderHelp(&b)
	case viewGoto:
		m.renderGoto(&b)
	}

	// Footer
	m.renderFooter(&b)

	return b.String()
}

func (m *model) renderHeader(b *strings.Builder) {
	viewNames := map[string]string{
		viewInbox:   "INBOX",
		viewArchive: "ARCHIVE",
		viewMessage: "MESSAGE",
		viewCompose: "COMPOSE",
		viewHelp:    "HELP",
	}

	currentView := viewNames[m.view]
	if m.selectedID != "" {
		currentView += " | " + m.selectedID[:17]
	}

	b.WriteString(headerStyle.Render(fmt.Sprintf(" Acoo Mail | %s | %s ", currentView, m.cfg.AgentName())))
	b.WriteString("\n")
	b.WriteString(borderStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")
}

func (m *model) renderInbox(b *strings.Builder) {
	b.WriteString(m.list.View())
}

func (m *model) renderArchive(b *strings.Builder) {
	b.WriteString(m.list.View())
}

func (m *model) renderMessage(b *strings.Builder) {
	b.WriteString(m.viewport.View())
}

func (m *model) renderCompose(b *strings.Builder) {
	b.WriteString(titleStyle.Render("  New Message"))
	b.WriteString("\n")

	// To field with inline autocomplete
	b.WriteString(titleStyle.Render("  To: "))
	input := m.composeTo.Value()
	if m.composeTo.Focused() && input != "" && len(m.composeAgents) > 0 {
		prefix := strings.ToLower(input)
		var match string
		for _, agent := range m.composeAgents {
			if strings.HasPrefix(strings.ToLower(agent), prefix) {
				match = agent
				break
			}
		}
		if match != "" {
			b.WriteString(itemStyle.Render(input))
			if len(match) > len(input) {
				b.WriteString(dimStyle.Render(match[len(input):]))
			}
		} else {
			b.WriteString(itemStyle.Render(input))
		}
	} else {
		b.WriteString(m.composeTo.View())
	}
	b.WriteString("\n")

	// Subject field
	b.WriteString(titleStyle.Render("  Subject: "))
	b.WriteString(m.composeSubject.View())

	// Body field
	b.WriteString(titleStyle.Render("  Body:"))
	b.WriteString("\n")
	b.WriteString(m.composeBody.View())
	b.WriteString("\n")

	if m.composeError != "" {
		b.WriteString(statusErrStyle.Render("Error: " + m.composeError))
		b.WriteString("\n")
	}
}

func (m *model) renderGoto(b *strings.Builder) {
	// Build modal box
	modalContent := modalStyle.
		Width(m.width - 10).
		Align(lipgloss.Center).
		Render(
			titleStyle.Render("Go to:") + "\n" +
			itemStyle.Render("  i  Inbox") + "\n" +
			itemStyle.Render("  a  Archive") + "\n" +
			dimStyle.Render("  q  Cancel"),
		)

	// Render the list we're coming from
	if m.prevView == viewInbox {
		m.renderInbox(b)
	} else {
		m.renderArchive(b)
	}

	// Position modal in center by moving cursor up
	// Calculate how many lines up to go to center the modal (modal is ~5 lines)
	upCount := (m.height - 5) / 2
	b.WriteString(fmt.Sprintf("\033[%dA", upCount))
	b.WriteString("\r")
	b.WriteString(fmt.Sprintf("\033[2C")) // Move right to center roughly
	b.WriteString(modalContent)
}

func (m *model) renderHelp(b *strings.Builder) {
	helpText := `
Go to:
  g         Show goto menu
  g i       Go to Inbox
  g a       Go to Archive
  ?         Toggle this help

List navigation:
  j/k/↑/↓   Navigate
  G         Bottom

List actions:
  Enter     View message
  a         Archive selected

Message view:
  r         Reply
  a         Archive
  q         Back to list

Compose:
  Tab       Next field
  Enter     Newline in body
  Ctrl+J    Send
  q/Esc     Cancel
`
	b.WriteString(itemStyle.Render(helpText))
}

func (m *model) renderFooter(b *strings.Builder) {
	b.WriteString(borderStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	footer := ""
	switch m.view {
	case viewInbox:
		footer = "g:go-to | j/k:up/down enter:view a:archive c:compose q:quit ?:help"
	case viewArchive:
		footer = "g:go-to | j/k:up/down enter:view a:archive c:compose q:quit ?:help"
	case viewMessage:
		footer = "r:reply a:archive | q:back ?:help"
	case viewCompose:
		footer = "Tab:field Enter:newline Ctrl+J:send | q:cancel"
	case viewHelp:
		footer = "q:back"
	case viewGoto:
		footer = "i:Inbox a:Archive | q:Cancel"
	}

	b.WriteString(helpStyle.Render(footer))
}

func runMailTUI() error {
	m, err := newModel()
	if err != nil {
		return err
	}

	p := tea.NewProgram(m,
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		return err
	}

	return nil
}
