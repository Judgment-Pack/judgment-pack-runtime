package mcp

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/conformance"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func expectationCall(t *testing.T, arguments any) map[string]any {
	t.Helper()
	response := runServer(t, message(t, 1, "tools/call", map[string]any{"name": expectationTool, "arguments": arguments}))[0]
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected tool response: %#v", response)
	}
	return result
}

type expectationFixture struct {
	Name      string
	Text      string
	Valid     bool
	Canonical string
	Message   string
}

func expectationFixtures(t *testing.T) []expectationFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []expectationFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

// expectationRows calls the tool with every text and returns one row per input.
func expectationRows(t *testing.T, texts []string, wantStatus string) []map[string]any {
	t.Helper()
	result := expectationCall(t, map[string]any{"spec_version": expectationSpec, "expectations": texts})
	if result["isError"] == true {
		t.Fatalf("invalid assertions are indexed findings, not a failed call: %#v", result)
	}
	report := result["structuredContent"].(map[string]any)
	if report["status"] != wantStatus || report["specVersion"] != expectationSpec {
		t.Fatalf("aggregate %v for %d inputs, want %q: %#v", report["status"], len(texts), wantStatus, report)
	}
	// The versioned envelope every payload this runtime writes carries, so a
	// later reshape has an outputVersion to move and a client can tell an
	// experimental payload from a stable one (VERSIONING.md, ADR-0007).
	if report["outputVersion"] != "2" || report["command"] != "mcp "+expectationTool || report["experimental"] != true {
		t.Fatalf("the report carries the versioned experimental envelope: %#v", report)
	}
	if tool := report["tool"].(map[string]any); tool["name"] != "jpack" {
		t.Fatalf("the report names the tool that wrote it: %#v", tool)
	}
	rows := report["results"].([]any)
	if len(rows) != len(texts) {
		t.Fatalf("a case was lost: %d rows for %d inputs", len(rows), len(texts))
	}
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		out[i] = row.(map[string]any)
	}
	return out
}

func TestExpectationAdmissionContract(t *testing.T) {
	fixtures := expectationFixtures(t)
	texts := make([]string, len(fixtures))
	for i, fixture := range fixtures {
		texts[i] = fixture.Text
	}
	rows := expectationRows(t, texts, "invalid")
	for i, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			row := rows[i]
			if row["index"] != float64(i) || (row["status"] == "valid") != fixture.Valid {
				t.Fatal(row)
			}
			if fixture.Valid {
				// The canonical text is what a client stores and compares, so it
				// is asserted byte for byte: member order, sorted sets and
				// de-duplication are the whole of what "canonical" promises.
				if row["canonical"] != fixture.Canonical {
					t.Fatalf("canonical is %q, want %q", row["canonical"], fixture.Canonical)
				}
				if _, present := row["code"]; present {
					t.Fatalf("a valid finding carries no code: %#v", row)
				}
				if _, present := row["message"]; present {
					t.Fatalf("a valid finding carries no message: %#v", row)
				}
				return
			}
			// An invalid finding must name the rule this fixture is named for.
			// Without that, a fixture that happens to be invalid for some other
			// reason stands in for the rule it was written to pin.
			message, ok := row["message"].(string)
			if !ok || !strings.Contains(message, fixture.Message) {
				t.Fatalf("message %q does not name %q", row["message"], fixture.Message)
			}
			if row["code"] != "JPS-EXPECTATION-INVALID" {
				t.Fatal(row)
			}
			if _, present := row["canonical"]; present {
				t.Fatalf("an invalid finding carries no canonical text: %#v", row)
			}
		})
	}
}

func TestExpectationAdmissionAggregate(t *testing.T) {
	fixtures := expectationFixtures(t)
	var valid, invalid string
	for _, fixture := range fixtures {
		if fixture.Valid && valid == "" {
			valid = fixture.Text
		}
		if !fixture.Valid && invalid == "" {
			invalid = fixture.Text
		}
	}
	// Asking only about valid inputs is a valid report, independent of any project.
	for _, row := range expectationRows(t, []string{valid, valid}, "valid") {
		if row["status"] != "valid" {
			t.Fatal(row)
		}
	}
	// "The aggregate is valid only when every finding is valid" (ADR-0035), so
	// the invalid row decides it wherever it sits in the batch.
	for _, texts := range [][]string{{invalid, valid}, {valid, invalid}} {
		rows := expectationRows(t, texts, "invalid")
		if (rows[0]["status"] == "invalid") != (texts[0] == invalid) {
			t.Fatalf("rows follow their inputs: %#v", rows)
		}
	}
}

