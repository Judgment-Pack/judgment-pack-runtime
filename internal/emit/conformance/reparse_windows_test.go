//go:build windows

package conformance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/junctiontest"
)

// A junction in the repository that leads out refuses the run with
// EMIT-HARNESS-BOUND at its path, before the emitter runs, fallback or not:
// Go reads it as irregular, not as a link, so the link check cannot follow
// it.
func TestAJunctionInTheRepositoryIsRefused(t *testing.T) {
	f := newFixture(t)
	outside := t.TempDir()
	write(t, f.root, map[string]string{"docs/a.md": "a\n"})
	junctiontest.Make(t, filepath.Join(f.root, "docs", "out"), outside)
	if info, err := os.Lstat(filepath.Join(f.root, "docs", "out")); err != nil || info.Mode()&os.ModeIrregular == 0 {
		t.Fatalf("the junction reads as %v: %v", info, err)
	}
	m := f.manifest([]string{"stray", "create", "docs/out/written.txt"}, map[string]any{"environments": "all"})
	f.registryVector("a", nil)
	report := f.run(m, true)
	if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 {
		t.Fatalf("report: %s", dump(report))
	}
	if d := report.Diagnostics[0]; d.Code != "EMIT-HARNESS-BOUND" || d.Location.Document != emit.DocumentRepository || d.Location.Path != "docs/out" {
		t.Fatalf("diagnostic: %+v", d)
	}
	if _, err := os.Lstat(filepath.Join(outside, "written.txt")); !os.IsNotExist(err) {
		t.Fatalf("the emitter wrote through the junction: %v", err)
	}
}
