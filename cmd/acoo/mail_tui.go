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

	statusOKStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9ece6a"))

	statusErrStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f7768e"))
)

// View constants
const (
	viewInbox    = "inbox"
	viewArchive  = "archive"
	viewThreads  = "threads"
	viewMessage  = "message"
	viewCompose  = "compose"
	viewHelp     = "help"
)

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
	threads    []*mail.Thread
	selectedID string

	// Compose state
	composeTo       textinput.Model
	composeSubject  textinput.Model
	composeBody     textarea.Model
	composeError    string
	composeThread   string  // Thread ID for replies
	composeParent   string  // Parent message ID for replies

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
	composeTo.Focus()
	composeTo.PromptStyle = titleStyle
	composeTo.TextStyle = itemStyle

	composeSubject := textinput.New()
	composeSubject.Placeholder = "subject"
	composeSubject.PromptStyle = titleStyle
	composeSubject.TextStyle = itemStyle

	composeBody := textarea.New()
	composeBody.Placeholder = "message body..."

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
	// Don't load inbox here - wait for WindowSizeMsg to set dimensions first
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle compose mode keys before text inputs see them
	if m.view == viewCompose {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			// Handle Tab, Enter, Ctrl+Enter, Esc, q BEFORE text inputs
			if keyMsg.String() == "tab" || keyMsg.String() == "enter" ||
				keyMsg.String() == "ctrl+enter" || keyMsg.String() == "ctrl+j" ||
				keyMsg.String() == "esc" || keyMsg.String() == "q" {
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

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.QuitMsg:
		m.quitting = true
		return m, nil
	}

	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys work in any view
	switch msg.String() {
	case "q", "ctrl+c":
		if m.view == viewMessage || m.view == viewCompose {
			m.view = viewInbox
			return m, nil
		}
		return m, tea.Quit

	case "?":
		if m.view == viewHelp {
			m.view = viewInbox
		} else {
			m.view = viewHelp
		}
		return m, nil

	case "i":
		if m.view != viewCompose {
			m.view = viewInbox
			m.loadInbox()
		}
		return m, nil

	case "a":
		if m.view == viewMessage {
			// Archive the current message
			m.archiveMessage(m.selectedID)
			m.view = viewInbox
			m.loadInbox()
		} else if m.view != viewCompose {
			m.view = viewArchive
			m.loadArchive()
		}
		return m, nil

	case "t":
		if m.view != viewCompose {
			m.view = viewThreads
			m.loadThreads()
		}
		return m, nil

	case "n":
		if m.view != viewCompose {
			m.startCompose("", "", "", "", "")
		}
		return m, nil
	}

	// View-specific keys
	switch m.view {
	case viewInbox, viewArchive:
		return m.handleListKey(msg)

	case viewThreads:
		return m.handleThreadsKey(msg)

	case viewMessage:
		return m.handleMessageKey(msg)

	case viewCompose:
		return m.handleComposeKey(msg)
	}

	return m, nil
}

func (m *model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.list.CursorDown()
	case "k", "up":
		m.list.CursorUp()
	case "g":
		m.list.GoToStart()
	case "G":
		m.list.GoToEnd()
	case "enter":
		if m.list.SelectedItem() != nil {
			item := m.list.SelectedItem().(MailItem)
			m.selectedID = item.id
			m.loadMessage(item.id)
			m.view = viewMessage
		}
	case "d":
		if m.list.SelectedItem() != nil {
			item := m.list.SelectedItem().(MailItem)
			m.archiveMessage(item.id)
			m.loadInbox()
		}
	case "r":
		if m.list.SelectedItem() != nil {
			item := m.list.SelectedItem().(MailItem)
			m.startReply(item.id)
		}
	}
	return m, nil
}

func (m *model) handleThreadsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.list.CursorDown()
	case "k", "up":
		m.list.CursorUp()
	case "g":
		m.list.GoToStart()
	case "G":
		m.list.GoToEnd()
	case "enter":
		if m.list.SelectedItem() != nil {
			item := m.list.SelectedItem().(MailItem)
			m.selectedID = item.id
			m.loadMessage(item.id)
			m.view = viewMessage
		}
	}
	return m, nil
}

func (m *model) handleMessageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		m.startReply(m.selectedID)
	case "q":
		m.view = viewInbox
	}
	return m, nil
}

