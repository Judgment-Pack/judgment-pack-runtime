//go:build windows

package conformance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/junctiontest"
)

// A suite root that is a junction is refused as a symlinked one is.
func TestASuiteRootThatIsAJunctionIsRefused(t *testing.T) {
	root := writeLocalSuite(t)
	link := filepath.Join(t.TempDir(), "suite")
	junctiontest.Make(t, link, root)
	if _, _, err := resolveSuiteRoot(link); err == nil {
		t.Fatal("a junction suite root was accepted")
	}
	if _, _, err := resolveSuiteRoot(root); err != nil {
		t.Fatalf("the real suite root: %v", err)
	}
}

// Each component of a fixture path is checked: a junction anywhere along it
// is refused, not only at its end.
func TestAFixturePathThroughAJunctionIsRefused(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "case.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "real", "case.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	junctiontest.Make(t, filepath.Join(root, "linked"), target)
	if _, err := resolveFixture(root, "linked/case.json"); err == nil {
		t.Fatal("a fixture path through a junction was accepted")
	}
	if _, err := resolveFixture(root, "real/case.json"); err != nil {
		t.Fatalf("a real fixture path: %v", err)
	}
}
