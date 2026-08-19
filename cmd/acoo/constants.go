package main

import (
	"os"
	"os/signal"
	"syscall"
)

const (
	// DoneMarker is the marker that signals job completion
	DoneMarker = "<<<<<DONE>>>>>"
)

// SignalHandler returns a channel that receives OS signals
func SignalHandler() chan os.Signal {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	return sigChan
}
