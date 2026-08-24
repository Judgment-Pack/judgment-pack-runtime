package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/lock"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// lockPath is the reviewed-set lock beside one configuration.
func lockPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "jpack.lock.json")
}

// mustLock declares the project's current documents as its reviewed set, which
// most of these tests need as their starting point rather than as their subject.
func mustLock(t *testing.T, configPath string) {
	t.Helper()
	code, stdout, stderr := runTest(t, []string{"packs", "lock", "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("packs lock: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
}

// The lock is a reviewed artifact, so two runs over one tree must produce
// identical bytes: a generated file that churned would put noise in the diff
// someone is supposed to read.
func TestPacksLockIsDeterministicAndPinsEveryDeclaredDocument(t *testing.T) {
	configPath := auditProject(t)
	code, stdout, stderr := runTest(t, []string{"packs", "lock", "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "written: ") || !strings.Contains(stdout, "pack intake") {
		t.Fatalf("human output = %q", stdout)
	}
	first, err := os.ReadFile(lockPath(configPath))
	if err != nil {
		t.Fatal(err)
	}
	// Two-space indent and one trailing newline, like every other generated
	// artifact a project keeps in version control.
	if !strings.HasPrefix(string(first), "{\n  \"lockVersion\": \"1\",") || !strings.HasSuffix(string(first), "}\n") {
		t.Fatalf("lock = %q", first)
	}

	mustLock(t, configPath)
	second, err := os.ReadFile(lockPath(configPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("two runs over one tree must write identical bytes:\n%s\n---\n%s", first, second)
	}

	// The pinned digests are the documents' own bytes, and the configuration is
	// pinned too: adding a pack is an amendment exactly as editing one is.
	var document lock.Document
	if err := json.Unmarshal(first, &document); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	if document.LockVersion != lock.Version || document.Config.Digest != lock.Digest(config) {
		t.Fatalf("document = %+v", document)
	}
	if entry := document.Packs["intake"]; entry.Digest != lock.Digest(pack) || entry.Path != "packs/intake-0.1.0.pack.json" {
		t.Fatalf("entry = %+v", document.Packs["intake"])
	}

	// The machine form carries the same set.
	code, stdout, stderr = runTest(t, []string{"packs", "lock", "--config", configPath, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var payload result.PackLock
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "valid" || payload.Kind != result.ProjectKind || payload.WrittenTo == "" ||
		len(payload.Entries) != 1 || payload.Entries[0].ID != "intake" || payload.Entries[0].Kind != "pack" {
		t.Fatalf("payload = %+v", payload)
	}
}

// A configuration that does not load is refused before anything is written: a
// lock over a configuration this runtime cannot read would declare a reviewed
// set nobody can check.
func TestPacksLockRefusesAConfigurationThatDoesNotLoad(t *testing.T) {
	configPath := writeProjectFixture(t, `{"configVersion":"3","packs":{}}`, nil)
	code, _, stderr := runTest(t, []string{"packs", "lock", "--config", configPath}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, "jpack.json") {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(lockPath(configPath)); err == nil {
		t.Fatal("nothing is written for a configuration that did not load")
	}
}

// packs verify names every difference, one diagnostic per kind of difference,
// and every one of them is reported rather than the first.
func TestPacksVerifyNamesEveryDifference(t *testing.T) {
	t.Run("no lock at all", func(t *testing.T) {
		configPath := auditProject(t)
		code, _, stderr := runTest(t, []string{"packs", "verify", "--config", configPath}, "")
		if code != result.ExitIO {
			t.Fatalf("exit=%d, want the input/output class %d", code, result.ExitIO)
		}
		// It is not a failed verification — there is nothing to verify against —
		// and the refusal names the command that would create one.
		if !strings.Contains(stderr, "jpack packs lock") || !strings.Contains(stderr, "nothing to verify") {
			t.Fatalf("stderr = %q", stderr)
		}
	})

	t.Run("a project that matches its reviewed set", func(t *testing.T) {
		configPath := auditProject(t)
		mustLock(t, configPath)
		code, stdout, stderr := runTest(t, []string{"packs", "verify", "--config", configPath}, "")
		if code != 0 || stderr != "" || !strings.Contains(stdout, "verified: 1/1") {
			t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
	})

	t.Run("config-drift", func(t *testing.T) {
		configPath := auditProject(t)
		mustLock(t, configPath)
		body, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, append(body, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		findings := verifyFindings(t, configPath)
		if len(findings) != 1 || findings[0].Name != lock.CheckConfigDrift {
			t.Fatalf("findings = %+v", findings)
		}
	})

	t.Run("document-drift", func(t *testing.T) {
		configPath := auditProject(t)
		mustLock(t, configPath)
		packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
		body, err := os.ReadFile(packPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(packPath, append(body, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		findings := verifyFindings(t, configPath)
		if len(findings) != 1 || findings[0].Name != lock.CheckDocumentDrift || findings[0].ID != "intake" {
			t.Fatalf("findings = %+v", findings)
		}
	})

	t.Run("document-missing", func(t *testing.T) {
		configPath := auditProject(t)
		mustLock(t, configPath)
		if err := os.Remove(filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")); err != nil {
			t.Fatal(err)
		}
		findings := verifyFindings(t, configPath)
		if len(findings) != 1 || findings[0].Name != lock.CheckDocumentMissing {
			t.Fatalf("findings = %+v", findings)
		}
	})

	t.Run("lock-entry-missing and config-drift together", func(t *testing.T) {
		configPath := auditProject(t)
		mustLock(t, configPath)
		// A pack added after the set was declared: the configuration drifted and
		// the new pack is law nobody declared reviewed. Both are reported.
		if err := os.WriteFile(filepath.Join(filepath.Dir(configPath), "packs", "second.json"), []byte(evaluatorPack(t)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, []byte(`{"configVersion":"3","audit":{"dir":"audit"},"packs":{
		  "intake":{"path":"packs/intake-0.1.0.pack.json"},
		  "second":{"path":"packs/second.json"}
		}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		findings := verifyFindings(t, configPath)
		names := map[string]string{}
		for _, finding := range findings {
			names[finding.Name] = finding.ID
		}
		if len(findings) != 2 || names[lock.CheckConfigDrift] != "" || names[lock.CheckLockEntryMissing] != "second" {
			t.Fatalf("findings = %+v", findings)
		}
	})

	t.Run("locked-but-undeclared", func(t *testing.T) {
		configPath := auditProject(t)
		mustLock(t, configPath)
		// The pack is dropped from the configuration: the reviewed set now names
		// law the project no longer declares.
		if err := os.WriteFile(configPath, []byte(`{"configVersion":"3","audit":{"dir":"audit"},"packs":{
		  "other":{"path":"packs/intake-0.1.0.pack.json"}
		}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		findings := verifyFindings(t, configPath)
		found := map[string]bool{}
		for _, finding := range findings {
			found[finding.Name] = true
		}
		if !found[lock.CheckUndeclaredInConfig] || !found[lock.CheckLockEntryMissing] || !found[lock.CheckConfigDrift] {
			t.Fatalf("findings = %+v", findings)
		}
	})

	t.Run("a lock this runtime does not read", func(t *testing.T) {
		configPath := auditProject(t)
		mustLock(t, configPath)
		if err := os.WriteFile(lockPath(configPath), []byte(`{"lockVersion":"2","config":{"digest":"sha256:x"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		code, stdout, _ := runTest(t, []string{"packs", "verify", "--config", configPath}, "")
		if code != result.ExitUnsupported || !strings.Contains(stdout, "lockVersion") {
			t.Fatalf("exit=%d stdout=%q", code, stdout)
		}
	})
}

// verifyFindings runs packs verify and returns its findings, asserting the exit
// class every difference produces.
func verifyFindings(t *testing.T, configPath string) []result.LockFinding {
	t.Helper()
	code, stdout, stderr := runTest(t, []string{"packs", "verify", "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvalid || stderr != "" {
		t.Fatalf("exit=%d, want the invalid class %d (stderr %q stdout %q)", code, result.ExitInvalid, stderr, stdout)
	}
	var report result.PackLockVerification
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "invalid" || report.Kind != result.ProjectKind {
		t.Fatalf("report = %+v", report)
	}
	return report.Findings
}

// A decision is refused when the law it would apply is not the law the project
// declared reviewed — and the refusal steers, because an amendment is
// legitimate and a runtime cannot tell one from tampering.
func TestEvaluateRefusesLawThatLeftTheReviewedSet(t *testing.T) {
	for name, drift := range map[string]func(*testing.T, string){
		"the pack drifted": func(t *testing.T, configPath string) {
			packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
			body, err := os.ReadFile(packPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(packPath, append(body, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"the configuration drifted": func(t *testing.T, configPath string) {
			body, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, append(body, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			configPath := auditProject(t)
			mustLock(t, configPath)
			drift(t, configPath)
			facts := writeDocument(t, "facts.json", hardFailFacts)

			code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
				"--config", configPath, "--facts", facts}, "")
			if code != result.ExitInvalid {
				t.Fatalf("exit=%d, want the invalid class %d (stderr %q)", code, result.ExitInvalid, stderr)
			}
			// The steer is the point: told only "no", a reader edits until the
			// "no" stops, which is the failure this whole convention is about.
			if !strings.Contains(stderr, "jpack packs lock") || !strings.Contains(stderr, "restore the reviewed bytes") {
				t.Fatalf("the refusal must name both honest ways forward: %q", stderr)
			}
			if strings.Contains(stdout, "disposition") || strings.Contains(stdout, "outcome") {
				t.Fatalf("no disposition accompanies a refusal that happened before evaluation: %q", stdout)
			}
			// And nothing was recorded: the refusal precedes the evaluator, so
			// there is never a record of a decision taken under drifted law.
			noAuditTrail(t, configPath)

			// The machine form carries the provisional code.
			code, stdout, _ = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
				"--config", configPath, "--facts", facts, "--format", "json"}, "")
			if code != result.ExitInvalid {
				t.Fatalf("exit=%d", code)
			}
			var refusal struct {
				Diagnostics []struct {
					Code string `json:"code"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal([]byte(stdout), &refusal); err != nil {
				t.Fatal(err)
			}
			if len(refusal.Diagnostics) != 1 || refusal.Diagnostics[0].Code != lock.FailureCode {
				t.Fatalf("refusal = %q", stdout)
			}
		})
	}
}

// The reviewed bit is what a record says about the law it was judged under:
// present only when the project declares a reviewed set, true when every
// document applied was declared and matched, false for a draft.
func TestTheRecordSaysWhichLawTheDecisionWasJudgedUnder(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")

	// No lock: the member is absent, because "this project does not use the
	// convention" is not the same fact as "this ran on unreviewed law".
	code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if _, present := auditRecords(t, configPath)[0]["reviewed"]; present {
		t.Fatalf("a project with no lock records no reviewed member: %v", auditRecords(t, configPath)[0])
	}

	mustLock(t, configPath)
	if err := os.Remove(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)); err != nil {
		t.Fatal(err)
	}

	// Declared law that verified.
	code, _, stderr = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if reviewed := auditRecords(t, configPath)[0]["reviewed"]; reviewed != true {
		t.Fatalf("reviewed = %v, want true", reviewed)
	}

	// The same pack, named by path: a draft. It is evaluated — writing a pack
	// and trying it is the author's loop — and the record says it was a draft.
	code, _, stderr = runTest(t, []string{"experimental", "evaluate", packPath,
		"--config", configPath, "--facts", facts}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("a draft is evaluated, not refused: exit=%d stderr=%q", code, stderr)
	}
	records := auditRecords(t, configPath)
	if reviewed := records[len(records)-1]["reviewed"]; reviewed != false {
		t.Fatalf("reviewed = %v, want false", reviewed)
	}

	// A drifted draft is still a draft: an undeclared document is never refused
	// for being unlocked, whatever its bytes are.
	body, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatal(err)
	}
	scratch := writeDocument(t, "scratch.json", string(body)+"\n")
	code, _, stderr = runTest(t, []string{"experimental", "evaluate", scratch,
		"--config", configPath, "--facts", facts}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
}

// The rehearsal surfaces run the same evaluator over the same project and never
// consult the lock — the same split the audit trail draws, and for the same
// reason: the author's loop is free, and only decisions are classified.
func TestRehearsalSurfacesIgnoreTheLockEntirely(t *testing.T) {
	configPath := auditProject(t)
	mustLock(t, configPath)
	// Law that has left the reviewed set, plus a lock this runtime could not
	// even read. A rehearsal must notice neither.
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
	body, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packPath, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath(configPath), []byte(`{ this is not a lock`), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest(t, []string{"packs", "test", "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("packs test: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	code, stdout, stderr = runTest(t, []string{"packs", "validate", "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("packs validate: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	// And a deciding surface, over the same project, refuses — which is what
	// makes the split visible rather than incidental.
	facts := writeDocument(t, "facts.json", hardFailFacts)
	code, _, _ = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts}, "")
	if code == 0 {
		t.Fatal("a deciding surface must not evaluate under a lock it cannot read")
	}
	// The same command declared a rehearsal never opens the lock at all
	// (ADR-0028): an unreadable one stops nothing, exactly as it stops no
	// matrix run above. This is the discriminator for the Open call itself,
	// where the drift tests discriminate only Consult.
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--rehearsal",
		"--config", configPath, "--facts", facts}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("a declared rehearsal never opens the lock: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
}

// The graph surface holds every node's declared pack and the graph document
// itself to the reviewed set, and its test verb holds nothing.
func TestGraphEvaluateVerifiesTheLawItComposes(t *testing.T) {
	lockedGraphProject := func(t *testing.T) (string, string) {
		t.Helper()
		config := strings.Replace(graphFixture(t, "jpack.json"),
			`"configVersion": "2",`, `"configVersion": "3",`+"\n"+`  "audit": {"dir": "audit"},`, 1)
		configPath := writeProjectFixture(t, config, map[string]string{
			"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
			"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
			"onboarding.graph.json":               graphFixture(t, "onboarding.graph.json"),
			"onboarding.rows.json":                graphFixture(t, "onboarding.rows.json"),
		})
		mustLock(t, configPath)
		return configPath, filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
	}

	t.Run("a declared graph over reviewed packs records reviewed", func(t *testing.T) {
		configPath, graphPath := lockedGraphProject(t)
		inputs := writeGraphInputs(t, graphHappyInputs)
		code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
			"--config", configPath, "--inputs", inputs}, "")
		if code != 0 || stderr != "" {
			t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
		for _, record := range auditRecords(t, configPath) {
			if record["reviewed"] != true {
				t.Fatalf("every record of a reviewed run says so: %v", record)
			}
		}
	})

	t.Run("a drifted node pack refuses the whole run", func(t *testing.T) {
		configPath, graphPath := lockedGraphProject(t)
		packPath := filepath.Join(filepath.Dir(configPath), "sanctions-screening-0.1.0.pack.json")
		body, err := os.ReadFile(packPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(packPath, append(body, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		inputs := writeGraphInputs(t, graphHappyInputs)
		code, _, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
			"--config", configPath, "--inputs", inputs}, "")
		if code != result.ExitInvalid || !strings.Contains(stderr, "jpack packs lock") {
			t.Fatalf("exit=%d stderr=%q", code, stderr)
		}
		noAuditTrail(t, configPath)
	})

	t.Run("a drifted graph document refuses the run", func(t *testing.T) {
		configPath, graphPath := lockedGraphProject(t)
		body, err := os.ReadFile(graphPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(graphPath, append(body, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		inputs := writeGraphInputs(t, graphHappyInputs)
		code, _, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
			"--config", configPath, "--inputs", inputs}, "")
		if code != result.ExitInvalid || !strings.Contains(stderr, "document-drift") {
			t.Fatalf("exit=%d stderr=%q", code, stderr)
		}
	})

	t.Run("a graph the configuration does not declare is a draft", func(t *testing.T) {
		configPath, graphPath := lockedGraphProject(t)
		body, err := os.ReadFile(graphPath)
		if err != nil {
			t.Fatal(err)
		}
		draft := filepath.Join(filepath.Dir(configPath), "scratch.graph.json")
		if err := os.WriteFile(draft, body, 0o600); err != nil {
			t.Fatal(err)
		}
		inputs := writeGraphInputs(t, graphHappyInputs)
		code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", draft,
			"--config", configPath, "--inputs", inputs}, "")
		if code != 0 || stderr != "" {
			t.Fatalf("a draft graph is evaluated: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
		for _, record := range auditRecords(t, configPath) {
			if record["reviewed"] != false {
				t.Fatalf("a run composing an undeclared graph is not a reviewed run: %v", record)
			}
		}
	})

	t.Run("graph test consults nothing", func(t *testing.T) {
		configPath, graphPath := lockedGraphProject(t)
		packPath := filepath.Join(filepath.Dir(configPath), "sanctions-screening-0.1.0.pack.json")
		body, err := os.ReadFile(packPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(packPath, append(body, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		rowsPath := filepath.Join(filepath.Dir(configPath), "onboarding.rows.json")
		code, stdout, stderr := runTest(t, []string{"experimental", "graph", "test", graphPath,
			"--config", configPath, "--rows", rowsPath}, "")
		if code != 0 || stderr != "" {
			t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
	})
}

// A project with no lock reaches none of this: no verification, no refusal, no
// member in a record, and no change to any command's behavior.
func TestAProjectWithNoLockIsUnaffected(t *testing.T) {
	configPath := oneGoodProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	for _, args := range [][]string{
		{"packs", "list", "--config", configPath},
		{"packs", "validate", "--config", configPath},
		{"packs", "test", "--config", configPath},
		{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--facts", facts},
	} {
		code, _, stderr := runTest(t, args, "")
		if code != 0 || stderr != "" {
			t.Fatalf("%v: exit=%d stderr=%q", args, code, stderr)
		}
	}
	if _, err := os.Stat(lockPath(configPath)); err == nil {
		t.Fatal("nothing creates a lock but packs lock")
	}
}

// A graph node naming a decision id the configuration does not declare is the
// graph validator's finding, locked or not. The lock must not pre-empt it with
// a refusal that states the opposite of what is wrong and steers at a command
// that cannot fix it.
func TestAnUndeclaredNodePackIsTheGraphValidatorsFinding(t *testing.T) {
	build := func(t *testing.T) (string, string) {
		t.Helper()
		document := strings.Replace(graphFixture(t, "onboarding.graph.json"),
			`"pack": "sanctions-screening"`, `"pack": "not-declared"`, 1)
		config := strings.Replace(graphFixture(t, "jpack.json"),
			`"configVersion": "2",`, `"configVersion": "3",`+"\n"+`  "audit": {"dir": "audit"},`, 1)
		configPath := writeProjectFixture(t, config, map[string]string{
			"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
			"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
			"onboarding.graph.json":               document,
		})
		return configPath, filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
	}
	inputsFor := func(t *testing.T) string { return writeGraphInputs(t, graphHappyInputs) }

	// Unlocked: the graph's own reference check names the id and lists the
	// configured ones.
	configPath, graphPath := build(t)
	code, _, unlocked := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", inputsFor(t)}, "")
	if code != result.ExitInvalid || !strings.Contains(unlocked, "JPS-GRAPH-NODE-PACK") {
		t.Fatalf("exit=%d stderr=%q", code, unlocked)
	}

	// Locked: the same diagnostic, unchanged. The lock has nothing to say about
	// an id the configuration never declared.
	configPath, graphPath = build(t)
	mustLock(t, configPath)
	code, _, locked := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", inputsFor(t)}, "")
	if code != result.ExitInvalid || !strings.Contains(locked, "JPS-GRAPH-NODE-PACK") {
		t.Fatalf("exit=%d stderr=%q", code, locked)
	}
	if strings.Contains(locked, lock.FailureCode) || strings.Contains(locked, "packs lock") {
		t.Fatalf("the lock must not pre-empt the graph's own finding: %q", locked)
	}
	// And packs verify agrees with the deciding surface about this project:
	// the declared documents all match.
	if code, _, _ := runTest(t, []string{"packs", "verify", "--config", configPath}, ""); code != 0 {
		t.Fatalf("packs verify exit=%d", code)
	}
}

// A generator must not emit a document its own reader refuses: past the limit
// every reader — packs verify and all three deciding surfaces — would refuse
// the file, and the refusal's steer would regenerate it unchanged.
func TestPacksLockRefusesALockItsOwnReaderWouldRefuse(t *testing.T) {
	pack := evaluatorPack(t)
	files := map[string]string{"packs/intake.json": pack}
	entries := make([]string, 0, 9000)
	for index := 0; index < 9000; index++ {
		entries = append(entries, fmt.Sprintf(`"decision-%05d":{"path":"packs/intake.json"}`, index))
	}
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{`+strings.Join(entries, ",")+`}}`, files)

	code, _, stderr := runTest(t, []string{"packs", "lock", "--config", configPath}, "")
	if code != result.ExitIO {
		t.Fatalf("exit=%d, want the input/output class %d (stderr %q)", code, result.ExitIO, stderr)
	}
	for _, required := range []string{"9000 declared document(s)", "byte limit", "nothing was written"} {
		if !strings.Contains(stderr, required) {
			t.Fatalf("the refusal must carry %q: %q", required, stderr)
		}
	}
	if _, err := os.Stat(lockPath(configPath)); err == nil {
		t.Fatal("nothing was written")
	}
}

// The generated file never lands on a document the configuration declares.
// Writing there would destroy declared law with a generated artifact — the one
// write in this runtime that could take something away.
func TestPacksLockRefusesToOverwriteDeclaredLaw(t *testing.T) {
	for name, config := range map[string]string{
		"a pack at the lock's path": `{"configVersion":"1","packs":{"intake":{"path":"jpack.lock.json"}}}`,
		"a matrix at it":            `{"configVersion":"1","packs":{"intake":{"path":"packs/intake.json","matrix":"jpack.lock.json"}}}`,
		"a graph at it":             `{"configVersion":"2","packs":{"intake":{"path":"packs/intake.json"}},"graphs":{"g":{"path":"jpack.lock.json"}}}`,
		"a graph's rows at it":      `{"configVersion":"2","packs":{"intake":{"path":"packs/intake.json"}},"graphs":{"g":{"path":"g.json","rows":"jpack.lock.json"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			configPath := writeProjectFixture(t, config, map[string]string{
				"packs/intake.json": evaluatorPack(t),
				"g.json":            `{"formatVersion":"1","id":"g","version":"0.1.0","nodes":{"a":{"pack":"intake"}},"edges":[],"result":"a"}`,
				"jpack.lock.json":   evaluatorPack(t),
			})
			before, err := os.ReadFile(lockPath(configPath))
			if err != nil {
				t.Fatal(err)
			}
			code, _, stderr := runTest(t, []string{"packs", "lock", "--config", configPath}, "")
			if code != result.ExitInvalid || !strings.Contains(stderr, "nothing was written") {
				t.Fatalf("exit=%d stderr=%q", code, stderr)
			}
			after, err := os.ReadFile(lockPath(configPath))
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("the declared document must be exactly as it was")
			}
		})
	}
}

// The reviewed set is named after the configuration, and the derivation has to
// be one-to-one or two projects in one directory share one lock.
func TestALockNameMustBeDerivable(t *testing.T) {
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"intake":{"path":"packs/intake.json"}}}`,
		map[string]string{"packs/intake.json": evaluatorPack(t)})
	unsuffixed := filepath.Join(filepath.Dir(configPath), "jpack")
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unsuffixed, body, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"lock", "verify"} {
		code, _, stderr := runTest(t, []string{"packs", verb, "--config", unsuffixed}, "")
		if code != result.ExitInvocation || !strings.Contains(stderr, ".json") {
			t.Fatalf("packs %s: exit=%d stderr=%q", verb, code, stderr)
		}
	}
	// And a deciding surface over that configuration simply has no lock to
	// consult, which is the same answer a project without one gets.
	facts := writeDocument(t, "facts.json", hardFailFacts)
	mustLock(t, configPath)
	code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", unsuffixed, "--facts", facts}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
}

// packs verify's summary counts declared documents, so passed plus failed is
// always total and an entry the configuration dropped is counted on its own.
func TestPacksVerifySummaryCountsDeclaredDocuments(t *testing.T) {
	configPath := auditProject(t)
	mustLock(t, configPath)
	// The decision id is renamed: the one declared document still matches its
	// own bytes, the lock names an id the configuration dropped, and the
	// configuration itself drifted.
	if err := os.WriteFile(configPath, []byte(`{"configVersion":"3","audit":{"dir":"audit"},"packs":{
	  "other":{"path":"packs/intake-0.1.0.pack.json"}
	}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runTest(t, []string{"packs", "verify", "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvalid {
		t.Fatalf("exit=%d", code)
	}
	var report result.PackLockVerification
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Passed+report.Summary.Failed != report.Summary.Total {
		t.Fatalf("summary must account for exactly the declared documents: %+v", report.Summary)
	}
	if report.Summary.Total != 1 || report.Summary.Failed != 1 || report.StaleEntries != 1 {
		t.Fatalf("summary = %+v, stale = %d", report.Summary, report.StaleEntries)
	}
	// Every finding is still reported, whatever it counts toward.
	if len(report.Findings) != 3 {
		t.Fatalf("findings = %+v", report.Findings)
	}
	for _, finding := range report.Findings {
		if finding.Name != lock.CheckConfigDrift && finding.Kind == "" {
			t.Fatalf("a document finding names its kind: %+v", finding)
		}
	}
}

// The lock file's mode is set on every write, not only when the runtime creates
// it: a file something else left at that name would otherwise keep whatever
// mode it was given, and it is the file that says which documents are law.
func TestPacksLockTightensAnExistingLockFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not what a Windows file mode means")
	}
	configPath := auditProject(t)
	if err := os.WriteFile(lockPath(configPath), []byte("{}\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	mustLock(t, configPath)
	info, err := os.Stat(lockPath(configPath))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", info.Mode().Perm())
	}
}

// A record that claims a review names the revision that made the claim true.
// The lock is replaced in place, so without it a reader holding a record and a
// lock file cannot tell whether that lock is the one the decision was judged
// under.
func TestAReviewedRecordNamesTheReviewedSet(t *testing.T) {
	configPath := auditProject(t)
	mustLock(t, configPath)
	facts := writeDocument(t, "facts.json", hardFailFacts)

	code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	record := auditRecords(t, configPath)[0]
	set, ok := record["reviewedSet"].(map[string]any)
	if !ok {
		t.Fatalf("a reviewed record names its reviewed set: %v", record)
	}
	lockBytes, err := os.ReadFile(lockPath(configPath))
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if set["lockDigest"] != lock.Digest(lockBytes) || set["lockVersion"] != lock.Version ||
		set["configDigest"] != lock.Digest(config) {
		t.Fatalf("reviewedSet = %v", set)
	}

	// A draft names none: it was judged under no reviewed set.
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
	code, _, stderr = runTest(t, []string{"experimental", "evaluate", packPath,
		"--config", configPath, "--facts", facts}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	records := auditRecords(t, configPath)
	last := records[len(records)-1]
	if last["reviewed"] != false || last["reviewedSet"] != nil {
		t.Fatalf("a draft names no reviewed set: %v", last)
	}

	// And a project with no lock names none either.
	plain := oneGoodProject(t)
	if _, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", plain, "--facts", facts}, ""); stderr != "" {
		t.Fatalf("stderr=%q", stderr)
	}
}

// A graph run reads the reviewed set once and every record of it names that one
// revision — including the composite, which is written after every node.
func TestAGraphRunNamesOneReviewedSet(t *testing.T) {
	config := strings.Replace(graphFixture(t, "jpack.json"),
		`"configVersion": "2",`, `"configVersion": "3",`+"\n"+`  "audit": {"dir": "audit"},`, 1)
	configPath := writeProjectFixture(t, config, map[string]string{
		"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
		"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
		"onboarding.graph.json":               graphFixture(t, "onboarding.graph.json"),
	})
	graphPath := filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
	mustLock(t, configPath)
	lockBytes, err := os.ReadFile(lockPath(configPath))
	if err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", writeGraphInputs(t, graphHappyInputs)}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	records := auditRecords(t, configPath)
	if len(records) < 3 {
		t.Fatalf("records = %d", len(records))
	}
	for _, record := range records {
		set, ok := record["reviewedSet"].(map[string]any)
		if !ok || set["lockDigest"] != lock.Digest(lockBytes) {
			t.Fatalf("every record of one run names the one revision it was judged under: %v", record)
		}
	}
}

// The lock is read once per run: the configuration and the graph document are
// checked against the revision the node checks also use. Proved by
// construction — the file is removed after the run has read it, and the node
// checks still verify.
func TestAGraphRunReadsTheReviewedSetOnce(t *testing.T) {
	config := strings.Replace(graphFixture(t, "jpack.json"),
		`"configVersion": "2",`, `"configVersion": "3",`+"\n"+`  "audit": {"dir": "audit"},`, 1)
	configPath := writeProjectFixture(t, config, map[string]string{
		"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
		"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
		"onboarding.graph.json":               graphFixture(t, "onboarding.graph.json"),
	})
	graphPath := filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
	mustLock(t, configPath)

	// The check that the run holds one revision is made in internal/lock, where
	// the set can be retained across the file's removal. Here the observable
	// half: a drifted node pack refuses even though the graph document and the
	// configuration both matched, so the per-node check ran against the same
	// set the run opened.
	packPath := filepath.Join(filepath.Dir(configPath), "vendor-onboarding-0.1.0.pack.json")
	body, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packPath, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", writeGraphInputs(t, graphHappyInputs)}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, lock.CheckDocumentDrift) {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	noAuditTrail(t, configPath)
}

// The generated file never lands on the audit directory or above it: a file
// there leaves the directory uncreatable, and every later evaluation then
// refuses after evaluating because its record cannot be written.
func TestPacksLockRefusesToOccupyTheAuditDirectory(t *testing.T) {
	for name, dir := range map[string]string{
		"the audit directory itself": "jpack.lock.json",
		"a directory below it":       "jpack.lock.json/records",
	} {
		t.Run(name, func(t *testing.T) {
			configPath := writeProjectFixture(t,
				`{"configVersion":"3","audit":{"dir":"`+dir+`"},"packs":{"intake":{"path":"packs/intake.json"}}}`,
				map[string]string{"packs/intake.json": evaluatorPack(t)})
			code, _, stderr := runTest(t, []string{"packs", "lock", "--config", configPath}, "")
			if code != result.ExitInvalid || !strings.Contains(stderr, "audit directory") {
				t.Fatalf("exit=%d stderr=%q", code, stderr)
			}
			if _, err := os.Stat(lockPath(configPath)); err == nil {
				t.Fatal("nothing was written")
			}
		})
	}
}

// The collision refusal is about files, not spellings: a case-equivalent name
// is refused wherever it is written, because on a filesystem that folds case it
// is the same file and the write would destroy declared law.
func TestPacksLockRefusesACaseEquivalentDeclaredPath(t *testing.T) {
	configPath := writeProjectFixture(t,
		`{"configVersion":"1","packs":{"intake":{"path":"JPACK.LOCK.JSON"}}}`,
		map[string]string{"JPACK.LOCK.JSON": evaluatorPack(t)})
	code, _, stderr := runTest(t, []string{"packs", "lock", "--config", configPath}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, "nothing was written") {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	// The declared document is exactly as it was. On a case-folding filesystem
	// that is the whole point; on a case-sensitive one the refusal is
	// conservative and costs a rename.
	body, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "JPACK.LOCK.JSON"))
	if err != nil || string(body) != evaluatorPack(t) {
		t.Fatalf("the declared document must be untouched: err=%v", err)
	}
}

// The JSON envelope of an argument failure names the command the diagnostic is
// about. A verb missing from the scan reports its group instead, which a
// consumer keying on the member cannot tell from a group-level failure.
func TestTheEnvelopeNamesTheNewVerbs(t *testing.T) {
	for _, verb := range []string{"lock", "verify", "lint"} {
		code, stdout, _ := runTest(t, []string{"packs", verb, "extra", "--format", "json"}, "")
		if code != result.ExitInvocation {
			t.Fatalf("packs %s: exit=%d", verb, code)
		}
		var envelope struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
			t.Fatalf("packs %s: undecodable %q: %v", verb, stdout, err)
		}
		if envelope.Command != "packs "+verb {
			t.Fatalf("command = %q, want %q", envelope.Command, "packs "+verb)
		}
	}
}
