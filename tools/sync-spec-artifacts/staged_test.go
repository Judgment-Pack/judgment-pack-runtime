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

// stagedCheckout extends specCheckout with the staged directories a
// specification commit carries for one draft selector, commits them, and
// returns the checkout with its full commit.
func stagedCheckout(t *testing.T) (string, string) {
	t.Helper()
	root := specCheckout(t)
	write := func(rel, body string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("schema/staged/judgment-pack-core-9.9.0-draft.schema.json", `{"$id":"staged-core"}`)
	write("conformance/staged/manifest.json",
		`{"status":"staged","specVersion":"9.9.0-draft","caseSchema":"../manifest.schema.json#/$defs/case","cases":[{"path":"valid/s.json"}]}`)
	write("conformance/staged/valid/s.json", `{"case":"s"}`)
	write("conformance/evaluation/manifest.schema.json", `{"$id":"rows"}`)
	write("conformance/evaluation/staged/rows-9/cases.json",
		`{"status":"staged","specVersion":"9.9.0-draft","caseSchema":"../../manifest.schema.json#/$defs/case","cases":[{"pack":"packs/p.json"}],"heldCases":[{"case":{"pack":"packs/h.json"},"heldBecause":"r"}]}`)
	write("conformance/evaluation/staged/rows-9/packs/p.json", `{"pack":"p"}`)
	write("conformance/evaluation/staged/rows-9/packs/h.json", `{"pack":"h"}`)
	write("conformance/evaluation/staged/rows-9/packs/unnamed.json", `{"pack":"not named by any row"}`)
	write("examples/staged/annotated.json", `{"specVersion":"9.9.0-draft"}`)
	write("examples/staged/other-selector.json", `{"specVersion":"9.8.0-draft"}`)
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "staged")
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return root, strings.TrimSpace(string(out))
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "commit.gpgSign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func readLock(t *testing.T, destination string) lockFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(destination, "lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock lockFile
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	return lock
}

func TestStagedImportPublishesALabelledBundle(t *testing.T) {
	source, commit := stagedCheckout(t)
	destination := filepath.Join(t.TempDir(), "9.9.0-draft")
	if err := dispatch(source, destination, commit, "9.9.0-draft", false, false); err != nil {
		t.Fatalf("staged import: %v", err)
	}
	lock := readLock(t, destination)
	if lock.SpecVersion != "9.9.0-draft" || lock.Source.Stage != stagedStage ||
		lock.Source.Kind != "immutable-git-ref" || lock.Source.Ref != commit || lock.Source.BaseCommit != commit || lock.Source.WorktreeDirty {
		t.Fatalf("the staged lock does not record a staged import of the exact commit: %+v", lock)
	}
	got := map[string]bool{}
	for _, file := range lock.Files {
		got[file.Path] = true
	}
	want := []string{
		"schema.json", "manifest.json", "manifest.schema.json", "cases/valid/s.json",
		"evaluation/cases.json", "evaluation/manifest.schema.json",
		"evaluation/packs/p.json", "evaluation/packs/h.json", "example/annotated.json",
	}
	for _, path := range want {
		if !got[path] {
			t.Errorf("%s missing from the staged bundle", path)
		}
	}
	if len(got) != len(want) {
		t.Errorf("the staged bundle holds %d files, want %d: %v", len(got), len(want), got)
	}
	if err := verifyMaterialized(destination, lock); err != nil {
		t.Fatalf("the staged bundle disagrees with its own lock: %v", err)
	}
	schema, err := os.ReadFile(filepath.Join(destination, "schema.json"))
	if err != nil || string(schema) != `{"$id":"staged-core"}` {
		t.Fatalf("schema.json is not the staged schema: %q %v", schema, err)
	}
	assertNoStagingResidue(t, filepath.Dir(destination))
}

