package conformance

import (
	"context"
	"testing"
	"time"
)

// An interrupted run (jpack was sent an interrupt or SIGTERM) refuses with
// EMIT-HARNESS-BOUND at the vector it was running, drops that vector's rows
// (they would report failures the emitter did not cause) and runs no later
// vector.
func TestAnInterruptedRunStopsAtItsVector(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(requestMode, nil)
	f.registryVector("a", nil)
	f.registryVector("b", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report := Run(ctx, m, f.vectors, Options{Root: f.root, Timeout: time.Minute, Update: true})
	if report.Outcome != OutcomeNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 {
		t.Fatalf("interrupted: %s", dump(report))
	}
	if d := report.Diagnostics[0]; d.Code != "EMIT-HARNESS-BOUND" || d.Location.Path != "a" {
		t.Fatalf("interrupted: %+v", d)
	}
}
