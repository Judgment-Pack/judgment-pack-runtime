package validation

import (
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
)

// A refused annotation key is named in its message in ASCII, so a look-alike
// letter or an invisible character in it reads differently from the key it
// imitates (RFC 0031 D2): a Cyrillic a, a precomposed e acute, and a
// zero-width space, each written here by hand as the message must give it.
// The pointer stays the key itself, as data.
func TestARefusedAnnotationKeyIsNamedInASCII(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	const pack = `{
  "specVersion": "0.5.0",
  "id": "https://example.com/packs/keys",
  "version": "1.0.0",
  "title": "Keys",
  "decision": {"intent": "Route", "question": "Route?"},
  "extensions": {"org.judgmentpack.annotations": {"\u0430pprove": "yes", "caf\u00e9": "yes", "tier\u200b": "yes", "team": "intake"}},
  "outcomes": [{"id": "accept", "label": "Accept"}, {"id": "refer", "label": "Refer"}],
  "rules": [{"id": "a", "description": "A", "when": {"op": "literal", "value": true}, "outcome": "accept", "onUnknown": "ignore"}]
}`
	const advice = " has an invalid shape; use lowercase words joined by single hyphens, for example \"review-tier\"."
	want := map[string]string{
		"/extensions/org.judgmentpack.annotations/\u0430pprove": `Annotation key "\u0430pprove"` + advice,
		"/extensions/org.judgmentpack.annotations/caf\u00e9":    `Annotation key "caf\u00e9"` + advice,
		"/extensions/org.judgmentpack.annotations/tier\u200b":   `Annotation key "tier\u200b"` + advice,
	}
	output, operational := engine.Validate([]byte(pack), Options{Through: "semantic", Limits: carrier.DefaultLimits()})
	if operational != nil {
		t.Fatal(operational)
	}
	if output.Status != "invalid" || len(output.Diagnostics) != len(want) {
		t.Fatalf("status %s, diagnostics %+v", output.Status, output.Diagnostics)
	}
	for _, diagnostic := range output.Diagnostics {
		if diagnostic.Code != "JPS-STRUCTURE-LOCAL-ID" || diagnostic.Message != want[diagnostic.InstancePath] {
			t.Errorf("%s at %q: %q", diagnostic.Code, diagnostic.InstancePath, diagnostic.Message)
		}
	}
}

