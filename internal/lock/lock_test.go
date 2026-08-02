package lock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// packFixture is the evaluator package's own conformant pack, reused so these
// tests are about the reviewed set rather than about a pack written for them.
func packFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// lockedProject lays out one project, writes its lock, and returns both the
// loaded project and the directory, so a test can drift a file and reload.
func lockedProject(t *testing.T, config string, files map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, root
}

// open loads one project for a test and closes it when the test ends.
func open(t *testing.T, configPath string) *project.Project {
	t.Helper()
	loaded, failure := project.Load(configPath)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	t.Cleanup(func() { loaded.Close() })
	return loaded
}

// declare generates and writes the reviewed set for one project.
func declare(t *testing.T, loaded *project.Project) {
	t.Helper()
	document, failure := Generate(loaded)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	contents, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.WriteLock(contents); err != nil {
		t.Fatal(err)
	}
}

const oneGoodConfig = `{"configVersion":"1","packs":{"intake":{"path":"packs/intake.json"}}}`

// The lock's filename is derived from the configuration's, never declared
// inside it: a configuration that named its own lock could rename it, and a
// reader following the rename would verify against whatever the edit chose.
func TestTheLockIsFoundByConventionAndNotByDeclaration(t *testing.T) {
	for configName, want := range map[string]string{
		"jpack.json":         "jpack.lock.json",
		"jpack.staging.json": "jpack.staging.lock.json",
	} {
		got, ok := project.LockNameFor(configName)
		if !ok || got != want {
			t.Fatalf("LockNameFor(%q) = %q,%v, want %q", configName, got, ok, want)
		}
	}
	// The derivation has to be one-to-one, or two configurations in one
	// directory share a reviewed set and each denies the other the convention.
	// A name that does not end in .json has no lock name rather than a shared
	// one: trimming a suffix that is not there maps "jpack" onto "jpack.json"'s.
	for _, configName := range []string{"jpack", "jpack.yaml", ".json", "packs"} {
		if got, ok := project.LockNameFor(configName); ok {
			t.Fatalf("LockNameFor(%q) = %q, want no derivation", configName, got)
		}
	}
	// And nothing in the configuration schema names it: the shape is closed, so
	// a member pointing at a lock would be refused outright.
	if strings.Contains(string(project.Schema()), "lock") {
		t.Fatal("the configuration schema must not name the lock: its presence is the opt-in, not a declaration")
	}
}

// Generate reads every declared document through the project's own handle and
// refuses rather than silently declaring a smaller set than the project has.
func TestGenerateRefusesADocumentItCannotRead(t *testing.T) {
	configPath, _ := lockedProject(t, oneGoodConfig, nil)
	loaded := open(t, configPath)
	if _, failure := Generate(loaded); failure == nil || failure.ExitCode != result.ExitIO {
		t.Fatalf("failure = %+v", failure)
	}
}

