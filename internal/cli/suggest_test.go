package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The whole motivating shape, end to end, over the contributed case
// thresholdPack already carries: the coverage report says the boundary is
// unwitnessed, suggest offers the lattice around it, nothing it offers loads as
// a row, and once a human writes the expectation from the policy text the
// boundary is witnessed and the origin is counted rather than gated.
func TestSuggestClosesTheBoundaryOnlyAfterAHumanWritesTheExpectation(t *testing.T) {
	configPath := greenSuiteWithUnwitnessedBoundary(t, "")

	code, stdout, _ := runTest(t, []string{"packs", "test", "--config", configPath}, "")
	if code != result.ExitSuccess {
		t.Fatalf("the blind suite passes its rows: exit=%d %s", code, stdout)
	}
	if !strings.Contains(stdout, "boundary:/expense/amount:5000") {
		t.Fatalf("the boundary is derived and reported missing: %s", stdout)
	}

	candidates := filepath.Join(filepath.Dir(configPath), "candidates.json")
	code, stdout, stderr := runTest(t, []string{"packs", "suggest", "--config", configPath, "--write", candidates}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	for _, required := range []string{"candidate input(s)", "written:", "not a row"} {
		if !strings.Contains(stdout, required) {
			t.Fatalf("the report must carry %q: %s", required, stdout)
		}
	}
	emitted, err := os.ReadFile(candidates)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(emitted), "expectedDisposition") || strings.Contains(string(emitted), "expectedErrorClass") {
		t.Fatalf("no candidate carries an expectation, not even an empty or sentinel one: %s", emitted)
	}
	var document project.Candidates
	if err := json.Unmarshal(emitted, &document); err != nil {
		t.Fatal(err)
	}
	if document.CandidatesVersion != project.CandidatesVersion {
		t.Fatalf("the emitted document declares its own version: %q", document.CandidatesVersion)
	}
	var at project.Candidate
	for _, candidate := range document.Candidates {
		var facts map[string]any
		if err := json.Unmarshal(candidate.Facts, &facts); err != nil {
			t.Fatal(err)
		}
		expense, _ := facts["expense"].(map[string]any)
		if expense != nil && expense["amount"] == "5000" {
			at = candidate
		}
	}
	if at.ID == "" {
		t.Fatalf("the lattice includes the literal itself: %s", emitted)
	}
	if at.Origin != project.CandidateOrigin || !strings.Contains(at.Rationale, "policy text") {
		t.Fatalf("every candidate declares its origin and says what is still owed: %+v", at)
	}

	// Layer 1: aiming the configuration's matrix at the raw candidate file is
	// refused by the matrix loader, which knows nothing about this generator.
	aimed := writeProjectFixture(t, `{"configVersion":"1","packs":{"expense":{"path":"packs/expense-0.1.0.pack.json","matrix":"packs/candidates.json"}}}`,
		map[string]string{
			"packs/expense-0.1.0.pack.json": thresholdPack,
			"packs/candidates.json":         string(emitted),
		})
	code, stdout, _ = runTest(t, []string{"packs", "test", "--config", aimed}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, "member this runtime does not know") {
		t.Fatalf("a candidate document is not a matrix: exit=%d %s", code, stdout)
	}

	// Layer 2: pasting a candidate into a cases array is refused with the
	// message that names the missing work — a refusal that predates this
	// generator entirely.
	pasted := `{"matrixVersion":"1","cases":[{"id":"at-threshold","origin":"generated","facts":{"expense":{"amount":"5000"}}}]}`
	pastedProject := writeProjectFixture(t, `{"configVersion":"1","packs":{"expense":{"path":"packs/expense-0.1.0.pack.json","matrix":"packs/pasted.json"}}}`,
		map[string]string{
			"packs/expense-0.1.0.pack.json": thresholdPack,
			"packs/pasted.json":             pasted,
		})
	code, stdout, _ = runTest(t, []string{"packs", "test", "--config", pastedProject}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, "must declare exactly one of expectedDisposition and expectedErrorClass") {
		t.Fatalf("a candidate is not a row until an expectation is authored: exit=%d %s", code, stdout)
	}

	// With the expectation written from the policy text — the description says
	// "5000 or more", so the row at exactly 5000 requires review — the row
	// loads, the boundary is witnessed, and the pack's operator is caught
	// disagreeing with its own prose.
	authored := `{"matrixVersion":"1","cases":[
	  {"id":"under","facts":{"expense":{"amount":"4999"}},"expectedDisposition":{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}},
	  {"id":"at-threshold","origin":"generated","facts":{"expense":{"amount":"5000"}},"expectedDisposition":{"kind":"outcome","outcomeId":"review","reasons":[],"handoff":{"state":"none"}}}
	]}`
	authoredProject := writeProjectFixture(t, `{"configVersion":"1","packs":{"expense":{"path":"packs/expense-0.1.0.pack.json","matrix":"packs/authored.json"}}}`,
		map[string]string{
			"packs/expense-0.1.0.pack.json": thresholdPack,
			"packs/authored.json":           authored,
		})
	code, stdout, _ = runTest(t, []string{"packs", "test", "--config", authoredProject}, "")
	if !strings.Contains(stdout, "origin generated: 1/2 row(s) declare it") {
		t.Fatalf("the origin count is rendered beside the rows: %s", stdout)
	}
	if strings.Contains(stdout, "boundary:/expense/amount:5000") {
		t.Fatalf("a row at the literal witnesses the boundary, so it is no longer reported missing: %s", stdout)
	}
	if code != result.ExitInvalid {
		t.Fatalf("the authored expectation and the pack's operator disagree, which is the defect this finds: exit=%d %s", code, stdout)
	}

	// The count is in the machine payload too, per pack, and it moves no status.
	_, stdout, _ = runTest(t, []string{"packs", "test", "--config", authoredProject, "--format", "json"}, "")
	var payload result.PackTest
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.OutputVersion != "2" {
		t.Fatalf("an added member is not a new output version: %q", payload.OutputVersion)
	}
	if len(payload.Packs) != 1 || len(payload.Packs[0].Origins) != 1 ||
		payload.Packs[0].Origins[0].Origin != project.CandidateOrigin || payload.Packs[0].Origins[0].Rows != 1 {
		t.Fatalf("the origins are reported per pack: %+v", payload.Packs)
	}
	// A suite in which nothing declares an origin carries no member: the
	// absence of the marker is not a claim that every row was hand written.
	_, stdout, _ = runTest(t, []string{"packs", "test", "--config", configPath, "--format", "json"}, "")
	var unmarked result.PackTest
	if err := json.Unmarshal([]byte(stdout), &unmarked); err != nil {
		t.Fatal(err)
	}
	if len(unmarked.Packs[0].Origins) != 0 {
		t.Fatalf("unmarked rows are counted nowhere: %+v", unmarked.Packs[0].Origins)
	}
}

