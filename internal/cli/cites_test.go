package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The values of markedCites, each distinctive enough that a refusal repeating
// any of them would show it.
var (
	citesMarkerSession   = "s-NOECHO-7f3a"
	citesMarkerIndex     = "8675309"
	citesMarkerSignature = strings.Repeat("c0ffee", 21) + "c0"
)

// markedCites writes a well-formed citations document whose every value is a
// marker, and returns its path.
func markedCites(t *testing.T) string {
	t.Helper()
	return writeDocument(t, "cites.json", `[{"sessionId":"`+citesMarkerSession+`","callIndex":`+citesMarkerIndex+`,"signature":"`+citesMarkerSignature+`"}]`)
}

// operationalCode is the one diagnostic code and message a refused run
// reports, read from its JSON output.
func operationalCode(t *testing.T, stdout string) (string, string) {
	t.Helper()
	var output result.OperationalError
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("undecodable %q: %v", stdout, err)
	}
	if len(output.Diagnostics) != 1 {
		t.Fatalf("one diagnostic: %+v", output)
	}
	return output.Diagnostics[0].Code, output.Diagnostics[0].Message
}

// refusedForCitesWithoutATrail holds one run to the no-trail refusal: exit 3,
// JPS-INVOCATION-CITES and the fixed sentence (the JSON message, or the human
// stderr line), no evaluation reported, and no value of the citations
// repeated anywhere in what the run wrote.
func refusedForCitesWithoutATrail(t *testing.T, label, format string, code int, stdout, stderr string) {
	t.Helper()
	if code != result.ExitInvocation {
		t.Fatalf("%s %s: exit=%d stdout=%q stderr=%q", label, format, code, stdout, stderr)
	}
	for _, evaluated := range []string{`"disposition"`, `"resultNode"`, `"nodes"`, "disposition:"} {
		if strings.Contains(stdout+stderr, evaluated) {
			t.Fatalf("%s %s: nothing is evaluated, yet %s: stdout=%q", label, format, evaluated, stdout)
		}
	}
	for _, marker := range []string{citesMarkerSession, citesMarkerIndex, citesMarkerSignature} {
		if strings.Contains(stdout+stderr, marker) {
			t.Fatalf("%s %s: the refusal repeats a value of the citations (%s): stdout=%q stderr=%q", label, format, marker, stdout, stderr)
		}
	}
	if format == "json" {
		if code, message := operationalCode(t, stdout); code != "JPS-INVOCATION-CITES" || message != citesNoTrailMessage {
			t.Fatalf("%s: code=%s message=%q", label, code, message)
		}
		return
	}
	if stdout != "" || stderr != "error: "+citesNoTrailMessage+"\n" {
		t.Fatalf("%s: stdout=%q stderr=%q", label, stdout, stderr)
	}
}

