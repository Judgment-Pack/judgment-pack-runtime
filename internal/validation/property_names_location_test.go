package validation

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The schema engine stores its live path buffer in a propertyNames failure,
// and a member visited after it, in map order, overwrites that buffer. A
// refused extension name in the decision's, the escalation's or the metadata's
// extensions (a refused character, the reserved prefix, or the annotations
// name where it is not admitted) could then read as a failure at the root annotations object when
// the root's extensions were visited later, and be reported a second time, as
// a generic schema finding at the document root. The name is refused once, at
// itself, on every run: each pack is validated many times in one process so
// that the map orders that reach the overwrite occur.
func TestARefusedExtensionNameBesideRootAnnotationsIsReportedOnceOnEveryRun(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	const message = "Extension name must use an allowed reverse-domain namespace."
	for _, position := range []string{"decision", "escalation", "metadata"} {
		for _, name := range []string{"com.example.Review", "com.example.review\u2028", "org.judgmentpack.review", "org.judgmentpack.annotations"} {
			t.Run(position+" "+name, func(t *testing.T) {
				pack := []byte(fmt.Sprintf(`{
  "specVersion": "0.5.0",
  "id": "https://example.com/packs/names",
  "version": "1.0.0",
  "title": "Names",
  "decision": {"intent": "Route", "question": "Route?"%[1]s},
  "escalation": {"triggers": ["no-match"], "target": {"kind": "queue", "name": "Review"}%[2]s},
  "metadata": {"license": "Apache-2.0"%[3]s},
  "extensions": {"org.judgmentpack.annotations": {"team": "intake"}},
  "outcomes": [{"id": "accept", "label": "Accept"}, {"id": "refer", "label": "Refer"}],
  "rules": [{"id": "a", "description": "A", "when": {"op": "literal", "value": true}, "outcome": "accept", "onUnknown": "ignore"}]
}`, member(position == "decision", name), member(position == "escalation", name), member(position == "metadata", name)))
				want := []result.Diagnostic{result.ErrorDiagnostic("JPS-STRUCTURE-EXTENSION-NAME", "structural", "/"+position+"/extensions/"+name, message)}
				for run := 0; run < 200; run++ {
					output, operational := engine.Validate(pack, Options{Through: "semantic", Limits: carrier.DefaultLimits()})
					if operational != nil {
						t.Fatal(operational)
					}
					if output.Status != "invalid" || !reflect.DeepEqual(output.Diagnostics, want) {
						t.Fatalf("run %d: status %s, diagnostics %+v", run, output.Status, output.Diagnostics)
					}
				}
			})
		}
	}
}

// The same packs under Core 0.3.0, whose collector has no annotations branch:
// the location a propertyNames failure carries is never read there, so the
// output is the one refusal at the name on every run. Under 0.3.0 the
// annotations name is an ordinary reserved name, and the root's extensions
// hold a permitted one.
func TestARefusedExtensionNameUnderCore030IsReportedOnceOnEveryRun(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	const message = "Extension name must use an allowed reverse-domain namespace."
	for _, position := range []string{"decision", "escalation", "metadata"} {
		for _, name := range []string{"com.example.Review", "com.example.review\u2028", "org.judgmentpack.review", "org.judgmentpack.annotations"} {
			t.Run(position+" "+name, func(t *testing.T) {
				pack := []byte(fmt.Sprintf(`{
  "specVersion": "0.3.0",
  "id": "https://example.com/packs/names",
  "version": "1.0.0",
  "title": "Names",
  "decision": {"intent": "Route", "question": "Route?"%[1]s},
  "escalation": {"triggers": ["no-match"], "target": {"kind": "queue", "name": "Review"}%[2]s},
  "metadata": {"license": "Apache-2.0"%[3]s},
  "extensions": {"com.example.root": {"team": "intake"}},
  "outcomes": [{"id": "accept", "label": "Accept"}, {"id": "refer", "label": "Refer"}],
  "rules": [{"id": "a", "description": "A", "when": {"op": "literal", "value": true}, "outcome": "accept", "onUnknown": "ignore"}]
}`, member(position == "decision", name), member(position == "escalation", name), member(position == "metadata", name)))
				want := []result.Diagnostic{result.ErrorDiagnostic("JPS-STRUCTURE-EXTENSION-NAME", "structural", "/"+position+"/extensions/"+name, message)}
				for run := 0; run < 200; run++ {
					output, operational := engine.Validate(pack, Options{Through: "semantic", Limits: carrier.DefaultLimits()})
					if operational != nil {
						t.Fatal(operational)
					}
					if output.Status != "invalid" || !reflect.DeepEqual(output.Diagnostics, want) {
						t.Fatalf("run %d: status %s, diagnostics %+v", run, output.Status, output.Diagnostics)
					}
				}
			})
		}
	}
}

// member writes an extensions member holding name, or nothing.
func member(present bool, name string) string {
	if !present {
		return ""
	}
	return fmt.Sprintf(`, "extensions": {%q: {}}`, name)
}
