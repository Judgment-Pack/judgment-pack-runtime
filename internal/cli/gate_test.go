package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
)

// gatePack is a synthetic pack whose outcome ids include "refused", an
// ordinary outcome a pack may declare: the gate judges it against the set like
// any other, and never takes it for the runtime's refusal of an evaluation.
// An item out of scope is not-applicable; one whose scope or kind is missing
// is unresolved.
const gatePack = `{"specVersion":"0.2.0-draft","id":"https://example.invalid/judgment-packs/gate-fixture","version":"0.1.0","title":"Synthetic gate fixture","description":"Synthetic content for runtime testing; it authorizes nothing.","decision":{"intent":"Synthetic.","question":"Synthetic?"},` +
	`"applicability":{"op":"fact","path":"/request/scope","operator":"equals","value":"in"},` +
	`"outcomes":[{"id":"approved","label":"Approved"},{"id":"refused","label":"Refused"},{"id":"needs-approval","label":"Needs approval"}],` +
	`"rules":[` + gateRule + `"a"},"outcome":"approved"},` + gateRule + `"r"},"outcome":"refused"},` + gateRule + `"n"},"outcome":"needs-approval"}],` +
	`"escalation":{"triggers":["unknown"],"target":{"kind":"human-role","name":"Reviewer"},"message":"Synthetic."}}`

const gateRule = `{"id":"rule-` + `x","description":"Synthetic rule.","onUnknown":"escalate","rationale":"Synthetic.","when":{"op":"fact","path":"/request/kind","operator":"equals","value":`

// gateFacts is a facts document in scope of the given kind.
func gateFacts(kind string) string {
	return `{"request":{"scope":"in","kind":"` + kind + `"}}`
}

const (
	gateNotApplicable = `{"request":{"scope":"out","kind":"a"}}`
	gateUnresolved    = `{"request":{"kind":"a"}}`
)

// gatePackFile writes the fixture pack with unique rule ids.
func gatePackFile(t *testing.T) string {
	t.Helper()
	pack := gatePack
	for _, id := range []string{"a", "r", "n"} {
		pack = strings.Replace(pack, `"rule-x"`, `"rule-`+id+`"`, 1)
	}
	return writeDocument(t, "pack.json", pack)
}

func runGate(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return runTest(t, append([]string{"experimental", "evaluate"}, args...), "")
}

