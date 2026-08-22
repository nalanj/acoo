// Package log provides a standardized logger for acoo.
//
// Format: <ISO8601> <scope> <level> <message> key=value
//
// Where scope is:
//   - "system" for system-level logs (manager, watcher, etc.)
//   - "@<agent-name>" for agent-specific logs
//
// Color output (when writing to a terminal):
//   - timestamp: dimmed
//   - system scope: cyan
//   - @agent scope: green
//   - warn level: yellow
//   - error level: red
//
// Example output:
//   2026-08-21T22:00:00 system info started agents=3
//   2026-08-21T22:00:01 @code-reviewer info job=calc starting
//   2026-08-21T22:00:02 @code-reviewer warn job=calc precondition_failed
package log

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// ANSI color codes
const (
	ColorReset  = "\033[0m"
	ColorDim    = "\033[2m"
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorCyan    = "\033[36m"
	ColorBoldRed = "\033[1;31m"
)

// isTerminal returns true if the writer is a terminal
func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return isTerminalFd(f)
	}
	return false
}

var isTerminalFd = func(f *os.File) bool {
	// Simple check - if it's stdout or stderr and has a terminal
	return f == os.Stdout || f == os.Stderr
}

// Logger writes structured log lines with a consistent format.
type Logger struct {
	mu    sync.Mutex
	scope string
	out   io.Writer
}

// System returns a logger scoped to system events.
func System() *Logger {
	return &Logger{
		scope: "system",
		out:   os.Stdout,
	}
}

// Agent returns a logger scoped to a specific agent.
func Agent(name string) *Logger {
	return &Logger{
		scope: "@" + name,
		out:   os.Stdout,
	}
}

// Print writes a log line at info level.
func (l *Logger) Print(message string, fields ...Field) {
	l.write("info", message, fields...)
}

// Info is an alias for Print.
func (l *Logger) Info(message string, fields ...Field) {
	l.write("info", message, fields...)
}

// Warn writes a warning log line.
func (l *Logger) Warn(message string, fields ...Field) {
	l.write("warn", message, fields...)
}

// Error writes an error log line to stderr.
func (l *Logger) Error(message string, fields ...Field) {
	l.write("error", message, fields...)
}

// colorForScope returns the color code for a scope
func colorForScope(scope string) string {
	if strings.HasPrefix(scope, "@") {
		return ColorGreen
	}
	return ColorCyan
}

// colorForLevel returns the color code for a level
func colorForLevel(level string) string {
	switch level {
	case "warn":
		return ColorYellow
	case "error":
		return ColorBoldRed
	default:
		return ""
	}
}

// write formats and writes a log line.
func (l *Logger) write(level, message string, fields ...Field) {
	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format(time.RFC3339)
	scopeColor := colorForScope(l.scope)
	levelColor := colorForLevel(level)

	// Build parts
	parts := []string{
		l.colorize(timestamp, ColorDim),
		l.colorize(l.scope, scopeColor),
		l.colorize(level, levelColor),
		message,
	}

	// Add fields
	for _, f := range fields {
		parts = append(parts, f.String())
	}

	out := l.out
	if level == "error" {
		out = os.Stderr
	}

	// Only colorize if writing to terminal
	if !isTerminal(out) {
		// Strip colors for non-terminal output
		fmt.Fprintln(out, stripColors(strings.Join(parts, " ")))
		return
	}

	fmt.Fprintln(out, strings.Join(parts, " ")+ColorReset)
}

// colorize wraps text with color codes if color is not empty
func (l *Logger) colorize(text, color string) string {
	if color == "" {
		return text
	}
	return color + text + ColorReset
}

// stripColors removes ANSI color codes from a string
func stripColors(s string) string {
	s = strings.ReplaceAll(s, ColorReset, "")
	s = strings.ReplaceAll(s, ColorDim, "")
	s = strings.ReplaceAll(s, ColorRed, "")
	s = strings.ReplaceAll(s, ColorGreen, "")
	s = strings.ReplaceAll(s, ColorYellow, "")
	s = strings.ReplaceAll(s, ColorCyan, "")
	s = strings.ReplaceAll(s, ColorBoldRed, "")
	return s
}

// Field represents a key=value log field.
type Field struct {
	Key   string
	Value any
}

// String returns the key=value representation.
func (f Field) String() string {
	if v, ok := f.Value.(string); ok {
		return fmt.Sprintf("%s=%q", f.Key, v)
	}
	return fmt.Sprintf("%s=%v", f.Key, f.Value)
}

// F creates a Field with the given key and value.
func F(key string, value any) Field {
	return Field{Key: key, Value: value}
}