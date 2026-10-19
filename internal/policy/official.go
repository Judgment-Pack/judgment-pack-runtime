package policy

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"
)

// official retains published source paths so the unchanged reference reader can
// run against its frozen schemas, complete corpus and shared canonicalizer.
//
//go:embed official
var official embed.FS

const officialProvenanceDigest = "sha256:398735dc63478806ff77d75af61ef3dd76459ffbef454cb4bdb1b25bc54f3e25"

var officialOnce sync.Once
var officialError error

// VerifyOfficialArtifacts verifies the independently pinned manifest and every
// retained release file. The same check guards schema compilation and releases;
// no network resolution or publication/authority assertion is involved.
func VerifyOfficialArtifacts() error {
	officialOnce.Do(func() { officialError = verifyOfficialArtifacts(official) })
	return officialError
}

func verifyOfficialArtifacts(source fs.FS) error {
	raw, err := fs.ReadFile(source, "official/provenance.json")
	if err != nil || digest(raw) != officialProvenanceDigest {
		return fmt.Errorf("official Policy draft provenance does not match its independent pin")
	}
	var manifest struct {
		SourceRepository string `json:"sourceRepository"`
		SourceRevision   string `json:"sourceRevision"`
		SourceRelease    string `json:"sourceRelease"`
		FormatVersion    string `json:"formatVersion"`
		Maturity         string `json:"maturity"`
		Files            []struct {
			SourcePath string `json:"sourcePath"`
			Path       string `json:"path"`
			SHA256     string `json:"sha256"`
			ByteLength int    `json:"byteLength"`
		} `json:"files"`
	}
	if json.Unmarshal(raw, &manifest) != nil || manifest.SourceRepository != "https://github.com/Judgment-Pack/judgment-pack-spec" || manifest.SourceRevision != DraftRevision || manifest.SourceRelease != DraftRelease || manifest.FormatVersion != CandidateVersion || manifest.Maturity != "draft" || len(manifest.Files) == 0 {
		return fmt.Errorf("official Policy draft provenance identity is invalid")
	}
	expected := map[string]bool{"official/provenance.json": true}
	for _, file := range manifest.Files {
		if !fs.ValidPath(file.Path) || file.Path != file.SourcePath || expected["official/"+file.Path] {
			return fmt.Errorf("official Policy draft manifest inventory is invalid")
		}
		name := "official/" + file.Path
		raw, err := fs.ReadFile(source, name)
		if err != nil || len(raw) != file.ByteLength || digest(raw) != "sha256:"+file.SHA256 {
			return fmt.Errorf("official Policy draft file differs from its pin: %s", file.Path)
		}
		expected[name] = true
	}
	return fs.WalkDir(source, "official", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && !expected[name] {
			return fmt.Errorf("unmanifested official Policy draft file: %s", strings.TrimPrefix(name, "official/"))
		}
		return nil
	})
}
