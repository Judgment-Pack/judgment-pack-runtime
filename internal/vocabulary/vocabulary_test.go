package vocabulary_test

import (
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/vocabulary"
)

func checkRow(t *testing.T, name string) result.VocabularyReport {
	t.Helper()
	data := readPack(t, "packs/"+name+".json")
	return vocabulary.Check(data, packsValidateCore(newEngine(t), data), carrier.DefaultLimits())
}

// A refusal names the outcome and the value as written, in printable ASCII,
// so a look-alike character is visible as its code point and a list reads as
// a list.
func TestARefusalNamesTheOutcomeAndTheValue(t *testing.T) {
	for _, tc := range []struct {
		row   string
		index int
		want  []string
	}{
		{"unknown-value", 0, []string{`Outcome "refer"`, `"hihg"`, "1.0.0"}},
		{"wrong-case", 0, []string{`Outcome "note"`, `"High"`}},
		{"wrong-case", 1, []string{`Outcome "decline"`, `"CRITICAL"`}},
		{"list-one", 0, []string{`Outcome "refer"`, `["high"]`, "list"}},
		{"list-two", 0, []string{`Outcome "refer"`, `["low","high"]`, "list"}},
		{"look-alike", 0, []string{`Outcome "refer"`, "\"h\\u0456gh\""}},
	} {
		got := checkRow(t, tc.row)
		if got.Result != vocabulary.ResultRefused || len(got.Findings) <= tc.index {
			t.Fatalf("%s: %+v", tc.row, got)
		}
		finding := got.Findings[tc.index]
		if finding.Code != vocabulary.ValueCode || finding.Layer != vocabulary.Layer || finding.Severity != "error" {
			t.Fatalf("%s: finding %+v", tc.row, finding)
		}
		for _, part := range tc.want {
			if !strings.Contains(finding.Message, part) {
				t.Fatalf("%s: the message %q does not name %s", tc.row, finding.Message, part)
			}
		}
		for _, character := range finding.Message {
			if character < 0x20 || character >= 0x7f {
				if character != '§' {
					t.Fatalf("%s: the message holds %U outside printable ASCII: %q", tc.row, character, finding.Message)
				}
			}
		}
	}
}

// The characters <, > and & are printable ASCII and a refusal writes them as
// themselves, so the value reads as written, not as the JSON encoder's HTML
// escapes.
func TestARefusalWritesHTMLCharactersAsThemselves(t *testing.T) {
	root := decode(t, readPack(t, "packs/unknown-value.json"))
	outcome := root["outcomes"].([]any)[2].(map[string]any)
	outcome["id"] = "refer<&>"
	outcome["extensions"] = map[string]any{vocabulary.AnnotationsName: map[string]any{"severity": "<b>high&</b>"}}
	got := vocabulary.Admitted(root)
	if got.Result != vocabulary.ResultRefused || len(got.Findings) != 1 {
		t.Fatalf("%+v", got)
	}
	message := got.Findings[0].Message
	for _, part := range []string{`Outcome "refer<&>"`, `"<b>high&</b>"`} {
		if !strings.Contains(message, part) {
			t.Fatalf("the message %q does not name %s", message, part)
		}
	}
	if strings.Contains(message, `\u00`) {
		t.Fatalf("the message %q escapes a printable character", message)
	}
}

// A pack Core refuses at the semantic layer, here a rule that names an
// outcome the pack does not define, beside a severity that is not a level,
// is not checked: the reason is Core's refusal, the layer is semantic, and
// the vocabulary reports no finding of its own.
func TestCoreRefusedAtTheSemanticLayerGetsNoVocabularyFinding(t *testing.T) {
	root := decode(t, readPack(t, "packs/unknown-value.json"))
	rule := root["rules"].([]any)[2].(map[string]any)
	rule["outcome"] = "escalate-elsewhere"
	data := encode(t, root)
	core := packsValidateCore(newEngine(t), data)
	if core == nil || core.Status == "valid" {
		t.Fatalf("Core admits the pack: %+v", core)
	}
	got := vocabulary.Check(data, core, carrier.DefaultLimits())
	if got.Result != vocabulary.ResultNotChecked || got.Reason != vocabulary.ReasonCoreRefused || got.CoreLayer != "semantic" ||
		len(got.Findings) != 0 || len(got.Advisories) != 0 {
		t.Fatalf("%+v", got)
	}
	// The same pack with its rule restored is refused for the severity, so
	// the result above is the semantic refusal's.
	rule["outcome"] = "refer"
	data = encode(t, root)
	if got := vocabulary.Check(data, packsValidateCore(newEngine(t), data), carrier.DefaultLimits()); got.Result != vocabulary.ResultRefused {
		t.Fatalf("restored: %+v", got)
	}
}