// Verify reports every difference by name and in a deterministic order.
func TestVerifyNamesEveryDifference(t *testing.T) {
	configPath, root := lockedProject(t, oneGoodConfig, map[string]string{"packs/intake.json": packFixture(t)})
	declare(t, open(t, configPath))

	loaded := open(t, configPath)
	document, failure := Load(loaded)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if checks := Verify(loaded, document); len(checks) != 0 {
		t.Fatalf("an unchanged project has nothing to report: %+v", checks)
	}

	// One drifted document, and the finding names it.
	if err := os.WriteFile(filepath.Join(root, "packs", "intake.json"), []byte(packFixture(t)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	drifted := open(t, configPath)
	checks := Verify(drifted, document)
	if len(checks) != 1 || checks[0].Name != CheckDocumentDrift || checks[0].ID != "intake" {
		t.Fatalf("checks = %+v", checks)
	}

	// A configuration that drifted too: the configuration is reported first,
	// because it is what says which pack an id names.
	if err := os.WriteFile(configPath, []byte(oneGoodConfig+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	both := open(t, configPath)
	checks = Verify(both, document)
	if len(checks) != 2 || checks[0].Name != CheckConfigDrift || checks[1].Name != CheckDocumentDrift {
		t.Fatalf("checks = %+v", checks)
	}
}

// Consult is the whole of what a deciding surface does about the lock, and its
// four answers are the four cases a surface has to tell apart.
func TestConsultAnswersTheFourCasesADecidingSurfaceHas(t *testing.T) {
	t.Run("no project in scope", func(t *testing.T) {
		reviewed, failure := Consult(nil, nil, false)
		if reviewed != nil || failure != nil {
			t.Fatalf("reviewed=%v failure=%v", reviewed, failure)
		}
	})

	t.Run("a project that declares no reviewed set", func(t *testing.T) {
		configPath, _ := lockedProject(t, oneGoodConfig, map[string]string{"packs/intake.json": packFixture(t)})
		loaded := open(t, configPath)
		reviewed, failure := Consult(loaded, []Applied{AppliedPack("intake", []byte(packFixture(t)))}, false)
		if reviewed != nil || failure != nil {
			t.Fatalf("a project without a lock reaches nothing: reviewed=%v failure=%v", reviewed, failure)
		}
	})

	t.Run("declared law that verified", func(t *testing.T) {
		configPath, _ := lockedProject(t, oneGoodConfig, map[string]string{"packs/intake.json": packFixture(t)})
		declare(t, open(t, configPath))
		reviewed, failure := Consult(open(t, configPath), []Applied{AppliedPack("intake", []byte(packFixture(t)))}, false)
		if failure != nil || reviewed == nil || !*reviewed {
			t.Fatalf("reviewed=%v failure=%v", reviewed, failure)
		}
	})

	t.Run("a draft", func(t *testing.T) {
		configPath, _ := lockedProject(t, oneGoodConfig, map[string]string{"packs/intake.json": packFixture(t)})
		declare(t, open(t, configPath))
		// No declared id is involved, so nothing is verified and the run is not
		// a reviewed one. It is never refused: drafting is the author's loop.
		reviewed, failure := Consult(open(t, configPath), nil, true)
		if failure != nil || reviewed == nil || *reviewed {
			t.Fatalf("reviewed=%v failure=%v", reviewed, failure)
		}
	})

	t.Run("law that left the reviewed set", func(t *testing.T) {
		configPath, root := lockedProject(t, oneGoodConfig, map[string]string{"packs/intake.json": packFixture(t)})
		declare(t, open(t, configPath))
		if err := os.WriteFile(filepath.Join(root, "packs", "intake.json"), []byte(packFixture(t)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		reviewed, failure := Consult(open(t, configPath), []Applied{AppliedPack("intake", []byte(packFixture(t)+"\n"))}, false)
		if reviewed != nil || failure == nil {
			t.Fatalf("reviewed=%v failure=%v", reviewed, failure)
		}
		if failure.Code != FailureCode || failure.ExitCode != result.ExitInvalid {
			t.Fatalf("failure = %+v", failure)
		}
		// The refusal names both honest ways forward. An amendment is
		// legitimate and this runtime cannot tell one from tampering, so a
		// message that only refused would train a reader to edit until it stops.
		for _, required := range []string{"jpack packs lock", "restore the reviewed bytes", CheckDocumentDrift} {
			if !strings.Contains(failure.Message, required) {
				t.Fatalf("the refusal must carry %q: %q", required, failure.Message)
			}
		}
	})

	t.Run("an unrelated document's drift does not refuse this decision", func(t *testing.T) {
		config := `{"configVersion":"1","packs":{
		  "intake":{"path":"packs/intake.json"},
		  "other":{"path":"packs/other.json"}
		}}`
		configPath, root := lockedProject(t, config, map[string]string{
			"packs/intake.json": packFixture(t),
			"packs/other.json":  packFixture(t),
		})
		declare(t, open(t, configPath))
		if err := os.WriteFile(filepath.Join(root, "packs", "other.json"), []byte(packFixture(t)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		reviewed, failure := Consult(open(t, configPath), []Applied{AppliedPack("intake", []byte(packFixture(t)))}, false)
		if failure != nil || reviewed == nil || !*reviewed {
			t.Fatalf("a decision is about the law it applies: reviewed=%v failure=%v", reviewed, failure)
		}
		// packs verify, which asks about the whole project, does see it.
		loaded := open(t, configPath)
		document, _ := Load(loaded)
		if checks := Verify(loaded, document); len(checks) != 1 || checks[0].ID != "other" {
			t.Fatalf("checks = %+v", checks)
		}
	})
}

// A lock this runtime cannot read is a defect to report and never a project
// that declined the convention: fail-closed, on every deciding surface.
func TestALockThatCannotBeReadRefusesRatherThanBeingIgnored(t *testing.T) {
	for name, contents := range map[string]string{
		"not JSON at all":       `{ this is not a lock`,
		"not an object":         `["lockVersion"]`,
		"a version we not read": `{"lockVersion":"2","config":{"digest":"sha256:x"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			configPath, root := lockedProject(t, oneGoodConfig, map[string]string{"packs/intake.json": packFixture(t)})
			if err := os.WriteFile(filepath.Join(root, "jpack.lock.json"), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			reviewed, failure := Consult(open(t, configPath), []Applied{AppliedPack("intake", []byte(packFixture(t)))}, false)
			if reviewed != nil || failure == nil {
				t.Fatalf("reviewed=%v failure=%v", reviewed, failure)
			}
			if !strings.HasPrefix(failure.Code, "JPS-LOCK-") {
				t.Fatalf("failure = %+v", failure)
			}
		})
	}
}

// Encode writes the exact bytes a reviewer diffs: sorted, two-space indented,
// one trailing newline, and no HTML escaping to mangle a path.
func TestEncodeIsTheBytesAReviewerReads(t *testing.T) {
	document := Document{
		LockVersion: Version,
		Config:      Config{Digest: "sha256:c"},
		Packs: map[string]Entry{
			"zeta":  {Path: "packs/z.json", Digest: "sha256:z"},
			"alpha": {Path: "packs/a<b>.json", Digest: "sha256:a"},
		},
	}
	first, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("two encodings of one document must be identical")
	}
	text := string(first)
	if !strings.HasSuffix(text, "}\n") || !strings.Contains(text, "\n  \"config\": {") {
		t.Fatalf("encoded = %q", text)
	}
	if strings.Index(text, `"alpha"`) > strings.Index(text, `"zeta"`) {
		t.Fatalf("entries are sorted: %q", text)
	}
	if !strings.Contains(text, "packs/a<b>.json") {
		t.Fatalf("a path is written as the project wrote it: %q", text)
	}
}

// The deciding check is about the bytes the evaluator applies, never a second
// read of the path they came from. This is the defect a race proved: with two
// reads, a writer between them yields a disposition from one document carrying
// a review established over another.
//
// It is asserted deterministically rather than by racing: the bytes handed to
// the consult are made to differ from the file on disk, in both directions, and
// the verdict must follow the bytes.
func TestTheDecidingCheckFollowsTheBytesAndNotThePath(t *testing.T) {
	configPath, root := lockedProject(t, oneGoodConfig, map[string]string{"packs/intake.json": packFixture(t)})
	declare(t, open(t, configPath))
	locked := []byte(packFixture(t))
	other := []byte(packFixture(t) + "\n")

	// The file still holds the locked bytes; the evaluation is about to apply
	// different ones. A check that re-read the path would pass this.
	if _, failure := Consult(open(t, configPath), []Applied{AppliedPack("intake", other)}, false); failure == nil {
		t.Fatal("bytes that are not the reviewed ones must be refused, whatever the file says")
	}

	// And the reverse: the file now holds bytes that were never locked, while
	// the evaluation applies the reviewed ones. A check that re-read the path
	// would refuse this — the run it is about is the reviewed one.
	if err := os.WriteFile(filepath.Join(root, "packs", "intake.json"), other, 0o600); err != nil {
		t.Fatal(err)
	}
	reviewed, failure := Consult(open(t, configPath), []Applied{AppliedPack("intake", locked)}, false)
	if failure != nil || reviewed == nil || !*reviewed {
		t.Fatalf("the verdict is about the bytes applied: reviewed=%v failure=%v", reviewed, failure)
	}

	// packs verify keeps the re-reading form, because re-reading is the
	// question that command asks: it reports the file, which now differs.
	loaded := open(t, configPath)
	document, _ := Load(loaded)
	if checks := Verify(loaded, document); len(checks) != 1 || checks[0].Name != CheckDocumentDrift {
		t.Fatalf("checks = %+v", checks)
	}
}

// A run that applies no declared document reaches the lock not at all — not
// even to read it. An unreadable lock, or one from a newer toolchain, must not
// stop someone drafting.
func TestADraftRunNeverReadsTheLock(t *testing.T) {
	for name, contents := range map[string]string{
		"a lock from a newer toolchain": `{"lockVersion":"2","config":{"digest":"sha256:x"}}`,
		"a lock that is not JSON":       `{ this is not a lock`,
	} {
		t.Run(name, func(t *testing.T) {
			configPath, root := lockedProject(t, oneGoodConfig, map[string]string{"packs/intake.json": packFixture(t)})
			if err := os.WriteFile(filepath.Join(root, "jpack.lock.json"), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			reviewed, failure := Consult(open(t, configPath), nil, true)
			if failure != nil {
				t.Fatalf("a draft run is untouched by a lock it never consults: %+v", failure)
			}
			if reviewed == nil || *reviewed {
				t.Fatalf("reviewed = %v, want false", reviewed)
			}
		})
	}
}

// An id the configuration does not declare is the resolving surface's finding,
// never the lock's: a refusal here would state the opposite of what is wrong
// and steer at a command that cannot fix it.
func TestAnUndeclaredIdIsNotTheLocksFinding(t *testing.T) {
	configPath, _ := lockedProject(t, oneGoodConfig, map[string]string{"packs/intake.json": packFixture(t)})
	declare(t, open(t, configPath))
	loaded := open(t, configPath)
	document, failure := Load(loaded)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if checks := VerifyDeciding(loaded, document, []Applied{AppliedPack("not-declared", []byte("{}"))}); len(checks) != 0 {
		t.Fatalf("checks = %+v", checks)
	}
	// The node check, which is the same rule applied per node, passes it over
	// too — so the graph's own reference diagnostic is what the reader sees.
	if check := NodeCheck(loaded, document); check == nil {
		t.Fatal("a locked project has a node check")
	} else if failure := check("not-declared", []byte("{}")); failure != nil {
		t.Fatalf("failure = %+v", failure)
	}
}