func TestStagedImportRefusesWhatIsNotAnExactCleanCommit(t *testing.T) {
	source, commit := stagedCheckout(t)
	for _, tc := range []struct {
		name, ref, selector string
		allowDirty          bool
	}{
		{"a tag", "v1.4", "9.9.0-draft", false},
		{"an abbreviated commit", commit[:12], "9.9.0-draft", false},
		{"no ref", "", "9.9.0-draft", false},
		{"--allow-dirty", commit, "9.9.0-draft", true},
		{"a released selector", commit, "9.9.0", false},
		{"a selector that is not a version", commit, "../9.9.0-draft", false},
		{"a selector the commit does not stage", commit, "9.8.0-draft", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "staged")
			if err := dispatch(source, destination, tc.ref, tc.selector, tc.allowDirty, false); err == nil {
				t.Fatal("the staged import must be refused")
			}
			if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("a refused import must create no destination")
			}
		})
	}
	t.Run("a dirty worktree", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(source, "conformance", "staged", "valid", "s.json"), []byte(`{"case":"edited"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(t.TempDir(), "staged")
		if err := dispatch(source, destination, commit, "9.9.0-draft", false, false); err == nil {
			t.Fatal("a staged import requires a clean worktree")
		}
		if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("a refused import must create no destination")
		}
	})
	t.Run("--replace without --staged", func(t *testing.T) {
		if err := dispatch(source, filepath.Join(t.TempDir(), "spec"), "v1.4", "", false, true); err == nil {
			t.Fatal("--replace applies only to a staged import")
		}
	})
}

func TestStagedReplaceRefreshesOnlyAStagedBundleOfTheSameSelector(t *testing.T) {
	source, commit := stagedCheckout(t)
	parent := t.TempDir()
	destination := filepath.Join(parent, "9.9.0-draft")
	if err := dispatch(source, destination, commit, "9.9.0-draft", false, false); err != nil {
		t.Fatal(err)
	}
	if err := dispatch(source, destination, commit, "9.9.0-draft", false, false); err == nil {
		t.Fatal("an existing staged bundle is replaced only under --replace")
	}

	// A later commit is re-vendored in one step.
	if err := os.WriteFile(filepath.Join(source, "conformance", "staged", "valid", "s.json"), []byte(`{"case":"later"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, source, "commit", "-q", "-am", "later")
	out, err := exec.Command("git", "-C", source, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	later := strings.TrimSpace(string(out))
	if err := dispatch(source, destination, later, "9.9.0-draft", false, true); err != nil {
		t.Fatalf("re-vendor under --replace: %v", err)
	}
	lock := readLock(t, destination)
	if lock.Source.BaseCommit != later || lock.Source.Stage != stagedStage {
		t.Fatalf("the replaced lock does not record the later commit: %+v", lock.Source)
	}
	fixture, err := os.ReadFile(filepath.Join(destination, "cases", "valid", "s.json"))
	if err != nil || string(fixture) != `{"case":"later"}` {
		t.Fatalf("the replaced bundle does not hold the later bytes: %q %v", fixture, err)
	}
	assertNoStagingResidue(t, parent)

	// A released bundle, or a staged bundle of another selector, is never
	// replaced, and is left untouched.
	for _, tc := range []struct{ name, lock string }{
		{"a released bundle", `{"specVersion":"9.9.0-draft","source":{"kind":"immutable-git-ref"}}`},
		{"another selector", `{"specVersion":"9.8.0-draft","source":{"stage":"staged-unreleased"}}`},
		{"not a bundle", ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			occupied := filepath.Join(t.TempDir(), "9.9.0-draft")
			if err := os.MkdirAll(occupied, 0o755); err != nil {
				t.Fatal(err)
			}
			keep := filepath.Join(occupied, "keep.json")
			if err := os.WriteFile(keep, []byte(`{"keep":true}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.lock != "" {
				if err := os.WriteFile(filepath.Join(occupied, "lock.json"), []byte(tc.lock), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := dispatch(source, occupied, later, "9.9.0-draft", false, true); err == nil {
				t.Fatal("--replace must refuse a destination that is not a staged bundle of the selector")
			}
			if body, err := os.ReadFile(keep); err != nil || string(body) != `{"keep":true}` {
				t.Fatalf("the refused destination was modified: %q %v", body, err)
			}
			assertNoStagingResidue(t, filepath.Dir(occupied))
		})
	}
}

// A git-ignored file is not in the commit the lock names, and a clean status
// does not report it, yet a directory the staged import lists would import it.
// Any ignored path under a directory the import reads refuses the import, and
// the destination is left as it was.
func TestStagedImportRefusesAGitIgnoredSourceFile(t *testing.T) {
	for _, tc := range []struct{ name, ignore, file, body string }{
		{"an ignored example", "examples/staged/local-*.json\n", "examples/staged/local-extra.json", `{"specVersion":"9.9.0-draft"}`},
		{"an ignored row pack", "conformance/evaluation/staged/rows-9/packs/scratch/\n", "conformance/evaluation/staged/rows-9/packs/scratch/p.json", `{"pack":"scratch"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, _ := stagedCheckout(t)
			if err := os.WriteFile(filepath.Join(source, ".gitignore"), []byte(tc.ignore), 0o644); err != nil {
				t.Fatal(err)
			}
			git(t, source, "add", ".gitignore")
			git(t, source, "commit", "-q", "-m", "ignore")
			out, err := exec.Command("git", "-C", source, "rev-parse", "HEAD").Output()
			if err != nil {
				t.Fatal(err)
			}
			commit := strings.TrimSpace(string(out))

			parent := t.TempDir()
			existing := filepath.Join(parent, "9.9.0-draft")
			if err := dispatch(source, existing, commit, "9.9.0-draft", false, false); err != nil {
				t.Fatalf("import before the ignored file exists: %v", err)
			}
			lockBefore, err := os.ReadFile(filepath.Join(existing, "lock.json"))
			if err != nil {
				t.Fatal(err)
			}

			full := filepath.Join(source, filepath.FromSlash(tc.file))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			status, err := exec.Command("git", "-C", source, "status", "--porcelain").Output()
			if err != nil || len(status) != 0 {
				t.Fatalf("the ignored file must leave the worktree clean: %q %v", status, err)
			}

			fresh := filepath.Join(t.TempDir(), "9.9.0-draft")
			err = dispatch(source, fresh, commit, "9.9.0-draft", false, false)
			want := "staged imports refuse a git-ignored source file, which the commit does not carry: " + tc.file
			if err == nil || err.Error() != want {
				t.Fatalf("got %v, want %q", err, want)
			}
			if _, err := os.Stat(fresh); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("a refused import must create no destination")
			}

			err = dispatch(source, existing, commit, "9.9.0-draft", false, true)
			if err == nil || err.Error() != want {
				t.Fatalf("--replace: got %v, want %q", err, want)
			}
			lockAfter, err := os.ReadFile(filepath.Join(existing, "lock.json"))
			if err != nil || string(lockAfter) != string(lockBefore) {
				t.Fatalf("the refused --replace changed the bundle's lock: %v", err)
			}
			if err := verifyMaterialized(existing, readLock(t, existing)); err != nil {
				t.Fatalf("the refused --replace changed the bundle: %v", err)
			}
			assertNoStagingResidue(t, parent)
		})
	}
}
