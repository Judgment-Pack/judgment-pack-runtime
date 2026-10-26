package dialect

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func draftJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func draftDocument(kind string) map[string]any {
	conversion := map[string]any{"contractVersion": "1", "column": "Column", "kind": kind}
	if kind == "boolean" {
		conversion["trueValue"], conversion["falseValue"] = "YES", "NO"
	}
	return map[string]any{"version": 2, "case": map[string]any{"facts": []any{map[string]any{"target": "/converted", "source": "/retained", "conversion": conversion}}, "evidence": []any{map[string]any{"requirement": "proof", "source": "/retained", "availability": map[string]any{"contractVersion": "1", "column": "Column"}}}}}
}
func draftFact(d map[string]any) map[string]any {
	return d["case"].(map[string]any)["facts"].([]any)[0].(map[string]any)
}
func draftConversion(d map[string]any) map[string]any {
	return draftFact(d)["conversion"].(map[string]any)
}
func draftGenerated() map[string]any {
	return map[string]any{"contractVersion": "1", "origin": "generated", "values": map[string]any{"Column": "-0", "Null": nil, "Empty": ""}, "research": map[string]any{
		"recordRecipeRevision": "recipe:7", "declaration": map[string]any{"recipeRevision": "recipe:7", "fields": []any{map[string]any{"name": "Column", "fieldJson": `{ "name":"Column","type":"number","opaque":{"n":1e100,"zero":-0,"label":"\u00e9"} }`}, map[string]any{"name": "Other", "fieldJson": `{"name":"Other","type":"string"}`}}},
		"fields": map[string]any{"Column": map[string]any{"status": "supported", "valueJson": "-0"}, "Other": map[string]any{"status": "interpretation", "valueJson": `"\ud800"`}, "Missing": map[string]any{"status": "missing"}, "Null": map[string]any{"status": "supported", "valueJson": "null"}}}}
}
func draftResearch(f map[string]any) map[string]any { return f["research"].(map[string]any) }
func draftDeclaration(f map[string]any) map[string]any {
	return draftResearch(f)["declaration"].(map[string]any)
}
func draftEntry(f map[string]any) map[string]any {
	return draftDeclaration(f)["fields"].([]any)[0].(map[string]any)
}

func TestRetainedColumnDraftIdentityAndSelection(t *testing.T) {
	for _, fixture := range []struct{ file, digest string }{{"contract.schema.json", RetainedColumnDraftSchema}, {"cases.json", RetainedColumnDraftCorpus}} {
		raw, err := os.ReadFile("testdata/retained-column-draft/" + fixture.file)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if "sha256:"+hex.EncodeToString(sum[:]) != fixture.digest {
			t.Fatal("Draft fixture pin changed", fixture.file)
		}
	}
	cases := []struct {
		name, want string
		change     func(*RetainedColumnDraftSelection)
	}{
		{"contract", "capability-unsupported", func(s *RetainedColumnDraftSelection) { s.Contract = "retained-column/2" }},
		{"version", "capability-unsupported", func(s *RetainedColumnDraftSelection) { s.Spec.Version = "1" }},
		{"revision", "capability-unsupported", func(s *RetainedColumnDraftSelection) { s.Spec.Revision = strings.Repeat("0", 40) }},
		{"schema", "capability-unsupported", func(s *RetainedColumnDraftSelection) { s.Spec.SchemaSHA256 = "wrong" }},
		{"corpus", "capability-unsupported", func(s *RetainedColumnDraftSelection) { s.Spec.CorpusSHA256 = "wrong" }},
		{"unknown profile", "input-malformed", func(s *RetainedColumnDraftSelection) { s.Profile = "unknown" }},
		{"missing profile", "input-malformed", func(s *RetainedColumnDraftSelection) { s.Profile = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			selection := RetainedColumnDraftBase()
			tc.change(&selection)
			for _, raw := range [][]byte{nil, []byte("{"), bytes.Repeat([]byte("x"), RetainedColumnDraftFrameBytes+1)} {
				r, code := ValidateRetainedColumnDraft("test", raw, selection)
				if code == 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != tc.want || r.Document != "" || r.Digest != "" {
					t.Fatalf("selector was not checked first: %+v", r)
				}
				frame, code := ValidateRetainedColumnDraftFrame("test", raw, selection)
				if code == 0 || len(frame.Diagnostics) != 1 || frame.Diagnostics[0].Code != tc.want || frame.Frame != nil {
					t.Fatalf("frame selector was not checked first: %+v", frame)
				}
			}
		})
	}
}

func TestRetainedColumnDraftDocuments(t *testing.T) {
	for _, version := range []int{2, 3} {
		for _, named := range []bool{false, true} {
			for _, kind := range []string{"text", "decimal", "number", "boolean", "research-value"} {
				d := draftDocument(kind)
				d["version"] = version
				if named {
					d["formatVersion"] = fmt.Sprintf("%d.0.0", version)
					d["id"] = "urn:test:retained"
					d["version"] = "1.0.0"
				}
				raw := draftJSON(t, d)
				r, code := ValidateRetainedColumnDraft("test", raw, RetainedColumnDraftBase())
				if code != 0 || r.Status != "document-valid" || r.ValidationScope != "experimental-retained-column-base-document" || r.Spec.Revision != RetainedColumnDraftRevision || r.FullDocumentConformance || r.Maturity != "experimental-draft" || r.Capabilities != nil {
					t.Fatalf("%d %v %s: %+v", version, named, kind, r)
				}
				if old, code := Validate("test", raw); code == 0 || old.Status != "refused" {
					t.Fatal("released validation admitted Draft", old)
				}
				encoded, _ := Encode("test", raw)
				if encoded.Document != r.Document || encoded.Digest != r.Digest {
					t.Fatal("Draft changed canonical document identity")
				}
			}
		}
	}
	// Converted targets participate in the real source-order parameter checks.
	d := sampleDocument(t)
	copy := sourceAt(d, 0)["read"].(map[string]any)["copy"].(map[string]any)
	copy["facts"].([]any)[0].(map[string]any)["conversion"] = draftConversion(draftDocument("text"))
	if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase()); code != 0 {
		t.Fatal(r)
	}
	// Removing the earlier converted producer invalidates the later parameter.
	copy["facts"] = []any{}
	if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase()); code == 0 || r.Diagnostics[0].InstancePath != "/sources/1/parameters/firstValue/pointer" {
		t.Fatal(r)
	}
}

