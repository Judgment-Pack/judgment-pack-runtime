package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// describeInputs are the packs the drift test describes: the SDK's view
// fixtures and every pack of both evaluation corpora.
func describeInputs(t *testing.T) map[string][]byte {
	t.Helper()
	inputs := map[string][]byte{}
	fixtures, err := filepath.Glob(filepath.Join("..", "..", "sdk", "view", "testdata", "packs", "*", "*.json"))
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("no view fixtures: %v", err)
	}
	for _, fixture := range fixtures {
		data, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		inputs[fixture] = data
	}
	for _, version := range []string{artifacts.EvaluatorDraftVersion, artifacts.CoreVersion} {
		set, err := artifacts.Load(version)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"packs/data-request-intake-triage.json", "packs/decimal-threshold-fee.json"} {
			data, err := set.EvaluationPack(name)
			if err != nil {
				t.Fatal(err)
			}
			inputs[version+"/"+name] = data
		}
	}
	return inputs
}

// The command writes the SDK's bytes and nothing else: the Markdown exactly,
// and in JSON the view's canonical bytes as the view member's string, whether
// the envelope is indented or not and whether the pack is a file or standard
// input.
func TestPacksDescribeWritesTheSDKBytes(t *testing.T) {
	for name, pack := range describeInputs(t) {
		t.Run(name, func(t *testing.T) {
			loaded, err := sdk.Load(context.Background(), pack, sdk.LoadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			view, err := loaded.View()
			if err != nil {
				t.Fatal(err)
			}
			path := writeFixture(t, pack)
			for _, args := range [][]string{{"packs", "describe", path}, {"packs", "describe", path, "--format", "markdown"}} {
				code, stdout, stderr := runTest(t, args, "")
				if code != 0 || stderr != "" || stdout != string(view.Markdown()) {
					t.Fatalf("%v: exit=%d stderr=%q; the Markdown is not the SDK's", args, code, stderr)
				}
			}
			code, stdout, stderr := runTest(t, []string{"packs", "describe", "-"}, string(pack))
			if code != 0 || stderr != "" || stdout != string(view.Markdown()) {
				t.Fatalf("standard input: exit=%d stderr=%q", code, stderr)
			}
			for _, args := range [][]string{{"packs", "describe", path, "--format", "json"}, {"packs", "describe", path, "--format", "json", "--pretty"}} {
				code, stdout, stderr := runTest(t, args, "")
				if code != 0 || stderr != "" {
					t.Fatalf("%v: exit=%d stderr=%q", args, code, stderr)
				}
				var envelope result.PackDescription
				decoder := json.NewDecoder(strings.NewReader(stdout))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&envelope); err != nil {
					t.Fatalf("%v: %v", args, err)
				}
				if envelope.View != string(view.JSON()) {
					t.Fatalf("%v: the view member is not the SDK's bytes:\n got %s\nwant %s", args, envelope.View, view.JSON())
				}
				if envelope.OutputVersion != result.OutputVersion || envelope.Command != "packs describe" || envelope.Status != "described" || envelope.Tool != result.CurrentTool() {
					t.Fatalf("%v: envelope %+v", args, envelope)
				}
			}
		})
	}
}