func TestGateExitCodes(t *testing.T) {
	pack := gatePackFile(t)
	facts := func(body string) string { return writeDocument(t, "facts.json", body) }
	batch := func(items ...testBatchItem) string { return writeDocument(t, "batch.jsonl", batchLines(items)) }
	empty := writeDocument(t, "empty.jsonl", "")
	cases := map[string]struct {
		args []string
		want int
	}{
		"an allowed outcome passes":            {[]string{pack, "--facts", facts(gateFacts("a")), "--require-outcome", "approved"}, 0},
		"one of several allowed outcomes":      {[]string{pack, "--facts", facts(gateFacts("n")), "--require-outcome", "approved,needs-approval"}, 0},
		"an outcome id refused is a finding":   {[]string{pack, "--facts", facts(gateFacts("r")), "--require-outcome", "approved"}, 1},
		"an outcome id refused can be allowed": {[]string{pack, "--facts", facts(gateFacts("r")), "--require-outcome", "refused"}, 0},
		"not-applicable is a finding":          {[]string{pack, "--facts", facts(gateNotApplicable), "--require-outcome", "approved"}, 1},
		"unresolved does not pass":             {[]string{pack, "--facts", facts(gateUnresolved), "--require-outcome", "approved"}, 2},
		"a refused evaluation does not pass":   {[]string{pack, "--facts", facts(refusedFacts), "--require-outcome", "approved"}, 2},
		"unreadable facts do not pass":         {[]string{pack, "--facts", filepath.Join(t.TempDir(), "missing.json"), "--require-outcome", "approved"}, 2},
		"an undeclared outcome id is refused":  {[]string{pack, "--facts", facts(gateFacts("a")), "--require-outcome", "approved,aproved"}, 2},
		"not-applicable is not an outcome id":  {[]string{pack, "--facts", facts(gateNotApplicable), "--require-outcome", "not-applicable"}, 2},
		"an empty id is refused":               {[]string{pack, "--facts", facts(gateFacts("a")), "--require-outcome", "approved,"}, 2},
		"an id twice is refused":               {[]string{pack, "--facts", facts(gateFacts("a")), "--require-outcome", "approved,approved"}, 2},
		"a summary on standard output":         {[]string{pack, "--facts", facts(gateFacts("a")), "--require-outcome", "approved", "--gate-summary", "-"}, 2},
		"--allow-empty without a batch":        {[]string{pack, "--facts", facts(gateFacts("a")), "--require-outcome", "approved", "--allow-empty"}, 2},
		"--allow-empty without the gate":       {[]string{pack, "--format", "json", "--batch", empty, "--allow-empty"}, 3},
		"--gate-summary without the gate":      {[]string{pack, "--facts", facts(gateFacts("a")), "--gate-summary", filepath.Join(t.TempDir(), "s.json")}, 3},
		// The gate was never read: exit 3, as for every command, which a
		// CI step treats as "did not judge" just as it treats 2.
		"an unparseable command line": {[]string{pack, "--facts", facts(gateFacts("a")), "--require-outcome", "approved", "--no-such-flag"}, 3},

		"a batch of allowed outcomes": {[]string{pack, "--format", "json", "--require-outcome", "approved,needs-approval", "--batch",
			batch(testBatchItem{id: "1", facts: gateFacts("a")}, testBatchItem{id: "2", facts: gateFacts("n")})}, 0},
		"a batch with an outcome outside": {[]string{pack, "--format", "json", "--require-outcome", "approved", "--batch",
			batch(testBatchItem{id: "1", facts: gateFacts("a")}, testBatchItem{id: "2", facts: gateFacts("r")})}, 1},
		"a batch with a refused item": {[]string{pack, "--format", "json", "--require-outcome", "approved", "--batch",
			batch(testBatchItem{id: "1", facts: gateFacts("r")}, testBatchItem{id: "2", facts: refusedFacts})}, 2},
		"a batch with an unresolved item": {[]string{pack, "--format", "json", "--require-outcome", "approved", "--batch",
			batch(testBatchItem{id: "1", facts: gateFacts("r")}, testBatchItem{id: "2", facts: gateUnresolved})}, 2},
		"a batch over --max-items": {[]string{pack, "--format", "json", "--require-outcome", "approved", "--max-items", "1", "--batch",
			batch(testBatchItem{id: "1", facts: gateFacts("a")}, testBatchItem{id: "2", facts: gateFacts("a")})}, 2},
		"an empty batch does not pass":      {[]string{pack, "--format", "json", "--require-outcome", "approved", "--batch", empty}, 2},
		"an empty batch with --allow-empty": {[]string{pack, "--format", "json", "--require-outcome", "approved", "--allow-empty", "--batch", empty}, 0},
		"a batch without the gate is unchanged": {[]string{pack, "--format", "json", "--batch",
			batch(testBatchItem{id: "1", facts: gateFacts("r")}, testBatchItem{id: "2", facts: gateNotApplicable})}, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := runGate(t, c.args...)
			if code != c.want {
				t.Fatalf("exit=%d, want %d\nstdout=%s\nstderr=%s", code, c.want, stdout, stderr)
			}
		})
	}
}

// An undeclared id is refused before anything is evaluated: the payload is
// the gate's refusal, and no evaluation is reported.
func TestGateUndeclaredOutcomeRefusedBeforeEvaluating(t *testing.T) {
	pack := gatePackFile(t)
	code, stdout, _ := runGate(t, pack, "--format", "json", "--facts", writeDocument(t, "facts.json", gateFacts("a")), "--require-outcome", "aproved")
	if code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
	if !strings.Contains(stdout, gateInvocationCode) || strings.Contains(stdout, `"disposition"`) {
		t.Fatalf("want the gate's refusal and no disposition, got %s", stdout)
	}
}

// The gate changes the exit code and adds to standard error; the results on
// standard output are the bytes the same run writes without it.
func TestGateLeavesStandardOutputAlone(t *testing.T) {
	pack := gatePackFile(t)
	batch := writeDocument(t, "batch.jsonl", batchLines([]testBatchItem{{id: "1", facts: gateFacts("a")}, {id: "2", facts: gateFacts("r")}}))
	_, plain, _ := runGate(t, pack, "--format", "json", "--batch", batch)
	code, gated, _ := runGate(t, pack, "--format", "json", "--batch", batch, "--require-outcome", "approved")
	if code != 1 || plain != gated {
		t.Fatalf("exit=%d; standard output changed under the gate:\n%s\n---\n%s", code, plain, gated)
	}
}