func TestRetainedColumnDraftDocumentRefusals(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"leaf version", func(d map[string]any) { draftConversion(d)["contractVersion"] = "2" }},
		{"leaf version type", func(d map[string]any) { draftConversion(d)["contractVersion"] = 1 }},
		{"missing column", func(d map[string]any) { delete(draftConversion(d), "column") }},
		{"empty column", func(d map[string]any) { draftConversion(d)["column"] = "" }},
		{"unknown kind", func(d map[string]any) { draftConversion(d)["kind"] = "future" }},
		{"unknown leaf member", func(d map[string]any) { draftConversion(d)["extra"] = true }},
		{"boolean absent spelling", func(d map[string]any) { delete(draftConversion(d), "falseValue") }},
		{"boolean repeated spelling", func(d map[string]any) { draftConversion(d)["falseValue"] = "YES" }},
		{"boolean blank spelling", func(d map[string]any) { draftConversion(d)["falseValue"] = "\u0085\u3000" }},
		{"boolean wrong scalar", func(d map[string]any) { draftConversion(d)["trueValue"] = true }},
		{"mixed quality target", func(d map[string]any) { draftFact(d)["qualityTarget"] = "x" }},
		{"mixed evidence leaf", func(d map[string]any) {
			draftFact(d)["availability"] = map[string]any{"contractVersion": "1", "column": "Column"}
		}},
		{"bad pointer", func(d map[string]any) { draftFact(d)["source"] = "/bad~2" }},
		{"duplicate target", func(d map[string]any) {
			c := d["case"].(map[string]any)
			c["facts"] = append(c["facts"].([]any), draftFact(d))
		}},
		{"overlap", func(d map[string]any) {
			c := d["case"].(map[string]any)
			c["facts"] = append(c["facts"].([]any), map[string]any{"target": "/converted/child", "source": ""})
		}},
		{"unknown root", func(d map[string]any) { d["unknown"] = true }},
		{"unknown declared profile", func(d map[string]any) { d["profile"] = "unknown" }},
		{"wrong declared profile", func(d map[string]any) { d["profile"] = "extended" }},
		{"missing case", func(d map[string]any) { delete(d, "case") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := draftDocument("boolean")
			tc.change(d)
			r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase())
			if code != result.ExitInvalid || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "input-malformed" || r.Document != "" || r.Digest != "" {
				t.Fatalf("invalid Draft admitted: %+v", r)
			}
		})
	}
	// The specified blank set deliberately excludes U+001C and zero-width space.
	for _, spelling := range []string{"\u001c", "\u200b"} {
		d := draftDocument("boolean")
		draftConversion(d)["falseValue"] = spelling
		if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase()); code != 0 {
			t.Fatal(r)
		}
	}
	for _, raw := range []string{`{"version":2,"version":2,"case":{"facts":[],"evidence":[]}}`, `{"version":2,"case":{"facts":[],"evidence":[]},"x":"\ud800"}`, `{"version":2,"case":{"facts":[],"evidence":[]}} {}`} {
		if r, code := ValidateRetainedColumnDraft("test", []byte(raw), RetainedColumnDraftBase()); code == 0 || r.Document != "" {
			t.Fatal(r)
		}
	}
}

func TestRetainedColumnDraftFramePreservation(t *testing.T) {
	f := draftGenerated()
	raw := draftJSON(t, f)
	r, code := ValidateRetainedColumnDraftFrame("test", raw, RetainedColumnDraftBase())
	if code != 0 || r.Status != "retained-frame-valid" || r.Document != string(raw) || r.Frame == nil || r.FullDocumentConformance {
		t.Fatal(r)
	}
	sum := sha256.Sum256(raw)
	if r.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatal("frame byte digest mismatch")
	}

	frame := r.Frame
	if frame.Research.RecordRecipeRevision != "recipe:7" || frame.Research.Declaration.RecipeRevision != "recipe:7" || len(frame.Research.Declaration.Fields) != 2 {
		t.Fatal("lost original inventory/revision", frame)
	}
	entry := frame.Research.Declaration.Fields[0]
	if entry.FieldJSON != draftEntry(f)["fieldJson"] || entry.Name != "Column" || frame.Research.Declaration.Fields[1].Name != "Other" {
		t.Fatal("declaration bytes/order changed")
	}
	sum = sha256.Sum256([]byte(entry.FieldJSON))
	if entry.FieldSHA256 != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatal("field JSON hashing changed")
	}
	if value, present := frame.Values["Null"]; !present || value != nil {
		t.Fatal("lost historical null")
	}
	if _, present := frame.Values["Absent"]; present {
		t.Fatal("invented absent column")
	}
	if *frame.Values["Empty"] != "" || *frame.Research.Fields["Column"].ValueJSON != "-0" || *frame.Research.Fields["Other"].ValueJSON != `"\ud800"` || frame.Research.Fields["Missing"].ValueJSON != nil || *frame.Research.Fields["Null"].ValueJSON != "null" {
		t.Fatal("raw/null/missing distinctions changed")
	}
	// Unavailable/mismatched original declaration and malformed typed raw remain inspectable.
	for _, value := range []any{nil, map[string]any{"recipeRevision": "recipe:8", "fields": []any{map[string]any{"name": "Column", "fieldJson": `{"name":"Column","type":"future"}`}}}} {
		f := draftGenerated()
		draftResearch(f)["declaration"] = value
		draftResearch(f)["fields"].(map[string]any)["Column"] = map[string]any{"status": "supported", "valueJson": "{"}
		if r, code := ValidateRetainedColumnDraftFrame("test", draftJSON(t, f), RetainedColumnDraftBase()); code != 0 {
			t.Fatal(r)
		}
	}
}

