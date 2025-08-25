package utils

import (
	"os"
	"os/signal"
	"syscall"
)

// WaitForSignal blocks until a SIGINT or SIGTERM is received.
func WaitForSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}