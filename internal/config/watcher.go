package config

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/nalanj/acoo/internal/log"
)

// Watcher monitors directories for changes and notifies on add/remove
type Watcher struct {
	agentsDir string
	jobsDir   string
	watcher   *fsnotify.Watcher
	handlers  []func(changed []string)

	mu          sync.Mutex
	knownFiles  map[string]bool
	pending     map[string][]string // file -> actions
	debounceTimer *time.Timer
}

// NewWatcher creates a new directory watcher
func NewWatcher(agentsDir, jobsDir string) (*Watcher, error) {
	w := &Watcher{
		agentsDir:  agentsDir,
		jobsDir:    jobsDir,
		knownFiles: make(map[string]bool),
		pending:    make(map[string][]string),
	}

	var err error
	w.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Initial scan to populate known files
	w.scanDirectory(agentsDir)
	w.scanDirectory(jobsDir)

	// Start watching
	if err := w.watcher.Add(agentsDir); err != nil {
		return nil, err
	}
	if err := w.watcher.Add(jobsDir); err != nil {
		return nil, err
	}

	return w, nil
}

// scanDirectory adds all files in a directory to known files
func (w *Watcher) scanDirectory(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			path := filepath.Join(dir, entry.Name())
			w.knownFiles[path] = true
		}
	}
}

// OnChange registers a handler to call when files are added or removed
func (w *Watcher) OnChange(handler func(changed []string)) {
	w.handlers = append(w.handlers, handler)
}

// Watch starts watching for changes in a goroutine
func (w *Watcher) Watch() {
	go w.run()
}

// run processes fsnotify events
func (w *Watcher) run() {
	logger := log.System()
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			logger.Error("watcher_error", log.F("error", err))
		}
	}
}

// handleEvent processes a single fsnotify event with debouncing
func (w *Watcher) handleEvent(event fsnotify.Event) {
	// Only care about create and remove events on .md files
	if !isMarkdownFile(event.Name) {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	switch {
	case event.Has(fsnotify.Chmod):
		if w.knownFiles[event.Name] {
			w.pending[event.Name] = append(w.pending[event.Name], "modified")
			w.scheduleDebounce()
		}

	case event.Has(fsnotify.Create):
		if !w.knownFiles[event.Name] {
			w.knownFiles[event.Name] = true
			w.pending[event.Name] = append(w.pending[event.Name], "added")
			w.scheduleDebounce()
		} else {
			// File already known but got recreated (e.g., sed -i)
			w.pending[event.Name] = append(w.pending[event.Name], "modified")
			w.scheduleDebounce()
		}

	case event.Has(fsnotify.Remove):
		if w.knownFiles[event.Name] {
			delete(w.knownFiles, event.Name)
			w.pending[event.Name] = append(w.pending[event.Name], "removed")
			w.scheduleDebounce()
		}

	case event.Has(fsnotify.Write):
		if w.knownFiles[event.Name] {
			w.pending[event.Name] = append(w.pending[event.Name], "modified")
			w.scheduleDebounce()
		}
	}
}

// scheduleDebounce starts or resets the debounce timer
func (w *Watcher) scheduleDebounce() {
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
	}
	w.debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.flushPending()
	})
}

// flushPending sends all pending changes to handlers
func (w *Watcher) flushPending() {
	if len(w.pending) == 0 {
		return
	}

	changed := []string{}
	for path, actions := range w.pending {
		// Deduplicate actions - modified/removed takes precedence over added
		action := "modified"
		if len(actions) == 1 && actions[0] == "added" {
			action = "added"
		} else if len(actions) == 1 && actions[0] == "removed" {
			action = "removed"
		}
		changed = append(changed, action+":"+path)
	}

	w.pending = make(map[string][]string)

	for _, h := range w.handlers {
		h(changed)
	}
}

// isMarkdownFile returns true if the path is a markdown file
func isMarkdownFile(path string) bool {
	return filepath.Ext(path) == ".md"
}

// Close stops the watcher
func (w *Watcher) Close() error {
	return w.watcher.Close()
}