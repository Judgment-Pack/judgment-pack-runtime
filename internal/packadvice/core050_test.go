package packadvice

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// The fixtures under testdata/core050 are written by hand: pack-0.5.0.json
// declares value sets (one never referenced, one entry described and
// annotated) referenced from the applicability, from rules and from an
// exception, and annotations on the root, an outcome and two rules;
// pack-0.3.0.json is its evaluation form (Core §8.5) written out as a Core
// 0.3.0 pack -- each reference replaced by its inline array, the sets and the
// annotations removed, every other member and order kept. The expected
// effects below are read off the domain by hand: gold/eu meets two accept rules
// (agreement, and each is covered by the other), bronze/eu meets no rule
// (fallback), us escalates, "other" is outside the applicability, and the
// contradictory rule never enters a candidate set and is the one condition the
// finite fragment does not decide.
var core050Effects = []string{"analysis-incomplete", "compatible-agreement", "direct-escalation", "fallback-reachable", "never-contributes", "outcome-redundancy"}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/core050/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// sameAnalysis compares two reports with the two members that name the pack
// rather than describe the analysis set aside, after checking each of them.
func sameAnalysis(t *testing.T, got, want map[string]any, gotBytes []byte, gotVersion string) {
	t.Helper()
	if got["targetSpecVersion"] != gotVersion || got["packDigest"] != digest(gotBytes) {
		t.Fatal("identity:", got["targetSpecVersion"], got["packDigest"])
	}
	if !reflect.DeepEqual(withoutIdentity(got), withoutIdentity(want)) {
		t.Fatalf("analysis differs:\n%s\n%s", bytesOf(got), bytesOf(want))
	}
}

func withoutIdentity(r map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range r {
		if k != "targetSpecVersion" && k != "packDigest" {
			out[k] = v
		}
	}
	return out
}

func TestPackAdviceCore050IsItsEvaluationForm(t *testing.T) {
	e := engine(t)
	p050, p030, d := fixture(t, "pack-0.5.0.json"), fixture(t, "pack-0.3.0.json"), fixture(t, "domain.json")
	twin, f := Analyze(e, p030, d, DefaultOptions())
	if f != nil {
		t.Fatal(f)
	}
	validateReport(t, twin)
	if got := effects(twin); !reflect.DeepEqual(got, core050Effects) {
		t.Fatal("twin:", got)
	}
	got, f := Analyze(e, p050, d, DefaultOptions())
	if f != nil {
		t.Fatal(f)
	}
	validateReport(t, got)
	sameAnalysis(t, got, twin, p050, "0.5.0")
	if twin["targetSpecVersion"] != "0.3.0" || twin["packDigest"] != digest(p030) {
		t.Fatal("twin identity:", twin["targetSpecVersion"], twin["packDigest"])
	}
	// Each source pointer names an entity of P and of EF(P) alike; none points
	// into the value sets, which EF(P) does not have.
	if strings.Contains(string(bytesOf(got)), "/valueSets") {
		t.Fatal("a source points into the value sets:", string(bytesOf(got)))
	}
	// A pack that only changes its selector to 0.5.0 is its own evaluation form.
	plain := pack(rule("r", "a", literal(true)))
	plain030 := bytesOf(plain)
	plain["specVersion"] = "0.5.0"
	plain050 := bytesOf(plain)
	v := domainOf(input(nil))
	want, f := Analyze(e, plain030, v, DefaultOptions())
	if f != nil {
		t.Fatal(f)
	}
	again, f := Analyze(e, plain050, v, DefaultOptions())
	if f != nil {
		t.Fatal(f)
	}
	sameAnalysis(t, again, want, plain050, "0.5.0")
}

// Annotations are inert (Core §8.5), and so are the descriptions EF(P) drops:
// changing, removing or adding them changes the pack's bytes, so its digest,
// and nothing else in the report.
func TestPackAdviceCore050AnnotationsAndSetDescriptionsAreInert(t *testing.T) {
	e := engine(t)
	p050, d := fixture(t, "pack-0.5.0.json"), fixture(t, "domain.json")
	base, f := Analyze(e, p050, d, DefaultOptions())
	if f != nil {
		t.Fatal(f)
	}
	for name, edits := range map[string][][2]string{
		"changed":   {{`"category": "core"}, "com.example.note"`, `"category": "other"}, "com.example.note"`}, {`"note": "second"`, `"note": ["first", "second"]`}, {`"owner": "intake"`, `"owner": "review"`}},
		"removed":   {{`"extensions": {"org.judgmentpack.annotations": {"category": ["core", "duplicate"]}}`, `"extensions": {}`}, {`, "extensions": {"org.judgmentpack.annotations": {"category": "core"}}}`, `}`}},
		"added":     {{`{"id": "refer", "label": "Refer"}`, `{"id": "refer", "label": "Refer", "extensions": {"org.judgmentpack.annotations": {"category": "new"}}}`}},
		"described": {{`"description": "Request kinds in scope."`, `"description": "Other words."`}, {`"description": "A renewal."`, `"description": "Renewed."`}, {`"id": "tiers",`, `"id": "tiers", "description": "Tiers.",`}},
	} {
		changed := string(p050)
		for _, edit := range edits {
			if !strings.Contains(changed, edit[0]) {
				t.Fatal(name, "fixture lacks", edit[0])
			}
			changed = strings.Replace(changed, edit[0], edit[1], 1)
		}
		got, f := Analyze(e, []byte(changed), d, DefaultOptions())
		if f != nil {
			t.Fatal(name, f)
		}
		if got["packDigest"] == base["packDigest"] {
			t.Fatal(name, "the edit did not change the bytes")
		}
		sameAnalysis(t, got, base, []byte(changed), "0.5.0")
	}
	// A set member is not inert: it is the operand, so changing one changes the
	// analysis and not only the digest.
	changed := strings.Replace(string(p050), `{"value": "silver"}`, `{"value": "bronze"}`, 1)
	got, f := Analyze(e, []byte(changed), d, DefaultOptions())
	if f != nil {
		t.Fatal(f)
	}
	if reflect.DeepEqual(withoutIdentity(got), withoutIdentity(base)) {
		t.Fatal("a changed set member left the analysis unchanged")
	}
}

