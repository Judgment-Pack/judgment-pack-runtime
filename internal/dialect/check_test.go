package dialect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func checkPack(t *testing.T, path string, evidence bool) []byte {
	t.Helper()
	raw, err := os.ReadFile("../artifacts/jps/0.2.0-draft/cases/valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	p["rules"].([]any)[0].(map[string]any)["when"] = map[string]any{"op": "fact", "path": path, "operator": "equals", "value": true}
	if evidence {
		p["evidenceRequirements"] = []any{map[string]any{"id": "receipt", "description": "Receipt", "required": true, "kind": "document"}}
	}
	raw, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func mapping(facts, evidence, omitted, omittedEvidence []string) []byte {
	d := minimalDocument()
	c := d["case"].(map[string]any)
	for _, p := range facts {
		c["facts"] = append(c["facts"].([]any), map[string]any{"target": p, "source": "/value"})
	}
	for _, e := range evidence {
		c["evidence"] = append(c["evidence"].([]any), map[string]any{"requirement": e, "source": "/evidence"})
	}
	if omitted != nil {
		d["unmapped"] = omitted
	}
	if omittedEvidence != nil {
		d["unmappedEvidence"] = omittedEvidence
	}
	b, _ := json.Marshal(d)
	return b
}
func TestDeclaredMappingCoverage(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path     string
		facts, omitted []string
		status, entry  string
		exit           int
	}{
		{"exact", "/company/size", []string{"/company/size"}, nil, "complete", "mapped", 0},
		{"declared subtree", "/company/size", []string{"/company"}, nil, "complete", "mapped", 0},
		{"partial object", "/company", []string{"/company/size"}, nil, "review-required", "partial", 0},
		{"root", "", nil, nil, "review-required", "partial", 0},
		{"missing", "/company/size", nil, nil, "incomplete", "missing", 1},
		{"sibling is not ancestor", "/company/size", []string{"/company/size2"}, nil, "incomplete", "missing", 1},
		{"escaped token", "/company~1size/value", []string{"/company~1size"}, nil, "complete", "mapped", 0},
		{"escaped slash is not separator", "/company/size", []string{"/company~1size"}, nil, "incomplete", "missing", 1},
		{"acknowledged", "/company/size", nil, []string{"/company"}, "review-required", "acknowledged", 0},
		{"redundant omission", "/company/size", []string{"/company"}, []string{"/company"}, "review-required", "mapped", 0},
		{"unused omission", "/company/size", []string{"/company"}, []string{"/other"}, "review-required", "mapped", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pack := checkPack(t, tc.path, false)
			data := mapping(tc.facts, nil, tc.omitted, nil)
			r, code := Check("test", data, pack, engine)
			if code != tc.exit || r.Status != tc.status || r.Compatibility == nil || len(r.Compatibility.Facts) != 1 || r.Compatibility.Facts[0].Status != tc.entry {
				t.Fatalf("exit=%d %+v", code, r)
			}
			c := r.Compatibility
			if c.ValueTypesChecked || c.ExecutionChecked || !c.InventoryOverApproximates || r.FullDocumentConformance {
				t.Fatal("overstated coverage", c)
			}
			sum := sha256.Sum256(pack)
			if c.Pack.SHA256 != hex.EncodeToString(sum[:]) {
				t.Fatal("wrong pack snapshot")
			}
			encoded, _ := Encode("test", data)
			if r.Digest != encoded.Digest || r.Document != encoded.Document {
				t.Fatal("coverage rewrote dialect")
			}
			again, againCode := Check("test", data, pack, engine)
			if againCode != code || !reflect.DeepEqual(r, again) {
				t.Fatal("nondeterministic report")
			}
		})
	}
}
func TestEvidenceMappingAndOmissions(t *testing.T) {
	engine, _ := validation.NewEngine()
	pack := checkPack(t, "/x", true)
	for _, tc := range []struct {
		e, omitted []string
		status     string
		exit       int
	}{
		{[]string{"receipt"}, nil, "complete", 0}, {nil, nil, "incomplete", 1},
		{nil, []string{"receipt"}, "review-required", 0},
		{[]string{"receipt"}, []string{"receipt"}, "review-required", 0},
		{[]string{"other"}, []string{"receipt"}, "incomplete", 1},
		{[]string{"receipt"}, []string{"other"}, "incomplete", 1},
	} {
		r, code := Check("test", mapping([]string{"/x"}, tc.e, nil, tc.omitted), pack, engine)
		if code != tc.exit || r.Status != tc.status {
			t.Fatal(code, r)
		}
	}
}
func TestCheckPackRefusalsAndNoChecks(t *testing.T) {
	engine, _ := validation.NewEngine()
	valid := checkPack(t, "/x", false)
	doc := mapping([]string{"/x"}, nil, nil, nil)
	for _, tc := range []struct {
		name string
		d, p []byte
		exit int
	}{
		{"unsupported dialect", []byte(`{"version":4}`), valid, 2},
		{"invalid dialect", []byte(`{"version":2}`), valid, 1},
		{"duplicate pack", doc, []byte(`{"specVersion":"0.2.0-draft","specVersion":"0.2.0-draft"}`), 1},
		{"malformed pack", doc, []byte(`{`), 1},
		{"unknown pack version", doc, []byte(`{"specVersion":"9.0"}`), 2},
		{"pack bytes", []byte(`{`), []byte(strings.Repeat(" ", int(MaxPackBytes)+1)), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, code := Check("test", tc.d, tc.p, engine)
			if code != tc.exit || r.Status != "refused" || r.Compatibility != nil || r.Document != "" || r.Digest != "" {
				t.Fatal(code, r)
			}
		})
	}
	p, err := os.ReadFile("../artifacts/jps/0.2.0-draft/cases/valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	r, code := Check("test", mapping(nil, nil, nil, nil), p, engine)
	if code != 1 || r.Status != "not-applicable" {
		t.Fatal(code, r)
	}
	// Changing whitespace changes the exact pack identity even though its declared
	// interface and verdict stay the same.
	a, _ := Check("test", doc, valid, engine)
	b, _ := Check("test", doc, append(valid, '\n'), engine)
	if a.Compatibility.Pack.SHA256 == b.Compatibility.Pack.SHA256 {
		t.Fatal("pack byte identity lost")
	}
}
func TestCheckUsesExistingOverApproximatedInventory(t *testing.T) {
	engine, _ := validation.NewEngine()
	raw := checkPack(t, "/actual", false)
	var p map[string]any
	json.Unmarshal(raw, &p)
	p["rules"].([]any)[0].(map[string]any)["when"].(map[string]any)["value"] = map[string]any{"op": "fact", "path": "/literal-data", "where": map[string]any{"op": "literal", "value": true}}
	raw, _ = json.Marshal(p)
	inventory, err := project.InspectDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	r, code := Check("test", mapping([]string{"/actual"}, nil, nil, nil), raw, engine)
	if code != 1 || len(r.Compatibility.Facts) != len(inventory.FactPaths) || r.Compatibility.Facts[1].Target != "/literal-data" {
		t.Fatal(code, r)
	}
}