func TestRetainedColumnDraftFrameRefusals(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"unknown", func(f map[string]any) { f["extra"] = true }},
		{"missing", func(f map[string]any) { delete(f, "values") }},
		{"version", func(f map[string]any) { f["contractVersion"] = "2" }},
		{"origin", func(f map[string]any) { f["origin"] = "record" }},
		{"asserted research", func(f map[string]any) { f["origin"] = "asserted" }},
		{"null research", func(f map[string]any) { f["research"] = nil }},
		{"value scalar", func(f map[string]any) { f["values"].(map[string]any)["Column"] = 0 }},
		{"empty column", func(f map[string]any) { f["values"].(map[string]any)[""] = "x" }},
		{"missing revision", func(f map[string]any) { delete(draftResearch(f), "recordRecipeRevision") }},
		{"declaration missing revision", func(f map[string]any) { delete(draftDeclaration(f), "recipeRevision") }},
		{"declaration duplicate name", func(f map[string]any) {
			d := draftDeclaration(f)
			d["fields"] = append(d["fields"].([]any), draftEntry(f))
		}},
		{"declaration wrong name", func(f map[string]any) { draftEntry(f)["fieldJson"] = `{"name":"Other","type":"text"}` }},
		{"declaration missing type", func(f map[string]any) { draftEntry(f)["fieldJson"] = `{"name":"Column"}` }},
		{"declaration type scalar", func(f map[string]any) { draftEntry(f)["fieldJson"] = `{"name":"Column","type":null}` }},
		{"declaration duplicate embedded", func(f map[string]any) {
			draftEntry(f)["fieldJson"] = `{"name":"Column","type":"text","x":{"v":1,"\u0076":2}}`
		}},
		{"declaration embedded surrogate", func(f map[string]any) { draftEntry(f)["fieldJson"] = `{"name":"Column","type":"text","x":"\ud800"}` }},
		{"declaration not object", func(f map[string]any) { draftEntry(f)["fieldJson"] = `[]` }},
		{"declaration invalid", func(f map[string]any) { draftEntry(f)["fieldJson"] = `{` }},
		{"declaration extra entry", func(f map[string]any) { draftEntry(f)["extra"] = true }},
		{"status", func(f map[string]any) {
			draftResearch(f)["fields"].(map[string]any)["Column"] = map[string]any{"status": "found"}
		}},
		{"raw value scalar", func(f map[string]any) {
			draftResearch(f)["fields"].(map[string]any)["Column"] = map[string]any{"status": "supported", "valueJson": nil}
		}},
		{"field extra", func(f map[string]any) {
			draftResearch(f)["fields"].(map[string]any)["Column"] = map[string]any{"status": "supported", "extra": true}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := draftGenerated()
			tc.change(f)
			r, code := ValidateRetainedColumnDraftFrame("test", draftJSON(t, f), RetainedColumnDraftBase())
			if code != result.ExitInvalid || r.Diagnostics[0].Code != "input-malformed" || r.Frame != nil || r.Document != "" || r.Digest != "" {
				t.Fatalf("malformed frame admitted: %+v", r)
			}
		})
	}
	for _, raw := range [][]byte{nil, []byte(`{"contractVersion":"1","origin":"asserted","values":{"x":"1","\u0078":null},"research":null}`), []byte(`{"contractVersion":"1","origin":"asserted","values":{"x":"\ud800"},"research":null}`), []byte("\xff")} {
		if r, code := ValidateRetainedColumnDraftFrame("test", raw, RetainedColumnDraftBase()); code == 0 || r.Frame != nil {
			t.Fatal(r)
		}
	}
}

