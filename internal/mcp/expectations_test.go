package mcp

import (
	"encoding/json"
	"fmt"
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
	// Code is the finding code an invalid fixture must be reported under, so a
	// fixture written for §8's step order cannot pass by being refused as a §8.3
	// grammar defect, or the reverse. It defaults to JPS-EXPECTATION-INVALID,
	// which is what every fixture that does not state one means.
	Code    string
	Message string
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
	for i, fixture := range fixtures {
		if fixture.Valid {
			if fixture.Code != "" {
				t.Fatalf("%s: a valid fixture carries no code", fixture.Name)
			}
			continue
		}
		if fixture.Code == "" {
			fixtures[i].Code = "JPS-EXPECTATION-INVALID"
		}
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
			// And under the code its class is reported by: a §8.3 grammar defect
			// under JPS-EXPECTATION-INVALID, a disposition §8's step order or §5's
			// grammar puts beyond every pack under JPS-EXPECTATION-UNREACHABLE.
			// The two are told apart by the code alone (ADR-0035, ADR-0036), so a
			// fixture that swapped them would otherwise pass.
			if row["code"] != fixture.Code {
				t.Fatalf("code is %v, want %q: %#v", row["code"], fixture.Code, row)
			}
			// Including an unreachable one: a client must not store the canonical
			// text of an expectation no pack can produce.
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

// TestUnreachableExpectationIsAnInvalidFindingOfItsOwn holds the wire shape of
// the one finding ADR-0036 adds, on its own rather than beside the fixtures of
// every other rule: a legal §8.3 disposition that §8's step order puts beyond
// every pack decides the aggregate the way any other non-valid row does, is
// reported under its own code, and carries no canonical text. Asserting this
// inside the whole-fixture batch would prove none of it, because that batch is
// already invalid for two dozen other reasons.
func TestUnreachableExpectationIsAnInvalidFindingOfItsOwn(t *testing.T) {
	const unreachable = `{"kind":"unresolved","reasons":["not-applicable"],"handoff":{"state":"none"}}`
	row := expectationRows(t, []string{unreachable}, "invalid")[0]
	if row["status"] != "invalid" || row["code"] != "JPS-EXPECTATION-UNREACHABLE" {
		t.Fatalf("an unreachable expectation is an invalid finding under its own code: %#v", row)
	}
	message, ok := row["message"].(string)
	if !ok || !strings.Contains(message, "§8 step 1") {
		t.Fatalf("the finding names the step that rules the shape out: %#v", row)
	}
	// The whole point of the code: the client must change the expectation, not
	// the candidate, so it must not keep the text as something to compare.
	if _, present := row["canonical"]; present {
		t.Fatalf("an unreachable expectation is not text a client stores: %#v", row)
	}
	// And the row is not valid by another name: a batch holding one decides the
	// aggregate exactly as ADR-0035's rule says any non-valid row does.
	valid := `{"kind":"outcome","outcomeId":"allow","reasons":[],"handoff":{"state":"none"}}`
	for _, texts := range [][]string{{unreachable, valid}, {valid, unreachable}} {
		rows := expectationRows(t, texts, "invalid")
		if (rows[0]["status"] == "invalid") != (texts[0] == unreachable) {
			t.Fatalf("rows follow their inputs: %#v", rows)
		}
	}
}

// TestUnreachableOutcomeIDEchoIsBoundedAtItsWorstCase measures the bound the §5
// message's echo has as a DECODED message, and pins the identifier admission
// that bounds it. The message is not the payload: a finding is serialized twice
// on its way out, each time under different escaping, and each of those paths
// has a worst identifier of its own — all three figures are measured by
// TestUnreachableFindingIsBoundedOnEachSerializationPath beside this.
//
// The worst case for the decoded message is not the longest identifier but the
// most escaped one, and the two limits it has to pass are measured on different
// bytes. carrier.Limits{MaxStringBytes: 8192} is measured on the DECODED string
// — internal/carrier/decode.go compares len(typed), the Go string after JSON
// unescaping, against it — while the 16 KiB input bound is measured on the JSON
// text. U+007F is the character that satisfies both at the largest expansion
// %q can reach: one byte decoded, one byte of JSON text, and four bytes under
// %q (a backslash, x, 7 and f). So 8,192 of them is the largest identifier
// admitted — at the 8 KiB string limit rather than over it, in 8,265 bytes of
// text. A backslash or a double quote expands only two bytes under %q and costs
// two bytes of JSON text, so 8,192 of either would exceed 16 KiB before reaching
// the string limit; an escaped control character reaches four bytes under %q but
// costs six bytes of JSON text; and a non-printable multi-byte rune such as
// U+0085 writes six bytes under %q for two decoded bytes, so it reaches the
// string limit in half as many runes and 24,576 bytes of %q.
//
// It is only the worst case for %q, though, and %q is not the last writer to
// touch this string: a character %q leaves printable can still be escaped by
// encoding/json, and U+003C is escaped to six bytes there. So this test bounds
// the message and the sibling bounds the payload.
func TestUnreachableOutcomeIDEchoIsBoundedAtItsWorstCase(t *testing.T) {
	const characters = 8192
	identifier := strings.Repeat("", characters)
	text := `{"kind":"outcome","outcomeId":"` + identifier + `","reasons":[],"handoff":{"state":"none"}}`
	if len(text) != 8265 {
		t.Fatalf("the expectation text is %d bytes, want 8265", len(text))
	}
	row := expectationRows(t, []string{text}, "invalid")[0]
	if row["code"] != "JPS-EXPECTATION-UNREACHABLE" {
		t.Fatalf("an identifier outside §5's grammar is unreachable however it is spelled: %#v", row)
	}
	// One character more is refused for the string limit, in 8,266 bytes of text
	// that the 16 KiB bound admits: that is what makes 8,192 the largest
	// identifier this message can ever have to quote.
	over := `{"kind":"outcome","outcomeId":"` + identifier + "" + `","reasons":[],"handoff":{"state":"none"}}`
	if refused := expectationRows(t, []string{over}, "invalid")[0]; refused["code"] != "JPS-EXPECTATION-LIMIT" {
		t.Fatalf("8,193 decoded bytes is over the 8 KiB string limit: %#v", refused)
	}
	message, ok := row["message"].(string)
	if !ok {
		t.Fatalf("the finding names the rule that refused it: %#v", row)
	}
	// Derived from the template rather than guessed: the sentence with the verb
	// removed, plus the two quotation marks %q adds, plus four bytes for each of
	// the 8,192 characters it escapes.
	if want := len(unreachableOutcomeIDRule) - len("%q") + 2 + 4*characters; len(message) != want {
		t.Fatalf("the decoded message is %d bytes; the template gives %d", len(message), want)
	}
	// And against the figure ADR-0036 states, which the derivation above cannot
	// hold on its own: it moves with the sentence, and the documented number does
	// not. 32,936 bytes is over 32,768 and under 33 KiB.
	if len(message) != 32936 {
		t.Fatalf("the documented decoded bound is 32,936 bytes; this message is %d", len(message))
	}
	// 32 KiB is the wrong number to size a limit against in both directions:
	// this message is over it, and the payload that carries the message is over
	// this message. The sibling test measures the payload.
	if row["status"] != "invalid" || row["index"] != float64(0) {
		t.Fatalf("the finding measured here is the row the tool returned: %#v", row)
	}
	if _, present := row["canonical"]; present {
		t.Fatalf("an unreachable expectation is not text a client stores: %#v", row)
	}
}

// findingSize is one measured figure of the sibling test's table: the size of
// one unreachable finding as each of the two serialization paths writes it, at
// one index.
type findingSize struct {
	index int
	// structured is the finding inside structuredContent, which the server's
	// own encoder writes with HTML escaping OFF (internal/mcp/server.go).
	structured int
	// text is the finding inside content[0].text, which jsonText writes with
	// json.Marshal -- HTML escaping ON, so U+003C, U+003E and U+0026 each
	// become a six-byte escape there and nowhere else.
	text int
}

// TestUnreachableFindingIsBoundedOnEachSerializationPath measures the size the
// §5 echo actually reaches on the wire, on each path that carries it and at the
// largest index a batch can report. The decoded message is not the bound a
// client sizes a limit from, and neither path's bound is the other's: toolResult
// writes the report twice, and the two writers escape different characters, so
// the worst identifier for one is not the worst identifier for the other.
//
//   - structuredContent is written by the server's json.Encoder, which is
//     configured SetEscapeHTML(false). It escapes what JSON must: the
//     backslashes %q introduced, and the quotation marks. The worst identifier
//     is therefore the worst one for %q, U+007F, at five bytes per character.
//   - content[0].text is jsonText(structured), which is json.Marshal with HTML
//     escaping ON. There U+003C costs six bytes -- one byte decoded, one byte of
//     input text, left printable by %q, then escaped to a six-byte sequence --
//     which is more than U+007F's five, so the worst identifier flips.
//
// Both figures move with the index, because the index sits inside the finding
// whose bytes are being counted, and 255 is the largest a 256-expectation batch
// can report. Each figure is asserted twice: once derived from the message and
// the escaping rule, so a change of escaping fails it, and once against the
// literal ADR-0036 documents, so rewording the sentence fails it. Each is also
// held to being the bytes the server actually wrote, by locating the finding
// inside the payload rather than re-serializing it here: a figure measured on a
// finding this test built itself would be a figure about this test.
func TestUnreachableFindingIsBoundedOnEachSerializationPath(t *testing.T) {
	const characters = 8192
	const filler = `{"kind":"outcome","outcomeId":"allow","reasons":[],"handoff":{"state":"none"}}`
	// The largest index a call can report, where the finding's own envelope is
	// two bytes longer than at index 0.
	const lastIndex = maxExpectations - 1
	for _, worst := range []struct {
		name string
		// character is repeated 8,192 times, which is the largest identifier
		// the carrier admits for either of them: one decoded byte and one byte
		// of input text each.
		character string
		// quoted is the bytes %q writes for one such character.
		quoted int
		// decoded is the message, which carries no index and so has one figure.
		decoded int
		sizes   []findingSize
		// block is the whole content[0].text string as the client receives it,
		// for the one-expectation call: the text block is itself escaped into
		// the response, so every backslash in it is doubled again.
		block int
	}{
		{name: "U+007F", character: "\x7f", quoted: 4, decoded: 32936, block: 57848,
			sizes: []findingSize{{index: 0, structured: 41212, text: 41212},
				{index: lastIndex, structured: 41214, text: 41214}}},
		{name: "U+003C", character: "<", quoted: 1, decoded: 8360, block: 57848,
			sizes: []findingSize{{index: 0, structured: 8444, text: 49404},
				{index: lastIndex, structured: 8446, text: 49406}}},
	} {
		t.Run(worst.name, func(t *testing.T) {
			identifier := strings.Repeat(worst.character, characters)
			text := `{"kind":"outcome","outcomeId":"` + identifier + `","reasons":[],"handoff":{"state":"none"}}`
			if len(text) != 8265 {
				t.Fatalf("the expectation text is %d bytes, want 8265", len(text))
			}
			for _, size := range worst.sizes {
				// The batch is padded with a valid expectation so the measured
				// finding lands at the index this case is about.
				texts := make([]string, size.index+1)
				for i := range texts {
					texts[i] = filler
				}
				texts[size.index] = text
				line := expectationWireLine(t, texts)
				var response map[string]any
				if err := json.Unmarshal([]byte(line), &response); err != nil {
					t.Fatalf("undecodable response line: %v", err)
				}
				answer := response["result"].(map[string]any)
				report := answer["structuredContent"].(map[string]any)
				row := report["results"].([]any)[size.index].(map[string]any)
				// Reported compactly rather than with the row: the row is
				// 40 KiB of escaped identifier, and a failure here is about
				// two members of it.
				if row["code"] != "JPS-EXPECTATION-UNREACHABLE" || row["index"] != float64(size.index) {
					t.Fatalf("the finding measured here is the row the tool returned: code %v at index %v, want index %d", row["code"], row["index"], size.index)
				}
				if _, present := row["canonical"]; present {
					t.Fatalf("an unreachable expectation is not text a client stores: index %v carries canonical", row["index"])
				}
				message := row["message"].(string)
				// The message carries no brace, so the spans located below end
				// at the finding's own closing brace and nowhere earlier.
				if strings.ContainsAny(message, "{}") {
					t.Fatalf("the message carries a brace, so a finding's span cannot be located by one: %q", message[:64])
				}
				if want := len(unreachableOutcomeIDRule) - len("%q") + 2 + worst.quoted*characters; len(message) != want {
					t.Errorf("index %d: the decoded message is %d bytes; the template gives %d", size.index, len(message), want)
				}
				if len(message) != worst.decoded {
					t.Errorf("index %d: the documented decoded message is %d bytes; this one is %d", size.index, worst.decoded, len(message))
				}

				envelope := len(fmt.Sprintf(`{"index":%d,"status":"invalid","code":"JPS-EXPECTATION-UNREACHABLE","message":""}`, size.index))
				// What JSON has to escape whatever it is configured for: the
				// backslashes %q introduced, one byte each, and the quotation
				// marks, the two the sentence carries and the two %q added.
				mandatory := strings.Count(message, `\`) + strings.Count(message, `"`)
				// And what only HTML escaping adds: five more bytes for each
				// character it rewrites as a six-byte escape.
				htmlEscaped := 5 * (strings.Count(message, "<") + strings.Count(message, ">") + strings.Count(message, "&"))

				structured := findingSpan(t, line, size.index)
				if want := envelope + len(message) + mandatory; len(structured) != want {
					t.Errorf("index %d: the finding in structuredContent is %d bytes; the envelope and JSON's own escaping give %d", size.index, len(structured), want)
				}
				if len(structured) != size.structured {
					t.Errorf("index %d: the documented finding in structuredContent is %d bytes; this one is %d", size.index, size.structured, len(structured))
				}

				block := answer["content"].([]any)[0].(map[string]any)["text"].(string)
				inText := findingSpan(t, block, size.index)
				if want := envelope + len(message) + mandatory + htmlEscaped; len(inText) != want {
					t.Errorf("index %d: the finding in content[0].text is %d bytes; the envelope with HTML escaping gives %d", size.index, len(inText), want)
				}
				if len(inText) != size.text {
					t.Errorf("index %d: the documented finding in content[0].text is %d bytes; this one is %d", size.index, size.text, len(inText))
				}

				// The text block is a JSON string in the response, so what the
				// client reads off the wire escapes those escapes once more. The
				// block carries every row, so this figure belongs to the
				// one-expectation call: at index 255 it also carries 255 others.
				if size.index == 0 {
					if literal := blockLiteral(t, line); len(literal) != worst.block {
						t.Errorf("the whole text block on the wire is %d bytes; the documented figure is %d", len(literal), worst.block)
					}
				}
			}
		})
	}
}

// expectationWireLine makes one expectation call and returns the response line
// as the server wrote it.
func expectationWireLine(t *testing.T, texts []string) string {
	t.Helper()
	lines := serveLines(t, message(t, 1, "tools/call", map[string]any{"name": expectationTool,
		"arguments": map[string]any{"spec_version": expectationSpec, "expectations": texts}}))
	if len(lines) != 1 {
		t.Fatalf("one call is one response line: %d", len(lines))
	}
	return lines[0]
}

// findingSpan returns the unreachable finding carrying index, as the bytes
// payload holds it in: from its own opening brace to the first closing brace
// after it. The caller has checked that the message carries no brace, so the
// span is the finding and the whole finding. Locating it rather than
// re-serializing it is the point: the bytes measured are the writer's.
func findingSpan(t *testing.T, payload string, index int) string {
	t.Helper()
	prefix := fmt.Sprintf(`{"index":%d,"status":"invalid","code":"JPS-EXPECTATION-UNREACHABLE","message":"`, index)
	start := strings.Index(payload, prefix)
	if start < 0 {
		t.Fatalf("no unreachable finding at index %d in %d bytes of payload", index, len(payload))
	}
	end := strings.Index(payload[start:], "}")
	if end < 0 {
		t.Fatalf("the finding at index %d is not closed", index)
	}
	return payload[start : start+end+1]
}

// blockLiteral returns the content[0].text member of a tool response as the
// JSON string literal the server wrote, quotation marks and all.
func blockLiteral(t *testing.T, line string) string {
	t.Helper()
	const opener = `"content":[{"text":`
	const closer = `,"type":"text"}]`
	start := strings.Index(line, opener)
	end := strings.Index(line, closer)
	if start < 0 || end < start {
		t.Fatalf("no text block in %d bytes of response", len(line))
	}
	return line[start+len(opener) : end]
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
	for _, phrase := range []string{"EXPERIMENTAL SURFACE (ADR-0035)", "without compatibility promise", "necessary, not sufficient",
		// A code a client is told to branch on has to be in the text the client
		// reads, beside what it means and beside each class it is reported for.
		"JPS-EXPECTATION-UNREACHABLE",
		"an unresolved result retaining not-applicable", "no-match beside another reason",
		"the expectation and not the candidate is what must change",
		"pack-dependent reachability is not checked",
		// And the order, as a whole sentence and with the code it decides. The
		// behaviour is pinned by the malformed-and-unreachable fixture; what is
		// pinned here is that the advertised text says the same thing. Holding
		// only a prefix such as "Pack-independent reachability is checked" leaves
		// a description that states the reverse order — the one case where a
		// client cannot recover the answer from the code it receives — passing.
		"Pack-independent reachability is checked after that grammar gate and only when the grammar gate passes",
		"an input that is both malformed and unreachable is reported for its grammar defect, under JPS-EXPECTATION-INVALID"} {
		if !strings.Contains(description, phrase) {
			t.Fatalf("the description states %q", phrase)
		}
	}
	// The third class is held to the grammar the tool actually applies rather
	// than to a copy of it: respelling localIdentifier without respelling the
	// description fails here, which a phrase list of its own could not catch.
	if !strings.Contains(description, localIdentifier.String()) {
		t.Fatalf("the description states the grammar the tool applies, %q", localIdentifier.String())
	}
	// Every code the tool actually emits is named in the text a client reads, in
	// the spelling it is emitted in. A description held only to phrases it happens
	// to contain drifts the moment a code beside it is added or respelled; this
	// reads the codes off the wire instead, over the whole fixture corpus, so it
	// stays a description-to-behaviour check and not a second copy of the fixture
	// table.
	fixtures := expectationFixtures(t)
	texts := make([]string, len(fixtures))
	for i, fixture := range fixtures {
		texts[i] = fixture.Text
	}
	emitted := map[string]bool{}
	for _, row := range expectationRows(t, texts, "invalid") {
		if code, ok := row["code"].(string); ok {
			emitted[code] = true
		}
	}
	if len(emitted) < 2 {
		t.Fatalf("the corpus emits too few codes to hold the description to any: %v", emitted)
	}
	for code := range emitted {
		if !strings.Contains(description, code) {
			t.Fatalf("the tool emits %q and the description does not name it", code)
		}
	}
}
