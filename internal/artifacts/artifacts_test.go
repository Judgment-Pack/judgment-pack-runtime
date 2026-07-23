package artifacts

import "testing"

func TestEmbeddedReleaseArtifactIntegrityAndProvenance(t *testing.T) {
	set, err := Load(DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	lock := set.Lock()
	if lock.Source.Repository != "https://github.com/protossai/judgment-pack-spec" ||
		lock.Source.Kind != "immutable-git-ref" ||
		lock.Source.BaseCommit != "80958f50c851e9809cb8036a23622391cf437c99" ||
		lock.Source.Ref != "v0.1.0-draft" ||
		lock.Source.WorktreeDirty {
		t.Fatalf("embedded artifacts must remain pinned to the approved JPS release: %#v", lock.Source)
	}
	if len(lock.Files) != 50 || len(lock.BundleDigest.Value) != 64 {
		t.Fatalf("unexpected lock contents: files=%d digest=%q", len(lock.Files), lock.BundleDigest.Value)
	}
}

func TestUnknownVersionIsNotSubstituted(t *testing.T) {
	if _, err := Load("0.1"); err == nil {
		t.Fatal("expected an exact-version error")
	}
}
