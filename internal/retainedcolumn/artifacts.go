// Package retainedcolumn verifies the independently installed retained-column leaf.
// It neither selects a Core version nor establishes publication or authenticity.
package retainedcolumn

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

const (
	Repository    = "https://github.com/Judgment-Pack/judgment-pack-spec"
	Release       = "v0.3.0"
	Version       = "0.3.0"
	Contract      = "retained-column/1"
	InventoryPath = "releases/v0.3.0.retained-column.json"
	SchemaPath    = "schema/retained-column/1/contract.schema.json"
	CorpusPath    = "conformance/retained-column/1/cases.json"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Identity struct {
	Version      string `json:"version"`
	Revision     string `json:"revision"`
	SchemaSHA256 string `json:"schemaSha256"`
	CorpusSHA256 string `json:"corpusSha256"`
}
type ReleaseIdentity struct {
	ReleaseVersion string `json:"releaseVersion"`
	Tag            string `json:"tag"`
	SpecVersion    string `json:"specVersion"`
	Maturity       string `json:"maturity"`
	// Distribution is present only on a release candidate's identity
	// ("release-candidate"). The installed final release carries none.
	Distribution string `json:"distribution,omitempty"`
	SourceCommit string `json:"sourceCommit"`
}
type Inventory struct {
	Contract     string            `json:"contract"`
	Distribution string            `json:"distribution"`
	Maturity     string            `json:"maturity"`
	Schema       string            `json:"schema"`
	SchemaSHA256 string            `json:"schemaSha256"`
	Corpus       string            `json:"corpus"`
	CorpusSHA256 string            `json:"corpusSha256"`
	Files        map[string]string `json:"files"`
	Claim        string            `json:"claim"`
}
type File struct {
	Path       string `json:"path"`
	ByteLength int    `json:"byteLength"`
	SHA256     string `json:"sha256"`
}
type Provenance struct {
	SourceRepository    string   `json:"sourceRepository"`
	SourceRelease       string   `json:"sourceRelease"`
	SourceRevision      string   `json:"sourceRevision"`
	SourceKind          string   `json:"sourceKind"`
	WorktreeDirty       bool     `json:"worktreeDirty"`
	SourceArchiveSHA256 string   `json:"sourceArchiveSha256"`
	InventorySHA256     string   `json:"inventorySha256"`
	Identity            Identity `json:"identity"`
	Files               []File   `json:"files"`
}

func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Inspect checks installation files against independently supplied source and
// inventory pins, never against a capability report or caller-selected identity.
func Inspect(source fs.FS, revision, inventoryPin string) (Identity, Inventory, error) {
	var id Identity
	var inv Inventory
	fail := func(message string) (Identity, Inventory, error) {
		return id, inv, fmt.Errorf("retained-column: %s", message)
	}
	if !commitPattern.MatchString(revision) || !digestPattern.MatchString(inventoryPin) {
		return fail("exact source and inventory pins are required")
	}
	raw, err := fs.ReadFile(source, "release-identity.json")
	var release ReleaseIdentity
	if err != nil || json.Unmarshal(raw, &release) != nil || release.ReleaseVersion != Version || release.Tag != Release || release.SpecVersion != "0.3.0" || release.Maturity != "draft" || release.Distribution != "" || release.SourceCommit != revision {
		return fail("release identity differs from the exact installation")
	}
	raw, err = fs.ReadFile(source, InventoryPath)
	if err != nil || Digest(raw) != inventoryPin || json.Unmarshal(raw, &inv) != nil {
		return fail("release inventory differs from its independent pin")
	}
	if inv.Contract != Contract || inv.Distribution != Release || inv.Maturity != "draft" || inv.Claim != "selected-leaf-support" || inv.Schema != SchemaPath || inv.Corpus != CorpusPath || !digestPattern.MatchString(inv.SchemaSHA256) || !digestPattern.MatchString(inv.CorpusSHA256) || inv.Files[SchemaPath] != inv.SchemaSHA256 || inv.Files[CorpusPath] != inv.CorpusSHA256 {
		return fail("release inventory identity is invalid")
	}
	for _, name := range []string{"spec/retained-column.md", "conformance/retained-column/1/manifest.json", "conformance/retained-column/1/capabilities.json", "conformance/retained-column/1/examples.json", "conformance/retained-column/1/preparations.json"} {
		if _, ok := inv.Files[name]; !ok {
			return fail("release inventory omits a required leaf input")
		}
	}
	for name, pin := range inv.Files {
		if !fs.ValidPath(name) || strings.Contains(name, "\\") || name == "provenance.json" || name == InventoryPath || name == "release-identity.json" || !digestPattern.MatchString(pin) {
			return fail("release inventory contains an invalid path or digest")
		}
		raw, err := fs.ReadFile(source, name)
		if err != nil || Digest(raw) != pin {
			return fail("release file differs from inventory: " + name)
		}
	}
	id = Identity{Version, revision, inv.SchemaSHA256, inv.CorpusSHA256}
	return id, inv, nil
}

// Verify checks the independently compiled provenance pin and the complete
// installed inventory, including generated release identity. Extra files fail.
func Verify(source fs.FS, provenancePin string, identity Identity) error {
	raw, err := fs.ReadFile(source, "provenance.json")
	if !digestPattern.MatchString(provenancePin) || err != nil || Digest(raw) != provenancePin {
		return fmt.Errorf("retained-column provenance differs from its independent pin")
	}
	var p Provenance
	if json.Unmarshal(raw, &p) != nil || p.SourceRepository != Repository || p.SourceRelease != Release || p.SourceKind != "immutable-git-ref" || p.WorktreeDirty || p.SourceRevision != identity.Revision || p.Identity != identity || !digestPattern.MatchString(p.SourceArchiveSHA256) {
		return fmt.Errorf("retained-column provenance identity is invalid")
	}
	installed, inv, err := Inspect(source, p.SourceRevision, p.InventorySHA256)
	if err != nil {
		return err
	}
	if installed != identity {
		return fmt.Errorf("retained-column installed identity differs from the supported identity")
	}
	expected := map[string]bool{"provenance.json": true}
	for _, file := range p.Files {
		if !fs.ValidPath(file.Path) || expected[file.Path] || file.ByteLength < 0 || !digestPattern.MatchString(file.SHA256) {
			return fmt.Errorf("retained-column provenance inventory is invalid")
		}
		raw, err := fs.ReadFile(source, file.Path)
		if err != nil || len(raw) != file.ByteLength || Digest(raw) != file.SHA256 {
			return fmt.Errorf("retained-column installed bytes differ: %s", file.Path)
		}
		expected[file.Path] = true
	}
	if !expected[InventoryPath] || !expected["release-identity.json"] || len(expected) != len(inv.Files)+3 {
		return fmt.Errorf("retained-column provenance inventory is incomplete")
	}
	for name := range inv.Files {
		if !expected[name] {
			return fmt.Errorf("retained-column provenance omits %s", name)
		}
	}
	return fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 || (!entry.IsDir() && !entry.Type().IsRegular()) {
			return fmt.Errorf("retained-column nonregular installed entry: %s", name)
		}
		if !entry.IsDir() && !expected[name] {
			return fmt.Errorf("unmanifested retained-column file: %s", name)
		}
		return nil
	})
}
