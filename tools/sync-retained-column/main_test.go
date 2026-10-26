package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/retainedcolumn"
)

// Handwritten synthetic archive inputs; no published/adopted release is used.
func syntheticArchive(t *testing.T) ([]byte, options, fstest.MapFS) {
	t.Helper()
	files := fstest.MapFS{}
	inv := retainedcolumn.Inventory{Contract: retainedcolumn.Contract, Distribution: retainedcolumn.Release, Maturity: "draft", Schema: retainedcolumn.SchemaPath, Corpus: retainedcolumn.CorpusPath, Claim: "selected-leaf-support", Files: map[string]string{}}
	for _, name := range []string{retainedcolumn.SchemaPath, retainedcolumn.CorpusPath, "spec/retained-column.md", "conformance/retained-column/1/manifest.json", "conformance/retained-column/1/capabilities.json", "conformance/retained-column/1/examples.json", "conformance/retained-column/1/preparations.json"} {
		raw := []byte("synthetic: " + name + "\n")
		files[name] = &fstest.MapFile{Data: raw}
		inv.Files[name] = retainedcolumn.Digest(raw)
	}
	inv.SchemaSHA256 = inv.Files[inv.Schema]
	inv.CorpusSHA256 = inv.Files[inv.Corpus]
	encode := func(name string, v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	encode(retainedcolumn.InventoryPath, inv)
	revision := strings.Repeat("1", 40)
	encode("release-identity.json", retainedcolumn.ReleaseIdentity{ReleaseVersion: retainedcolumn.Version, Tag: retainedcolumn.Release, SpecVersion: "0.3.0", Maturity: "draft", Distribution: "release-candidate", SourceCommit: revision})
	raw := makeArchive(t, files, nil)
	return raw, options{archivePin: retainedcolumn.Digest(raw), revision: revision, inventoryPin: retainedcolumn.Digest(files[retainedcolumn.InventoryPath].Data)}, files
}
func makeArchive(t *testing.T, files fstest.MapFS, extra *tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	zip := gzip.NewWriter(&out)
	writer := tar.NewWriter(zip)
	for name, file := range files {
		if err := writer.WriteHeader(&tar.Header{Name: archivePrefix + name, Mode: 0644, Size: int64(len(file.Data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(file.Data); err != nil {
			t.Fatal(err)
		}
	}
	if extra != nil {
		if err := writer.WriteHeader(extra); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func TestPrepareSyntheticArchiveAndRefusals(t *testing.T) {
	for _, name := range []string{"valid", "archive pin", "inventory pin", "source identity", "source bytes", "missing source", "missing schema", "wrong schema", "wrong inventory identity", "missing release identity", "placeholder source", "bad gzip checksum", "traversal", "symlink", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			raw, o, files := syntheticArchive(t)
			source := func(name string) ([]byte, error) {
				file, ok := files[name]
				if !ok {
					return nil, fmt.Errorf("missing source")
				}
				return file.Data, nil
			}
			rearchive := func() { raw = makeArchive(t, files, nil); o.archivePin = retainedcolumn.Digest(raw) }
			switch name {
			case "archive pin":
				o.archivePin = "sha256:" + strings.Repeat("0", 64)
			case "inventory pin":
				o.inventoryPin = "sha256:" + strings.Repeat("0", 64)
			case "source identity":
				o.revision = strings.Repeat("2", 40)
			case "source bytes":
				source = func(string) ([]byte, error) { return []byte("mismatch"), nil }
			case "missing source":
				source = func(string) ([]byte, error) { return nil, fmt.Errorf("source missing") }
			case "missing schema":
				delete(files, retainedcolumn.SchemaPath)
				rearchive()
			case "wrong schema":
				files[retainedcolumn.SchemaPath].Data = []byte("{}")
				rearchive()
			case "wrong inventory identity":
				var inv retainedcolumn.Inventory
				_ = json.Unmarshal(files[retainedcolumn.InventoryPath].Data, &inv)
				inv.Contract = "wrong/1"
				files[retainedcolumn.InventoryPath].Data, _ = json.Marshal(inv)
				o.inventoryPin = retainedcolumn.Digest(files[retainedcolumn.InventoryPath].Data)
				rearchive()
			case "missing release identity":
				delete(files, "release-identity.json")
				rearchive()
			case "placeholder source":
				o.revision = "main"
			case "bad gzip checksum":
				raw[len(raw)-8] ^= 128
				o.archivePin = retainedcolumn.Digest(raw)
			case "traversal", "symlink", "duplicate":
				extra := &tar.Header{Name: archivePrefix + "../outside", Mode: 0644}
				if name == "symlink" {
					extra.Name = archivePrefix + "link"
					extra.Typeflag = tar.TypeSymlink
					extra.Linkname = "outside"
				}
				if name == "duplicate" {
					extra.Name = archivePrefix + retainedcolumn.SchemaPath
				}
				raw = makeArchive(t, files, extra)
				o.archivePin = retainedcolumn.Digest(raw)
			}
			selected, pin, err := prepare(raw, o, source)
			if name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				if len(selected) != len(files)+1 || pin != retainedcolumn.Digest(selected["provenance.json"].Data) {
					t.Fatal("incomplete synthetic import")
				}
				if err := retainedcolumn.Verify(selected, pin, importIdentity(selected)); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe synthetic import accepted")
			}
		})
	}
}
func TestVerifySourceRefusals(t *testing.T) {
	for _, name := range []string{"valid", "moving ref", "wrong origin", "dirty", "untracked", "wrong HEAD", "wrong tag"} {
		t.Run(name, func(t *testing.T) {
			o := options{source: "synthetic-source", sourceRef: retainedcolumn.Release, revision: strings.Repeat("1", 40)}
			if name == "moving ref" {
				o.sourceRef = "main"
			}
			read := func(_ string, args ...string) ([]byte, error) {
				switch args[0] {
				case "remote":
					if name == "wrong origin" {
						return []byte("https://example.invalid/spec"), nil
					}
					return []byte(retainedcolumn.Repository), nil
				case "status":
					if name == "dirty" {
						return []byte(" M schema.json\n"), nil
					}
					if name == "untracked" {
						return []byte("?? new.json\n"), nil
					}
					return nil, nil
				case "rev-parse":
					if name == "wrong HEAD" && args[2] == "HEAD^{commit}" || name == "wrong tag" && strings.HasPrefix(args[2], "refs/tags/") {
						return []byte(strings.Repeat("2", 40)), nil
					}
					return []byte(o.revision), nil
				}
				return nil, fmt.Errorf("unexpected source command")
			}
			err := verifySourceWith(o, read)
			if name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe source accepted")
			}
		})
	}
}

func TestRunRefusesExistingDestination(t *testing.T) {
	destination := t.TempDir()
	sentinel := filepath.Join(destination, "existing.txt")
	if err := os.WriteFile(sentinel, []byte("preserve existing artifact bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(options{destination: destination, archive: "unused-synthetic-input"}); err == nil || !strings.Contains(err.Error(), "destination must not exist") {
		t.Fatal("existing destination was not refused", err)
	}
	raw, err := os.ReadFile(sentinel)
	if err != nil || string(raw) != "preserve existing artifact bytes" {
		t.Fatal("existing destination changed", err)
	}
}
