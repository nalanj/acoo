package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Store handles persistence of agent state using JSONL files
type Store struct {
	dir       string
	agentName string
	mu        sync.Mutex // Protects file writes
}

// Message represents a stored conversation message
type Message struct {
	ID          string    `json:"id"`
	Prev        string    `json:"prev,omitempty"`
	Role        string    `json:"role"` // user, assistant, system, tool
	Type        string    `json:"type,omitempty"`
	Content     string    `json:"content"`
	Timestamp   time.Time `json:"timestamp"`
	ToolName    string    `json:"tool_name,omitempty"`
}

// Metadata stores session metadata
type Metadata struct {
	LastUpdated      time.Time `json:"last_updated"`
	MessageCount     int       `json:"message_count"`
	SessionNumber    int       `json:"session_number"`
}

// NewStore creates a store in the given directory for the given agent
func NewStore(dir, agentName string) (*Store, error) {
	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating store directory: %w", err)
	}

	return &Store{
		dir:       dir,
		agentName: agentName,
	}, nil
}

// Close closes the store (no-op)
func (s *Store) Close() error {
	return nil
}

// sessionPath returns the path for a given session number
func (s *Store) sessionPath(num int) string {
	return filepath.Join(s.dir, s.agentName, fmt.Sprintf("session_%03d.jsonl", num))
}

// metadataPath returns the path to the metadata file
func (s *Store) metadataPath() string {
	return filepath.Join(s.dir, s.agentName, "meta.json")
}

// currentSession returns the current session number
func (s *Store) currentSession() (int, error) {
	agentDir := filepath.Join(s.dir, s.agentName)
	entries, err := os.ReadDir(agentDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	re := regexp.MustCompile(`session_(\d+)\.jsonl`)
	maxNum := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := re.FindStringSubmatch(entry.Name())
		if len(matches) >= 2 {
			num, _ := strconv.Atoi(matches[1])
			if num > maxNum {
				maxNum = num
			}
		}
	}

	return maxNum, nil
}

// MessagesPath returns the path to the current session file
func (s *Store) MessagesPath() (string, error) {
	num, err := s.currentSession()
	if err != nil {
		return "", err
	}
	if num == 0 {
		num = 1
	}
	return s.sessionPath(num), nil
}

// AddMessage adds a message to the current session
func (s *Store) AddMessage(msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure agent directory exists
	if err := os.MkdirAll(filepath.Join(s.dir, s.agentName), 0755); err != nil {
		return fmt.Errorf("creating agent directory: %w", err)
	}

	// Get or create current session
	sessionNum, err := s.currentSession()
	if err != nil {
		return err
	}
	if sessionNum == 0 {
		sessionNum = 1
	}

	path := s.sessionPath(sessionNum)

	// Generate ID if not set
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// Find previous message ID
	prevID, err := s.getLastMessageID(path)
	if err == nil && prevID != "" {
		msg.Prev = prevID
	}

	// Open file for appending
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening session file: %w", err)
	}
	defer f.Close()

	// Write JSON line
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}

	// Update metadata
	s.updateMetadata(sessionNum, 1)

	return nil
}

// getLastMessageID reads the last message ID from a session file
func (s *Store) getLastMessageID(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	var lastID string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		lastID = msg.ID
	}
	return lastID, scanner.Err()
}

// GetMessages returns all messages from the current session
func (s *Store) GetMessages() ([]Message, error) {
	return s.getMessages(false)
}

// GetAllMessages returns all messages from the current session (same for now)
func (s *Store) GetAllMessages() ([]Message, error) {
	return s.getMessages(true)
}

func (s *Store) getMessages(includeArchived bool) ([]Message, error) {
	path, err := s.MessagesPath()
	if err != nil {
		return nil, err
	}
	return s.readSession(path)
}

// readSession reads all messages from a session file
func (s *Store) readSession(path string) ([]Message, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Message{}, nil
		}
		return nil, fmt.Errorf("opening session file: %w", err)
	}
	defer f.Close()

	var messages []Message
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// SaveSystemPrompt saves the system prompt as a special message
func (s *Store) SaveSystemPrompt(content string) error {
	msg := Message{
		Role:      "system",
		Content:   content,
		Timestamp: time.Now(),
	}
	return s.AddMessage(msg)
}

