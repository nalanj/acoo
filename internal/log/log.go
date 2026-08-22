// Package log provides a standardized logger for acoo.
//
// Format: <ISO8601> <scope> <message> key=value key=value
//
// Where scope is:
//   - "system" for system-level logs (manager, watcher, etc.)
//   - "@<agent-name>" for agent-specific logs
//
// All output is written to stdout/stderr with an ISO8601 timestamp prefix.
// Fields after the message are key=value pairs, separated by spaces.
//
// Example output:
//   2026-08-21T22:00:00 system started agents count=3
//   2026-08-21T22:00:01 @code-reviewer job=calc starting
//   2026-08-21T22:00:02 @code-reviewer job=calc next_run_in=29s
package log

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

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

// write formats and writes a log line.
func (l *Logger) write(level, message string, fields ...Field) {
	l.mu.Lock()
	defer l.mu.Unlock()

	parts := []string{
		time.Now().Format(time.RFC3339),
		l.scope,
		level,
		message,
	}

	for _, f := range fields {
		parts = append(parts, f.String())
	}

	out := l.out
	if level == "error" {
		out = os.Stderr
	}

	fmt.Fprintln(out, strings.Join(parts, " "))
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