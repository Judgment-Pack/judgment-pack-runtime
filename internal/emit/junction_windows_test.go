//go:build windows

package emit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/junctiontest"
)

// A repository root that is a junction is never walked as an empty root:
// RealRoot either refuses it, or resolves it to a real directory whose owned
// files the scan finds.
func TestAJunctionRootIsNotScannedAsEmpty(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "gen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "gen", "stale.txt"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "root")
	junctiontest.Make(t, link, target)
	real, err := RealRoot(link)
	if err != nil {
		return // refused: the scan never starts
	}
	if info, err := os.Lstat(real); err != nil || !rootIsDirectory(info.Mode()) {
		t.Fatalf("RealRoot gave %q, which reads as %v: %v", real, info, err)
	}
	glob, err := parseGlob("gen/**")
	if err != nil {
		t.Fatal(err)
	}
	files, diagnostics := scanOwned(real, []Glob{glob})
	if _, ok := files["gen/stale.txt"]; !ok || len(diagnostics) != 0 {
		t.Fatalf("the scan from %q found %v, %v; want gen/stale.txt", real, files, diagnostics)
	}
}

// A junction below the root where a glob can match refuses the run with
// EMIT-LINK at its path, as a symbolic link does.
func TestAJunctionBelowTheRootIsALink(t *testing.T) {
	root, err := RealRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "gen"), 0o755); err != nil {
		t.Fatal(err)
	}
	junctiontest.Make(t, filepath.Join(root, "gen", "out"), t.TempDir())
	glob, err := parseGlob("gen/**")
	if err != nil {
		t.Fatal(err)
	}
	_, diagnostics := scanOwned(root, []Glob{glob})
	if len(diagnostics) != 1 || diagnostics[0].Code != "EMIT-LINK" || diagnostics[0].Location.Path != "gen/out" {
		t.Fatalf("diagnostics: %+v; want one EMIT-LINK at gen/out", diagnostics)
	}
}

// A junction at or above a response path refuses the run with EMIT-LINK at
// it, though Go may read the junction as a directory as well as irregular.
func TestAJunctionAboveAResponsePathIsALink(t *testing.T) {
	root, err := RealRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "gen"), 0o755); err != nil {
		t.Fatal(err)
	}
	junctiontest.Make(t, filepath.Join(root, "gen", "out"), t.TempDir())
	_, _, diagnostics := preflight(root, []File{{Path: "gen/out/a.txt", Data: []byte("a\n"), Mode: ModeRegular}})
	if len(diagnostics) != 1 || diagnostics[0].Code != "EMIT-LINK" || diagnostics[0].Location.Path != "gen/out" {
		t.Fatalf("diagnostics: %+v; want one EMIT-LINK at gen/out", diagnostics)
	}
}

// A root below a junction is refused with ErrRootReparse, not resolved.
func TestARootBelowAJunctionIsRefused(t *testing.T) {
	target := t.TempDir()
	if err := os.Mkdir(filepath.Join(target, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "j")
	junctiontest.Make(t, link, target)
	if _, err := RealRoot(filepath.Join(link, "repo")); !errors.Is(err, ErrRootReparse) {
		t.Fatalf("a root below a junction: %v", err)
	}
}