// GetSystemPrompt returns the latest actual system prompt (not summaries)
func (s *Store) GetSystemPrompt() (string, error) {
	messages, err := s.getMessages(false)
	if err != nil {
		return "", err
	}

	// Find the last non-summary system message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "system" {
			content := messages[i].Content
			// Skip summary markers
			if strings.HasPrefix(content, "[Prior conversation") {
				continue
			}
			return content, nil
		}
	}

	return "", nil
}

// CompactStart begins a new session for compaction
// Returns the new session number and the path to the new session
func (s *Store) CompactStart(summary string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get current session
	currentNum, err := s.currentSession()
	if err != nil {
		return 0, err
	}

	// Read current session
	currentPath := s.sessionPath(currentNum)
	messages, err := s.readSession(currentPath)
	if err != nil {
		return 0, err
	}

	// Keep last 6 messages (user/assistant pairs + last prompt)
	keepCount := 6
	if len(messages) > keepCount {
		recentMessages := messages[len(messages)-keepCount:]
		messages = recentMessages
	}

	// Create new session
	newNum := currentNum + 1
	newPath := s.sessionPath(newNum)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return 0, err
	}

	// Write summary message first
	summaryMsg := Message{
		Role:      "system",
		Content:   fmt.Sprintf("[Prior conversation summarized: %s]", summary),
		Timestamp: time.Now(),
	}
	data, _ := json.Marshal(summaryMsg)
	f, err := os.Create(newPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	f.Write(append(data, '\n'))

	// Write recent messages with reset prev chain
	var lastID string
	for _, msg := range messages {
		msg.Prev = lastID
		msg.Timestamp = time.Now()
		msg.ID = fmt.Sprintf("%d", time.Now().UnixNano())
		data, _ = json.Marshal(msg)
		f.Write(append(data, '\n'))
		lastID = msg.ID
	}

	// Update metadata
	meta, _ := s.getMetadata()
	meta.SessionNumber = newNum
	meta.LastUpdated = time.Now()
	meta.MessageCount = len(messages) + 1 // +1 for summary
	data, _ = json.Marshal(meta)
	os.WriteFile(s.metadataPath(), data, 0644)

	return newNum, nil
}

// GetMetadata returns session metadata
func (s *Store) GetMetadata() (*Metadata, error) {
	return s.getMetadata()
}

func (s *Store) getMetadata() (*Metadata, error) {
	path := s.metadataPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Metadata{}, nil
		}
		return nil, fmt.Errorf("reading metadata: %w", err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshaling metadata: %w", err)
	}

	return &meta, nil
}

func (s *Store) updateMetadata(sessionNum, messageDelta int) {
	meta, err := s.getMetadata()
	if err != nil {
		meta = &Metadata{}
	}
	meta.LastUpdated = time.Now()
	if sessionNum > meta.SessionNumber {
		meta.SessionNumber = sessionNum
	}
	meta.MessageCount += messageDelta

	data, _ := json.Marshal(meta)
	os.WriteFile(s.metadataPath(), data, 0644)
}

// ListSessions lists all agent directories
func ListSessions(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	var agents []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip hidden directories
		if strings.HasPrefix(name, ".") {
			continue
		}
		agents = append(agents, name)
	}
	return agents, nil
}

// ListSessionFiles returns all session files for an agent, sorted by number
func ListSessionFiles(agentDir string) ([]string, error) {
	entries, err := os.ReadDir(agentDir)
	if err != nil {
		return nil, fmt.Errorf("reading agent directory: %w", err)
	}

	re := regexp.MustCompile(`session_(\d+)\.jsonl`)
	var sessions []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if re.MatchString(entry.Name()) {
			sessions = append(sessions, filepath.Join(agentDir, entry.Name()))
		}
	}

	// Sort by session number
	sort.Slice(sessions, func(i, j int) bool {
		numI := extractSessionNum(sessions[i])
		numJ := extractSessionNum(sessions[j])
		return numI < numJ
	})

	return sessions, nil
}

func extractSessionNum(path string) int {
	re := regexp.MustCompile(`session_(\d+)\.jsonl`)
	matches := re.FindStringSubmatch(path)
	if len(matches) >= 2 {
		num, _ := strconv.Atoi(matches[1])
		return num
	}
	return 0
}