func TestExpectationAdmissionArgumentsAndLimits(t *testing.T) {
	for _, args := range []map[string]any{
		{}, {"spec_version": "0.1.0-draft", "expectations": []string{"{}"}},
		{"spec_version": expectationSpec, "expectations": []string{}},
		{"spec_version": expectationSpec, "expectations": make([]string, maxExpectations+1)},
		{"spec_version": expectationSpec, "expectations": []any{nil}},
		{"spec_version": expectationSpec, "expectations": []any{map[string]any{}}},
		{"spec_version": expectationSpec, "expectations": []string{"{}"}, "pack": "any"},
	} {
		if result := expectationCall(t, args); result["isError"] != true {
			t.Fatalf("bad arguments accepted: %#v", result)
		}
	}
	// Which gate answers, and in what words -- not only that a bad call is
	// refused. A call wrong in two ways is answered for its shape, because the
	// shape of the expectations member is settled ahead of the version; and
	// absent or null is the empty batch the count refuses, not a wrong shape.
	// Asserting isError alone passes for either answer, so the order and the
	// two arms of that shape gate are held here by their text.
	for _, refusal := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"a string batch under a wrong version", map[string]any{"spec_version": "0.1.0-draft", "expectations": "nope"},
			"Expected spec_version and an array of expectation JSON strings."},
		{"an object batch under a wrong version", map[string]any{"spec_version": "0.1.0-draft", "expectations": map[string]any{}},
			"Expected spec_version and an array of expectation JSON strings."},
		{"a null batch", map[string]any{"spec_version": expectationSpec, "expectations": nil},
			"An expectation validation call must carry 1–256 expectations."},
		{"no batch at all", map[string]any{"spec_version": expectationSpec},
			"An expectation validation call must carry 1–256 expectations."},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			result := expectationCall(t, refusal.args)
			if result["isError"] != true {
				t.Fatalf("bad arguments accepted: %#v", result)
			}
			if text := result["content"].([]any)[0].(map[string]any)["text"].(string); text != refusal.want {
				t.Fatalf("refused with %q, want %q", text, refusal.want)
			}
		})
	}
	// Both sides of every documented bound, written as the literals the ADR, the
	// tool description and docs/mcp-clients.md state. A bound derived from the
	// constant it guards moves with a typo; these do not.
	padded := func(size int) string {
		text := `{"kind":"outcome","outcomeId":"allow","reasons":[],"handoff":{"state":"none"}}`
		return text[:len(text)-1] + strings.Repeat(" ", size-len(text)) + "}"
	}
	nested := func(depth int) string { return strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth) }
	elements := func(count int) string { return "[" + strings.Repeat("0,", count-1) + "0]" }
	outcomeOf := func(size int) string {
		return `{"kind":"outcome","outcomeId":"` + strings.Repeat("x", size) + `","reasons":[],"handoff":{"state":"none"}}`
	}
	// "admitted" means decoded and judged as a disposition, whatever the verdict:
	// a padded disposition and an 8 KiB outcome id are legal §8.3 values, while a
	// bare nested array is admitted and then refused as a disposition. Only the
	// oversized inputs are refused for their size.
	for _, bound := range []struct {
		name  string
		text  string
		admit bool
		valid bool
	}{
		{"16 KiB", padded(16384), true, true},
		{"16 KiB and one byte", padded(16385), false, false},
		{"depth 16", nested(16), true, false},
		{"depth 17", nested(17), false, false},
		{"1,024 nodes", elements(1023), true, false},
		{"1,025 nodes", elements(1024), false, false},
		{"an 8 KiB string", outcomeOf(8192), true, true},
		{"an 8 KiB string and one byte", outcomeOf(8193), false, false},
	} {
		t.Run(bound.name, func(t *testing.T) {
			aggregate := "invalid"
			if bound.valid {
				aggregate = "valid"
			}
			row := expectationRows(t, []string{bound.text}, aggregate)[0]
			if limited := row["code"] == "JPS-EXPECTATION-LIMIT"; limited == bound.admit {
				t.Fatalf("admitted=%v at the documented bound: %#v", bound.admit, row)
			}
		})
	}
	// A full batch is carried, and one more is refused as a whole call.
	texts := make([]string, maxExpectations)
	for i := range texts {
		texts[i] = `{"kind":"outcome","outcomeId":"allow","reasons":[],"handoff":{"state":"none"}}`
	}
	if rows := expectationRows(t, texts, "valid"); len(rows) != 256 {
		t.Fatalf("a 256-expectation call is carried: %d", len(rows))
	}
}