// Citations given in a project that keeps no trail would be accepted and
// recorded nowhere (ADR-0051): experimental evaluate refuses them before
// anything is evaluated, however the pack is named and when there is no
// configuration at all, in both formats, and creates nothing. The same run
// without citations, or with an empty array, which cites nothing, runs; a
// rehearsal with citations keeps its rule and runs, recording nothing.
func TestEvaluateRefusesCitationsWhereNoTrailIsKept(t *testing.T) {
	if citesNoTrailMessage != "--cites is not taken here: the project keeps no trail, so citations would be accepted and not recorded." {
		t.Fatalf("the sentence: %q", citesNoTrailMessage)
	}
	configPath := oneGoodProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	cites := markedCites(t)
	pack := writeDocument(t, "pack.json", evaluatorPack(t))
	noConfiguration := filepath.Join(t.TempDir(), "jpack.json")
	for _, tc := range []struct {
		label string
		args  []string
	}{
		{"pack by id", []string{"--pack-id", "intake", "--config", configPath}},
		{"pack by path", []string{pack, "--config", configPath}},
		{"no configuration at all", []string{pack, "--config", noConfiguration}},
	} {
		for _, format := range []string{"json", "human"} {
			args := append([]string{"experimental", "evaluate"}, tc.args...)
			args = append(args, "--facts", facts, "--cites", cites, "--format", format)
			code, stdout, stderr := runTest(t, args, "")
			refusedForCitesWithoutATrail(t, tc.label, format, code, stdout, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("nothing is created: %v", err)
	}
	if _, err := os.Stat(noConfiguration); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("nothing is created where no configuration is: %v", err)
	}

	empty := writeDocument(t, "empty.json", "[]")
	for _, tc := range []struct {
		label string
		extra []string
		want  string
	}{
		{"without citations", nil, `"disposition"`},
		{"an empty array, which cites nothing", []string{"--cites", empty}, `"disposition"`},
		{"a rehearsal with citations", []string{"--cites", cites, "--rehearsal"}, `"rehearsal":true`},
	} {
		args := append([]string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath,
			"--facts", facts, "--format", "json"}, tc.extra...)
		code, stdout, stderr := runTest(t, args, "")
		if code != 0 || stderr != "" || !strings.Contains(stdout, tc.want) {
			t.Fatalf("%s runs: exit=%d stdout=%q stderr=%q", tc.label, code, stdout, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the runs that ran record nothing either: %v", err)
	}
}

// The refusal sits where the binding's does: after the configuration is read,
// so a configuration that does not load is what is reported; before any input
// is read, so an unreadable facts document is not; and, given a binding too,
// the citations are what is refused, as their shape is checked first.
func TestEvaluateRefusesCitationsWhereNoTrailIsKeptInTheBindingsPlace(t *testing.T) {
	configPath := oneGoodProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	cites := markedCites(t)
	binding := writeDocument(t, "binding.json", `{"kind":"k","digest":"`+bindingDigest+`"}`)
	absent := filepath.Join(t.TempDir(), "absent.json")
	broken := filepath.Join(t.TempDir(), "jpack.json")
	if err := os.WriteFile(broken, []byte(`{"configVersion":"1",`), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		t.Helper()
		code, stdout, _ := runTest(t, append([]string{"experimental", "evaluate", "--format", "json"}, args...), "")
		diagnostic, message := operationalCode(t, stdout)
		return code, diagnostic, message
	}

	// A configuration that does not load is reported first, as for a binding.
	for _, flag := range []string{"--cites", "--binding"} {
		document := map[string]string{"--cites": cites, "--binding": binding}[flag]
		_, diagnostic, message := run("--pack-id", "intake", "--config", broken, "--facts", facts, flag, document)
		if diagnostic != "JPS-PROJECT-CONFIG-JSON" {
			t.Fatalf("%s under a broken configuration: %s %q", flag, diagnostic, message)
		}
	}
	// An unreadable input is not: the refusal comes before it is read, as a
	// binding's does, and without either the read failure is what is reported.
	for _, tc := range []struct {
		flag, document, wantCode, wantMessage string
	}{
		{"--cites", cites, "JPS-INVOCATION-CITES", citesNoTrailMessage},
		{"--binding", binding, "JPS-INVOCATION-BINDING", bindingNoTrailMessage},
	} {
		code, diagnostic, message := run("--pack-id", "intake", "--config", configPath, "--facts", absent, tc.flag, tc.document)
		if code != result.ExitInvocation || diagnostic != tc.wantCode || message != tc.wantMessage {
			t.Fatalf("%s with an unreadable facts document: exit=%d %s %q", tc.flag, code, diagnostic, message)
		}
	}
	if code, diagnostic, _ := run("--pack-id", "intake", "--config", configPath, "--facts", absent); code != result.ExitIO || diagnostic != "JPS-INPUT-READ" {
		t.Fatalf("without either, the unreadable facts document is reported: exit=%d %s", code, diagnostic)
	}
	// Both given: the citations are refused, and the binding alone is
	// refused by its own sentence.
	code, diagnostic, message := run("--pack-id", "intake", "--config", configPath, "--facts", facts, "--cites", cites, "--binding", binding)
	if code != result.ExitInvocation || diagnostic != "JPS-INVOCATION-CITES" || message != citesNoTrailMessage {
		t.Fatalf("citations and a binding: exit=%d %s %q", code, diagnostic, message)
	}
	code, diagnostic, message = run("--pack-id", "intake", "--config", configPath, "--facts", facts, "--binding", binding)
	if code != result.ExitInvocation || diagnostic != "JPS-INVOCATION-BINDING" || message != bindingNoTrailMessage {
		t.Fatalf("a binding alone: exit=%d %s %q", code, diagnostic, message)
	}
}

// experimental graph evaluate refuses citations in a project that keeps no
// trail as experimental evaluate does (ADR-0051), in both formats: no node
// evaluates and nothing is created. Without citations, with an empty array,
// and on a rehearsal, the run goes ahead.
func TestGraphEvaluateRefusesCitationsWhereNoTrailIsKept(t *testing.T) {
	configPath, graphPath := graphProject(t, graphFixture(t, "onboarding.graph.json"), nil)
	inputs := writeGraphInputs(t, graphHappyInputs)
	cites := markedCites(t)
	for _, format := range []string{"json", "human"} {
		code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
			"--config", configPath, "--inputs", inputs, "--cites", cites, "--format", format}, "")
		refusedForCitesWithoutATrail(t, "graph", format, code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("nothing is created: %v", err)
	}

	empty := writeDocument(t, "empty.json", "[]")
	for _, tc := range []struct {
		label string
		extra []string
		want  string
	}{
		{"without citations", nil, `"resultNode"`},
		{"an empty array, which cites nothing", []string{"--cites", empty}, `"resultNode"`},
		{"a rehearsal with citations", []string{"--cites", cites, "--rehearsal"}, `"rehearsal":true`},
	} {
		args := append([]string{"experimental", "graph", "evaluate", graphPath, "--config", configPath,
			"--inputs", inputs, "--format", "json"}, tc.extra...)
		code, stdout, stderr := runTest(t, args, "")
		if code != 0 || stderr != "" || !strings.Contains(stdout, tc.want) {
			t.Fatalf("%s runs: exit=%d stdout=%q stderr=%q", tc.label, code, stdout, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the runs that ran record nothing either: %v", err)
	}
}

// On the graph surface the refusal comes once the configuration is read: a
// configuration that does not load is reported first, and an unreadable inputs
// document is not, since it is read after. This verb takes no binding. It reads
// the graph document before the configuration, as it always has, so an
// unreadable graph document is reported first too.
func TestGraphEvaluateRefusesCitationsWhereNoTrailIsKeptInItsPlace(t *testing.T) {
	configPath, graphPath := graphProject(t, graphFixture(t, "onboarding.graph.json"), nil)
	inputs := writeGraphInputs(t, graphHappyInputs)
	cites := markedCites(t)
	absent := filepath.Join(t.TempDir(), "absent.json")
	run := func(graph string, args ...string) (int, string, string) {
		t.Helper()
		code, stdout, _ := runTest(t, append([]string{"experimental", "graph", "evaluate", graph, "--format", "json"}, args...), "")
		diagnostic, message := operationalCode(t, stdout)
		return code, diagnostic, message
	}

	broken := writeProjectFixture(t, `{"configVersion":"2",`, map[string]string{
		"onboarding.graph.json": graphFixture(t, "onboarding.graph.json"),
	})
	_, diagnostic, message := run(filepath.Join(filepath.Dir(broken), "onboarding.graph.json"), "--config", broken, "--inputs", inputs, "--cites", cites)
	if diagnostic != "JPS-PROJECT-CONFIG-JSON" {
		t.Fatalf("citations under a broken configuration: %s %q", diagnostic, message)
	}
	code, diagnostic, message := run(graphPath, "--config", configPath, "--inputs", absent, "--cites", cites)
	if code != result.ExitInvocation || diagnostic != "JPS-INVOCATION-CITES" || message != citesNoTrailMessage {
		t.Fatalf("citations with an unreadable inputs document: exit=%d %s %q", code, diagnostic, message)
	}
	if code, diagnostic, _ := run(graphPath, "--config", configPath, "--inputs", absent); code != result.ExitIO || diagnostic != "JPS-INPUT-READ" {
		t.Fatalf("without citations, the unreadable inputs document is reported: exit=%d %s", code, diagnostic)
	}
	if code, diagnostic, _ := run(absent, "--config", configPath, "--inputs", inputs, "--cites", cites); code != result.ExitIO || diagnostic != "JPS-INPUT-READ" {
		t.Fatalf("the graph document, read before the configuration: exit=%d %s", code, diagnostic)
	}
}