// The key anywhere but an outcome's annotations is not read, whatever it
// holds; nor is any other key on an outcome.
func TestTheKeyElsewhereIsIgnored(t *testing.T) {
	for _, name := range []string{"rule-position", "root-position", "entry-position", "other-keys"} {
		got := checkRow(t, name)
		if got.Result != vocabulary.ResultPass || len(got.Findings) != 0 || len(got.Advisories) != 0 {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	// The same value on an outcome is refused, so the pass above is the
	// position's, not the value's.
	root := decode(t, readPack(t, "packs/rule-position.json"))
	outcome := root["outcomes"].([]any)[0].(map[string]any)
	outcome["extensions"] = map[string]any{vocabulary.AnnotationsName: map[string]any{"severity": "sev1"}}
	if got := vocabulary.Admitted(root); got.Result != vocabulary.ResultRefused || len(got.Findings) != 1 || got.Findings[0].InstancePath != "/outcomes/0/extensions/org.judgmentpack.annotations/severity" {
		t.Fatalf("sev1 on an outcome: %+v", got)
	}
}

// A pack Core refuses is reported not applied, with Core's layer, and gets
// no finding of the vocabulary; Core's own diagnostic is Core's report.
func TestCoreRefusedGetsNoVocabularyFinding(t *testing.T) {
	for _, name := range []string{"core-empty-string", "core-trailing-space", "core-number", "core-empty-list"} {
		data := readPack(t, "packs/"+name+".json")
		core := packsValidateCore(newEngine(t), data)
		got := vocabulary.Check(data, core, carrier.DefaultLimits())
		if got.Result != vocabulary.ResultNotChecked || got.Reason != vocabulary.ReasonCoreRefused || got.CoreLayer != "structural" ||
			len(got.Findings) != 0 || len(got.Advisories) != 0 {
			t.Fatalf("%s: %+v", name, got)
		}
		for _, diagnostic := range core.Diagnostics {
			if strings.HasPrefix(diagnostic.Code, "JPS-VOCABULARY-") {
				t.Fatalf("%s: Core reports the vocabulary's code %s", name, diagnostic.Code)
			}
		}
	}
	// Bytes that are not JSON, and a missing selector, are Core's refusals
	// too, at the carrier and structural layers.
	engine := newEngine(t)
	for _, tc := range []struct {
		data  string
		layer string
	}{
		{`{"specVersion":`, "carrier"},
		{`{"id":"x"}`, "structural"},
		{`{"specVersion":5}`, "structural"},
	} {
		got := vocabulary.Check([]byte(tc.data), packsValidateCore(engine, []byte(tc.data)), carrier.DefaultLimits())
		if got.Result != vocabulary.ResultNotChecked || got.Reason != vocabulary.ReasonCoreRefused || got.CoreLayer != tc.layer {
			t.Fatalf("%s: %+v", tc.data, got)
		}
	}
	if got := vocabulary.Check([]byte(`{"specVersion":"0.5.0"}`), nil, carrier.DefaultLimits()); got.Result != vocabulary.ResultNotChecked || got.Reason != vocabulary.ReasonCoreRefused {
		t.Fatalf("an operational failure of Core: %+v", got)
	}
}

// Another selector is reported not applied for that reason, before Core: a
// misspelt level in a 0.3.0 pack is never refused, and a 0.2.0-draft pack
// is not read.
func TestAnotherSelectorIsNotChecked(t *testing.T) {
	for _, data := range []string{
		`{"specVersion":"0.3.0","outcomes":[{"id":"a","label":"A","extensions":{"org.judgmentpack.annotations":{"severity":"hihg"}}}]}`,
		`{"specVersion":"0.2.0-draft"}`,
	} {
		got := vocabulary.Check([]byte(data), packsValidateCore(newEngine(t), []byte(data)), carrier.DefaultLimits())
		if got.Result != vocabulary.ResultNotChecked || got.Reason != vocabulary.ReasonSpecVersion || got.CoreLayer != "" || len(got.Findings) != 0 {
			t.Fatalf("%s: %+v", data, got)
		}
	}
}

// The advisory is claimed, so it is reported exactly where §3.1's three
// conditions hold.
func TestTheIdentifierAdvisoryHoldsExactly(t *testing.T) {
	outcome := func(id string, severity any) map[string]any {
		item := map[string]any{"id": id, "label": "x"}
		if severity != nil {
			item["extensions"] = map[string]any{vocabulary.AnnotationsName: map[string]any{"severity": severity}}
		}
		return item
	}
	for _, tc := range []struct {
		id       string
		severity any
		advised  bool
	}{
		{"refer-high", "low", true},
		{"critical", "low", true},
		{"a-b-medium", "critical", true},
		{"refer-high", "high", false},
		{"high", "high", false},
		{"review-highest", "low", false},
		{"refer-lowest", "high", false},
		{"high-volume-refer", "low", false},
		{"refer-high", nil, false},
		{"refer-high", "hihg", false},
		{"refer-high", []any{"low"}, false},
		{"refer-High", "low", false},
		{"refer", "low", false},
	} {
		root := map[string]any{"specVersion": "0.5.0", "outcomes": []any{outcome("first", nil), outcome(tc.id, tc.severity)}}
		got := vocabulary.Admitted(root)
		if advised := len(got.Advisories) == 1; advised != tc.advised || len(got.Advisories) > 1 {
			t.Fatalf("%s with %v: advisories %+v", tc.id, tc.severity, got.Advisories)
		}
		if tc.advised {
			advisory := got.Advisories[0]
			if advisory.Code != vocabulary.IdentifierCode || advisory.InstancePath != "/outcomes/1/id" || advisory.Severity != "warning" || got.Result != vocabulary.ResultPass {
				t.Fatalf("%s: %+v in %+v", tc.id, advisory, got)
			}
		}
	}
}
