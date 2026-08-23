package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/nalanj/acoo/internal/mail"
)

const (
	ioctlReadWinsize = 0x5411
)

var (
	mailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

func buildMailCommands() *cobra.Command {
	mailCmd := &cobra.Command{
		Use:   "mail",
		Short: "Mail commands",
	}

	mailCmd.AddCommand(
		mailInboxCmd(),
		mailSendCmd(),
		mailReadCmd(),
		mailReplyCmd(),
		mailArchiveCmd(),
		mailThreadsCmd(),
		mailThreadCmd(),
		mailAgentsCmd(),
		mailTUICmd(),
	)

	return mailCmd
}

func mailInboxCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inbox",
		Short: "List messages in inbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := mail.Default()
			if err != nil {
				return err
			}

			inboxMgr := mail.NewInboxManager(cfg.MessagesDir)
			store := mail.NewStore(cfg.MessagesDir)

			inboxIDs, err := inboxMgr.ListInboxMessages(cfg.AgentName())
			if err != nil {
				return err
			}

			if len(inboxIDs) == 0 {
				cmd.Println("No messages in inbox.")
				return nil
			}

			var messages []*mail.Message
			for _, id := range inboxIDs {
				msg, err := store.Load(id)
				if err != nil {
					continue
				}
				messages = append(messages, msg)
			}

			if len(messages) == 0 {
				cmd.Println("No messages in inbox.")
				return nil
			}

			for _, m := range messages {
				date := m.Timestamp.Format("2006-01-02 15:04")
				cmd.Printf("%s %s\n", mailStyle.Render("│"), m.ID)
				cmd.Printf("%s %s %s\n", mailStyle.Render("│"), styleBold.Render("From:"), m.From)
				cmd.Printf("%s %s %s\n", mailStyle.Render("│"), styleBold.Render("Subject:"), m.Subject)
				cmd.Printf("%s %s %s\n", mailStyle.Render("│"), styleBold.Render("Date:"), date)
				cmd.Println()
			}

			return nil
		},
	}
}

func mailSendCmd() *cobra.Command {
	var subject string
	var bodyFile string

	c := &cobra.Command{
		Use:   "send <recipient>",
		Short: "Send a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := mail.Default()
			if err != nil {
				return err
			}

			if err := cfg.EnsureDirs(); err != nil {
				return err
			}

			recipient := args[0]
			body := ""
			if bodyFile != "" {
				data, err := os.ReadFile(bodyFile)
				if err != nil {
					return err
				}
				body = string(data)
			} else {
				var err error
				body, err = editWithEditor("")
				if err != nil {
					return err
				}
			}

			msg := &mail.Message{
				ID:        mail.GenerateID(),
				From:      cfg.AgentName(),
				To:        []string{recipient},
				Subject:   subject,
				Timestamp: time.Now().UTC(),
				Body:      body,
				Thread:    mail.GenerateID(),
			}

			store := mail.NewStore(cfg.MessagesDir)
			if err := store.Save(msg); err != nil {
				return err
			}

			inboxMgr := mail.NewInboxManager(cfg.MessagesDir)
			if err := inboxMgr.AddToInboxes(msg); err != nil {
				return err
			}

			cmd.Println("Message sent:", msg.ID)
			return nil
		},
	}

	c.Flags().StringVarP(&subject, "subject", "s", "", "message subject")
	c.Flags().StringVarP(&bodyFile, "body", "b", "", "file containing message body")

	return c
}

func mailReadCmd() *cobra.Command {
	var noDecorate bool

	c := &cobra.Command{
		Use:   "read <message-id>",
		Short: "Read a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := mail.Default()
			if err != nil {
				return err
			}

			store := mail.NewStore(cfg.MessagesDir)
			msg, err := store.Load(args[0])
			if err != nil {
				return err
			}

			if noDecorate {
				cmd.Println(msg.Body)
				return nil
			}

			cmd.Printf("%s %s\n", mailStyle.Render("│"), msg.ID)
			cmd.Printf("%s %s %s\n", mailStyle.Render("│"), styleBold.Render("From:"), msg.From)
			cmd.Printf("%s %s %s\n", mailStyle.Render("│"), styleBold.Render("Subject:"), msg.Subject)
			cmd.Printf("%s %s %s\n", mailStyle.Render("│"), styleBold.Render("Date:"), msg.Timestamp.Format("2006-01-02 15:04"))
			if msg.Parent != "" {
				cmd.Printf("%s %s %s\n", mailStyle.Render("│"), styleBold.Render("Reply:"), msg.Parent)
			}
			cmd.Println()
			cmd.Println(msg.Body)
			cmd.Println()
			cmd.Println(styleDim.Render("────────────────────────────────────────────────────────────────"))
			cmd.Printf("  %s to reply, %s to archive\n", "acoo mail reply "+msg.ID, "acoo mail archive "+msg.ID)

			return nil
		},
	}

	c.Flags().BoolVarP(&noDecorate, "no-decorate", "n", false, "output raw message without headers")

	return c
}