func TestRetainedColumnDraftBounds(t *testing.T) {
	// Limits count complete supplied serialized artifacts, including whitespace.
	doc := draftJSON(t, draftDocument("text"))
	exactDoc := append(append([]byte{}, doc...), bytes.Repeat([]byte(" "), RetainedColumnDraftDocumentBytes-len(doc))...)
	if r, code := ValidateRetainedColumnDraft("test", exactDoc, RetainedColumnDraftBase()); code != 0 {
		t.Fatal(r)
	}
	if r, code := Validate("test", exactDoc); code == 0 || r.Diagnostics[0].Code != "JPS-DIALECT-BYTE-LIMIT" {
		t.Fatal("released document byte limit changed", r)
	}
	if r, code := ValidateRetainedColumnDraft("test", append(exactDoc, ' '), RetainedColumnDraftBase()); code == 0 || r.Diagnostics[0].Code != "input-over-bound" {
		t.Fatal("Draft document byte bound missing", r)
	}
	frame := []byte(`{"contractVersion":"1","origin":"asserted","values":{},"research":null}`)
	exactFrame := append(append([]byte{}, frame...), bytes.Repeat([]byte(" "), RetainedColumnDraftFrameBytes-len(frame))...)
	if r, code := ValidateRetainedColumnDraftFrame("test", exactFrame, RetainedColumnDraftBase()); code != 0 {
		t.Fatal(r)
	}
	if r, code := ValidateRetainedColumnDraftFrame("test", append(exactFrame, ' '), RetainedColumnDraftBase()); code == 0 || r.Diagnostics[0].Code != "input-over-bound" {
		t.Fatal("frame byte bound missing", r)
	}
	// Existing document parser work budgets are independent of the new byte cap.
	for _, tc := range []struct{ name, raw, want string }{
		{"nodes", `{"version":2,"case":{"facts":[],"evidence":[]},"x":[` + strings.Repeat("null,", MaxDocumentBytes) + `null]}`, "JPS-RESOURCE-NODE-LIMIT"},
		{"string", `{"version":2,"case":{"facts":[],"evidence":[]},"x":"` + strings.Repeat("x", MaxDocumentBytes+1) + `"}`, "JPS-RESOURCE-STRING-LIMIT"},
		{"depth", `{"version":2,"case":{"facts":[],"evidence":[]},"x":` + strings.Repeat("[", MaxDepth) + "null" + strings.Repeat("]", MaxDepth) + `}`, "JPS-RESOURCE-DEPTH-LIMIT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, code := ValidateRetainedColumnDraft("test", []byte(tc.raw), RetainedColumnDraftBase())
			if code == 0 || r.Diagnostics[0].Code != tc.want {
				t.Fatalf("independent parser budget missing: %+v", r)
			}
		})
	}
	for _, n := range []int{16000, 16001} {
		f := draftGenerated()
		f["values"].(map[string]any)["Column"] = strings.Repeat("x", n)
		r, code := ValidateRetainedColumnDraftFrame("test", draftJSON(t, f), RetainedColumnDraftBase())
		if (code == 0) != (n == 16000) || code != 0 && r.Diagnostics[0].Code != "input-over-bound" {
			t.Fatalf("raw column bound %d: %+v", n, r)
		}
	}
	// UTF-8 byte ceilings, rather than rune counts, cover names and booleans.
	for _, n := range []int{80, 81} {
		d := draftDocument("boolean")
		draftConversion(d)["column"] = strings.Repeat("é", n)
		draftConversion(d)["trueValue"] = strings.Repeat("é", n)
		r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase())
		if (code == 0) != (n == 80) {
			t.Fatalf("UTF8 bound %d: %+v", n, r)
		}
	}
	for _, n := range []int{16384, 16385} {
		f := draftGenerated()
		base := `{"name":"Column","type":"number","opaque":""}`
		draftEntry(f)["fieldJson"] = strings.Replace(base, `"opaque":""`, `"opaque":"`+strings.Repeat("x", n-len(base))+`"`, 1)
		r, code := ValidateRetainedColumnDraftFrame("test", draftJSON(t, f), RetainedColumnDraftBase())
		if (code == 0) != (n == 16384) || code != 0 && r.Diagnostics[0].Code != "input-over-bound" {
			t.Fatalf("fieldJson byte bound %d: %+v", n, r)
		}
	}
	for _, depth := range []int{31, 32} {
		f := draftGenerated()
		draftEntry(f)["fieldJson"] = `{"name":"Column","type":"text","opaque":` + strings.Repeat("[", depth) + `null` + strings.Repeat("]", depth) + `}`
		r, code := ValidateRetainedColumnDraftFrame("test", draftJSON(t, f), RetainedColumnDraftBase())
		if (code == 0) != (depth == 31) {
			t.Fatalf("fieldJson depth %d: %+v", depth, r)
		}
	}
	// Opaque typed data may exceed the 512KiB conversion parser limit; document
	// admission does not execute that parser and cannot turn its issue into refusal.
	f := draftGenerated()
	large := strings.Repeat("x", 524289)
	draftResearch(f)["fields"].(map[string]any)["Column"] = map[string]any{"status": "supported", "valueJson": large}
	if r, code := ValidateRetainedColumnDraftFrame("test", draftJSON(t, f), RetainedColumnDraftBase()); code != 0 || *r.Frame.Research.Fields["Column"].ValueJSON != large {
		t.Fatal("opaque retained raw was interpreted", r)
	}
	// All three complete inventories fit the independent frame node allowance.
	f = draftGenerated()
	values := map[string]any{}
	fields := map[string]any{}
	entries := []any{}
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("c%d", i)
		values[name] = ""
		fields[name] = map[string]any{"status": "supported", "valueJson": "null"}
		entries = append(entries, map[string]any{"name": name, "fieldJson": fmt.Sprintf(`{"name":"%s","type":"string"}`, name)})
	}
	f["values"] = values
	draftResearch(f)["fields"] = fields
	draftDeclaration(f)["fields"] = entries
	if r, code := ValidateRetainedColumnDraftFrame("test", draftJSON(t, f), RetainedColumnDraftBase()); code != 0 || len(r.Frame.Research.Declaration.Fields) != 1000 {
		t.Fatal("complete inventories refused", r)
	}
	for _, part := range []string{"values", "fields", "declarations"} {
		t.Run(part, func(t *testing.T) {
			switch part {
			case "values":
				values["overflow"] = ""
				defer delete(values, "overflow")
			case "fields":
				fields["overflow"] = map[string]any{"status": "missing"}
				defer delete(fields, "overflow")
			case "declarations":
				draftDeclaration(f)["fields"] = append(entries, map[string]any{"name": "overflow", "fieldJson": `{"name":"overflow","type":"string"}`})
				defer func() { draftDeclaration(f)["fields"] = entries }()
			}
			r, code := ValidateRetainedColumnDraftFrame("test", draftJSON(t, f), RetainedColumnDraftBase())
			if code == 0 || r.Diagnostics[0].Code != "input-over-bound" {
				t.Fatalf("inventory bound %s missing: %+v", part, r)
			}
		})
	}
}

