//go:build unix

package cli

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// A pipe beyond the count is never opened: a writer waiting on it is still
// waiting when the command ends, and the command gives the row's own report
// within a bound rather than an input error.
func TestRegistryLockVerifyNeverOpensAPipeBeyondTheCount(t *testing.T) {
	for _, c := range beyondTheCount {
		t.Run(c.id, func(t *testing.T) {
			r := verifyCLIRowNamed(t, c.id)
			dir := t.TempDir()
			chmod(t, dir, 0o700)
			pipe := makeFIFO(t)
			opened := make(chan struct{})
			go func() {
				// Blocks until something opens the pipe to read it.
				if writer, err := os.OpenFile(pipe, os.O_WRONLY, 0); err == nil {
					close(opened)
					writer.Close()
				}
			}()
			t.Cleanup(func() {
				// Release the writer, whatever the command did.
				if reader, err := os.OpenFile(pipe, os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
					select {
					case <-opened:
					case <-time.After(5 * time.Second):
					}
					reader.Close()
				}
			})
			args := replacedAt(t, verifyArguments(t, r, dir, *r.Expect.RegistryID), c.flag, c.at, pipe)
			type outcome struct {
				code           int
				stdout, stderr string
			}
			done := make(chan outcome, 1)
			go func() {
				code, stdout, stderr := runTest(t, args, "")
				done <- outcome{code, stdout, stderr}
			}()
			var got outcome
			select {
			case got = <-done:
			case <-time.After(30 * time.Second):
				t.Fatal("the command did not end within 30 seconds")
			}
			select {
			case <-opened:
				t.Error("the pipe beyond the count was opened")
			case <-time.After(200 * time.Millisecond):
			}
			checkFederationReport(t, r.federationRow, got.code, got.stdout, got.stderr)
		})
	}
}
