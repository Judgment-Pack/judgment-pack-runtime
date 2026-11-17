package cli

import "testing"

// The write may start only while no signal has come; once it has started,
// a signal no longer restores the default disposition, so the write is not
// cut short.
func TestTheWritePhaseGate(t *testing.T) {
	in, stop := interruptContext()
	defer stop()
	if !in.beginWrite() {
		t.Fatal("the write was refused with no signal")
	}
	in.stop()
	<-in.ctx.Done()
	if !in.writing {
		t.Fatal("the write phase was not marked")
	}

	in, stop = interruptContext()
	defer stop()
	in.stop() // as a first signal does: the context is done
	<-in.ctx.Done()
	if in.beginWrite() {
		t.Fatal("the write began after a signal")
	}
}