func TestRetainedColumnDraftPinnedCorpusAdmission(t *testing.T) {
	raw, err := os.ReadFile("testdata/retained-column-draft/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			ID               string          `json:"id"`
			Kind             string          `json:"kind"`
			InputJSON        *string         `json:"inputJson"`
			InputState       string          `json:"inputState"`
			Column           string          `json:"column"`
			Origin           string          `json:"origin"`
			Type             *string         `json:"type"`
			Status           string          `json:"status"`
			Rule             map[string]any  `json:"rule"`
			Declaration      json.RawMessage `json:"declaration"`
			Undeclared       bool            `json:"undeclared"`
			RevisionMismatch bool            `json:"revisionMismatch"`
			FieldEntryAbsent bool            `json:"fieldEntryAbsent"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 72 {
		t.Fatal("pinned Draft admission coverage changed")
	}
	for _, c := range corpus.Cases {
		t.Run(c.ID, func(t *testing.T) {
			column := c.Column
			if column == "" {
				column = "Column"
			}
			kind := c.Kind
			if kind == "evidence-availability" {
				kind = "text"
			}
			d := draftDocument(kind)
			conversion := draftConversion(d)
			conversion["column"] = column
			for k, v := range c.Rule {
				conversion[k] = v
			}
			if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase()); code != 0 {
				t.Fatal("pinned Draft document case refused", r)
			}
			f := map[string]any{"contractVersion": "1", "origin": "asserted", "values": map[string]any{}, "research": nil}
			if c.Kind != "research-value" && c.InputJSON != nil {
				var value any
				if err := json.Unmarshal([]byte(*c.InputJSON), &value); err != nil {
					t.Fatal(err)
				}
				f["values"].(map[string]any)[column] = value
			}
			if c.Origin == "generated" || c.Origin == "" && c.Kind == "research-value" {
				f["origin"] = "generated"
				typ := "text"
				if c.Type != nil {
					typ = *c.Type
				}
				entries := []any{}
				if !c.Undeclared {
					entries = append(entries, map[string]any{"name": column, "fieldJson": string(draftJSON(t, map[string]any{"name": column, "type": typ}))})
				}
				revision := "recipe:1"
				if c.RevisionMismatch {
					revision = "recipe:2"
				}
				var declaration any = map[string]any{"recipeRevision": revision, "fields": entries}
				if string(c.Declaration) == "null" {
					declaration = nil
				}
				status := c.Status
				if status == "" || c.ID == "planned-research-zero-records" {
					status = "supported"
				}
				field := map[string]any{"status": status}
				if c.InputJSON != nil && c.InputState != "missing" {
					field["valueJson"] = *c.InputJSON
				}
				fields := map[string]any{}
				if !c.FieldEntryAbsent {
					fields[column] = field
				}
				f["research"] = map[string]any{"recordRecipeRevision": "recipe:1", "declaration": declaration, "fields": fields}
			}
			if r, code := ValidateRetainedColumnDraftFrame("test", draftJSON(t, f), RetainedColumnDraftBase()); code != 0 {
				t.Fatal("pinned Draft frame case refused", r)
			}
		})
	}
}

func TestRetainedColumnDraftReleasedContractUnchanged(t *testing.T) {
	contract := Contract("test")
	raw := string(draftJSON(t, contract))
	if strings.Contains(raw, "conversionContracts") || strings.Contains(raw, "retained-column") || contract.Capabilities.MaxDocumentBytes != 131072 || contract.SpecRevision != SpecRevision {
		t.Fatal("released contract advertises Draft", contract)
	}
	for _, version := range []int{2, 3} {
		legacy, code := Init("test", version)
		if code != 0 {
			t.Fatal(legacy)
		}
		released, code := Validate("test", []byte(legacy.Document))
		if code != 0 {
			t.Fatal(released)
		}
		draft, code := ValidateRetainedColumnDraft("test", []byte(legacy.Document), RetainedColumnDraftBase())
		if code != 0 || draft.Document != released.Document || draft.Digest != released.Digest {
			t.Fatal("historical document identity changed", draft)
		}
	}
}

func TestRetainedColumnDraftResearchFramePointer(t *testing.T) {
	d := researchSample(t)
	copy := sourceAt(d, 0)["read"].(map[string]any)["copy"].(map[string]any)
	fact := copy["facts"].([]any)[0].(map[string]any)
	fact["source"] = "/retained"
	fact["conversion"] = map[string]any{"contractVersion": "1", "column": "OriginalColumn", "kind": "research-value"}
	copy["evidence"] = []any{map[string]any{"requirement": "proof", "source": "/retained", "availability": map[string]any{"contractVersion": "1", "column": "OriginalColumn"}}}
	if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase()); code != 0 {
		t.Fatal("Draft research frame pointer refused", r)
	}
	// Ordinary copies keep current-research-field restrictions in both entries.
	delete(fact, "conversion")
	copy["evidence"] = []any{}
	if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase()); code == 0 {
		t.Fatal("ordinary research pointer restriction removed", r)
	}
	if r, code := Validate("test", draftJSON(t, d)); code == 0 {
		t.Fatal("released research pointer restriction removed", r)
	}
	// Even a Draft leaf does not bypass malformed pointer or unwrap restrictions.
	fact["conversion"] = map[string]any{"contractVersion": "1", "column": "OriginalColumn", "kind": "research-value"}
	fact["source"] = "bad"
	if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase()); code == 0 {
		t.Fatal("Draft source syntax bypassed", r)
	}
	fact["source"] = "/retained"
	sourceAt(d, 0)["read"].(map[string]any)["unwrap"] = []any{"/unknown"}
	if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase()); code == 0 {
		t.Fatal("ordinary research unwrap restriction removed", r)
	}
}

func TestRetainedColumnDraftPinnedFieldIdentity(t *testing.T) {
	raw, err := os.ReadFile("testdata/retained-column-draft/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			ID      string `json:"id"`
			Request struct {
				Lineage json.RawMessage `json:"lineage"`
			} `json:"request"`
			Expected struct {
				Identity struct {
					Revision string `json:"recipeRevision"`
					Hash     string `json:"fieldSha256"`
				} `json:"researchIdentity"`
			} `json:"expected"`
		} `json:"interfaceCases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, c := range corpus.Cases {
		if c.Expected.Identity.Hash == "" {
			continue
		}
		t.Run(c.ID, func(t *testing.T) {
			var lineage struct {
				Frame json.RawMessage `json:"frame"`
			}
			if err := json.Unmarshal(c.Request.Lineage, &lineage); err != nil {
				t.Fatal(err)
			}
			r, code := ValidateRetainedColumnDraftFrame("test", lineage.Frame, RetainedColumnDraftBase())
			if code != 0 {
				t.Fatal(r)
			}
			if r.Frame.Research.RecordRecipeRevision != c.Expected.Identity.Revision || r.Frame.Research.Declaration.Fields[0].FieldSHA256 != c.Expected.Identity.Hash {
				t.Fatal("original declaration identity differs from pinned handwritten vector", r.Frame)
			}
		})
		count++
	}
	if count != 4 {
		t.Fatal("pinned field identity vectors changed", count)
	}
}