func TestGateSummary(t *testing.T) {
	pack := gatePackFile(t)
	// A value that must never leave the facts: not in the summary, not on
	// standard error.
	const secret = "zz-fact-value-7f3a"
	items := []testBatchItem{
		{id: "first", facts: gateFacts("a")},
		{id: "second", facts: `{"request":{"scope":"in","kind":"r","note":"` + secret + `"}}`},
		{id: "third", facts: `{"request":{"scope":"out","kind":"` + secret + `"}}`},
	}
	batch := writeDocument(t, "batch.jsonl", batchLines(items))
	summaryPath := filepath.Join(t.TempDir(), "summary.json")
	code, _, stderr := runGate(t, pack, "--format", "json", "--batch", batch, "--require-outcome", "approved", "--gate-summary", summaryPath)
	if code != 1 {
		t.Fatalf("exit=%d, want 1; stderr=%s", code, stderr)
	}
	raw, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || strings.Contains(stderr, secret) {
		t.Fatalf("a fact value left the facts:\nsummary=%s\nstderr=%s", raw, stderr)
	}
	var summary gateSummary
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&summary); err != nil {
		t.Fatalf("undecodable summary %s: %v", raw, err)
	}
	if summary.Version != gateSummaryVersion || summary.Verdict != "fail" || summary.ExitCode != 1 || summary.RunExitCode != 0 ||
		summary.Items != 3 || summary.Decisions != 3 || summary.PackVersion != "0.1.0" {
		t.Fatalf("summary: %s", raw)
	}
	if len(summary.Failing) != 2 ||
		summary.Failing[0] != (gateCase{Line: 2, ID: summary.Failing[0].ID, Kind: "outcome", OutcomeID: "refused"}) || *summary.Failing[0].ID != "second" ||
		summary.Failing[1] != (gateCase{Line: 3, ID: summary.Failing[1].ID, Kind: "not-applicable"}) || *summary.Failing[1].ID != "third" {
		t.Fatalf("failing: %s", raw)
	}
	digest := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	}
	if len(summary.Inputs) != 2 ||
		summary.Inputs[0].Covers != "pack" || summary.Inputs[0].Path != pack || summary.Inputs[0].SHA256 != digest(pack) ||
		summary.Inputs[1].Covers != "batch" || summary.Inputs[1].Path != batch || summary.Inputs[1].SHA256 != digest(batch) ||
		summary.Inputs[1].ReadToEnd == nil || !*summary.Inputs[1].ReadToEnd {
		t.Fatalf("inputs: %s", raw)
	}
	for _, want := range []string{`gate: fail: 2 of 3`, `line 2 (id "second"): outcome "refused"`, `line 3 (id "third"): not-applicable`} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr lacks %q: %s", want, stderr)
		}
	}
}

// A single run's summary covers the pack and the facts file it read.
func TestGateSummarySingleRun(t *testing.T) {
	pack := gatePackFile(t)
	facts := writeDocument(t, "facts.json", gateFacts("a"))
	summaryPath := filepath.Join(t.TempDir(), "summary.json")
	if code, _, stderr := runGate(t, pack, "--facts", facts, "--require-outcome", "approved", "--gate-summary", summaryPath); code != 0 {
		t.Fatalf("exit=%d; stderr=%s", code, stderr)
	}
	var summary gateSummary
	raw, _ := os.ReadFile(summaryPath)
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Verdict != "pass" || len(summary.Inputs) != 2 || summary.Inputs[1].Covers != "facts" || summary.Inputs[1].Path != facts ||
		summary.Inputs[1].SHA256 == "" || summary.Inputs[1].ReadToEnd != nil || len(summary.Failing) != 0 {
		t.Fatalf("summary: %s", raw)
	}
}

// gateProject is a project that keeps an audit trail and declares the gate
// fixture as the pack "gate".
func gateProject(t *testing.T) string {
	t.Helper()
	pack, err := os.ReadFile(gatePackFile(t))
	if err != nil {
		t.Fatal(err)
	}
	return writeProjectFixture(t, `{"configVersion":"3","audit":{"dir":"audit"},"packs":{"gate":{"path":"packs/gate-0.1.0.pack.json","expectedVersion":"0.1.0"}}}`,
		map[string]string{"packs/gate-0.1.0.pack.json": string(pack)})
}

func trailLines(t *testing.T, configPath string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Split(strings.TrimSpace(string(data)), "\n")) - map[bool]int{true: 1, false: 0}[strings.TrimSpace(string(data)) == ""]
}

// An undeclared id is refused before anything is recorded; the same run
// with a declared id records, so the trail is live.
func TestGateUndeclaredOutcomeRecordsNothing(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	configPath := gateProject(t)
	facts := writeDocument(t, "facts.json", gateFacts("a"))
	summaryPath := filepath.Join(t.TempDir(), "summary.json")
	args := []string{"--pack-id", "gate", "--config", configPath, "--facts", facts, "--gate-summary", summaryPath, "--require-outcome"}
	if code, stdout, stderr := runGate(t, append(args, "aproved")...); code != 2 {
		t.Fatalf("exit=%d, want 2\n%s\n%s", code, stdout, stderr)
	}
	if lines := trailLines(t, configPath); lines != 0 {
		t.Fatalf("an undeclared id left %d records", lines)
	}
	if code, stdout, stderr := runGate(t, append(args, "approved")...); code != 0 {
		t.Fatalf("exit=%d, want 0\n%s\n%s", code, stdout, stderr)
	}
	if lines := trailLines(t, configPath); lines != 1 {
		t.Fatalf("a recorded run left %d records, want 1", lines)
	}
	var summary gateSummary
	raw, _ := os.ReadFile(summaryPath)
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Inputs[0] != (gateInput{Covers: "pack", PackIDRef: "gate", SHA256: summary.Inputs[0].SHA256}) || summary.Inputs[0].SHA256 == "" {
		t.Fatalf("a pack named by id: %s", raw)
	}
}

