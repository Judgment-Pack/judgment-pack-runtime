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
  "specVersion": "0.4.0-draft",
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