func TestCheckCoverageWorkBoundHasNoPartialResult(t *testing.T) {
	engine, _ := validation.NewEngine()
	pack := checkPack(t, "/x", false)
	var p map[string]any
	json.Unmarshal(pack, &p)
	conditions := []any{}
	for i := 0; i < 2100; i++ {
		conditions = append(conditions, map[string]any{"op": "fact", "path": fmt.Sprintf("/field%d", i), "operator": "equals", "value": true})
	}
	p["rules"].([]any)[0].(map[string]any)["when"] = map[string]any{"op": "all", "conditions": conditions}
	pack, _ = json.Marshal(p)
	targets := []string{}
	for i := 0; i < 256; i++ {
		targets = append(targets, fmt.Sprintf("/field%d", i))
	}
	r, code := Check("test", mapping(targets, nil, nil, nil), pack, engine)
	if code != 2 || r.Diagnostics[0].Code != "JPS-DIALECT-COVERAGE-LIMIT" || r.Compatibility != nil || r.Document != "" || r.Digest != "" {
		t.Fatal(code, r)
	}
}

func TestCoverageOutputLimitRefusesWholeReport(t *testing.T) {
	engine, _ := validation.NewEngine()
	raw := checkPack(t, "/x", false)
	var p map[string]any
	json.Unmarshal(raw, &p)
	conditions := []any{}
	for _, prefix := range []string{"/a", "/b"} {
		conditions = append(conditions, map[string]any{"op": "fact", "path": prefix + strings.Repeat("x", 600000), "operator": "equals", "value": true})
	}
	p["rules"].([]any)[0].(map[string]any)["when"] = map[string]any{"op": "all", "conditions": conditions}
	raw, _ = json.Marshal(p)
	r, code := Check("test", mapping(nil, nil, nil, nil), raw, engine)
	if code != 2 || r.Diagnostics[0].Code != "JPS-DIALECT-COVERAGE-OUTPUT-LIMIT" || r.Compatibility != nil || r.Document != "" || r.Digest != "" {
		t.Fatal(code, r)
	}
}

func TestCoverageOutputLimitStopsRepeatedAncestorOmissions(t *testing.T) {
	engine, _ := validation.NewEngine()
	base := "/" + strings.Repeat("a", 500)
	omitted := make([]string, 100)
	for i := range omitted {
		omitted[i] = base + strings.Repeat("/x", i)
	}
	var pack map[string]any
	json.Unmarshal(checkPack(t, "/x", false), &pack)
	conditions := make([]any, 2000)
	for i := range conditions {
		conditions[i] = map[string]any{"op": "fact", "path": fmt.Sprintf("%s/p%d", omitted[99], i), "operator": "equals", "value": true}
	}
	pack["rules"].([]any)[0].(map[string]any)["when"] = map[string]any{"op": "all", "conditions": conditions}
	raw, _ := json.Marshal(pack)
	r, code := Check("test", mapping(nil, nil, omitted, nil), raw, engine)
	if code != 2 || r.Diagnostics[0].Code != "JPS-DIALECT-COVERAGE-OUTPUT-LIMIT" || r.Compatibility != nil || r.Document != "" || r.Digest != "" {
		t.Fatal(code, r)
	}
}

