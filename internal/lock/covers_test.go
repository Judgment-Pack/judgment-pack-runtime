package lock

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Decode is the reader Open uses, not a second one: for every lock a project
// refuses, a caller holding the same bytes is refused with the same code, exit
// and words, and a lock a project accepts decodes to the same set.
func TestDecodeIsTheReaderOpenUses(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for name, contents := range map[string]string{
		"not one JSON value":           `{"lockVersion":"1"}{}`,
		"a duplicate member":           `{"lockVersion":"1","lockVersion":"1","config":{"digest":"` + digest + `"}}`,
		"not an object":                `["lockVersion"]`,
		"no lockVersion":               `{"config":{"digest":"` + digest + `"}}`,
		"a lockVersion we do not read": `{"lockVersion":"2"}`,
		"no configuration digest":      `{"lockVersion":"1","config":{}}`,
		"an entry with no path":        `{"lockVersion":"1","config":{"digest":"` + digest + `"},"graphs":{"g":{"digest":"` + digest + `"}}}`,
		"an entry with no digest":      `{"lockVersion":"1","config":{"digest":"` + digest + `"},"packs":{"intake":{"path":"p.json"}}}`,
		"a member in another case":     `{"lockVersion":"1","config":{"digest":"` + digest + `"},"Config":{"digest":"` + digest + `"}}`,
		"accepted":                     `{"lockVersion":"1","config":{"digest":"` + digest + `"},"packs":{"intake":{"path":"p.json","digest":"` + digest + `"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			configPath, root := lockedProject(t, oneGoodConfig, map[string]string{"packs/intake.json": packFixture(t)})
			if err := os.WriteFile(filepath.Join(root, "jpack.lock.json"), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded := open(t, configPath)
			fromProject, projectFailure := Open(loaded)
			fromBytes, bytesFailure := Decode(loaded.LockPath(), []byte(contents))
			if !reflect.DeepEqual(fromProject, fromBytes) || !reflect.DeepEqual(projectFailure, bytesFailure) {
				t.Fatalf("Open=(%+v, %+v) Decode=(%+v, %+v)", fromProject, projectFailure, fromBytes, bytesFailure)
			}
			if name == "accepted" {
				if fromBytes == nil || fromBytes.Digest != Digest([]byte(contents)) {
					t.Fatalf("set = %+v", fromBytes)
				}
			} else if bytesFailure == nil || !strings.Contains(bytesFailure.Message, loaded.LockPath()) {
				t.Fatalf("failure = %+v", bytesFailure)
			}
		})
	}
}

// Without a configuration, Covers asks the two questions the bytes and the
// lock can answer and nothing about a configuration, whatever the lock pins
// for one. Each kind answers from its own entries.
func TestCoversWithoutAConfigurationComparesOnlyTheBytes(t *testing.T) {
	pack, graphBytes := []byte("pack bytes"), []byte("graph bytes")
	document := Document{
		LockVersion: Version,
		Config:      Config{Digest: "sha256:" + strings.Repeat("0", 64)},
		Packs:       map[string]Entry{"shared": {Path: "packs/shared.json", Digest: Digest(pack)}},
		Graphs:      map[string]Entry{"shared": {Path: "graphs/shared.json", Digest: Digest(graphBytes)}},
	}
	if checks := Covers(document, AppliedPack("shared", pack), nil); len(checks) != 0 {
		t.Fatalf("checks = %+v", checks)
	}
	if checks := Covers(document, AppliedGraph("shared", Digest(graphBytes)), nil); len(checks) != 0 {
		t.Fatalf("checks = %+v", checks)
	}
	want := []Check{{Name: CheckDocumentDrift, Kind: "graph", ID: "shared", Path: "graphs/shared.json", Detail: "The graph document's bytes differ from the reviewed set."}}
	if checks := Covers(document, AppliedGraph("shared", Digest(pack)), nil); !reflect.DeepEqual(checks, want) {
		t.Fatalf("checks = %+v", checks)
	}
	want = []Check{{Name: CheckLockEntryMissing, Kind: "pack", ID: "absent", Detail: "The reviewed set does not name this pack."}}
	if checks := Covers(document, AppliedPack("absent", pack), nil); !reflect.DeepEqual(checks, want) {
		t.Fatalf("checks = %+v", checks)
	}
}

// With the configuration a project's own lock was generated from, Covers
// agrees with Verify about that project's one document: the same findings,
// named and worded alike.
func TestCoversAgreesWithVerifyForTheConfigurationTheLockPins(t *testing.T) {
	configPath, root := lockedProject(t, oneGoodConfig, map[string]string{"packs/intake.json": packFixture(t)})
	declare(t, open(t, configPath))
	edited := packFixture(t) + " "
	if err := os.WriteFile(filepath.Join(root, "packs", "intake.json"), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := open(t, configPath)
	set, failure := Open(loaded)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	verified := Verify(loaded, set.Document)
	covered := Covers(set.Document, AppliedPack("intake", []byte(edited)), loaded)
	if len(verified) != 1 || !reflect.DeepEqual(verified, covered) {
		t.Fatalf("Verify = %+v, Covers = %+v", verified, covered)
	}
}