func TestGateStandardInputAndRehearsal(t *testing.T) {
	pack := gatePackFile(t)
	if code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", "-", "--require-outcome", "approved"}, gateFacts("a")); code != 0 {
		t.Fatalf("facts on standard input: exit=%d\n%s\n%s", code, stdout, stderr)
	}
	batch := batchLines([]testBatchItem{{id: "1", facts: gateFacts("a")}, {id: "2", facts: gateFacts("r")}})
	if code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--format", "json", "--batch", "-", "--require-outcome", "approved"}, batch); code != 1 {
		t.Fatalf("a batch on standard input: exit=%d\n%s\n%s", code, stdout, stderr)
	}
	configPath := gateProject(t)
	facts := writeDocument(t, "facts.json", gateFacts("r"))
	if code, stdout, stderr := runGate(t, "--pack-id", "gate", "--config", configPath, "--rehearsal", "--facts", facts, "--require-outcome", "approved"); code != 1 {
		t.Fatalf("a rehearsal: exit=%d\n%s\n%s", code, stdout, stderr)
	}
}

// A summary that cannot be written is "did not judge", and standard error
// never says "pass" first.
func TestGateSummaryWriteFailure(t *testing.T) {
	pack := gatePackFile(t)
	facts := writeDocument(t, "facts.json", gateFacts("a"))
	missing := filepath.Join(t.TempDir(), "no-such-directory", "summary.json")
	code, _, stderr := runGate(t, pack, "--facts", facts, "--require-outcome", "approved", "--gate-summary", missing)
	if code != 2 || strings.Contains(stderr, "gate: pass") {
		t.Fatalf("exit=%d, want 2 with no pass; stderr=%s", code, stderr)
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "target.json")
	link := filepath.Join(directory, "summary.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symbolic links unavailable:", err)
	}
	if code, _, _ := runGate(t, pack, "--facts", facts, "--require-outcome", "approved", "--gate-summary", link); code != 2 {
		t.Fatalf("a summary through a symbolic link: exit=%d, want 2", code)
	}
	if data, _ := os.ReadFile(target); string(data) != "{}\n" {
		t.Fatalf("the link's target was written: %q", data)
	}
}

// A run whose gate arguments are refused replaces an earlier run's summary
// with its own "error" verdict.
func TestGateRefusedArgumentsReplaceTheSummary(t *testing.T) {
	pack := gatePackFile(t)
	facts := writeDocument(t, "facts.json", gateFacts("a"))
	summaryPath := filepath.Join(t.TempDir(), "summary.json")
	if code, _, _ := runGate(t, pack, "--facts", facts, "--require-outcome", "approved", "--gate-summary", summaryPath); code != 0 {
		t.Fatalf("first run: exit=%d", code)
	}
	if code, _, _ := runGate(t, pack, "--facts", facts, "--require-outcome", "approved,", "--gate-summary", summaryPath); code != 2 {
		t.Fatalf("refused run: exit=%d, want 2", code)
	}
	var summary gateSummary
	raw, _ := os.ReadFile(summaryPath)
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Verdict != "error" || summary.ExitCode != 2 || summary.RunExitCode != 3 || len(summary.AllowedOutcomes) != 0 {
		t.Fatalf("summary after a refused run: %s", raw)
	}
}

// The pack's outcome ids are read by their exact member names: a member
// spelled "ID" declares nothing the gate may allow.
func TestGateReadsDeclaredOutcomesExactly(t *testing.T) {
	g, problem := newGate("experimental evaluate", gateFlags{requireOutcome: "approved"}, true, false, false, false)
	if problem != "" {
		t.Fatal(problem)
	}
	if message := g.checkPack([]byte(`{"outcomes":[{"ID":"approved"}]}`), false, "pack.json", ""); message == "" {
		t.Fatal("an outcome under the member ID was taken as declared")
	}
	// Case-insensitive decoding would let the later "Outcomes" stand for
	// "outcomes"; the engine reads "outcomes", which declares only "held".
	if message := g.checkPack([]byte(`{"outcomes":[{"id":"held"}],"Outcomes":[{"id":"approved"}]}`), false, "pack.json", ""); message == "" {
		t.Fatal("outcomes under the member Outcomes were taken as declared")
	}
}