func (m *model) handleComposeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		// Cycle focus between fields
		if m.composeTo.Focused() {
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
	} else if m.view == viewThreads {
		listTitle = "THREADS"
	}

	m.list = list.New(items, delegate, m.width, m.height-3)
	m.list.Title = listTitle
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
		b.WriteString(msg.Body)
	}

	m.viewport.SetContent(b.String())
}

func (m *model) loadThreads() {
	threads, err := m.store.ListThreadsForRecipient(m.cfg.AgentName())
	if err != nil {
		m.threads = nil
		return
	}
	m.threads = threads

	items := make([]list.Item, len(threads))
	for i, t := range threads {
		items[i] = MailItem{
			id:      t.ID,
			from:    fmt.Sprintf("%d messages", t.MessageCount),
			subject: t.Subject,
			date:    t.LastMessage.Format("Jan 02 15:04"),
		}
	}
	m.list = list.New(items, m.newStyledDelegate(), m.width, m.height-3)
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
}

func (m *model) startReply(id string) {
	msg, err := m.store.Load(id)
	if err != nil {
		return
	}
	replyBody := "\n\n> " + strings.ReplaceAll(msg.Body, "\n", "\n> ")

	subject := msg.Subject
	if !strings.HasPrefix(subject, "Re: ") {
		subject = "Re: " + subject
	}

	// Use the parent message's thread ID
	threadID := msg.Thread
	if threadID == "" {
		threadID = msg.ID
	}

	m.startCompose(msg.From, subject, replyBody, threadID, msg.ID)
}

func (m *model) sendCompose() tea.Msg {
	to := m.composeTo.Value()
	subject := m.composeSubject.Value()
	body := m.composeBody.Value()

	if to == "" || subject == "" || body == "" {
		m.composeError = "All fields required"
		return nil
	}

	// Use existing thread ID if replying, otherwise generate new thread
	threadID := m.composeThread
	if threadID == "" {
		threadID = mail.GenerateID()
	}

	msg := &mail.Message{
		ID:        mail.GenerateID(),
		From:      m.cfg.AgentName(),
		To:        []string{to},
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
		m.composeError = err.Error()
		return nil
	}

	m.view = viewInbox
	m.loadInbox()
	return nil
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
	case viewThreads:
		m.renderThreads(&b)
	case viewMessage:
		m.renderMessage(&b)
	case viewCompose:
		m.renderCompose(&b)
	case viewHelp:
		m.renderHelp(&b)
	}

	// Footer
	m.renderFooter(&b)

	return b.String()
}

func (m *model) renderHeader(b *strings.Builder) {
	viewNames := map[string]string{
		viewInbox:   "INBOX",
		viewArchive: "ARCHIVE",
		viewThreads: "THREADS",
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

func (m *model) renderThreads(b *strings.Builder) {
	b.WriteString(m.list.View())
}

func (m *model) renderMessage(b *strings.Builder) {
	b.WriteString(m.viewport.View())
}

func (m *model) renderCompose(b *strings.Builder) {
	b.WriteString(titleStyle.Render("New Message\n"))
	b.WriteString(borderStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// To field
	b.WriteString(titleStyle.Render("To: "))
	b.WriteString(m.composeTo.View())
	b.WriteString("\n")

	// Subject field
	b.WriteString(titleStyle.Render("Subject: "))
	b.WriteString(m.composeSubject.View())
	b.WriteString("\n")

	// Body field
	b.WriteString(titleStyle.Render("Body:\n"))
	b.WriteString(m.composeBody.View())
	b.WriteString("\n")

	if m.composeError != "" {
		b.WriteString(statusErrStyle.Render("Error: " + m.composeError))
		b.WriteString("\n")
	}
}

func (m *model) renderHelp(b *strings.Builder) {
	helpText := `
Views:
  i         Inbox
  a         Archive
  t         Threads
  n         New message
  ?         Toggle this help

List navigation:
  j/k/↑/↓   Navigate
  g/G       Top/bottom

List actions:
  Enter     View message
  d         Archive selected
  r         Reply to selected

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
		footer = "i:inbox a:archive t:threads | j/k:up/down d:archive r:reply n:new q:quit ?/help"
	case viewArchive:
		footer = "i:inbox a:archive t:threads | j/k:up/down d:archive n:new q:quit"
	case viewThreads:
		footer = "i:inbox t:threads | j/k:up/down n:new q:quit"
	case viewMessage:
		footer = "r:reply a:archive | q:back"
	case viewCompose:
		footer = "Tab:field Enter:newline Ctrl+J:send | q:cancel"
	case viewHelp:
		footer = "q:back"
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
