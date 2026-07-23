//go:build unix

package cli

import (
	"os/signal"
	"syscall"
)

func configureSignals() {
	signal.Ignore(syscall.SIGPIPE)
}