// The dialect check reads a Core 0.5.0 pack's inventory (ADR-0082). The
// bundled pack reads one fact through a declared value set and carries
// annotations on an outcome and on value-set entries: the one fact pointer is
// the whole inventory, so neither the value set's entries nor any annotation
// is reported as a pointer, and the mapping decides the coverage as it does
// for 0.3.0.
func TestCore050PackIsChecked(t *testing.T) {
	pack, err := os.ReadFile("../artifacts/jps/0.5.0/cases/valid/annotations-value-set-entry.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pack), `"valueSet": "priority-tiers"`) || !strings.Contains(string(pack), `"org.judgmentpack.annotations"`) {
		t.Fatal("the fixture no longer uses a value set and an annotation")
	}
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	r, code := Check("test", mapping([]string{"/request/tier"}, nil, nil, nil), pack, engine)
	if code != result.ExitSuccess || len(r.Diagnostics) != 0 || r.Compatibility == nil || r.Compatibility.Status != "complete" ||
		r.Compatibility.Pack.SpecVersion != "0.5.0" || len(r.Compatibility.Facts) != 1 ||
		r.Compatibility.Facts[0].Target != "/request/tier" || r.Compatibility.Facts[0].Status != "mapped" {
		t.Fatalf("0.5.0 pack, mapped: code %d %+v %+v", code, r, r.Compatibility)
	}
	r, code = Check("test", mapping([]string{"/request/other"}, nil, nil, nil), pack, engine)
	if code != result.ExitInvalid || r.Compatibility == nil || r.Compatibility.Status != "incomplete" ||
		len(r.Compatibility.Facts) != 1 || r.Compatibility.Facts[0].Target != "/request/tier" || r.Compatibility.Facts[0].Status != "missing" {
		t.Fatalf("0.5.0 pack, unmapped: code %d %+v", code, r.Compatibility)
	}
}

// A pack declaring a version pack validation does not admit is still refused
// before any coverage is reported: 0.6.0 is unsupported, exit 2.
func TestUnknownSpecVersionPackIsStillRefused(t *testing.T) {
	raw, err := os.ReadFile("../artifacts/jps/0.5.0/cases/valid/annotations-value-set-entry.json")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	pack := []byte(strings.Replace(string(raw), `"specVersion": "0.5.0"`, `"specVersion": "0.6.0"`, 1))
	r, code := Check("test", mapping([]string{"/request/tier"}, nil, nil, nil), pack, engine)
	if code != result.ExitUnsupported || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "JPS-DIALECT-PACK-INVALID" || r.Compatibility != nil {
		t.Fatalf("0.6.0 pack: code %d %+v", code, r)
	}
}

// The retained-column draft path is unchanged by ADR-0082: its leaf bundles
// Core 0.3.0 text, so a valid 0.5.0 pack is refused there exactly as before,
// with no mapping coverage.
func TestRetainedColumnDraftStillRefusesCore050(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"minimal-literal.json", "annotations-value-set-entry.json"} {
		pack, err := os.ReadFile("../artifacts/jps/0.5.0/cases/valid/" + name)
		if err != nil {
			t.Fatal(err)
		}
		d := draftDocument("text")
		d["case"].(map[string]any)["evidence"] = []any{}
		r, code := CheckRetainedColumnDraft("test", draftJSON(t, d), pack, engine, RetainedColumnDraftBase())
		if code != result.ExitUnsupported || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "JPS-SURFACE-SPEC-VERSION-UNSUPPORTED" || r.Diagnostics[0].InstancePath != "/specVersion" || r.Diagnostics[0].Message != `The dialect check does not read packs declaring specVersion "0.5.0"; it reads Core 0.1.0-draft, 0.2.0-draft and 0.3.0 packs.` || r.Compatibility != nil || r.PackValidation == nil || r.PackValidation.Status != "valid" {
			t.Fatalf("%s on the draft path: code %d %+v", name, code, r)
		}
	}
}

// The check reads the Core versions it names and no other: a version
// validation came to admit later is refused until the list names it.
func TestDialectCheckReadsNamedVersionsOnly(t *testing.T) {
	for _, version := range []string{"0.1.0-draft", "0.2.0-draft", "0.3.0", "0.5.0"} {
		if !readsVersion(version) {
			t.Fatalf("%s is read", version)
		}
	}
	for _, version := range []string{"0.6.0", "0.4.0", "0.4.0-draft", "", "0.5"} {
		if readsVersion(version) {
			t.Fatalf("%q is not read", version)
		}
	}
	if got := result.SurfaceSpecVersionUnsupportedMessage("The dialect check", "0.6.0", readableVersions()); got != `The dialect check does not read packs declaring specVersion "0.6.0"; it reads Core 0.1.0-draft, 0.2.0-draft, 0.3.0 and 0.5.0 packs.` {
		t.Fatal(got)
	}
}