func draftExtended() RetainedColumnDraftSelection {
	s := RetainedColumnDraftBase()
	s.Profile = "extended"
	return s
}

func draftOperation(name string) map[string]any {
	return map[string]any{"name": name, "kind": "operation", "profile": "input", "profileDigest": "sha256:" + strings.Repeat("a", 64), "maxAge": 1, "arguments": map[string]any{}, "read": map[string]any{"copy": map[string]any{"facts": []any{}, "evidence": []any{}}}}
}

func draftExtendedDocument() map[string]any {
	return map[string]any{"version": 2, "profile": "extended", "case": map[string]any{"facts": []any{}, "evidence": []any{}}}
}

func draftRule(clauses, conditions int) map[string]any {
	list := []any{}
	for i := 0; i < clauses; i++ {
		when := map[string]any{"op": "always"}
		if i == 0 && conditions > 0 {
			of := []any{}
			for j := 0; j < conditions; j++ {
				of = append(of, map[string]any{"op": "exists", "field": "/value"})
			}
			when = map[string]any{"op": "all", "of": of}
		}
		list = append(list, map[string]any{"when": when, "claim": map[string]any{"facts": []any{}, "evidence": map[string]any{}, "acquisitionStatus": "unknown"}, "reason": ""})
	}
	return map[string]any{"ruleVersion": "1", "parameters": map[string]any{}, "clauses": list}
}

func TestRetainedColumnDraftExtendedStructuralCeilings(t *testing.T) {
	cases := []struct {
		name string
		max  int
		make func(int) map[string]any
	}{
		{"sources", 32, func(n int) map[string]any {
			d := draftExtendedDocument()
			sources := []any{}
			for i := 0; i < n; i++ {
				sources = append(sources, draftOperation(fmt.Sprint("s", i)))
			}
			d["sources"] = sources
			return d
		}},
		{"parameters", 128, func(n int) map[string]any {
			d := draftExtendedDocument()
			params := map[string]any{}
			for i := 1; i < n; i++ {
				params[fmt.Sprint("p", i)] = map[string]any{"pointer": "/value", "type": "integer"}
			}
			d["case"].(map[string]any)["parameters"] = params
			return d
		}},
		{"fact_targets", 512, func(n int) map[string]any {
			d := draftExtendedDocument()
			facts := []any{}
			for i := 0; i < n; i++ {
				facts = append(facts, map[string]any{"target": fmt.Sprint("/f", i), "source": "/retained", "conversion": map[string]any{"contractVersion": "1", "column": "Column", "kind": "boolean", "trueValue": "YES", "falseValue": "NO"}})
			}
			d["case"].(map[string]any)["facts"] = facts
			return d
		}},
		{"evidence_targets", 256, func(n int) map[string]any {
			d := draftExtendedDocument()
			evidence := []any{}
			for i := 0; i < n; i++ {
				evidence = append(evidence, map[string]any{"requirement": fmt.Sprint("e", i), "source": "/retained", "availability": map[string]any{"contractVersion": "1", "column": "Column"}})
			}
			d["case"].(map[string]any)["evidence"] = evidence
			return d
		}},
		{"unwrap", 8, func(n int) map[string]any {
			d := draftExtendedDocument()
			s := draftOperation("s")
			unwrap := []any{}
			for i := 0; i < n; i++ {
				unwrap = append(unwrap, "")
			}
			s["read"].(map[string]any)["unwrap"] = unwrap
			d["sources"] = []any{s}
			return d
		}},
		{"clauses", 128, func(n int) map[string]any {
			d := draftExtendedDocument()
			s := draftOperation("s")
			s["read"] = map[string]any{"rule": draftRule(n, 0)}
			d["sources"] = []any{s}
			return d
		}},
		{"conditions", 128, func(n int) map[string]any {
			d := draftExtendedDocument()
			s := draftOperation("s")
			s["read"] = map[string]any{"rule": draftRule(2, n)}
			d["sources"] = []any{s}
			return d
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.make(tc.max)
			if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), draftExtended()); code != 0 {
				t.Fatal("extended boundary refused", r)
			}
			d["profile"] = "base"
			if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase()); code == 0 || r.Diagnostics[0].Code != "input-over-bound" {
				t.Fatal("base ceiling widened", r)
			}
			if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, tc.make(tc.max+1)), draftExtended()); code == 0 || r.Diagnostics[0].Code != "input-over-bound" {
				t.Fatal("extended ceiling missing", r)
			}
		})
	}
}

