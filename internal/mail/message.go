package mail

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"gopkg.in/yaml.v3"
)

var (
	ErrNotFound      = errors.New("message not found")
	ErrAlreadyExists = errors.New("message already exists")
)

type Message struct {
	ID        string    `yaml:"id"`
	From      string    `yaml:"from"`
	To        []string  `yaml:"to"`
	Subject   string    `yaml:"subject"`
	Thread    string    `yaml:"thread"`
	Parent    string    `yaml:"parent"`
	Timestamp time.Time `yaml:"timestamp"`
	Unread    bool      `yaml:"-"` // Computed field, not stored
	Body      string    `yaml:"-"`
}

type Thread struct {
	ID           string
	Subject      string
	LastMessage  time.Time
	MessageCount int
	UnreadCount  int
	Participants []string
	Messages     []*Message
}

type Store struct {
	messagesDir string
}

func NewStore(messagesDir string) *Store {
	return &Store{
		messagesDir: messagesDir,
	}
}

func (s *Store) Save(msg *Message) error {
	if msg.ID == "" {
		return errors.New("message ID is required")
	}

	filename := s.messagePath(msg.ID)
	if _, err := os.Stat(filename); err == nil {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, msg.ID)
	}

	data, err := s.marshalMessage(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}

	return nil
}

func (s *Store) Load(id string) (*Message, error) {
	// Try exact match first
	filename := s.messagePath(id)
	data, err := os.ReadFile(filename)
	if err == nil {
		return s.unmarshalMessage(data)
	}

	// Try prefix match
	entries, err := os.ReadDir(s.messagesDir)
	if err != nil {
		return nil, fmt.Errorf("reading messages directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		msgID := strings.TrimSuffix(entry.Name(), ".md")
		if strings.HasPrefix(msgID, id) {
			data, err := os.ReadFile(s.messagePath(msgID))
			if err != nil {
				continue
			}
			return s.unmarshalMessage(data)
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

func (s *Store) Update(msg *Message) error {
	filename := s.messagePath(msg.ID)
	if _, err := os.Stat(filename); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrNotFound, msg.ID)
		}
		return fmt.Errorf("checking message: %w", err)
	}

	data, err := s.marshalMessage(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}

	return nil
}

func (s *Store) ListMessages() ([]*Message, error) {
	entries, err := os.ReadDir(s.messagesDir)
	if err != nil {
		return nil, fmt.Errorf("reading messages directory: %w", err)
	}

	var messages []*Message
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".md")
		msg, err := s.Load(id)
		if err != nil {
			continue
		}
		messages = append(messages, msg)
	}

	// Sort by timestamp, oldest first
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	return messages, nil
}

func (s *Store) ListMessagesForRecipient(recipient string) ([]*Message, error) {
	all, err := s.ListMessages()
	if err != nil {
		return nil, err
	}

	var messages []*Message
	for _, msg := range all {
		for _, to := range msg.To {
			if to == recipient {
				messages = append(messages, msg)
				break
			}
		}
	}

	return messages, nil
}

func (s *Store) ListThreadsForRecipient(recipient string) ([]*Thread, error) {
	all, err := s.ListMessages()
	if err != nil {
		return nil, err
	}

	// Find all threads where the recipient participated (sent or received)
	threadIDs := make(map[string]bool)
	for _, msg := range all {
		if msg.From == recipient {
			threadIDs[msg.Thread] = true
			if msg.Thread == "" {
				threadIDs[msg.ID] = true
			}
		}
		for _, to := range msg.To {
			if to == recipient {
				threadIDs[msg.Thread] = true
				if msg.Thread == "" {
					threadIDs[msg.ID] = true
				}
			}
		}
	}

	// Group by thread
	threadMap := make(map[string]*Thread)
	for _, msg := range all {
		threadID := msg.Thread
		if threadID == "" {
			threadID = msg.ID
		}

		// Only include messages from threads the recipient participated in
		if !threadIDs[threadID] {
			continue
		}

		if _, ok := threadMap[threadID]; !ok {
			threadMap[threadID] = &Thread{
				ID:           threadID,
				Subject:      msg.Subject,
				LastMessage:  msg.Timestamp,
				MessageCount: 0,
				UnreadCount:  0,
				Participants: []string{},
				Messages:     []*Message{},
			}
		}

		thread := threadMap[threadID]
		thread.MessageCount++
		thread.Messages = append(thread.Messages, msg)

		if msg.Timestamp.After(thread.LastMessage) {
			thread.LastMessage = msg.Timestamp
		}

		if msg.Unread {
			thread.UnreadCount++
		}

		// Track participants
		found := false
		for _, p := range thread.Participants {
			if p == msg.From {
				found = true
				break
			}
		}
		if !found {
			thread.Participants = append(thread.Participants, msg.From)
		}
	}

	// Convert to slice and sort by last message
	threads := make([]*Thread, 0, len(threadMap))
	for _, t := range threadMap {
		threads = append(threads, t)
	}

	sort.Slice(threads, func(i, j int) bool {
		return threads[i].LastMessage.After(threads[j].LastMessage)
	})

	return threads, nil
}

