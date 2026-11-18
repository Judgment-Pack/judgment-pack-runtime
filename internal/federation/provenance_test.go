package federation_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The rows of registry format 1.1.0, proposed, copied byte for byte from the
// Spec revision testdata/provenance.json names. No release tag names them
// yet. The rows' second reader, the rows and this reader are by the same
// vendor: agreement on them is not a cross-vendor claim.
const (
	rowsDir      = "testdata/1.1.0"
	rowsRevision = "e0d5a986fc6e24e34bcc26631f2af6aa6b1b7515"
	rowsRelease  = "proposed (not tagged)"
)

func TestVendoredRowsKeepTheirUpstreamDigests(t *testing.T) {
	var pin struct {
		Repository  string   `json:"repository"`
		Release     string   `json:"release"`
		PullRequest []string `json:"pullRequest"`
		Revision    string   `json:"revision"`
		Files       map[string]struct {
			Upstream string `json:"upstream"`
			SHA256   string `json:"sha256"`
		} `json:"files"`
	}
	raw, err := os.ReadFile("testdata/provenance.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &pin); err != nil {
		t.Fatal(err)
	}
	pulls := []string{
		"https://github.com/Judgment-Pack/judgment-pack-spec/pull/383",
		"https://github.com/Judgment-Pack/judgment-pack-spec/pull/384",
		"https://github.com/Judgment-Pack/judgment-pack-spec/pull/386",
	}
	if pin.Repository != "https://github.com/Judgment-Pack/judgment-pack-spec" || pin.Release != rowsRelease || pin.Revision != rowsRevision || !slices.Equal(pin.PullRequest, pulls) {
		t.Fatalf("the rows are pinned to %s at %q, %s, %v", pin.Repository, pin.Release, pin.Revision, pin.PullRequest)
	}
	listed := 0
	err = filepath.WalkDir("testdata", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		name := filepath.ToSlash(path[len("testdata/"):])
		if name == "provenance.json" || name == "README.md" {
			return nil
		}
		file, ok := pin.Files[name]
		if !ok {
			t.Errorf("%s is vendored without a provenance entry", name)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Errorf("%s no longer has the bytes of %s at the pinned revision", name, file.Upstream)
		}
		listed++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if listed != len(pin.Files) {
		t.Fatalf("provenance lists %d files and %d are vendored", len(pin.Files), listed)
	}
}