func TestRetainedColumnDraftExtendedResearchCeilings(t *testing.T) {
	cases := []struct {
		name string
		max  int
		set  func(map[string]any, int)
	}{
		{"fields", 128, func(s map[string]any, n int) {
			fields := []any{}
			for i := 0; i < n; i++ {
				name := "retention"
				if i > 0 {
					name = fmt.Sprint("field", i)
				}
				fields = append(fields, map[string]any{"name": name, "type": "integer", "required": true, "description": "Field."})
			}
			s["fields"] = fields
		}},
		{"field_name", 128, func(s map[string]any, n int) {
			s["fields"].([]any)[0].(map[string]any)["name"] = strings.Repeat("x", n)
			s["read"] = map[string]any{"copy": map[string]any{"facts": []any{}, "evidence": []any{}}}
		}},
		{"description", 4096, func(s map[string]any, n int) {
			s["fields"].([]any)[0].(map[string]any)["description"] = strings.Repeat("x", n)
		}},
		{"model", 512, func(s map[string]any, n int) { s["model"] = strings.Repeat("x", n) }},
		{"instruction", 8192, func(s map[string]any, n int) { s["evidence"] = []any{strings.Repeat("x", n)} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			makeDoc := func(n int) map[string]any {
				d := researchSample(t)
				d["profile"] = "extended"
				delete(d, "admits")
				tc.set(sourceAt(d, 0), n)
				return d
			}
			d := makeDoc(tc.max)
			if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), draftExtended()); code != 0 {
				t.Fatal("extended research boundary refused", r)
			}
			d["profile"] = "base"
			if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase()); code == 0 || r.Diagnostics[0].Code != "input-over-bound" {
				t.Fatal("base research ceiling widened", r)
			}
			if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, makeDoc(tc.max+1)), draftExtended()); code == 0 || r.Diagnostics[0].Code != "input-over-bound" {
				t.Fatal("extended research ceiling missing", r)
			}
		})
	}
	// A larger resource ceiling invents no capability names or repeated assignments.
	d := researchSample(t)
	d["profile"] = "extended"
	sourceAt(d, 0)["capabilities"] = []any{"answer", "search", "page", "answer"}
	if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), draftExtended()); code == 0 || r.Diagnostics[0].Code != "input-malformed" {
		t.Fatal("closed capability vocabulary widened", r)
	}
}

func TestRetainedColumnDraftExtendedIdentityAndWork(t *testing.T) {
	d := draftExtendedDocument()
	d["formatVersion"], d["id"], d["version"] = "2.0.0", strings.Repeat("x", 2048), "1.0.0"
	if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), draftExtended()); code != 0 || r.ArtifactID != d["id"] {
		t.Fatal("extended artifact ID refused", r)
	}
	d["id"] = strings.Repeat("x", 2049)
	if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), draftExtended()); code == 0 || r.Diagnostics[0].Code != "JPS-DIALECT-ID" {
		t.Fatal("extended artifact ID ceiling missing", r)
	}
	// Nested operation arguments exercise the actual document parser and template walk.
	for _, depth := range []int{128, 129} {
		// root, sources array, source object, arguments object account for four levels.
		raw := `{"version":2,"profile":"extended","case":{"facts":[],"evidence":[]},"sources":[{"name":"s","kind":"operation","profile":"p","profileDigest":"sha256:` + strings.Repeat("a", 64) + `","maxAge":1,"arguments":{"value":` + strings.Repeat("[", depth-4) + `0` + strings.Repeat("]", depth-4) + `},"read":{"copy":{"facts":[],"evidence":[]}}}]}`
		r, code := ValidateRetainedColumnDraft("test", []byte(raw), draftExtended())
		if (code == 0) != (depth == 128) || code != 0 && r.Diagnostics[0].Code != "JPS-RESOURCE-DEPTH-LIMIT" {
			t.Fatal("extended depth boundary changed", depth, r)
		}
	}
	for _, n := range []int{RetainedColumnDraftExtendedStringBytes, RetainedColumnDraftExtendedStringBytes + 1} {
		d := draftExtendedDocument()
		s := draftOperation("s")
		s["arguments"].(map[string]any)["value"] = strings.Repeat("x", n)
		d["sources"] = []any{s}
		r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), draftExtended())
		if (code == 0) != (n == RetainedColumnDraftExtendedStringBytes) || code != 0 && r.Diagnostics[0].Code != "JPS-RESOURCE-STRING-LIMIT" {
			t.Fatal("extended string work budget changed", n, r)
		}
	}
	// Carrier counts values and containers, not keys. This document has exactly
	// 19 nodes before its argument-array elements, derived from its written shape.
	for _, n := range []int{RetainedColumnDraftExtendedNodes - 19, RetainedColumnDraftExtendedNodes - 18} {
		raw := `{"version":2,"profile":"extended","case":{"facts":[],"evidence":[]},"sources":[{"name":"s","kind":"operation","profile":"p","profileDigest":"sha256:` + strings.Repeat("a", 64) + `","maxAge":1,"arguments":{"value":[` + strings.Repeat("0,", n-1) + `0]},"read":{"copy":{"facts":[],"evidence":[]}}}]}`
		r, code := ValidateRetainedColumnDraft("test", []byte(raw), draftExtended())
		if (code == 0) != (n == RetainedColumnDraftExtendedNodes-19) || code != 0 && r.Diagnostics[0].Code != "JPS-RESOURCE-NODE-LIMIT" {
			t.Fatal("extended node work boundary changed", n, r)
		}
	}
	// Same valid document with explicit and absent base declarations has distinct identity.
	base := draftDocument("text")
	a, code := ValidateRetainedColumnDraft("test", draftJSON(t, base), RetainedColumnDraftBase())
	if code != 0 {
		t.Fatal(a)
	}
	base["profile"] = "base"
	b, code := ValidateRetainedColumnDraft("test", draftJSON(t, base), RetainedColumnDraftBase())
	if code != 0 || a.Digest == b.Digest {
		t.Fatal("profile declaration identity changed", b)
	}
	for _, selection := range []RetainedColumnDraftSelection{RetainedColumnDraftBase(), draftExtended()} {
		doc := draftExtendedDocument()
		if selection.Profile == "extended" {
			delete(doc, "profile")
		}
		if r, code := ValidateRetainedColumnDraft("test", draftJSON(t, doc), selection); code == 0 || r.Diagnostics[0].Code != "input-malformed" {
			t.Fatal("trusted/document profile mismatch admitted", r)
		}
	}
}

