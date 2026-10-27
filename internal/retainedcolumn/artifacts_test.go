package retainedcolumn

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

// This installation is handwritten synthetic test data. Its bytes, source and
// digests are deliberately unrelated to any published Spec release.
func syntheticInstallation(t *testing.T) (fstest.MapFS, string, Identity) {
	t.Helper()
	revision := strings.Repeat("1", 40)
	files := fstest.MapFS{}
	names := []string{SchemaPath, CorpusPath, "spec/retained-column.md", "conformance/retained-column/1/manifest.json", "conformance/retained-column/1/capabilities.json", "conformance/retained-column/1/examples.json", "conformance/retained-column/1/preparations.json"}
	inventory := Inventory{Contract: Contract, Distribution: Release, Maturity: "draft", Schema: SchemaPath, Corpus: CorpusPath, Claim: "selected-leaf-support", Files: map[string]string{}}
	for _, name := range names {
		raw := []byte("synthetic fixture: " + name + "\n")
		files[name] = &fstest.MapFile{Data: raw}
		inventory.Files[name] = Digest(raw)
	}
	inventory.SchemaSHA256 = inventory.Files[SchemaPath]
	inventory.CorpusSHA256 = inventory.Files[CorpusPath]
	encode := func(name string, value any) {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	encode(InventoryPath, inventory)
	encode("release-identity.json", ReleaseIdentity{Version, Release, "0.3.0", "draft", "release-candidate", revision})
	identity := Identity{Version, revision, inventory.SchemaSHA256, inventory.CorpusSHA256}
	p := Provenance{SourceRepository: Repository, SourceRelease: Release, SourceRevision: revision, SourceKind: "immutable-git-ref", SourceArchiveSHA256: "sha256:" + strings.Repeat("2", 64), InventorySHA256: Digest(files[InventoryPath].Data), Identity: identity}
	for name, file := range files {
		p.Files = append(p.Files, File{name, len(file.Data), Digest(file.Data)})
	}
	encode("provenance.json", p)
	return files, Digest(files["provenance.json"].Data), identity
}
func TestVerifySyntheticInstallationAndRefusals(t *testing.T) {
	for _, name := range []string{"valid", "manifest pin", "selected identity", "archive identity", "dirty", "moving source", "wrong repository", "missing schema", "changed schema", "changed corpus", "changed inventory", "changed release identity", "extra file", "unmanifested inventory", "duplicate inventory", "bad length"} {
		t.Run(name, func(t *testing.T) {
			files, pin, identity := syntheticInstallation(t)
			editManifest := func(edit func(*Provenance)) {
				var p Provenance
				if err := json.Unmarshal(files["provenance.json"].Data, &p); err != nil {
					t.Fatal(err)
				}
				edit(&p)
				raw, _ := json.Marshal(p)
				files["provenance.json"] = &fstest.MapFile{Data: raw}
				pin = Digest(raw)
			}
			switch name {
			case "manifest pin":
				pin = "sha256:" + strings.Repeat("0", 64)
			case "selected identity":
				identity.Revision = strings.Repeat("3", 40)
			case "archive identity":
				editManifest(func(p *Provenance) { p.SourceArchiveSHA256 = "placeholder" })
			case "dirty":
				editManifest(func(p *Provenance) { p.WorktreeDirty = true })
			case "moving source":
				editManifest(func(p *Provenance) { p.SourceKind = "branch" })
			case "wrong repository":
				editManifest(func(p *Provenance) { p.SourceRepository = "https://example.invalid/spec" })
			case "missing schema":
				delete(files, SchemaPath)
			case "changed schema":
				files[SchemaPath].Data = []byte("{}")
			case "changed corpus":
				files[CorpusPath].Data = []byte("[]")
			case "changed inventory":
				files[InventoryPath].Data = append(files[InventoryPath].Data, ' ')
			case "changed release identity":
				files["release-identity.json"].Data = []byte("{}")
			case "extra file":
				files["extra.json"] = &fstest.MapFile{Data: []byte("{}")}
			case "unmanifested inventory":
				editManifest(func(p *Provenance) { p.Files = p.Files[1:] })
			case "duplicate inventory":
				editManifest(func(p *Provenance) { p.Files = append(p.Files, p.Files[0]) })
			case "bad length":
				editManifest(func(p *Provenance) { p.Files[0].ByteLength++ })
			}
			err := Verify(files, pin, identity)
			if name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe synthetic installation accepted")
			}
		})
	}
}
