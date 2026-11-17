//go:build windows

package conformance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/gitindex"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/gitindex/gitindextest"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/junctiontest"
)

// A junction in the repository that leads out refuses the run with
// EMIT-HARNESS-BOUND at its path, before the emitter runs, fallback or not,
// when the repository has no .git, so that what git tracks cannot be read.
// Go reads a junction as irregular, not as a link; os.Readlink reads where
// it leads.
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

// A junction in the repository that leads out, that git does not track, and
// that no path the run writes or reads could name, is left out of the copy
// and reported as EMIT-LINK-OMITTED at its path: a write at its path is a
// stray write of the copy and never reaches where the junction leads.
func TestAJunctionOutOfTheRepositoryIsLeftOut(t *testing.T) {
	f := newFixture(t)
	outside := t.TempDir()
	write(t, f.root, map[string]string{"docs/a.md": "a\n"})
	writeIndex(t, f.root, gitindextest.Entry{Path: emitterCommand}, gitindextest.Entry{Path: "docs/a.md"})
	junctiontest.Make(t, filepath.Join(f.root, "docs", "out"), outside)
	m := f.manifest([]string{"stray", "create", "docs/out/written.txt"}, map[string]any{"environments": "all"})
	f.registryVector("a", nil)
	report := f.run(m, true)
	if report.Outcome != OutcomeFail || len(report.Diagnostics) != 1 {
		t.Fatalf("report: %s", dump(report))
	}
	if d := report.Diagnostics[0]; d.Code != "EMIT-LINK-OMITTED" || d.Location != (emit.Location{Document: emit.DocumentRepository, Path: "docs/out"}) {
		t.Fatalf("diagnostic: %+v", d)
	}
	if row := rowOf(t, report, "a", checkStrayWrites); row.Status != StatusFail || len(row.StrayWrites) != 1 || row.StrayWrites[0].Path != "docs" {
		t.Fatalf("check 6: %+v", row)
	}
	if _, err := os.Lstat(filepath.Join(outside, "written.txt")); !os.IsNotExist(err) {
		t.Fatalf("the emitter wrote through the junction: %v", err)
	}
}

// A junction that git tracks, or that an owned path could name, refuses the
// run with EMIT-HARNESS-BOUND at its path, before the emitter runs, as a
// symbolic link there does; so does a junction that stays in the
// repository, which the copy does not make.
func TestAJunctionTheCopyCannotLeaveOutIsRefused(t *testing.T) {
	cases := map[string]struct {
		link    string
		inside  bool
		tracked bool
	}{
		"tracked":                   {link: "docs/out", tracked: true},
		"at an owned path":          {link: "gen/out"},
		"staying in the repository": {link: "docs/in", inside: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			target := t.TempDir()
			write(t, f.root, map[string]string{"docs/a.md": "a\n", "gen/a.txt": "a\n"})
			entries := []gitindextest.Entry{{Path: emitterCommand}, {Path: "docs/a.md"}, {Path: "gen/a.txt"}}
			if c.tracked {
				entries = append(entries, gitindextest.Entry{Path: c.link, Mode: gitindex.ModeLink})
			}
			writeIndex(t, f.root, entries...)
			if c.inside {
				target = filepath.Join(f.root, "docs")
			}
			junctiontest.Make(t, filepath.Join(f.root, filepath.FromSlash(c.link)), target)
			m := f.manifest([]string{"stray", "create", "ran.txt"}, map[string]any{"environments": "all"})
			f.registryVector("a", nil)
			report := f.run(m, true)
			if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 {
				t.Fatalf("report: %s", dump(report))
			}
			if d := report.Diagnostics[0]; d.Code != "EMIT-HARNESS-BOUND" || d.Location != (emit.Location{Document: emit.DocumentRepository, Path: c.link}) {
				t.Fatalf("diagnostic: %+v", d)
			}
			if _, err := os.Lstat(filepath.Join(f.root, "ran.txt")); !os.IsNotExist(err) {
				t.Fatalf("the emitter ran: %v", err)
			}
		})
	}
}
