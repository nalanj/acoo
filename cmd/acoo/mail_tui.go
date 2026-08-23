package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

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
	composeTo        string
	composeSubject  string
	composeBody     string
	composeField    int    // 0=to, 1=subject, 2=body
	composeCursor   int    // cursor position within current field
	composeError    string

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

	return &model{
		cfg:        cfg,
		store:      mail.NewStore(cfg.MessagesDir),
		inboxMgr:   mail.NewInboxManager(cfg.MessagesDir),
		archiveMgr: mail.NewArchiveManager(cfg.MessagesDir, cfg.MailRoot),
		view:       viewInbox,
	}, nil
}

func (m *model) Init() tea.Cmd {
	// Don't load inbox here - wait for WindowSizeMsg to set dimensions first
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport = viewport.New(msg.Width, msg.Height-3)
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
		if m.view != viewCompose {
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
			m.startCompose("", "", "")
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
	case "a":
		m.archiveMessage(m.selectedID)
		m.view = viewInbox
		m.loadInbox()
	case "q":
		m.view = viewInbox
	}
	return m, nil
}

func (m *model) handleComposeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle character input for text fields
	if len(msg.Runes) > 0 && m.composeField < 2 {
		r := msg.Runes[0]
		switch {
		case r == '\n':
			// Enter moves to next field
			if m.composeField < 2 {
				m.composeField++
				m.composeCursor = 0
			}
		case r == '\b':
			// Backspace
			field := m.getComposeField()
			if m.composeCursor > 0 && len(field) > 0 {
				newField := field[:m.composeCursor-1] + field[m.composeCursor:]
				m.setComposeField(newField)
				m.composeCursor--
			}
		default:
			// Insert character at cursor
			field := m.getComposeField()
			if m.composeCursor >= len(field) {
				field += string(r)
			} else {
				field = field[:m.composeCursor] + string(r) + field[m.composeCursor:]
			}
			m.setComposeField(field)
			m.composeCursor++
		}
		return m, nil
	}

	switch msg.String() {
	case "tab":
		m.composeField = (m.composeField + 1) % 3
		m.composeCursor = len(m.getComposeField())
	case "enter":
		if m.composeField < 2 {
			m.composeField++
			m.composeCursor = len(m.getComposeField())
		}
	case "ctrl+r":
		body, err := m.editInEditor(m.composeBody)
		if err == nil {
			m.composeBody = body
		}
	case "ctrl+enter", "ctrl+s":
		return m, m.sendCompose
	case "esc":
		m.view = viewInbox
	case "left":
		if m.composeCursor > 0 {
			m.composeCursor--
		}
	case "right":
		if m.composeCursor < len(m.getComposeField()) {
			m.composeCursor++
		}
	case "home", "ctrl+a":
		m.composeCursor = 0
	case "end", "ctrl+e":
		m.composeCursor = len(m.getComposeField())
	}
	return m, nil
}

func (m *model) getComposeField() string {
	switch m.composeField {
	case 0:
		return m.composeTo
	case 1:
		return m.composeSubject
	default:
		return m.composeBody
	}
}

func (m *model) setComposeField(value string) {
	switch m.composeField {
	case 0:
		m.composeTo = value
	case 1:
		m.composeSubject = value
	default:
		m.composeBody = value
	}
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

func (m *model) startCompose(to, subject, body string) {
	m.composeTo = to
	m.composeSubject = subject
	m.composeBody = body
	m.composeField = 0
	m.composeCursor = len(to)
	m.composeError = ""
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

	m.startCompose(msg.From, subject, replyBody)
}

func (m *model) sendCompose() tea.Msg {
	if m.composeTo == "" || m.composeSubject == "" || m.composeBody == "" {
		m.composeError = "All fields required"
		return nil
	}

	msg := &mail.Message{
		ID:        mail.GenerateID(),
		From:      m.cfg.AgentName(),
		To:        []string{m.composeTo},
		Subject:   m.composeSubject,
		Timestamp: time.Now().UTC(),
		Body:      m.composeBody,
		Thread:    mail.GenerateID(),
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

func (m *model) editInEditor(content string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	tmpfile, err := os.CreateTemp("", "acoo-compose-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpfile.Name())
	defer tmpfile.Close()

	if _, err := tmpfile.WriteString(content); err != nil {
		return "", err
	}
	tmpfile.Close()

	// Exit alternate screen before running editor
	fmt.Print("\033[?1049l")

	execCmd := exec.Command(editor, tmpfile.Name())
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	if err := execCmd.Run(); err != nil {
		// Re-enter alternate screen on error
		fmt.Print("\033[?1049h")
		return "", err
	}

	// Re-enter alternate screen
	fmt.Print("\033[?1049h")

	data, err := os.ReadFile(tmpfile.Name())
	if err != nil {
		return "", err
	}

	return string(data), nil
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
	b.WriteString(titleStyle.Render("New Message"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Fill in fields below. Type to edit. Tab=next field.\n"))
	b.WriteString(helpStyle.Render("─────────────────────────────────────────────────────\n"))

	fields := []string{m.composeTo, m.composeSubject, m.composeBody}
	labels := []string{"To:", "Subject:", "Body:"}
	placeholders := []string{"[recipient]", "[subject]", "[body - Ctrl+R to edit]"}

	for i := range fields {
		label := labels[i]
		value := fields[i]
		placeholder := placeholders[i]
		isActive := i == m.composeField

		prefix := "  "
		if isActive {
			prefix = "▸ "
		}

		if isActive {
			if value == "" {
				b.WriteString(titleStyle.Render(prefix))
				b.WriteString(itemStyle.Render(fmt.Sprintf("%-10s%s", label, helpStyle.Render(placeholder))))
			} else {
				b.WriteString(titleStyle.Render(prefix))
				b.WriteString(itemStyle.Render(fmt.Sprintf("%-10s%s", label, value)))
			}
		} else {
			if value == "" {
				b.WriteString(helpStyle.Render(fmt.Sprintf("%s%-10s%s", prefix, label, placeholder)))
			} else {
				b.WriteString(helpStyle.Render(fmt.Sprintf("%s%-10s", prefix, label)))
				b.WriteString(itemStyle.Render(value))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("─────────────────────────────────────────────────────\n"))

	if m.composeError != "" {
		b.WriteString(statusErrStyle.Render("Error: " + m.composeError))
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("\nCtrl+R=edit body in $EDITOR | Ctrl+S=send | q=cancel\n"))
}

func (m *model) renderHelp(b *strings.Builder) {
	helpText := `
Navigation:
  i         Go to inbox
  a         Go to archive
  t         Go to threads
  n         New message
  ?         Toggle this help

List view:
  j/k/↑/↓   Navigate
  g/G       Go to top/bottom
  Enter     Open message
  d         Archive selected
  r         Reply to selected

Message view:
  r         Reply
  a         Archive
  q         Back to list

Compose:
  Tab       Next field
  Ctrl+R    Edit body in $EDITOR
  Ctrl+S    Send
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
		footer = "q:quit i:inbox a:archive t:threads n:new ?/help"
	case viewArchive:
		footer = "q:quit i:inbox a:archive t:threads n:new ?/help"
	case viewThreads:
		footer = "q:quit i:inbox a:archive t:threads n:new ?/help"
	case viewMessage:
		footer = "q:back r:reply a:archive"
	case viewCompose:
		footer = "Tab:field Ctrl+R:edit Ctrl+S:send q:cancel"
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

func init() {
	// Ensure runewidth uses correct terminal width
	runewidth.DefaultCondition.EastAsianWidth = false
}
