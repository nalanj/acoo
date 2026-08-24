package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/nalanj/acoo/internal/mail"
)

// Tools returns the standard tools available to all agents
func Tools() []fantasy.AgentTool {
	return []fantasy.AgentTool{
		ReadFileTool(),
		EditFileTool(),
		BashTool(),
		GlobTool(),
		ListDirTool(),
		MailInboxTool(),
		MailSendTool(),
		MailReadTool(),
		MailReplyTool(),
		MailArchiveTool(),
	}
}

// ReadFileTool returns a tool for reading files
func ReadFileTool() fantasy.AgentTool {
	type ReadFileInput struct {
		Path string `json:"path" description:"The path to the file to read"`
	}

	fn := func(ctx context.Context, input ReadFileInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		content, err := os.ReadFile(input.Path)
		if err != nil {
			return fantasy.NewTextErrorResponse("Error reading file: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(string(content)), nil
	}

	return fantasy.NewAgentTool("read_file", "Read the contents of a file", fn)
}

// EditFileTool returns a tool for editing files
func EditFileTool() fantasy.AgentTool {
	type EditFileInput struct {
		Path    string `json:"path" description:"The path to the file to edit"`
		Content string `json:"content" description:"The new content for the file"`
		Append  bool   `json:"append,omitempty" description:"If true, append to the file instead of overwriting"`
	}

	fn := func(ctx context.Context, input EditFileInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if input.Append {
			f, err := os.OpenFile(input.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fantasy.NewTextErrorResponse("Error opening file: " + err.Error()), nil
			}
			defer f.Close()
			if _, err := f.WriteString(input.Content); err != nil {
				return fantasy.NewTextErrorResponse("Error writing file: " + err.Error()), nil
			}
			return fantasy.NewTextResponse("Content appended to file: " + input.Path), nil
		}
		if err := os.WriteFile(input.Path, []byte(input.Content), 0644); err != nil {
			return fantasy.NewTextErrorResponse("Error writing file: " + err.Error()), nil
		}
		return fantasy.NewTextResponse("File written: " + input.Path), nil
	}

	return fantasy.NewAgentTool("edit_file", "Write or append content to a file", fn)
}

// BashTool returns a tool for running shell commands
func BashTool() fantasy.AgentTool {
	type BashInput struct {
		Command string `json:"command" description:"The shell command to run"`
		Timeout int    `json:"timeout,omitempty" description:"Timeout in seconds (default 30)"`
	}

	fn := func(ctx context.Context, input BashInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		// Create a context with timeout if specified
		cmdCtx := ctx
		var cancel context.CancelFunc
		if input.Timeout > 0 {
			cmdCtx, cancel = context.WithTimeout(ctx, time.Duration(input.Timeout)*time.Second)
			defer cancel()
		}

		cmd := exec.CommandContext(cmdCtx, "/bin/sh", "-c", input.Command)
		output, err := cmd.CombinedOutput()

		if err != nil {
			return fantasy.NewTextErrorResponse(string(output) + "\nError: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(string(output)), nil
	}

	return fantasy.NewAgentTool("bash", "Run a shell command", fn)
}

// GlobTool returns a tool for listing files matching a pattern
func GlobTool() fantasy.AgentTool {
	type GlobInput struct {
		Pattern string `json:"pattern" description:"Glob pattern to match files (e.g., '*.go', 'src/**/*.ts')"`
		Dir     string `json:"dir,omitempty" description:"Directory to search in (default current directory)"`
	}

	fn := func(ctx context.Context, input GlobInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		dir := "."
		if input.Dir != "" {
			dir = input.Dir
		}

		matches, err := filepath.Glob(filepath.Join(dir, input.Pattern))
		if err != nil {
			return fantasy.NewTextErrorResponse("Error globbing: " + err.Error()), nil
		}

		if len(matches) == 0 {
			return fantasy.NewTextResponse("No files matched"), nil
		}

		return fantasy.NewTextResponse(strings.Join(matches, "\n")), nil
	}

	return fantasy.NewAgentTool("glob", "List files matching a glob pattern", fn)
}

// ListDirTool returns a tool for listing directory contents
func ListDirTool() fantasy.AgentTool {
	type ListDirInput struct {
		Path string `json:"path" description:"The directory path to list"`
	}

	fn := func(ctx context.Context, input ListDirInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		entries, err := os.ReadDir(input.Path)
		if err != nil {
			return fantasy.NewTextErrorResponse("Error listing directory: " + err.Error()), nil
		}

		var lines []string
		for _, entry := range entries {
			lines = append(lines, entry.Name())
		}

		return fantasy.NewTextResponse(strings.Join(lines, "\n")), nil
	}

	return fantasy.NewAgentTool("list_dir", "List contents of a directory", fn)
}

// agentMailConfig returns the mail config for the current agent
func agentMailConfig() (*mail.Config, error) {
	cfg, err := mail.Default()
	if err != nil {
		return nil, err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// MailInboxTool returns a tool for checking inbox
func MailInboxTool() fantasy.AgentTool {
	type MailInboxInput struct {
		All bool `json:"all,omitempty" description:"Show all messages including archived"`
	}

	fn := func(ctx context.Context, input MailInboxInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		cfg, err := agentMailConfig()
		if err != nil {
			return fantasy.NewTextErrorResponse("Error getting mail config: " + err.Error()), nil
		}

		store := mail.NewStore(cfg.MessagesDir)
		inboxMgr := mail.NewInboxManager(cfg.MessagesDir)

		agentName := cfg.AgentName()
		inboxIDs, err := inboxMgr.ListInboxMessages(agentName)
		if err != nil {
			return fantasy.NewTextErrorResponse("Error listing inbox: " + err.Error()), nil
		}

		if len(inboxIDs) == 0 {
			return fantasy.NewTextResponse("No messages in inbox."), nil
		}

		var lines []string
		for _, id := range inboxIDs {
			msg, err := store.Load(id)
			if err != nil {
				continue
			}
			date := msg.Timestamp.Format("2006-01-02 15:04")
			lines = append(lines, fmt.Sprintf("%s | From: %s | Subject: %s | Date: %s", msg.ID[:17], msg.From, msg.Subject, date))
		}

		return fantasy.NewTextResponse("Messages in inbox:\n" + strings.Join(lines, "\n")), nil
	}

	return fantasy.NewAgentTool("mail_inbox", "Check inbox for new messages", fn)
}

// MailSendTool returns a tool for sending messages
func MailSendTool() fantasy.AgentTool {
	type MailSendInput struct {
		Recipients []string `json:"recipients" description:"The recipients to send to (array)"`
		Subject   string   `json:"subject" description:"The message subject"`
		Body      string   `json:"body" description:"The message body"`
	}

	fn := func(ctx context.Context, input MailSendInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		cfg, err := agentMailConfig()
		if err != nil {
			return fantasy.NewTextErrorResponse("Error getting mail config: " + err.Error()), nil
		}

		msg := &mail.Message{
			ID:        mail.GenerateID(),
			From:      cfg.AgentName(),
			To:        input.Recipients,
			Subject:   input.Subject,
			Timestamp: time.Now().UTC(),
			Body:      input.Body,
			Thread:    mail.GenerateID(),
		}

		store := mail.NewStore(cfg.MessagesDir)
		if err := store.Save(msg); err != nil {
			return fantasy.NewTextErrorResponse("Error saving message: " + err.Error()), nil
		}

		inboxMgr := mail.NewInboxManager(cfg.MessagesDir)
		if err := inboxMgr.AddToInboxes(msg); err != nil {
			// Clean up the saved message file since inbox creation failed
			store.Delete(msg.ID)
			return fantasy.NewTextErrorResponse("Error adding to inbox: " + err.Error()), nil
		}

		return fantasy.NewTextResponse("Message sent: " + msg.ID), nil
	}

	return fantasy.NewAgentTool("mail_send", "Send a message to a recipient", fn)
}

// MailReadTool returns a tool for reading messages
func MailReadTool() fantasy.AgentTool {
	type MailReadInput struct {
		MessageID string `json:"message_id" description:"The message ID to read"`
	}

	fn := func(ctx context.Context, input MailReadInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		cfg, err := agentMailConfig()
		if err != nil {
			return fantasy.NewTextErrorResponse("Error getting mail config: " + err.Error()), nil
		}

		store := mail.NewStore(cfg.MessagesDir)
		msg, err := store.Load(input.MessageID)
		if err != nil {
			return fantasy.NewTextErrorResponse("Error loading message: " + err.Error()), nil
		}

		date := msg.Timestamp.Format("2006-01-02 15:04")
		var lines []string
		lines = append(lines, fmt.Sprintf("From: %s", msg.From))
		lines = append(lines, fmt.Sprintf("Subject: %s", msg.Subject))
		lines = append(lines, fmt.Sprintf("Date: %s", date))
		if msg.Parent != "" {
			lines = append(lines, fmt.Sprintf("Reply-To: %s", msg.Parent))
		}
		lines = append(lines, "")
		lines = append(lines, msg.Body)

		return fantasy.NewTextResponse(strings.Join(lines, "\n")), nil
	}

	return fantasy.NewAgentTool("mail_read", "Read a message by ID", fn)
}

// MailReplyTool returns a tool for replying to messages
func MailReplyTool() fantasy.AgentTool {
	type MailReplyInput struct {
		MessageID string `json:"message_id" description:"The message ID to reply to"`
		Body      string `json:"body" description:"The reply body"`
	}

	fn := func(ctx context.Context, input MailReplyInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		cfg, err := agentMailConfig()
		if err != nil {
			return fantasy.NewTextErrorResponse("Error getting mail config: " + err.Error()), nil
		}

		store := mail.NewStore(cfg.MessagesDir)
		parent, err := store.Load(input.MessageID)
		if err != nil {
			return fantasy.NewTextErrorResponse("Error loading parent message: " + err.Error()), nil
		}

		replySubject := "Re: " + parent.Subject
		msg := &mail.Message{
			ID:        mail.GenerateID(),
			From:      cfg.AgentName(),
			To:        []string{parent.From},
			Subject:   replySubject,
			Thread:    parent.Thread,
			Parent:    parent.ID,
			Timestamp: time.Now().UTC(),
			Body:      input.Body,
		}

		if msg.Thread == "" {
			msg.Thread = msg.ID
		}

		if err := store.Save(msg); err != nil {
			return fantasy.NewTextErrorResponse("Error saving reply: " + err.Error()), nil
		}

		inboxMgr := mail.NewInboxManager(cfg.MessagesDir)
		if err := inboxMgr.AddToInboxes(msg); err != nil {
			// Clean up the saved message file since inbox creation failed
			store.Delete(msg.ID)
			return fantasy.NewTextErrorResponse("Error adding to inbox: " + err.Error()), nil
		}

		return fantasy.NewTextResponse("Reply sent: " + msg.ID), nil
	}

	return fantasy.NewAgentTool("mail_reply", "Reply to a message", fn)
}

// MailArchiveTool returns a tool for archiving messages
func MailArchiveTool() fantasy.AgentTool {
	type MailArchiveInput struct {
		MessageID string `json:"message_id" description:"The message ID to archive"`
	}

	fn := func(ctx context.Context, input MailArchiveInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		cfg, err := agentMailConfig()
		if err != nil {
			return fantasy.NewTextErrorResponse("Error getting mail config: " + err.Error()), nil
		}

		store := mail.NewStore(cfg.MessagesDir)
		msg, err := store.Load(input.MessageID)
		if err != nil {
			return fantasy.NewTextErrorResponse("Error loading message: " + err.Error()), nil
		}

		archiveMgr := mail.NewArchiveManager(cfg.MessagesDir, cfg.MailRoot)
		if err := archiveMgr.Archive(cfg.AgentName(), msg.ID); err != nil {
			return fantasy.NewTextErrorResponse("Error archiving message: " + err.Error()), nil
		}

		return fantasy.NewTextResponse("Archived: " + msg.ID), nil
	}

	return fantasy.NewAgentTool("mail_archive", "Archive a message", fn)
}
