package federation

import (
	"bytes"
	"slices"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

var lockSeeds = []string{
	"locks/single-42.lock.json",
	"locks/single-43-emitters.lock.json",
	"locks/assembled-8-emitters.lock.json",
	"vectors/check-final-line-feed/lock.json",
	"vectors/check-not-canonical/lock.json",
	"vectors/check-lock-form/lock.json",
}

func platformExpect() Expect {
	id, require := "https://example.invalid/registries/platform", false
	return Expect{RegistryID: &id, RequireIndex: &require}
}

// FuzzLock reads a fuzzed lock as §9's step 1 does: a lock it accepts is of
// its form, and its bytes, without one final line feed, are its JCS bytes.
func FuzzLock(f *testing.F) {
	for _, name := range lockSeeds {
		f.Add(testdataFile(f, name))
	}
	f.Add([]byte(`{"lockFormatVersion":"1.1.0","serial":9007199254740992,"registry":{"id":"a","id":"b"}}`))
	f.Add([]byte(`{"emitters":[{"files":[{"mode":"0644","path":"a"},{"mode":"0755","path":"a"}],"path":"x"}],"lockFormatVersion":"1.1.0","serial":1.0}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		read := func() (map[string]any, []string) {
			collected := &collector{}
			root := readLock(registry.Document{Data: data}, platformExpect(), collected)
			return root, found(collected.kept)
		}
		var root map[string]any
		var codes []string
		withinBudget(t, allocatedBy(func() { root, codes = read() }), len(data), 64, 1<<20)
		again, second := read()
		if !slices.Equal(codes, second) || (root == nil) != (again == nil) {
			t.Fatalf("two reads differ:\n%q\n%q", codes, second)
		}
		if root == nil {
			if len(codes) == 0 {
				t.Fatal("a lock refused with no diagnostic")
			}
			return
		}
		if len(codes) > 0 || len(lockForm(root)) > 0 {
			t.Fatalf("a lock accepted with %q", codes)
		}
		canonical, err := jcs.EncodeDocument(root)
		if err != nil || !bytes.Equal(canonical, bytes.TrimSuffix(data, []byte("\n"))) {
			t.Fatalf("an accepted lock is not its JCS bytes: %s", data)
		}
	})
}

// FuzzCheckLock checks a fuzzed lock against the single registry, its kinds
// and an emitter manifest with its files: a check passes only for a lock
// that names that registry, its kinds and its resolved digests, and names
// no emitters or those the manifest's files are.
func FuzzCheckLock(f *testing.F) {
	for _, name := range lockSeeds {
		f.Add(testdataFile(f, name))
	}
	in := CheckInput{
		Registry: registry.Document{Data: testdataFile(f, "single/registry.json")},
		Kinds:    []registry.Document{{Data: testdataFile(f, "kinds/database.json")}, {Data: testdataFile(f, "kinds/db-role.json")}},
		Emitters: []Emitter{{
			Path:     "emitters/db-roles.json",
			Manifest: registry.Document{Data: testdataFile(f, "emitters/db-roles.json")},
			Files: []EmitterFile{
				{Path: "gen/db-roles.sql", Mode: ModeRegular, Digest: FileDigest(testdataFile(f, "emitters/gen/db-roles.sql"))},
				{Path: "gen/grant.sh", Mode: ModeExecutable, Digest: FileDigest(testdataFile(f, "emitters/gen/grant.sh"))},
			},
		}},
		Expect: platformExpect(),
	}
	withoutSerial := func(t *testing.T, data []byte, keep bool) map[string]any {
		root, _ := registry.AdmitDocument(data, MaxLockBytes, "lockFormatVersion", LockFormatVersion, registry.Location{Document: DocumentLock})
		if root == nil {
			t.Fatalf("a lock that is not admitted: %s", data)
		}
		delete(root, "serial")
		if !keep {
			delete(root, "emitters")
		}
		return root
	}
	single := testdataFile(f, "locks/single-42.lock.json")
	emitters := testdataFile(f, "locks/single-43-emitters.lock.json")
	f.Fuzz(func(t *testing.T, data []byte) {
		in := in
		in.Lock = registry.Document{Data: data}
		var report Report
		var err error
		allocated := allocatedBy(func() { report, err = CheckLock(in) })
		if err != nil {
			t.Fatal(err)
		}
		withinBudget(t, allocated, len(data), 256, 64<<20)
		checkReport(t, report)
		again, err := CheckLock(in)
		if err != nil {
			t.Fatal(err)
		}
		first, _ := report.Canonical()
		second, _ := again.Canonical()
		if !bytes.Equal(first, second) {
			t.Fatalf("two checks differ:\n%s\n%s", first, second)
		}
		if !report.Valid {
			return
		}
		got := withoutSerial(t, data, true)
		if _, named := got["emitters"]; named {
			if !registry.JSONEqual(got, withoutSerial(t, emitters, true)) {
				t.Fatalf("a check passed a lock whose emitters are not the files': %s", data)
			}
			return
		}
		if !registry.JSONEqual(got, withoutSerial(t, single, false)) {
			t.Fatalf("a check passed a lock that is not the registry's: %s", data)
		}
	})
}