func TestRetainedColumnDraftExtendedByteBounds(t *testing.T) {
	doc := draftJSON(t, draftExtendedDocument())
	exact := append(append([]byte{}, doc...), bytes.Repeat([]byte(" "), RetainedColumnDraftExtendedDocumentBytes-len(doc))...)
	if r, code := ValidateRetainedColumnDraft("test", exact, draftExtended()); code != 0 || r.Limits.DocumentBytes != 2097152 {
		t.Fatal("extended document boundary refused", r)
	}
	if r, code := ValidateRetainedColumnDraft("test", append(exact, ' '), draftExtended()); code == 0 || r.Diagnostics[0].Code != "input-over-bound" {
		t.Fatal("extended document byte bound missing", r)
	}
	frame := []byte(`{"contractVersion":"1","origin":"asserted","values":{},"research":null}`)
	exact = append(append([]byte{}, frame...), bytes.Repeat([]byte(" "), RetainedColumnDraftExtendedFrameBytes-len(frame))...)
	if r, code := ValidateRetainedColumnDraftFrame("test", exact, draftExtended()); code != 0 || r.Limits.FrameBytes != 4194304 {
		t.Fatal("extended frame boundary refused", r)
	}
	if r, code := ValidateRetainedColumnDraftFrame("test", append(exact, ' '), draftExtended()); code == 0 || r.Diagnostics[0].Code != "input-over-bound" {
		t.Fatal("extended frame byte bound missing", r)
	}
	// The large opaque member is retained exactly and never parsed as JSON.
	f := draftGenerated()
	large := strings.Repeat("x", RetainedColumnDraftFrameBytes+1)
	draftResearch(f)["fields"].(map[string]any)["Column"] = map[string]any{"status": "supported", "valueJson": large}
	if r, code := ValidateRetainedColumnDraftFrame("test", draftJSON(t, f), draftExtended()); code != 0 || *r.Frame.Research.Fields["Column"].ValueJSON != large {
		t.Fatal("extended opaque string interpreted or clipped", r)
	}
	if r, code := ValidateRetainedColumnDraftFrame("test", draftJSON(t, f), RetainedColumnDraftBase()); code == 0 || r.Diagnostics[0].Code != "input-over-bound" {
		t.Fatal("base frame widened", r)
	}
}

func TestRetainedColumnDraftExtendedReconciliation(t *testing.T) {
	for _, n := range []int{30, 31} {
		d := researchSample(t)
		d["profile"] = "extended"
		delete(d, "admits")
		sources, names := []any{}, []any{}
		for i := 0; i < n; i++ {
			s := sourceAt(researchSample(t), 0)
			name := fmt.Sprint("r", i)
			s["name"] = name
			s["read"] = map[string]any{"copy": map[string]any{"facts": []any{}, "evidence": []any{}}}
			sources = append(sources, s)
			names = append(names, name)
		}
		last := sourceAt(researchSample(t), 0)
		last["name"] = "reconciled"
		last["read"] = map[string]any{"sources": names, "rule": draftRule(1, 0)}
		d["sources"] = append(sources, last)
		r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), draftExtended())
		if (code == 0) != (n == 30) || code != 0 && (r.Diagnostics[0].Code != "input-over-bound" || r.Diagnostics[0].InstancePath != "/sources/31/read/sources") {
			t.Fatal("earlier reconciliation source boundary changed", n, r)
		}
	}
}

func TestRetainedColumnDraftExtendedAggregateTargets(t *testing.T) {
	for _, evidence := range []bool{false, true} {
		max := 512
		if evidence {
			max = 256
		}
		for _, n := range []int{max, max + 1} {
			d := draftExtendedDocument()
			s := draftOperation("s")
			d["sources"] = []any{s}
			c := d["case"].(map[string]any)
			other := s["read"].(map[string]any)["copy"].(map[string]any)
			for i := 0; i < n; i++ {
				dest := c
				if i >= max/2 {
					dest = other
				}
				if evidence {
					dest["evidence"] = append(dest["evidence"].([]any), map[string]any{"requirement": fmt.Sprint("e", i), "source": ""})
				} else {
					dest["facts"] = append(dest["facts"].([]any), map[string]any{"target": fmt.Sprint("/f", i), "source": ""})
				}
			}
			r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), draftExtended())
			if (code == 0) != (n == max) || code != 0 && (r.Diagnostics[0].Code != "input-over-bound" || r.Diagnostics[0].InstancePath != "") {
				t.Fatal("aggregate target ceiling changed", evidence, n, r)
			}
		}
	}
}

func TestRetainedColumnDraftMaximumEscapedPolicies(t *testing.T) {
	for _, profile := range []string{"base", "extended"} {
		selection := RetainedColumnDraftBase()
		factsMax, evidenceMax := 256, 128
		if profile == "extended" {
			selection = draftExtended()
			factsMax, evidenceMax = 512, 256
		}
		d := draftExtendedDocument()
		d["profile"] = profile
		c := d["case"].(map[string]any)
		for i := 0; i < factsMax; i++ {
			distinct := strings.Repeat("\x00", 157) + fmt.Sprintf("%03d", i)
			c["facts"] = append(c["facts"].([]any), map[string]any{"target": fmt.Sprint("/f", i), "source": "/retained", "conversion": map[string]any{"contractVersion": "1", "column": distinct, "kind": "boolean", "trueValue": distinct, "falseValue": strings.Repeat("\x01", 160)}})
		}
		for i := 0; i < evidenceMax; i++ {
			c["evidence"] = append(c["evidence"].([]any), map[string]any{"requirement": strings.Repeat("\x00", 157) + fmt.Sprintf("%03d", i), "source": "/retained", "availability": map[string]any{"contractVersion": "1", "column": strings.Repeat("\x00", 160)}})
		}
		raw := draftJSON(t, d)
		r, code := ValidateRetainedColumnDraft("test", raw, selection)
		if code != 0 || len(raw) <= 262144 {
			t.Fatal("maximum escaped policies refused", profile, len(raw), r)
		}
	}
}
