//go:build !unix

package cli

import "testing"

// makeFIFO is "" where this platform makes no FIFO.
func makeFIFO(*testing.T) string { return "" }