// The write guards: a remote path, an existing file, and — the new one — any
// document this configuration declares. The exclusive open cannot make the last
// refusal, because a declared matrix that does not exist yet is a path it would
// happily create.
func TestSuggestRefusesToWriteOverDeclaredLaw(t *testing.T) {
	configPath := greenSuiteWithUnwitnessedBoundary(t, "")
	root := filepath.Dir(configPath)

	code, _, stderr := runTest(t, []string{"packs", "suggest", "--config", configPath, "--write", filepath.Join(root, "packs", "expense.matrix.json")}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, `the matrix of "expense"`) {
		t.Fatalf("the declared matrix is refused by name: exit=%d %s", code, stderr)
	}
	if !strings.Contains(stderr, "nothing was written") {
		t.Fatalf("the refusal says nothing was written: %s", stderr)
	}
	// The declared pack document itself is refused for the same reason: a
	// generated file must not land on reviewed law of any kind.
	code, _, stderr = runTest(t, []string{"packs", "suggest", "--config", configPath, "--write", filepath.Join(root, "packs", "expense-0.1.0.pack.json")}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, `the pack "expense"`) {
		t.Fatalf("the declared pack is refused by name: exit=%d %s", code, stderr)
	}
	// A declared path reached by a spelling that leaves the project directory
	// and comes back is the same file, and is refused as one: the check
	// resolves the destination rather than comparing its text.
	code, _, stderr = runTest(t, []string{"packs", "suggest", "--config", configPath, "--write",
		filepath.Join(root, "packs", "..", "packs", "expense.matrix.json")}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, `the matrix of "expense"`) {
		t.Fatalf("a declared document is declared however it is spelled: exit=%d %s", code, stderr)
	}

	// An existing file that nothing declares is refused by the exclusive open.
	existing := filepath.Join(root, "already-here.json")
	if err := os.WriteFile(existing, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = runTest(t, []string{"packs", "suggest", "--config", configPath, "--write", existing}, "")
	if code != result.ExitIO || !strings.Contains(stderr, "must be a new writable file") {
		t.Fatalf("an existing destination is refused: exit=%d %s", code, stderr)
	}

	// The candidate document and the report about the run are two documents, so
	// one stream cannot carry both.
	code, stdout, stderr := runTest(t, []string{"packs", "suggest", "--config", configPath, "--write", "-", "--format", "json"}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, "cannot be combined") {
		t.Fatalf("--write - and --format json are refused together: exit=%d %s %s", code, stdout, stderr)
	}
}

// A symlink aliasing the configuration's own directory is a second spelling of
// every declared document under it, and it is the spelling a lexical check
// cannot see: the destination does not exist yet, so there are no inodes for
// sameFile to compare, and the relative form under the alias leaves the
// project's directory and reads as declaring nothing. What lands there is a
// declared matrix that was missing until the generator created it, which is
// exactly the case the exclusive open cannot refuse and a reviewer would never
// notice. The check resolves both ends instead.
func TestSuggestRefusesADeclaredDocumentReachedThroughAnAliasedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is privileged on Windows")
	}
	// The matrix is declared and is not there yet, which is the only case that
	// matters: an existing file the exclusive open refuses on its own, and a
	// declared path it would happily create is what this check exists for.
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"expense":{
	  "path":"packs/expense-0.1.0.pack.json",
	  "matrix":"packs/expense.matrix.json"
	}}}`, map[string]string{"packs/expense-0.1.0.pack.json": thresholdPack})
	root := filepath.Dir(configPath)
	declared := filepath.Join(root, "packs", "expense.matrix.json")
	if _, err := os.Stat(declared); !os.IsNotExist(err) {
		t.Fatalf("the declared matrix must not exist for this probe: %v", err)
	}
	mirror := filepath.Join(t.TempDir(), "mirror")
	if err := os.Symlink(root, mirror); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	code, _, stderr := runTest(t, []string{"packs", "suggest", "--config", configPath, "--write",
		filepath.Join(mirror, "packs", "expense.matrix.json")}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, `the matrix of "expense"`) {
		t.Fatalf("a declared matrix reached through an aliased root is the same file: exit=%d %s", code, stderr)
	}
	if _, err := os.Stat(declared); !os.IsNotExist(err) {
		t.Fatalf("nothing was written at the declared matrix: %v", err)
	}

	// The alias is not thereby a refusal of its own: a destination under it that
	// the configuration declares nothing about is written, because this check
	// answers about declared documents and never about spellings it dislikes.
	fresh := filepath.Join(mirror, "candidates.json")
	code, _, stderr = runTest(t, []string{"packs", "suggest", "--config", configPath, "--write", fresh}, "")
	if code != result.ExitSuccess {
		t.Fatalf("an undeclared destination under the alias is written: exit=%d %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "candidates.json")); err != nil {
		t.Fatalf("the candidate document landed through the alias: %v", err)
	}
}

// --max bounds how many candidates a run may emit, so a non-positive one bounds
// nothing it could offer. It is refused by name rather than read as the
// default: a caller who asked for at most zero candidates and silently received
// five hundred was not answered, and silent substitution is what this whole
// family refuses.
func TestSuggestRefusesANonPositiveMax(t *testing.T) {
	configPath := greenSuiteWithUnwitnessedBoundary(t, "")
	for _, stated := range []string{"0", "-1"} {
		code, _, stderr := runTest(t, []string{"packs", "suggest", "--config", configPath, "--max", stated}, "")
		if code != result.ExitInvocation {
			t.Fatalf("--max %s is an invocation error: exit=%d %s", stated, code, stderr)
		}
		if !strings.Contains(stderr, "--max") || !strings.Contains(stderr, "positive count") {
			t.Fatalf("--max %s must be refused by name: %s", stated, stderr)
		}
	}
	// An absent --max carries the default, which is how the refusal above can be
	// about a value somebody typed rather than about one nobody stated.
	code, stdout, stderr := runTest(t, []string{"packs", "suggest", "--config", configPath}, "")
	if code != result.ExitSuccess || !strings.Contains(stdout, "candidate input(s)") {
		t.Fatalf("an absent --max is the default: exit=%d %s %s", code, stdout, stderr)
	}
}

// Two runs write identical bytes, and the JSON report names the pack the
// candidates came from — the provenance the document itself deliberately does
// not carry.
func TestSuggestIsDeterministicAndReportsItsProvenance(t *testing.T) {
	configPath := greenSuiteWithUnwitnessedBoundary(t, "")
	code, first, _ := runTest(t, []string{"packs", "suggest", "--config", configPath, "--write", "-"}, "")
	if code != result.ExitSuccess {
		t.Fatalf("exit=%d", code)
	}
	for range 4 {
		_, again, _ := runTest(t, []string{"packs", "suggest", "--config", configPath, "--write", "-"}, "")
		if again != first {
			t.Fatal("two runs over an unchanged pack must write identical bytes")
		}
	}
	_, stdout, _ := runTest(t, []string{"packs", "suggest", "--config", configPath, "--format", "json"}, "")
	var payload result.PackSuggestion
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Kind != result.ProjectKind || payload.CandidatesVersion != project.CandidatesVersion {
		t.Fatalf("the report labels itself: %+v", payload)
	}
	if len(payload.Packs) != 1 || payload.Packs[0].PackID != "https://example.invalid/judgment-packs/expense-threshold" ||
		payload.Packs[0].PackVersion != "0.1.0" {
		t.Fatalf("the report names the pack identity the candidates were derived from: %+v", payload.Packs)
	}
	if payload.Summary.Candidates == 0 || payload.Packs[0].Candidates != payload.Summary.Candidates {
		t.Fatalf("the counts agree: %+v", payload)
	}
	if payload.WrittenTo != "" {
		t.Fatalf("nothing is written without --write: %q", payload.WrittenTo)
	}
	// --base varies from an already-reviewed row, and the run still refuses to
	// state an expectation for any of them.
	code, stdout, _ = runTest(t, []string{"packs", "suggest", "--config", configPath, "--id", "expense", "--base", "under", "--write", "-"}, "")
	if code != result.ExitSuccess || strings.Contains(stdout, "expected") {
		t.Fatalf("a based run emits inputs only: exit=%d %s", code, stdout)
	}
	if !strings.Contains(stdout, `"expense":{"amount":"5000"}`) && !strings.Contains(stdout, `"amount": "5000"`) {
		t.Fatalf("the reviewed row's facts are carried with the one pointer moved: %s", stdout)
	}
}

// A dimension the generator cannot address is reported rather than silently
// omitted, and it is reported even when the document itself went to stdout: a
// run that emitted the candidates and swallowed the report would be silence
// over a gap.
func TestSuggestReportsASkippedDimensionOnStderrWhenTheDocumentTakesStdout(t *testing.T) {
	quantifier := `{
	  "specVersion": "0.2.0-draft", "id": "q", "version": "0.1.0", "title": "Q",
	  "description": "A pack using a draft collection quantifier.",
	  "decision": {"question": "Any line over the limit?", "subject": "order"},
	  "outcomes": [{"id": "review", "label": "Review"}],
	  "rules": [{"id": "any-line", "outcome": "review", "onUnknown": "escalate",
	    "when": {"op": "exists", "path": "/lines", "where": {"op": "fact", "path": "/amount", "operator": "greater-than", "value": "100"}}}],
	  "fallbackOutcome": "review"
	}`
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"orders":{"path":"packs/q-0.1.0.pack.json"}}}`,
		map[string]string{"packs/q-0.1.0.pack.json": quantifier})

	code, stdout, stderr := runTest(t, []string{"packs", "suggest", "--config", configPath, "--write", "-"}, "")
	if code != result.ExitSuccess {
		t.Fatalf("a run that derives nothing still ran: exit=%d %s", code, stderr)
	}
	var document project.Candidates
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("stdout is exactly the candidate document: %v", err)
	}
	if len(document.Candidates) != 0 {
		t.Fatalf("a quantifier's element-relative comparison derives no candidate: %+v", document.Candidates)
	}
	if !strings.Contains(stderr, "skipped draft-rfc-quantifiers") {
		t.Fatalf("the skipped dimension is named on stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "skipped: no selected pack's conditions state a comparison") {
		t.Fatalf("a run that derived nothing says so rather than passing: %q", stderr)
	}
}

