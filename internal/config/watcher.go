package config

import (
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors directories for changes and notifies on add/remove
type Watcher struct {
	agentsDir string
	jobsDir   string
	watcher   *fsnotify.Watcher
	handlers  []func(changed []string)

	mu       sync.Mutex
	knownFiles map[string]bool
}

// NewWatcher creates a new directory watcher
func NewWatcher(agentsDir, jobsDir string) (*Watcher, error) {
	w := &Watcher{
		agentsDir: agentsDir,
		jobsDir:   jobsDir,
		knownFiles: make(map[string]bool),
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
func (w *Watcher) Watch(logger *log.Logger) {
	go w.run(logger)
}

// run processes fsnotify events
func (w *Watcher) run(logger *log.Logger) {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event, logger)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			logger.Printf("Watcher error: %v", err)
		}
	}
}

// handleEvent processes a single fsnotify event
func (w *Watcher) handleEvent(event fsnotify.Event, logger *log.Logger) {
	// Only care about create and remove events on .md files
	if !isMarkdownFile(event.Name) {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	changed := []string{}

	switch {
	case event.Has(fsnotify.Chmod):
		// Treat chmod as modified (touch causes chmod)
		if w.knownFiles[event.Name] {
			changed = append(changed, "modified:"+event.Name)
		}

	case event.Has(fsnotify.Create):
		if !w.knownFiles[event.Name] {
			w.knownFiles[event.Name] = true
			changed = append(changed, "added:"+event.Name)
			logger.Printf("Agent added: %s", filepath.Base(filepath.Dir(event.Name)))
		} else {
			// File already known but got recreated (e.g., sed -i)
			changed = append(changed, "modified:"+event.Name)
		}

	case event.Has(fsnotify.Remove):
		if w.knownFiles[event.Name] {
			delete(w.knownFiles, event.Name)
			changed = append(changed, "removed:"+event.Name)
			logger.Printf("Agent removed: %s", filepath.Base(filepath.Dir(event.Name)))
		}

	case event.Has(fsnotify.Write):
		// File was modified - treat as changed
		if w.knownFiles[event.Name] {
			changed = append(changed, "modified:"+event.Name)
			logger.Printf("File modified: %s", event.Name)
		}
	}

	if len(changed) > 0 {
		for _, h := range w.handlers {
			h(changed)
		}
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
