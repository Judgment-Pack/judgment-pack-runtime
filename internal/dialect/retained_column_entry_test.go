package dialect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func TestRetainedColumnEntryPackTargets(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	pack := checkPack(t, "/converted", true)
	for _, kind := range []string{"text", "decimal", "number", "boolean", "research-value"} {
		for _, version := range []int{2, 3} {
			for _, named := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/%v", kind, version, named), func(t *testing.T) {
					d := draftDocument(kind)
					d["version"] = version
					d["case"].(map[string]any)["evidence"].([]any)[0].(map[string]any)["requirement"] = "receipt"
					if named {
						d["formatVersion"] = fmt.Sprintf("%d.0.0", version)
						d["id"] = "urn:test:converted"
						d["version"] = "1.0.0"
					}
					r, code := CheckRetainedColumnDraft("test", draftJSON(t, d), pack, engine, RetainedColumnDraftBase())
					if code != 0 || r.Status != "complete" || r.Compatibility == nil || !reflect.DeepEqual(r.Compatibility.Facts, []CoverageEntry{{Target: "/converted", Status: "mapped", Producers: []string{"/converted"}, AcknowledgedBy: []string{}}}) || !reflect.DeepEqual(r.Compatibility.Evidence, []CoverageEntry{{Target: "receipt", Status: "mapped", Producers: []string{"receipt"}, AcknowledgedBy: []string{}}}) {
						t.Fatalf("converted targets did not reach ordinary inventory: code=%d %+v", code, r)
					}
					if r.Compatibility.ValueTypesChecked || r.Compatibility.ExecutionChecked || !r.Compatibility.InventoryOverApproximates || r.FullDocumentConformance || r.Maturity != "draft" || r.Spec.Revision != RetainedColumnDraftRevision {
						t.Fatal("overstated Draft check", r)
					}
					encoded, code := EncodeRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase())
					if code != 0 || encoded.Document != r.Document || encoded.Digest != r.Digest || encoded.ValidationScope != "experimental-retained-column-base-encoding-only" {
						t.Fatal("checked canonical identity drift", encoded, r)
					}
				})
			}
		}
	}
	// A converted read in a real operation source reaches the same coverage walk.
	d := sampleDocument(t)
	first := sourceAt(d, 0)["read"].(map[string]any)["copy"].(map[string]any)
	first["facts"].([]any)[0].(map[string]any)["conversion"] = draftConversion(draftDocument("text"))
	r, code := CheckRetainedColumnDraft("test", draftJSON(t, d), checkPack(t, "/first", false), engine, RetainedColumnDraftBase())
	// Other sample evidence targets remain undeclared in this pack and keep the
	// ordinary incomplete result; the converted source fact must still be mapped.
	if code != 1 || r.Status != "incomplete" || r.Compatibility == nil || r.Compatibility.Facts[0].Target != "/first" || r.Compatibility.Facts[0].Status != "mapped" {
		t.Fatal("source converted target lost", code, r)
	}
}

func TestRetainedColumnEntrySelectionBeforeAdmission(t *testing.T) {
	engine, _ := validation.NewEngine()
	for _, tc := range []struct{ selector, profile, want string }{
		{"", "base", "capability-unsupported"}, {"retained-column/1", "base", "capability-unsupported"}, {RetainedColumnDraftSelector + "extra", "base", "capability-unsupported"},
		{RetainedColumnDraftSelector, "", "input-malformed"}, {RetainedColumnDraftSelector, "unknown", "input-malformed"},
	} {
		r, selection, code := SelectRetainedColumnDraft("test", tc.selector, tc.profile)
		if code == 0 || r.Diagnostics[0].Code != tc.want {
			t.Fatal("public selector admitted", r)
		}
		checked, code := CheckRetainedColumnDraft("test", []byte("{"), bytes.Repeat([]byte("x"), int(MaxPackBytes)+1), engine, selection)
		if code == 0 || checked.Diagnostics[0].Code != tc.want || checked.PackValidation != nil || checked.Compatibility != nil {
			t.Fatal("selection did not precede artifacts", checked)
		}
	}
	r, code := ContractRetainedColumnDraft("test", RetainedColumnDraftBase())
	raw, _ := json.Marshal(r)
	var wire map[string]any
	json.Unmarshal(raw, &wire)
	caps := wire["capabilities"].(map[string]any)
	entries := caps["conversionContracts"].([]any)
	entry := entries[0].(map[string]any)
	if code != 0 || len(entries) != 1 || len(entry) != 5 || entry["contract"] != "retained-column/1" || entry["role"] != "document-validation" || entry["profile"] != "base" || len(entry["limits"].(map[string]any)) != 2 || strings.Contains(string(raw), "preparationBytes") || strings.Contains(string(raw), "combinedBytes") {
		t.Fatal("unsupported capability claim", string(raw))
	}
	old, _ := json.Marshal(Contract("test"))
	if strings.Contains(string(old), "conversionContracts") || strings.Contains(string(old), "retained-column") {
		t.Fatal("legacy default changed", string(old))
	}
}