// GetThreadByMessageID returns the thread containing the given message ID
func (s *Store) GetThreadByMessageID(msgID string) (*Thread, error) {
	msg, err := s.Load(msgID)
	if err != nil {
		return nil, err
	}

	// Get the thread ID (use message ID if no thread)
	threadID := msg.Thread
	if threadID == "" {
		threadID = msg.ID
	}

	// Load all messages and find ones in the same thread
	all, err := s.ListMessages()
	if err != nil {
		return nil, err
	}

	var threadMsgs []*Message
	for _, m := range all {
		mThreadID := m.Thread
		if mThreadID == "" {
			mThreadID = m.ID
		}
		if mThreadID == threadID {
			threadMsgs = append(threadMsgs, m)
		}
	}

	if len(threadMsgs) == 0 {
		return nil, fmt.Errorf("thread not found: %s", threadID)
	}

	// Sort by timestamp
	sort.Slice(threadMsgs, func(i, j int) bool {
		return threadMsgs[i].Timestamp.Before(threadMsgs[j].Timestamp)
	})

	// Build participant list
	participants := make([]string, 0)
	seen := make(map[string]bool)
	for _, m := range threadMsgs {
		if !seen[m.From] {
			participants = append(participants, m.From)
			seen[m.From] = true
		}
	}

	return &Thread{
		ID:           threadID,
		Subject:      threadMsgs[0].Subject,
		LastMessage:  threadMsgs[len(threadMsgs)-1].Timestamp,
		MessageCount: len(threadMsgs),
		Participants: participants,
		Messages:     threadMsgs,
	}, nil
}

func (s *Store) messagePath(id string) string {
	return filepath.Join(s.messagesDir, id+".md")
}

func (s *Store) marshalMessage(msg *Message) ([]byte, error) {
	var sb strings.Builder

	fm := struct {
		ID        string    `yaml:"id"`
		From      string    `yaml:"from"`
		To        []string  `yaml:"to"`
		Subject   string    `yaml:"subject"`
		Thread    string    `yaml:"thread"`
		Parent    string    `yaml:"parent"`
		Timestamp time.Time `yaml:"timestamp"`
	}{
		ID:        msg.ID,
		From:      msg.From,
		To:        msg.To,
		Subject:   msg.Subject,
		Thread:    msg.Thread,
		Parent:    msg.Parent,
		Timestamp: msg.Timestamp,
	}

	fmData, err := yaml.Marshal(fm)
	if err != nil {
		return nil, fmt.Errorf("marshaling front matter: %w", err)
	}

	sb.WriteString("---\n")
	sb.Write(fmData)
	sb.WriteString("---\n")
	sb.WriteString(msg.Body)

	return []byte(sb.String()), nil
}

func (s *Store) unmarshalMessage(data []byte) (*Message, error) {
	lines := strings.Split(string(data), "\n")

	var inBody bool
	var fmLines []string
	var bodyLines []string

	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if i == 0 {
				continue
			} else if !inBody {
				inBody = true
				continue
			}
		}

		if !inBody {
			fmLines = append(fmLines, line)
		} else {
			bodyLines = append(bodyLines, line)
		}
	}

	var fm struct {
		ID        string    `yaml:"id"`
		From      string    `yaml:"from"`
		To        []string  `yaml:"to"`
		Subject   string    `yaml:"subject"`
		Thread    string    `yaml:"thread"`
		Parent    string    `yaml:"parent"`
		Timestamp time.Time `yaml:"timestamp"`
	}

	if err := yaml.Unmarshal([]byte(strings.Join(fmLines, "\n")), &fm); err != nil {
		return nil, fmt.Errorf("unmarshaling front matter: %w", err)
	}

	return &Message{
		ID:        fm.ID,
		From:      fm.From,
		To:        fm.To,
		Subject:   fm.Subject,
		Thread:    fm.Thread,
		Parent:    fm.Parent,
		Timestamp: fm.Timestamp,
		Body:      strings.TrimRight(strings.Join(bodyLines, "\n"), "\n"),
	}, nil
}

func GenerateID() string {
	timestamp := time.Now().Format("20060102-150405")
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return fmt.Sprintf("%s-%s", timestamp, hex.EncodeToString(bytes))
}

func RenderMarkdown(body string) string {
	md := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
	)

	var buf strings.Builder
	if err := md.Convert([]byte(body), &buf); err != nil {
		return body
	}
	return buf.String()
}
