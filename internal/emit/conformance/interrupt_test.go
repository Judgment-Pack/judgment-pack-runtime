package conformance

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

// An interrupted run (jpack was sent an interrupt or SIGTERM) refuses with
// EMIT-INTERRUPTED at the vector it was running, drops that vector's rows
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
	if d := report.Diagnostics[0]; d.Code != "EMIT-INTERRUPTED" || d.Location.Path != "a" {
		t.Fatalf("interrupted: %+v", d)
	}
}

// An interrupt after a vector's clean run, before its expected/ is replaced
// (--update), rewrites nothing: the gate refuses the write, the run is
// refused at that vector as an interrupted one is, and no row says
// expected/ was written.
func TestAnInterruptBeforeTheUpdateRewritesNothing(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(requestMode, nil)
	f.registryVector("a", map[string]string{"expected/gen/old": "old\n"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	asked := 0
	ended := false
	report := Run(ctx, m, f.vectors, Options{Root: f.root, Timeout: time.Minute, Update: true,
		BeginWrite: func() bool { asked++; cancel(); return false }, // as a signal just before the write
		EndWrite:   func() { ended = true }})
	if report.Outcome != OutcomeNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "EMIT-INTERRUPTED" || report.Diagnostics[0].Location.Path != "a" {
		t.Fatalf("interrupted before the write: %s", dump(report))
	}
	if asked != 1 || ended {
		t.Fatalf("the gate: asked %d times, ended %v", asked, ended)
	}
	wantExpected(t, filepath.Join(f.vectors, "a"), []string{"gen"}, "old")
}

// With the run already interrupted when its vector's run ends, the context
// is seen done before the gate is asked: expected/ is not touched.
func TestAnInterruptedUpdateChecksTheContextFirst(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "v")
	write(t, dir, map[string]string{"request.json": "{}", "expected/gen/old": "old\n"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := &harness{ctx: ctx, opts: Options{Update: true}}
	run := &vectorRun{v: &Vector{Name: "v", Dir: dir}, files: []emit.File{{Path: "gen/new", Data: []byte("new\n"), Mode: emit.ModeRegular}}}
	row, failure := h.golden(run, Row{Vector: "v", Check: checkGolden})
	if failure == nil || failure.Code != "EMIT-INTERRUPTED" || row.Status != "" || !h.interrupted {
		t.Fatalf("golden after an interrupt: %+v %+v", row, failure)
	}
	wantExpected(t, dir, []string{"gen"}, "old")
}

// Once the renames have begun, an interrupt does not cut them short:
// expected/ is the new one, never missing, with no temporary directory
// beside it. The run is then refused as interrupted, and the vector's
// check 1 row still says expected/ was written; the gate is ended.
func TestAnInterruptDuringTheRenamesFinishesThem(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(requestMode, nil)
	f.registryVector("a", map[string]string{"expected/gen/old": "old\n"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	saved := rename
	defer func() { rename = saved }()
	renames := 0
	rename = func(from, to string) error {
		renames++
		cancel() // as a signal between the renames
		return saved(from, to)
	}
	writing := false
	report := Run(ctx, m, f.vectors, Options{Root: f.root, Timeout: time.Minute, Update: true,
		BeginWrite: func() bool { writing = true; return true },
		EndWrite:   func() { writing = false }})
	if renames != 2 || writing {
		t.Fatalf("renames %d, still writing %v", renames, writing)
	}
	wantExpected(t, filepath.Join(f.vectors, "a"), []string{"gen"}, "")
	if names := vectorNames(t, filepath.Join(f.vectors, "a", "expected", "gen")); !slices.Equal(names, []string{"req"}) {
		t.Fatalf("expected/gen after the update: %v", names)
	}
	if report.Outcome != OutcomeNotRun || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "EMIT-INTERRUPTED" {
		t.Fatalf("interrupted during the write: %s", dump(report))
	}
	if len(report.Rows) != 1 || report.Rows[0].Vector != "a" || report.Rows[0].Check != checkGolden || report.Rows[0].Reason != reasonUpdated {
		t.Fatalf("rows after an interrupted update: %s", dump(report))
	}
}

// A signal that comes once a vector's update is written, after EndWrite, is
// still acted on: the run stops at that vector, keeps its updated row and
// leaves the next vector's expected/ as it was.
func TestAnInterruptAfterAnUpdateStopsTheNextVector(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(requestMode, nil)
	f.registryVector("a", map[string]string{"expected/gen/old": "old\n"})
	f.registryVector("b", map[string]string{"expected/gen/old": "old\n"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes, ends := 0, 0
	report := Run(ctx, m, f.vectors, Options{Root: f.root, Timeout: time.Minute, Update: true,
		BeginWrite: func() bool { writes++; return true },
		EndWrite: func() {
			ends++
			cancel() // as a signal once the write has ended
		}})
	if writes != 1 || ends != 1 {
		t.Fatalf("writes %d, ends %d", writes, ends)
	}
	if names := vectorNames(t, filepath.Join(f.vectors, "a", "expected", "gen")); !slices.Equal(names, []string{"req"}) {
		t.Fatalf("a's expected/gen after the update: %v", names)
	}
	if names := vectorNames(t, filepath.Join(f.vectors, "b", "expected", "gen")); !slices.Equal(names, []string{"old"}) {
		t.Fatalf("b's expected/gen after the interrupt: %v", names)
	}
	if report.Outcome != OutcomeNotRun || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "EMIT-INTERRUPTED" || report.Diagnostics[0].Location.Path != "a" {
		t.Fatalf("interrupted after a's update: %s", dump(report))
	}
	if len(report.Rows) != 1 || report.Rows[0].Vector != "a" || report.Rows[0].Reason != reasonUpdated {
		t.Fatalf("rows after an interrupt that followed an update: %s", dump(report))
	}
}

// wantExpected checks that dir holds expected/ and no temporary directory
// beside it, that expected/ lists names, and, unless old is empty, that
// expected/gen/old still reads old.
func wantExpected(t *testing.T, dir string, names []string, old string) {
	t.Helper()
	for _, name := range vectorNames(t, dir) {
		if strings.HasPrefix(name, expectedUpdatePrefix) {
			t.Fatalf("a temporary directory is left: %s", name)
		}
	}
	if got := vectorNames(t, filepath.Join(dir, dirExpected)); !slices.Equal(got, names) {
		t.Fatalf("expected/: %v, want %v", got, names)
	}
	if old == "" {
		return
	}
	if data, err := os.ReadFile(filepath.Join(dir, dirExpected, "gen", "old")); err != nil || string(data) != old+"\n" {
		t.Fatalf("expected/ was rewritten: %q %v", data, err)
	}
}
