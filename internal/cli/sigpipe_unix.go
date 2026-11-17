//go:build unix

package cli

import (
	"os"
	"os/signal"
	"syscall"
)

// configureSignals makes a write to a closed pipe an EPIPE error rather than
// the end of jpack. It catches SIGPIPE rather than ignoring it: a caught
// signal returns to its default disposition in a program jpack starts, such
// as an emitter, while an ignored one would stay ignored there too. The
// channel is never read; a signal that finds it full is dropped.
func configureSignals() {
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
}