func TestRetainedColumnEntryCheckRefusals(t *testing.T) {
	engine, _ := validation.NewEngine()
	pack := checkPack(t, "/converted", false)
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"unknown kind", func(d map[string]any) { draftConversion(d)["kind"] = "future" }},
		{"unknown version", func(d map[string]any) { draftConversion(d)["contractVersion"] = "2" }},
		{"unknown key", func(d map[string]any) { draftConversion(d)["extra"] = true }},
		{"mixed quality", func(d map[string]any) { draftFact(d)["qualityTarget"] = "/quality" }},
		{"mixed copy", func(d map[string]any) {
			draftFact(d)["availability"] = map[string]any{"contractVersion": "1", "column": "Column"}
		}},
		{"overlap", func(d map[string]any) {
			c := d["case"].(map[string]any)
			c["facts"] = append(c["facts"].([]any), map[string]any{"target": "/converted/child", "source": "/v"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := draftDocument("boolean")
			tc.change(d)
			r, code := CheckRetainedColumnDraft("test", draftJSON(t, d), pack, engine, RetainedColumnDraftBase())
			if code != result.ExitInvalid || r.Diagnostics[0].Code != "input-malformed" || r.Compatibility != nil || r.PackValidation != nil || r.Digest != "" {
				t.Fatal("invalid converted document reached pack", r)
			}
		})
	}
	d := draftDocument("text")
	d["case"].(map[string]any)["evidence"] = []any{}
	for _, tc := range []struct {
		name string
		pack []byte
		want string
	}{
		{"invalid", []byte("{"), "JPS-DIALECT-PACK-INVALID"},
		{"unsupported", []byte(`{"specVersion":"future"}`), "JPS-DIALECT-PACK-INVALID"},
		{"oversized", bytes.Repeat([]byte(" "), int(MaxPackBytes)+1), "JPS-DIALECT-PACK-BYTE-LIMIT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, code := CheckRetainedColumnDraft("test", draftJSON(t, d), tc.pack, engine, RetainedColumnDraftBase())
			if code == 0 || r.Diagnostics[0].Code != tc.want || r.Compatibility != nil {
				t.Fatal("pack gate bypassed", r)
			}
		})
	}
	// Actual inventory mismatch remains incomplete; declaring a different target cannot satisfy it.
	r, code := CheckRetainedColumnDraft("test", draftJSON(t, d), checkPack(t, "/missing", false), engine, RetainedColumnDraftBase())
	if code != 1 || r.Status != "incomplete" || r.Compatibility.Facts[0].Status != "missing" {
		t.Fatal("coverage was fabricated", r)
	}
}

func TestRetainedColumnEntryCanonicalAndLegacyIdentity(t *testing.T) {
	base := draftDocument("boolean")
	first, code := EncodeRetainedColumnDraft("test", draftJSON(t, base), RetainedColumnDraftBase())
	if code != 0 {
		t.Fatal(first)
	}
	for _, change := range []func(map[string]any){
		func(d map[string]any) { draftConversion(d)["trueValue"] = "Yes" },
		func(d map[string]any) {
			draftConversion(d)["kind"] = "text"
			delete(draftConversion(d), "trueValue")
			delete(draftConversion(d), "falseValue")
		},
		func(d map[string]any) { draftFact(d)["source"] = "/other" },
		func(d map[string]any) { draftConversion(d)["column"] = "Other" },
	} {
		d := draftDocument("boolean")
		change(d)
		r, code := ValidateRetainedColumnDraft("test", draftJSON(t, d), RetainedColumnDraftBase())
		if code != 0 || r.Digest == first.Digest || r.Document == first.Document {
			t.Fatal("conversion policy omitted from identity", r)
		}
	}
	// These exact historical bytes and canonical identities are handwritten.
	for _, raw := range []string{
		`{"case":{"evidence":[],"facts":[{"source":"/raw","target":"/value"}]},"version":2}`,
		`{"case":{"evidence":[],"facts":[{"source":"/raw","target":"/value"}]},"version":3}`,
		`{"case":{"evidence":[],"facts":[{"source":"/raw","target":"/value"}]},"formatVersion":"2.0.0","id":"urn:test:old","version":"1.2.3"}`,
		`{"case":{"evidence":[],"facts":[{"source":"/raw","target":"/value"}]},"formatVersion":"3.0.0","id":"urn:test:old","version":"1.2.3"}`,
	} {
		old, oldCode := Validate("test", []byte(raw))
		fresh, freshCode := ValidateRetainedColumnDraft("test", []byte(raw), RetainedColumnDraftBase())
		if oldCode != 0 || freshCode != 0 || old.Document != raw || fresh.Document != raw || old.Digest != retainedDraftDigest(raw) || fresh.Digest != old.Digest {
			t.Fatal("historical identity changed", old, fresh)
		}
	}
}