// Core 0.5.0 puts a not beside the pack version's, a local identifier's, a
// decimal operand's and a required extension's pattern, refusing a line
// terminator for engines whose $ admits a trailing one (§4). A value both
// refuse is one finding, under the definition's code and with the message
// its pattern gives, never a second generic schema finding. Each message is
// written here by hand from the pattern messages.
func TestALineTerminatorIsRefusedOnceUnderItsDefinitionsCode(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	const localID = ` has an invalid shape; use lowercase words joined by single hyphens, for example "needs-review".`
	const packVersion = ` has an invalid shape; use MAJOR.MINOR.PATCH, for example "1.0.0".`
	for _, tc := range []struct {
		name, version, ruleID, operand, required string
		pointer, code, message                   string
	}{
		{name: "pack version, trailing line feed", version: `0.1.0\n`, pointer: "/version", code: "JPS-STRUCTURE-PACK-VERSION", message: `Pack version "0.1.0\n"` + packVersion},
		{name: "pack version, line separator", version: `0.1.0\u2028`, pointer: "/version", code: "JPS-STRUCTURE-PACK-VERSION", message: `Pack version "0.1.0\u2028"` + packVersion},
		{name: "local identifier, trailing line feed", ruleID: `literal-rule\n`, pointer: "/rules/0/id", code: "JPS-STRUCTURE-LOCAL-ID", message: `Local identifier "literal-rule\n"` + localID},
		{name: "local identifier, next line", ruleID: `literal\u0085rule`, pointer: "/rules/0/id", code: "JPS-STRUCTURE-LOCAL-ID", message: `Local identifier "literal\u0085rule"` + localID},
		{name: "decimal operand, trailing line feed", operand: `5000\n`, pointer: "/rules/1/when/value", code: "JPS-STRUCTURE-DECIMAL-OPERAND", message: `Ordered comparison operand "5000\n" must be a decimal string, for example "5000".`},
		{name: "required extension, trailing line feed", required: `com.example.review\n`, pointer: "/metadata/requiredExtensions/0", code: "JPS-STRUCTURE-EXTENSION-NAME", message: "Extension name must use an allowed reverse-domain namespace."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			version, ruleID, operand := "0.1.0", "literal-rule", "5000"
			if tc.version != "" {
				version = tc.version
			}
			if tc.ruleID != "" {
				ruleID = tc.ruleID
			}
			if tc.operand != "" {
				operand = tc.operand
			}
			metadata := ""
			if tc.required != "" {
				metadata = `, "metadata": {"requiredExtensions": ["` + tc.required + `"]}`
			}
			pack := `{
  "specVersion": "0.5.0",
  "id": "https://example.com/packs/line-terminators",
  "version": "` + version + `",
  "title": "Line terminators",
  "decision": {"intent": "Route", "question": "Route?"},
  "outcomes": [{"id": "accept", "label": "Accept"}, {"id": "reject", "label": "Reject"}],
  "rules": [
    {"id": "` + ruleID + `", "description": "A", "when": {"op": "literal", "value": true}, "outcome": "accept", "onUnknown": "ignore"},
    {"id": "large", "description": "B", "when": {"op": "fact", "path": "/amount", "operator": "greater-than", "value": "` + operand + `"}, "outcome": "reject", "onUnknown": "ignore"}
  ]` + metadata + `
}`
			output, operational := engine.Validate([]byte(pack), Options{Limits: carrier.DefaultLimits()})
			if operational != nil {
				t.Fatal(operational)
			}
			if output.Status != "invalid" || len(output.Diagnostics) != 1 {
				t.Fatalf("status %s, diagnostics %+v", output.Status, output.Diagnostics)
			}
			got := output.Diagnostics[0]
			if got.Code != tc.code || got.InstancePath != tc.pointer || got.Message != tc.message {
				t.Errorf("%s at %q: %q, want %s at %q: %q", got.Code, got.InstancePath, got.Message, tc.code, tc.pointer, tc.message)
			}
		})
	}
}

// A pack declaring 0.4.0-draft or 0.4.0 is refused as an unsupported version,
// and the refusal says where the draft went; any other unbundled version keeps
// the message alone.
func TestARetiredSelectorIsRefusedWithItsNote(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	const unbundled = "The exact JPS specification version is not bundled with this CLI."
	for version, message := range map[string]string{
		"0.4.0-draft": unbundled + ` The staged draft selector 0.4.0-draft was never released: its text was released as Core 0.5.0, and a pack written to it moves up by changing its specVersion to "0.5.0" (Core §11).`,
		"0.4.0":       unbundled + " No specVersion 0.4.0 exists: the staged draft selector 0.4.0-draft was released as Core 0.5.0 (Core §11).",
		"0.6.0":       unbundled,
	} {
		pack := `{"specVersion": "` + version + `", "id": "https://example.com/packs/retired", "version": "1.0.0", "title": "Retired", "decision": {"intent": "Route", "question": "Route?"}, "outcomes": [{"id": "accept", "label": "Accept"}], "rules": [{"id": "a", "description": "A", "when": {"op": "literal", "value": true}, "outcome": "accept", "onUnknown": "ignore"}]}`
		output, operational := engine.Validate([]byte(pack), Options{Limits: carrier.DefaultLimits()})
		if operational != nil {
			t.Fatal(operational)
		}
		if output.Status != "unsupported" || len(output.Diagnostics) != 1 || output.Diagnostics[0].Code != "JPS-CAPABILITY-SPEC-VERSION" || output.Diagnostics[0].Message != message {
			t.Errorf("%s: status %s, diagnostics %+v", version, output.Status, output.Diagnostics)
		}
	}
}
