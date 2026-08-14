package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestImmutableRevisionSpec(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{name: "tag", ref: "v0.1.0-draft", want: "refs/tags/v0.1.0-draft^{commit}"},
		{name: "full tag ref", ref: "refs/tags/v0.1.0-draft", want: "refs/tags/v0.1.0-draft^{commit}"},
		{name: "commit", ref: "0123456789abcdef0123456789abcdef01234567", want: "0123456789abcdef0123456789abcdef01234567^{commit}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := immutableRevisionSpec(test.ref)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestImmutableRevisionSpecRejectsNonExactRefs(t *testing.T) {
	for _, ref := range []string{"", "refs/heads/main", "v0.1.0\nmain"} {
		t.Run(ref, func(t *testing.T) {
			if _, err := immutableRevisionSpec(ref); err == nil {
				t.Fatal("expected ref to be rejected")
			}
		})
	}
}

// The evaluation corpus contributes one manifest, one schema, and one pack
// fixture per named path. Only a single-segment path under packs/ is imported,
// so a manifest cannot direct the import at anything else.
func TestValidateEvaluationPackPath(t *testing.T) {
	for _, accepted := range []string{"packs/data-request-intake-triage.json", "packs/a1.json"} {
		if err := validateEvaluationPackPath(accepted); err != nil {
			t.Errorf("%q must be accepted: %v", accepted, err)
		}
	}
	for _, rejected := range []string{
		"", "packs/../schema.json", "../packs/a.json", "packs/nested/a.json",
		"packs/A.json", "packs/a.yaml", "/packs/a.json", "packs\\a.json", "cases/valid/a.json",
	} {
		if err := validateEvaluationPackPath(rejected); err == nil {
			t.Errorf("%q must be rejected", rejected)
		}
	}
}

// --- end-to-end import behaviour (issue #97) -------------------------------

// specCheckout builds a throwaway specification checkout whose origin carries
// the official spelling, since provenance is checked before anything is read.
func specCheckout(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("schema/judgment-pack-core.schema.json", `{"$id":"core"}`)
	write("conformance/manifest.schema.json", `{"$id":"manifest"}`)
	write("conformance/manifest.json",
		`{"specVersion":"1.4","cases":[{"path":"valid/a.json"},{"path":"structural/b.json"}]}`)
	write("conformance/valid/a.json", `{"case":"a"}`)
	write("conformance/structural/b.json", `{"case":"b"}`)

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"remote", "add", "origin", sourceRepository},
		{"add", "-A"},
		{"commit", "-q", "-m", "spec"},
		{"tag", "-a", "-m", "spec", "v1.4"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "tag.gpgSign=false", "-c", "commit.gpgSign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return root
}

func TestImportPublishesACompleteBundle(t *testing.T) {
	source := specCheckout(t)
	destination := filepath.Join(t.TempDir(), "spec")

	if err := run(source, destination, "v1.4", false); err != nil {
		t.Fatalf("import: %v", err)
	}
	for _, rel := range []string{
		"lock.json", "schema.json", "manifest.json", "manifest.schema.json",
		"cases/valid/a.json", "cases/structural/b.json",
	} {
		if _, err := os.Stat(filepath.Join(destination, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s missing from the published bundle: %v", rel, err)
		}
	}

	// Every file the lock names is present at the size and digest it records.
	raw, err := os.ReadFile(filepath.Join(destination, "lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock lockFile
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	if err := verifyMaterialized(destination, lock); err != nil {
		t.Fatalf("the published bundle disagrees with its own lock: %v", err)
	}
	if lock.Source.Ref == "" || lock.Source.WorktreeDirty {
		t.Fatalf("an immutable import records its ref and a clean worktree: %+v", lock.Source)
	}

	// No staging directory is left beside the destination.
	assertNoStagingResidue(t, filepath.Dir(destination))
}

func TestImportRefusesAnExistingDestinationWithoutTouchingIt(t *testing.T) {
	source := specCheckout(t)
	parent := t.TempDir()
	destination := filepath.Join(parent, "spec")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(destination, "keep.json")
	original := []byte(`{"do":"not touch"}`)
	if err := os.WriteFile(occupied, original, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run(source, destination, "v1.4", false); err == nil {
		t.Fatal("an existing destination must be refused")
	}
	after, err := os.ReadFile(occupied)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("the existing destination was modified: %q", after)
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the existing destination gained %d entries", len(entries)-1)
	}
	assertNoStagingResidue(t, parent)
}

// The defect this issue is about: a failure partway through materialization
// used to leave a partial destination, which then blocked the retry because the
// next invocation correctly refuses an existing destination.
func TestFailedMaterializationLeavesNoDestinationAndRetrySucceeds(t *testing.T) {
	source := specCheckout(t)
	parent := t.TempDir()
	destination := filepath.Join(parent, "spec")

	calls := 0
	original := writeFile
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		calls++
		if calls == 3 { // partway through, after real files exist
			return errors.New("injected materialization failure")
		}
		return original(name, data, perm)
	}
	defer func() { writeFile = original }()

	err := run(source, destination, "v1.4", false)
	if err == nil {
		t.Fatal("an injected write failure must fail the import")
	}
	if !strings.Contains(err.Error(), "injected materialization failure") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a failed import left a destination behind: %v", statErr)
	}
	assertNoStagingResidue(t, parent)

	// And the retry is not blocked, which is the whole point.
	writeFile = original
	if err := run(source, destination, "v1.4", false); err != nil {
		t.Fatalf("the retry after a failed import must succeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "lock.json")); err != nil {
		t.Fatalf("the retry did not publish a complete bundle: %v", err)
	}
}

func TestImportRefusesWrongOrDirtyProvenance(t *testing.T) {
	t.Run("dirty worktree under an immutable ref", func(t *testing.T) {
		source := specCheckout(t)
		if err := os.WriteFile(filepath.Join(source, "conformance", "valid", "a.json"),
			[]byte(`{"case":"edited"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(t.TempDir(), "spec")
		if err := run(source, destination, "v1.4", false); err == nil {
			t.Fatal("an immutable import requires a clean worktree")
		}
		if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("a refused import must create no destination")
		}
	})

	t.Run("origin is not the specification repository", func(t *testing.T) {
		source := specCheckout(t)
		cmd := exec.Command("git", "-C", source, "remote", "set-url", "origin",
			"https://example.com/not-the-spec")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, out)
		}
		destination := filepath.Join(t.TempDir(), "spec")
		if err := run(source, destination, "v1.4", false); err == nil {
			t.Fatal("a foreign origin must be refused")
		}
		if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("a refused import must create no destination")
		}
	})
}

func assertNoStagingResidue(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".sync-spec-artifacts-") {
			t.Fatalf("staging directory left behind: %s", entry.Name())
		}
	}
}