func mailReplyCmd() *cobra.Command {
	var subject string
	var bodyFile string

	c := &cobra.Command{
		Use:   "reply <message-id>",
		Short: "Reply to a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := mail.Default()
			if err != nil {
				return err
			}

			if err := cfg.EnsureDirs(); err != nil {
				return err
			}

			store := mail.NewStore(cfg.MessagesDir)
			parent, err := store.Load(args[0])
			if err != nil {
				return err
			}

			body := ""
			if bodyFile != "" {
				data, err := os.ReadFile(bodyFile)
				if err != nil {
					return err
				}
				body = string(data)
			} else {
				body, err = editWithEditor("")
				if err != nil {
					return err
				}
			}

			replySubject := "Re: " + parent.Subject
			if subject != "" {
				replySubject = subject
			}

			msg := &mail.Message{
				ID:        mail.GenerateID(),
				From:      cfg.AgentName(),
				To:        []string{parent.From},
				Subject:   replySubject,
				Thread:    parent.Thread,
				Parent:    parent.ID,
				Timestamp: time.Now().UTC(),
				Body:      body,
			}

			if msg.Thread == "" {
				msg.Thread = msg.ID
			}

			if err := store.Save(msg); err != nil {
				return err
			}

			inboxMgr := mail.NewInboxManager(cfg.MessagesDir)
			if err := inboxMgr.AddToInboxes(msg); err != nil {
				return err
			}

			cmd.Println("Reply sent:", msg.ID)
			return nil
		},
	}

	c.Flags().StringVarP(&subject, "subject", "s", "", "message subject (auto-generated if not provided)")
	c.Flags().StringVarP(&bodyFile, "body", "b", "", "file containing message body")

	return c
}

func mailArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive <message-id>",
		Short: "Archive a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := mail.Default()
			if err != nil {
				return err
			}

			store := mail.NewStore(cfg.MessagesDir)
			msg, err := store.Load(args[0])
			if err != nil {
				return err
			}

			archive := mail.NewArchiveManager(cfg.MessagesDir, cfg.MailRoot)
			if err := archive.Archive(cfg.AgentName(), msg.ID); err != nil {
				return err
			}

			cmd.Println("Archived:", msg.ID)
			return nil
		},
	}
}

func mailThreadsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "threads",
		Short: "List threads",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := mail.Default()
			if err != nil {
				return err
			}

			store := mail.NewStore(cfg.MessagesDir)
			threads, err := store.ListThreadsForRecipient(cfg.AgentName())
			if err != nil {
				return err
			}

			if len(threads) == 0 {
				cmd.Println("No threads.")
				return nil
			}

			cmd.Println(styleBold.Render("Threads:"))
			cmd.Println()

			for _, t := range threads {
				date := t.LastMessage.Format("2006-01-02 15:04")
				cmd.Printf("[%s] %s\n", date, t.Subject)
				cmd.Printf("    %s | %d messages\n", t.ID, t.MessageCount)
				cmd.Println()
			}

			return nil
		},
	}
}

func mailThreadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "thread <thread-id>",
		Short: "View a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := mail.Default()
			if err != nil {
				return err
			}

			store := mail.NewStore(cfg.MessagesDir)
			threads, err := store.ListThreadsForRecipient(cfg.AgentName())
			if err != nil {
				return err
			}

			var thread *mail.Thread
			for _, t := range threads {
				if strings.HasPrefix(t.ID, args[0]) {
					thread = t
					break
				}
			}

			if thread == nil {
				// Try loading as message
				msg, err := store.Load(args[0])
				if err != nil {
					return fmt.Errorf("thread not found")
				}
				cmd.Println(styleBold.Render("Thread: " + msg.Subject))
				cmd.Println()
				cmd.Printf("%s %s\n", styleDim.Render("─"), msg.From)
				cmd.Println(msg.Body)
				return nil
			}

			cmd.Println(styleBold.Render("Thread: " + thread.Subject))
			cmd.Println()

			for _, m := range thread.Messages {
				date := m.Timestamp.Format("2006-01-02 15:04:05")
				cmd.Printf("%s %s (%s)\n", styleDim.Render("─"), m.From, date)
				if m.Parent != "" {
					cmd.Printf("  %s\n", styleDim.Render("↳ replying to "+m.Parent))
				}
				cmd.Println()
				cmd.Println(m.Body)
				cmd.Println()
			}

			return nil
		},
	}
}

func mailAgentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agents",
		Short: "List known agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := mail.Default()
			if err != nil {
				return err
			}

			inboxMgr := mail.NewInboxManager(cfg.MessagesDir)
			agents, err := inboxMgr.ListAgents()
			if err != nil {
				return err
			}

			if len(agents) == 0 {
				cmd.Println("No agents configured.")
				return nil
			}

			for _, agent := range agents {
				cmd.Println(agent)
			}

			return nil
		},
	}
}

func mailTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open mail TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check if running as an agent (AGENT_NAME set)
			if os.Getenv("AGENT_NAME") != "" {
				return fmt.Errorf("tui cannot run when AGENT_NAME is set")
			}

			// Check if stdin is a terminal
			if !isTerminal(os.Stdin.Fd()) {
				return fmt.Errorf("tui requires a terminal (stdin is not a tty)")
			}

			return runMailTUI()
		},
	}
}

func isTerminal(fd uintptr) bool {
	// Simple check using unix package
	var ws struct {
		Rows    uint16
		Cols    uint16
		Xpixel  uint16
		Ypixel  uint16
	}
	retcode, _, _ := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlReadWinsize, uintptr(unsafe.Pointer(&ws)))
	return retcode == 0
}

func editWithEditor(initial string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	tmpfile, err := os.CreateTemp("", "acoo-mail-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpfile.Name())
	defer tmpfile.Close()

	if _, err := tmpfile.WriteString(initial); err != nil {
		return "", err
	}
	tmpfile.Close()

	execCmd := exec.Command(editor, tmpfile.Name())
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	if err := execCmd.Run(); err != nil {
		return "", err
	}

	data, err := os.ReadFile(tmpfile.Name())
	if err != nil {
		return "", err
	}

	return string(data), nil
}