// A Core 0.5.0 pack evaluation refuses is refused before analysis, with no
// report, and the refusal names Core's class, evaluation's code and Core's
// first diagnostic. A 0.3.0 pack's refusal keeps the message it had.
func TestPackAdviceCore050RefusalsNameCoreDiagnostic(t *testing.T) {
	e := engine(t)
	p050, d := fixture(t, "pack-0.5.0.json"), fixture(t, "domain.json")
	for _, row := range []struct {
		name, from, to string
		note           string
	}{
		{"unresolved reference", `"valueSet": "regions"`, `"valueSet": "missing"`, " Evaluation refuses this Core 0.5.0 pack: pack-not-conformant (JPS-EVALUATION-PACK-NOT-CONFORMANT), first diagnostic JPS-SEMANTIC-UNRESOLVED-VALUE-SET; spec validate reports why."},
		{"empty annotations", `"org.judgmentpack.annotations": {"note": "second"}`, `"org.judgmentpack.annotations": {}`, " Evaluation refuses this Core 0.5.0 pack: pack-not-conformant (JPS-EVALUATION-PACK-NOT-CONFORMANT), first diagnostic JPS-STRUCTURE-MIN-PROPERTIES; spec validate reports why."},
		{"annotation value", `"note": "second"`, `"note": {}`, " Evaluation refuses this Core 0.5.0 pack: pack-not-conformant (JPS-EVALUATION-PACK-NOT-CONFORMANT), first diagnostic JPS-STRUCTURE-TYPE; spec validate reports why."},
		{"required extension", `"extensions": {
    "org.judgmentpack.annotations"`, `"metadata": {"requiredExtensions": ["com.example.custom"]},
  "extensions": {
    "com.example.custom": {},
    "org.judgmentpack.annotations"`, " Evaluation refuses this Core 0.5.0 pack: unsupported-required-extension (JPS-CAPABILITY-REQUIRED-EXTENSION); spec validate reports why."},
		// Profile negotiation refuses before Core conformance, with no class.
		{"extended profile", `"specVersion": "0.5.0",`, `"specVersion": "0.5.0", "profile": "extended",`, ""},
		{"later Core", `"specVersion": "0.5.0",`, `"specVersion": "0.6.0",`, ""},
		{"earlier Core", `"specVersion": "0.5.0",`, `"specVersion": "0.4.0",`, ""},
	} {
		if !strings.Contains(string(p050), row.from) {
			t.Fatal(row.name, "fixture lacks", row.from)
		}
		bad := []byte(strings.Replace(string(p050), row.from, row.to, 1))
		r, f := Analyze(e, bad, d, DefaultOptions())
		if r != nil || f == nil || f.Code != "pack-unsupported" || f.Status != "unavailable" {
			t.Fatal(row.name, r, f)
		}
		if want := Refuse("pack-unsupported").Message + row.note; f.Message != want {
			t.Fatalf("%s:\n got %q\nwant %q", row.name, f.Message, want)
		}
		if len(bytesOf(f)) > 512 {
			t.Fatal(row.name, f)
		}
	}
	// The same unresolved fault in Core 0.3.0 constructs is refused as before.
	bad030 := strings.Replace(string(fixture(t, "pack-0.3.0.json")), `"value": ["us"]`, `"valueSet": "regions"`, 1)
	r, f := Analyze(e, []byte(bad030), d, DefaultOptions())
	if r != nil || f == nil || *f != *Refuse("pack-unsupported") {
		t.Fatal("0.3.0:", r, f)
	}
}

func TestPackAdviceFirstDiagnosticCodeKeepsOnlyACode(t *testing.T) {
	for message, want := range map[string]string{
		"x. First diagnostic: JPS-STRUCTURE-TYPE at /a: text": "JPS-STRUCTURE-TYPE",
		"x. First diagnostic: JPS-A1-B at <root>: text":       "JPS-A1-B",
		"no marker": "",
		"x. First diagnostic: jps-lower at /a: text":                          "",
		"x. First diagnostic: OTHER-CODE at /a: text":                         "",
		"x. First diagnostic: JPS-<b> at /a: text":                            "",
		"x. First diagnostic: " + "JPS-" + strings.Repeat("A", 61) + " at /a": "",
		"x. First diagnostic: ":                                               "",
	} {
		if got := firstDiagnosticCode(message); got != want {
			t.Fatalf("%q: got %q, want %q", message, got, want)
		}
	}
}