// TestExpectationBatchIsCountedBeforeItIsHeld pins what the refusal of an
// over-large batch costs, not only how it is worded. One frame can carry
// millions of short elements, so a bound read off a materialized array makes
// the call that is refused outright the most expensive one the server answers.
// The measure is allocations rather than bytes or wall time, because the count
// of allocations does not move with the machine: an implementation that holds
// the array pays one allocation per element sent, and one that counts first
// pays for the 256 it is allowed to hold.
func TestExpectationBatchIsCountedBeforeItIsHeld(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(engine, conformance.NewRunner(engine))
	const elements = 1 << 20
	var call strings.Builder
	call.WriteString(`{"spec_version":"` + expectationSpec + `","expectations":[0`)
	for i := 1; i < elements; i++ {
		call.WriteString(",0")
	}
	call.WriteString("]}")
	arguments := json.RawMessage(call.String())

	var answer any
	allocations := testing.AllocsPerRun(1, func() { answer = server.toolValidateExpectations(arguments) })

	// The refusal itself is unchanged: same message, and still a tool error and
	// not an indexed finding. Nothing past the count is decided, so the
	// elements are never held to being strings.
	result, ok := answer.(map[string]any)
	if !ok || result["isError"] != true {
		t.Fatalf("an over-large batch is a tool error: %#v", answer)
	}
	if text := result["content"].([]map[string]any)[0]["text"]; text != "An expectation validation call must carry 1–256 expectations." {
		t.Fatalf("the count refusal reads %q", text)
	}
	// Far above what counting to 257 costs (about 1,100 here, most of it the
	// 256 elements the bound does allow) and far below one allocation per
	// element sent (1,048,640 when the array is built first): only an
	// implementation that holds what it refuses reaches this number.
	if allocations > 8192 {
		t.Fatalf("refusing %d elements took %.0f allocations; the count is read before the batch is held", elements, allocations)
	}
}

// TestExpectationToolIsAdvertisedAsItBehaves holds the advertised schema to the
// arguments the tool actually accepts: a model that builds a call from the
// schema must not be told a batch may be larger, or a member optional, than the
// tool admits.
func TestExpectationToolIsAdvertisedAsItBehaves(t *testing.T) {
	schema := expectationToolDefinition()["inputSchema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Fatal(schema)
	}
	required := schema["required"].([]string)
	if len(required) != 2 || required[0] != "spec_version" || required[1] != "expectations" {
		t.Fatal(required)
	}
	properties := schema["properties"].(map[string]any)
	version := properties["spec_version"].(map[string]any)
	if enum := version["enum"].([]string); len(enum) != 1 || enum[0] != expectationSpec {
		t.Fatal(version)
	}
	expectations := properties["expectations"].(map[string]any)
	if expectations["minItems"] != 1 || expectations["maxItems"] != maxExpectations {
		t.Fatal(expectations)
	}
	if items := expectations["items"].(map[string]any); items["type"] != "string" {
		t.Fatal(items)
	}
	description := expectationToolDefinition()["description"].(string)
	for _, phrase := range []string{"EXPERIMENTAL SURFACE (ADR-0035)", "without compatibility promise", "necessary, not sufficient"} {
		if !strings.Contains(description, phrase) {
			t.Fatalf("the description states %q", phrase)
		}
	}
}