// Every refusal is reported, in each format, with nothing described: an
// unusable invocation, input that cannot be read or is past the byte limit,
// and each kind of pack the loader refuses. In JSON each is one envelope on
// stdout naming the command; in Markdown, the default, stdout carries no view.
func TestPacksDescribeRefusals(t *testing.T) {
	directory := t.TempDir()
	write := func(name string, data []byte) string {
		target := filepath.Join(directory, name)
		if err := os.WriteFile(target, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return target
	}
	invalid := strings.Replace(viewlessPack, `"title": "Viewless"`, `"title": ""`, 1)
	extended := strings.Replace(viewlessPack, `"specVersion": "0.3.0",`, `"specVersion": "0.3.0", "profile": "extended",`, 1)
	for _, test := range []struct {
		name   string
		args   []string
		stdin  string
		exit   int
		code   string
		status string
	}{
		{"an empty pack", []string{write("empty.json", nil)}, "", result.ExitInvocation, "JPS-EVALUATION-INPUT-JSON", "error"},
		{"an empty standard input", []string{"-"}, "", result.ExitInvocation, "JPS-EVALUATION-INPUT-JSON", "error"},
		{"a missing file", []string{filepath.Join(directory, "absent.json")}, "", result.ExitIO, "JPS-INPUT-READ", "error"},
		{"a directory", []string{directory}, "", result.ExitIO, "JPS-INPUT-READ", "error"},
		{"an oversized pack", []string{write("large.json", bytes.Repeat([]byte(" "), int(carrier.HardMaxBytes)+1))}, "", result.ExitIO, "JPS-RESOURCE-INPUT-BYTE-LIMIT", "error"},
		{"text that is not JSON", []string{write("text.json", []byte("not json"))}, "", result.ExitInvocation, "JPS-EVALUATION-INPUT-JSON", "error"},
		{"a pack that is not conformant", []string{write("invalid.json", []byte(invalid))}, "", result.ExitInvalid, "JPS-EVALUATION-PACK-NOT-CONFORMANT", "error"},
		{"a profile this runtime does not implement", []string{write("extended.json", []byte(extended))}, "", result.ExitUnsupported, "profile-unsupported", "unsupported"},
		{"a URL", []string{"https://example.invalid/pack.json"}, "", result.ExitInvocation, "JPS-INVOCATION-INPUT", "error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"packs", "describe"}, test.args...)
			jsonArgs := append(append([]string{}, args...), "--format", "json")
			code, stdout, stderr := runTest(t, jsonArgs, test.stdin)
			if code != test.exit || stderr != "" || strings.Count(stdout, "\n") != 1 {
				t.Fatalf("exit=%d (want %d) stdout=%q stderr=%q", code, test.exit, stdout, stderr)
			}
			var envelope struct {
				OutputVersion string `json:"outputVersion"`
				Command       string `json:"command"`
				Status        string `json:"status"`
				View          *string
				Diagnostics   []struct {
					Code string `json:"code"`
				} `json:"diagnostics"`
				EvaluationError *struct{} `json:"evaluationError"`
			}
			if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.OutputVersion != result.OutputVersion || envelope.Command != "packs describe" || envelope.Status != test.status || envelope.View != nil || envelope.EvaluationError != nil {
				t.Fatalf("envelope %s", stdout)
			}
			if test.code != "profile-unsupported" && (len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != test.code) {
				t.Fatalf("diagnostics %s, want %s", stdout, test.code)
			}
			if test.code == "profile-unsupported" && !strings.Contains(stdout, `"profile-unsupported"`) {
				t.Fatalf("profile refusal %s", stdout)
			}

			code, stdout, stderr = runTest(t, args, test.stdin)
			if code != test.exit || strings.Contains(stdout, "# ") || strings.Contains(stdout, "View version") {
				t.Fatalf("markdown: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if test.status == "unsupported" {
				if !strings.HasPrefix(stdout, "unsupported:") || stderr != "" {
					t.Fatalf("markdown: stdout=%q stderr=%q", stdout, stderr)
				}
			} else if stdout != "" || !strings.HasPrefix(stderr, "error:") {
				t.Fatalf("markdown: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
	// A format the command does not write is refused as every command
	// refuses one; it is not JSON, so the refusal is not either.
	code, stdout, stderr := runTest(t, []string{"packs", "describe", write("format.json", []byte(viewlessPack)), "--format", "human"}, "")
	if code != result.ExitInvocation || stdout != "" || stderr != "error: --format must be markdown or json\n" {
		t.Fatalf("an unknown format: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, args := range [][]string{{"packs", "describe", "--format", "json"}, {"packs", "describe", "one", "two", "--format", "json"}} {
		code, stdout, _ := runTest(t, args, "")
		var envelope struct {
			Command string `json:"command"`
			Status  string `json:"status"`
		}
		if code != result.ExitInvocation || json.Unmarshal([]byte(stdout), &envelope) != nil || envelope.Command != "packs describe" || envelope.Status != "error" {
			t.Fatalf("%v: exit=%d stdout=%q", args, code, stdout)
		}
	}
}

// --filter writes the SDK's filtered view bytes and nothing else, in
// Markdown and as the filteredView member of the JSON envelope, never a view
// member; the flag repeats, each use one criterion that every match must
// meet, and a comma is part of a value, never a separator.
func TestPacksDescribeFilterWritesTheSDKBytes(t *testing.T) {
	set, err := artifacts.Load(artifacts.Core050Version)
	if err != nil {
		t.Fatal(err)
	}
	pack, _, err := set.Example("expense-approval-annotated")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := sdk.Load(context.Background(), pack, sdk.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	path := writeFixture(t, pack)
	for _, test := range []struct {
		flags    []string
		criteria []view.Criterion
		rules    int
	}{
		{[]string{"--filter", "category=threshold"}, []view.Criterion{{Key: "category", Value: "threshold"}}, 2},
		{[]string{"--filter", "category=threshold", "--filter", "review-cycle=monthly"}, []view.Criterion{{Key: "category", Value: "threshold"}, {Key: "review-cycle", Value: "monthly"}}, 1},
		{[]string{"--filter", "category=threshold,policy"}, []view.Criterion{{Key: "category", Value: "threshold,policy"}}, 0},
	} {
		filtered, err := loaded.FilteredView(test.criteria)
		if err != nil {
			t.Fatal(err)
		}
		if len(filtered.Rules) != test.rules {
			t.Fatalf("%v: %d rules matched, want %d", test.flags, len(filtered.Rules), test.rules)
		}
		args := append([]string{"packs", "describe", path}, test.flags...)
		code, stdout, stderr := runTest(t, args, "")
		if code != 0 || stderr != "" || stdout != string(filtered.Markdown()) {
			t.Fatalf("%v: exit=%d stderr=%q; the Markdown is not the SDK's:\n%s", args, code, stderr, stdout)
		}
		code, stdout, stderr = runTest(t, append(args, "--format", "json"), "")
		if code != 0 || stderr != "" {
			t.Fatalf("%v: exit=%d stderr=%q", args, code, stderr)
		}
		var envelope result.PackFilteredDescription
		decoder := json.NewDecoder(strings.NewReader(stdout))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&envelope); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if envelope.FilteredView != string(filtered.JSON()) || envelope.Status != "filtered" || envelope.Command != "packs describe" || envelope.OutputVersion != result.OutputVersion {
			t.Fatalf("%v: envelope %s", args, stdout)
		}
	}
}

// A --filter criterion no annotation could hold, and --filter on a pack whose
// specVersion defines no annotations, are refused as an invocation the
// command cannot carry out, with nothing described, in each format.
func TestPacksDescribeFilterRefusals(t *testing.T) {
	set, err := artifacts.Load(artifacts.Core050Version)
	if err != nil {
		t.Fatal(err)
	}
	pack, _, err := set.Example("expense-approval-annotated")
	if err != nil {
		t.Fatal(err)
	}
	annotated := writeFixture(t, pack)
	// viewlessPack with a second outcome, which Core 0.3.0 requires.
	core := writeFixture(t, []byte(strings.Replace(viewlessPack, `[{"id": "yes", "label": "Yes"}]`, `[{"id": "yes", "label": "Yes"}, {"id": "no", "label": "No"}]`, 1)))
	for _, args := range [][]string{
		{annotated, "--filter", "Category=threshold"},
		{annotated, "--filter", "category"},
		{annotated, "--filter", "category="},
		{annotated, "--filter", "category=a", "--filter", "category=a"},
		// The draft's character rules: an isolate, a zero-width space, a tag
		// character, and white space at either end.
		{annotated, "--filter", "category=thre\u2066shold"},
		{annotated, "--filter", "category=threshold\u200b"},
		{annotated, "--filter", "category=threshold\U000e0020"},
		{annotated, "--filter", "category= threshold"},
		{annotated, "--filter", "category=threshold\u00a0"},
		{core, "--filter", "category=threshold"},
	} {
		full := append([]string{"packs", "describe"}, args...)
		code, stdout, stderr := runTest(t, append(append([]string{}, full...), "--format", "json"), "")
		var envelope struct {
			Status       string  `json:"status"`
			View         *string `json:"view"`
			FilteredView *string `json:"filteredView"`
			Diagnostics  []struct {
				Code string `json:"code"`
			} `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
			t.Fatalf("%v: %v %q", args, err, stdout)
		}
		if code != result.ExitInvocation || stderr != "" || envelope.Status != "error" || envelope.View != nil || envelope.FilteredView != nil ||
			len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "JPS-INVOCATION-FILTER" {
			t.Fatalf("%v: exit=%d stdout=%s stderr=%q", args, code, stdout, stderr)
		}
		code, stdout, stderr = runTest(t, full, "")
		if code != result.ExitInvocation || stdout != "" || !strings.HasPrefix(stderr, "error: --filter: ") {
			t.Fatalf("%v markdown: exit=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
}

// D2 through the CLI: the Markdown states a criterion holding a look-alike in
// ASCII beside it, the JSON envelope's filtered view carries it exactly, and a
// refused criterion's message names a refused character by its U+ number and
// a refused key in ASCII, in both formats, each written here by hand. A
// criterion is refused before the pack is read, so no message says that
// nothing was described.
func TestPacksDescribeFilterShowsEveryCodePointRecoverably(t *testing.T) {
	path := writeFixture(t, []byte(`{
  "specVersion": "0.5.0",
  "id": "https://example.invalid/judgment-packs/look-alike",
  "version": "0.1.0",
  "title": "Look-alike",
  "decision": {"intent": "Test the filter's display.", "question": "Which label?"},
  "outcomes": [{"id": "yes", "label": "Yes"}, {"id": "no", "label": "No"}],
  "rules": [
    {"id": "a", "description": "A", "when": {"op": "literal", "value": true}, "outcome": "yes", "onUnknown": "ignore", "extensions": {"org.judgmentpack.annotations": {"label": "c\u0430fe"}}},
    {"id": "b", "description": "B", "when": {"op": "literal", "value": true}, "outcome": "no", "onUnknown": "ignore", "extensions": {"org.judgmentpack.annotations": {"label": "cafe"}}}
  ]
}`))
	args := []string{"packs", "describe", path, "--filter", "label=c\u0430fe"}
	code, stdout, stderr := runTest(t, args, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "\n- Filter, every criterion matching:\n  - label is c\u0430fe (ASCII `\"c\\u0430fe\"`)\n- Outcomes matched: 0 of 2\n- Rules matched: 1 of 2\n") {
		t.Fatalf("exit=%d stderr=%q stdout:\n%s", code, stderr, stdout)
	}
	code, stdout, stderr = runTest(t, append(args, "--format", "json"), "")
	var envelope result.PackFilteredDescription
	if err := json.Unmarshal([]byte(stdout), &envelope); code != 0 || stderr != "" || err != nil {
		t.Fatalf("exit=%d stderr=%q %v %s", code, stderr, err, stdout)
	}
	var filtered struct {
		Filter struct {
			Criteria []struct{ Key, Value string }
		}
	}
	if err := json.Unmarshal([]byte(envelope.FilteredView), &filtered); err != nil || len(filtered.Filter.Criteria) != 1 ||
		filtered.Filter.Criteria[0].Key != "label" || filtered.Filter.Criteria[0].Value != "c\u0430fe" {
		t.Fatalf("the filtered view states %+v (%v)", filtered.Filter.Criteria, err)
	}

	const refused = "--filter: view: invalid filter criterion: "
	for _, test := range []struct{ filter, message string }{
		{"label=cafe\u200b", refused + "the value for label holds U+200B, which no annotation string may hold"},
		{"label=cafe\u00a0", refused + "the value for label ends with U+00A0, white space, which no annotation string may begin or end with"},
		{"l\u0430bel=cafe", refused + "the key \"l\\u0430bel\" is not an annotation key, which is lower-case letters and digits in hyphen-separated words, starting with a letter, at most 64 characters"},
	} {
		args := []string{"packs", "describe", path, "--filter", test.filter}
		code, stdout, stderr := runTest(t, args, "")
		if code != result.ExitInvocation || stdout != "" || stderr != "error: "+test.message+"\n" {
			t.Errorf("%q: exit=%d stdout=%q stderr=%q", test.filter, code, stdout, stderr)
		}
		code, stdout, _ = runTest(t, append(args, "--format", "json"), "")
		var failure struct {
			Diagnostics []struct{ Code, Message string }
		}
		if err := json.Unmarshal([]byte(stdout), &failure); err != nil || code != result.ExitInvocation || len(failure.Diagnostics) != 1 ||
			failure.Diagnostics[0].Code != "JPS-INVOCATION-FILTER" || failure.Diagnostics[0].Message != test.message {
			t.Errorf("%q: exit=%d %v %s", test.filter, code, err, stdout)
		}
	}
}

// viewlessPack is a valid Core 0.3.0 pack written by hand.
const viewlessPack = `{
  "specVersion": "0.3.0",
  "id": "https://example.invalid/judgment-packs/viewless",
  "version": "0.1.0",
  "title": "Viewless",
  "decision": {"intent": "Test the describe refusals.", "question": "Is it refused?"},
  "outcomes": [{"id": "yes", "label": "Yes"}],
  "rules": [{"id": "always", "description": "Always yes.", "when": {"op": "literal", "value": true}, "outcome": "yes", "onUnknown": "ignore"}]
}`

// engineFailure is the refusal the shared engine's own preflight gives the
// same pack bytes, by the steps sdk.Load takes, with the exit code the CLI's
// evaluate command exits with.
func engineFailure(t *testing.T, validator *validation.Engine, pack []byte) *evaluation.Failure {
	t.Helper()
	contract := result.LegacyEvaluatorContract()
	selected, failure := evaluation.SelectContract(pack)
	if failure != nil && failure.Code != "JPS-EVALUATION-PACK-SPEC-VERSION" {
		return failure
	}
	if failure == nil {
		contract = selected
	}
	return evaluation.NewEngine(validator).AdmitPack(pack).PackFailure(evaluation.Options{SpecVersion: contract.Version(), Command: "experimental evaluate"})
}

// describeRefusalExit derives the exit code from what a refusal carries. It
// is held equal to the engine's own exit code, and the refusal's code to the
// engine's, for every pack Load refuses among every conformance case of every
// bundled version, and for each purpose-built refusal; a sample runs through
// the command itself.
func TestDescribeExitsAsTheEvaluatorRefuses(t *testing.T) {
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"empty":                   nil,
		"not JSON":                []byte("not json"),
		"an array":                []byte("[]"),
		"a duplicate member":      []byte(`{"specVersion": "0.3.0", "specVersion": "0.3.0"}`),
		"too deep":                []byte(strings.Repeat("[", 200) + strings.Repeat("]", 200)),
		"too deep in an object":   []byte(`{"specVersion": "0.3.0", "x": ` + strings.Repeat("[", 200) + strings.Repeat("]", 200) + `}`),
		"oversized":               bytes.Repeat([]byte(" "), int(carrier.HardMaxBytes)+1),
		"no specVersion":          []byte(strings.Replace(viewlessPack, `"specVersion": "0.3.0",`, "", 1)),
		"a numeric specVersion":   []byte(strings.Replace(viewlessPack, `"specVersion": "0.3.0"`, `"specVersion": 3`, 1)),
		"an unknown specVersion":  []byte(strings.Replace(viewlessPack, `"specVersion": "0.3.0"`, `"specVersion": "9.9.9"`, 1)),
		"the first draft":         []byte(strings.Replace(viewlessPack, `"specVersion": "0.3.0"`, `"specVersion": "0.1.0-draft"`, 1)),
		"an extended profile":     []byte(strings.Replace(viewlessPack, `"specVersion": "0.3.0",`, `"specVersion": "0.3.0", "profile": "extended",`, 1)),
		"an unknown profile":      []byte(strings.Replace(viewlessPack, `"specVersion": "0.3.0",`, `"specVersion": "0.3.0", "profile": "other",`, 1)),
		"an empty title":          []byte(strings.Replace(viewlessPack, `"title": "Viewless"`, `"title": ""`, 1)),
		"an unknown member":       []byte(strings.Replace(viewlessPack, `"title": "Viewless"`, `"title": "Viewless", "owner": "x"`, 1)),
		"an unresolved reference": []byte(strings.Replace(viewlessPack, `"outcome": "yes"`, `"outcome": "no"`, 1)),
	}
	for _, version := range artifacts.SupportedVersions() {
		set, err := artifacts.Load(version)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := set.Manifest()
		if err != nil {
			t.Fatal(err)
		}
		var rows struct {
			Cases []struct {
				Path string `json:"path"`
			} `json:"cases"`
		}
		if err := json.Unmarshal(manifest, &rows); err != nil || len(rows.Cases) == 0 {
			t.Fatalf("%s manifest: %v", version, err)
		}
		for _, row := range rows.Cases {
			data, err := set.Case(row.Path)
			if err != nil {
				t.Fatal(err)
			}
			cases[version+"/"+row.Path] = data
		}
	}
	refused := map[int]int{}
	for name, pack := range cases {
		failure := engineFailure(t, validator, pack)
		_, err := sdk.Load(context.Background(), pack, sdk.LoadOptions{})
		if failure == nil {
			if err != nil {
				t.Errorf("%s: the engine admits the pack and Load says %v", name, err)
			}
			continue
		}
		var refusal *sdk.Refusal
		if !errors.As(err, &refusal) {
			t.Errorf("%s: the engine refuses with %s and Load says %v", name, failure.Code, err)
			continue
		}
		if refusal.Code != failure.Code || describeRefusalExit(refusal) != failure.ExitCode {
			t.Errorf("%s: Load refuses with %s, exit %d; the engine with %s, exit %d", name, refusal.Code, describeRefusalExit(refusal), failure.Code, failure.ExitCode)
		}
		refused[failure.ExitCode]++
	}
	if total := refused[result.ExitInvalid] + refused[result.ExitUnsupported] + refused[result.ExitInvocation] + refused[result.ExitIO]; total < 200 {
		t.Fatalf("only %d refusals were compared, from %d cases: %v", total, len(cases), refused)
	}
	for _, exit := range []int{result.ExitInvalid, result.ExitUnsupported, result.ExitInvocation, result.ExitIO} {
		if refused[exit] == 0 {
			t.Errorf("no refusal exits %d, so the rule for it was not held: %v", exit, refused)
		}
	}
	for _, name := range []string{"not JSON", "too deep", "an unknown specVersion", "the first draft", "an unknown profile", "an unresolved reference"} {
		code, _, _ := runTest(t, []string{"packs", "describe", writeFixture(t, cases[name])}, "")
		if want := engineFailure(t, validator, cases[name]).ExitCode; code != want {
			t.Errorf("%s: packs describe exits %d, the engine %d", name, code, want)
		}
	}
}

// packs describe is the one CLI file that reaches package sdk, and it loads
// and views a pack and never evaluates one: it is not a surface that reaches
// the evaluator, which TestConformanceClaimNamesEverySurfaceReachingTheEvaluator
// counts by constructor site, and it constructs none of its own.
func TestPacksDescribeLoadsAndNeverEvaluates(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	importers := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(source), `"github.com/Judgment-Pack/judgment-pack-runtime/sdk"`) {
			importers = append(importers, name)
		}
	}
	if strings.Join(importers, ",") != "packs_describe.go" {
		t.Fatalf("the CLI files importing package sdk are %v", importers)
	}
	source, err := os.ReadFile("packs_describe.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Count(text, "sdk.Load(") != 1 || strings.Count(text, ".View()") != 1 {
		t.Fatal("packs describe no longer loads and views through package sdk exactly once")
	}
	for _, deciding := range []string{".Evaluate(", ".RunCase", ".RunCorpus", "evaluation.NewEngine(", "a.engine"} {
		if strings.Contains(text, deciding) {
			t.Fatalf("packs describe adds %s; review the claim scope in CONFORMANCE.md", deciding)
		}
	}
}
