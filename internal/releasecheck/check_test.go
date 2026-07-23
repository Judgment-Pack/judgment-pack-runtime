package releasecheck

import (
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

func validLock() artifacts.Lock {
	return artifacts.Lock{
		FormatVersion: 1,
		SpecVersion:   "0.1.0-draft",
		Source: artifacts.LockSource{
			Repository: specificationRepository,
			Kind:       "immutable-git-ref",
			BaseCommit: testCommit,
			Ref:        "v0.1.0-draft",
		},
		BundleDigest: artifacts.Digest{
			Algorithm: "sha256-length-prefixed-v1",
			Value:     strings.Repeat("a", 64),
		},
		Files: []artifacts.FileLock{{Path: "schema.json", Bytes: 1, SHA256: strings.Repeat("b", 64)}},
	}
}

func TestValidateAcceptsReleaseReadyInput(t *testing.T) {
	if err := Validate("v0.1.0", validLock()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAcceptsExactCommitRef(t *testing.T) {
	lock := validLock()
	lock.Source.Ref = testCommit
	if err := Validate("v1.2.3-rc.1", lock); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsUnsafeReleaseInput(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		edit func(*artifacts.Lock)
	}{
		{name: "tag without v", tag: "0.1.0"},
		{name: "partial tag", tag: "v0.1"},
		{name: "leading zero", tag: "v00.1.0"},
		{name: "wrong repository", tag: "v0.1.0", edit: func(lock *artifacts.Lock) { lock.Source.Repository = "https://example.test/spec" }},
		{name: "local snapshot", tag: "v0.1.0", edit: func(lock *artifacts.Lock) { lock.Source.Kind = "unreleased-local-snapshot" }},
		{name: "dirty", tag: "v0.1.0", edit: func(lock *artifacts.Lock) { lock.Source.WorktreeDirty = true }},
		{name: "short commit", tag: "v0.1.0", edit: func(lock *artifacts.Lock) { lock.Source.BaseCommit = "abc123" }},
		{name: "moving branch", tag: "v0.1.0", edit: func(lock *artifacts.Lock) { lock.Source.Ref = "main" }},
		{name: "wrong commit ref", tag: "v0.1.0", edit: func(lock *artifacts.Lock) { lock.Source.Ref = strings.Repeat("f", 40) }},
		{name: "unknown digest", tag: "v0.1.0", edit: func(lock *artifacts.Lock) { lock.BundleDigest.Algorithm = "sha256" }},
		{name: "empty files", tag: "v0.1.0", edit: func(lock *artifacts.Lock) { lock.Files = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lock := validLock()
			if test.edit != nil {
				test.edit(&lock)
			}
			if err := Validate(test.tag, lock); err == nil {
				t.Fatal("expected release input to be rejected")
			}
		})
	}
}

func TestCurrentEmbeddedArtifactsPassReleaseGate(t *testing.T) {
	if err := CheckEmbedded("v0.1.0"); err != nil {
		t.Fatalf("release-pinned artifacts failed the release gate: %v", err)
	}
}