// The byte budget is a bound on the file, so the surface that writes the file
// is where "writes nothing" has to be checked. A base row that is deep rather
// than wide — a hundred nested containers wrapping very many one-character
// tokens, legal on every carrier bound and under a megabyte as it sits in the
// matrix — multiplies through EncodeCandidates' indentation into a document
// hundreds of megabytes long. At DEFAULT flags the run refuses with the budget
// diagnostic and the destination is never created: the refusal comes before a
// document that size could exist, which is the whole reason the charge is taken
// on the written form rather than the composed one.
func TestSuggestRefusesADeeplyNestedBaseRowAndCreatesNoFile(t *testing.T) {
	const levels, tokens = 100, 150_000
	leaves := make([]string, tokens)
	for index := range leaves {
		leaves[index] = `"a"`
	}
	nested := "[" + strings.Join(leaves, ",") + "]"
	for level := 0; level < levels; level++ {
		nested = `{"n":` + nested + `}`
	}
	expectation := `"expectedDisposition":{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}`
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"deep","facts":{"expense":{"amount":"10"},"deep":` + nested + `},` + expectation + `}
	]}`
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"expense":{
	  "path":"packs/expense-0.1.0.pack.json",
	  "matrix":"packs/expense.matrix.json"
	}}}`, map[string]string{
		"packs/expense-0.1.0.pack.json": thresholdPack,
		"packs/expense.matrix.json":     matrix,
	})

	destination := filepath.Join(filepath.Dir(configPath), "candidates.json")
	arguments := []string{"packs", "suggest", "--config", configPath, "--id", "expense", "--base", "deep", "--write", destination}
	code, stdout, stderr := runTest(t, arguments, "")
	if code != result.ExitInvocation {
		t.Fatalf("a base row past the byte budget refuses the run: exit=%d %s %s", code, stdout, stderr)
	}
	for _, required := range []string{"nothing was written", "indentation included", "--max", "--base"} {
		if !strings.Contains(stderr, required) {
			t.Fatalf("the refusal must name %q: %s", required, stderr)
		}
	}
	// The contract this test exists for: no file at the destination. The refusal
	// fired while composing, so the hundreds of megabytes it is about were never
	// written and never held.
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("a refused run creates nothing at the destination: %v", err)
	}

	// The same refusal under --format json, which is where the diagnostic code
	// is machine-readable — and still nothing at the destination.
	code, stdout, _ = runTest(t, append(append([]string{}, arguments...), "--format", "json"), "")
	if code != result.ExitInvocation || !strings.Contains(stdout, "JPS-RESOURCE-SUGGEST-OUTPUT-BYTES") {
		t.Fatalf("the refusal names the resource it is about: exit=%d %s", code, stdout)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("a refused run creates nothing at the destination: %v", err)
	}
}
