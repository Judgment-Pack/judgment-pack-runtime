package dialect

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

// Exercise the published bytes in this owner's document-validation role. Typed
// conversion expectations belong to preview/preparation owners, not this reader.
func TestRetainedColumnRC2PublishedCorpus(t *testing.T) {
	raw, err := fs.ReadFile(retainedColumnArtifacts, "retained-column/official/conformance/retained-column/1/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			ID               string  `json:"id"`
			Kind             string  `json:"kind"`
			Type             string  `json:"type"`
			Origin           string  `json:"origin"`
			Column           string  `json:"column"`
			Status           string  `json:"status"`
			InputJSON        *string `json:"inputJson"`
			InputState       string  `json:"inputState"`
			RevisionMismatch bool    `json:"revisionMismatch"`
			Undeclared       bool    `json:"undeclared"`
			FieldEntryAbsent bool    `json:"fieldEntryAbsent"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 72 {
		t.Fatal("published case inventory changed", len(corpus.Cases))
	}
	kinds, types := map[string]bool{}, map[string]bool{}
	for _, tc := range corpus.Cases {
		for _, profile := range []string{"base", "extended"} {
			t.Run(tc.ID+"/"+profile, func(t *testing.T) {
				selection := RetainedColumnDraftBase()
				selection.Profile = profile
				kind := tc.Kind
				if kind == "evidence-availability" {
					kind = "text"
				} else {
					kinds[kind] = true
				}
				document := draftDocument(kind)
				if profile == "extended" {
					document["profile"] = profile
				}
				if r, code := ValidateRetainedColumnDraft("published-corpus", draftJSON(t, document), selection); code != 0 {
					t.Fatal("conversion document refused", r.Diagnostics)
				}
				column := tc.Column
				if column == "" {
					column = "Column"
				}
				origin := tc.Origin
				if origin == "" {
					origin = "asserted"
					if tc.Kind == "research-value" {
						origin = "generated"
					}
				}
				frame := map[string]any{"contractVersion": "1", "origin": origin, "values": map[string]any{}, "research": nil}
				values := frame["values"].(map[string]any)
				// Preserve the corpus's raw spelling inside opaque strings. Runtime never
				// interprets malformed typed raw JSON or substitutes the preview's answer.
				if tc.InputState != "missing" && tc.InputJSON != nil {
					var text *string
					if json.Unmarshal([]byte(*tc.InputJSON), &text) == nil {
						values[column] = text
					}
				}
				if origin == "generated" {
					revision := "recipe:7"
					declarationRevision := revision
					if tc.RevisionMismatch {
						declarationRevision = "recipe:8"
					}
					declarations := []any{}
					if !tc.Undeclared && tc.Type != "" {
						types[tc.Type] = true
						fieldJSON := string(draftJSON(t, map[string]any{"name": column, "type": tc.Type}))
						declarations = append(declarations, map[string]any{"name": column, "fieldJson": fieldJSON})
					}
					fields := map[string]any{}
					if !tc.FieldEntryAbsent && tc.ID != "planned-research-zero-records" {
						status := tc.Status
						if status == "" {
							status = "supported"
						}
						entry := map[string]any{"status": status}
						if tc.InputJSON != nil {
							entry["valueJson"] = *tc.InputJSON
						}
						fields[column] = entry
					}
					frame["research"] = map[string]any{"recordRecipeRevision": revision, "declaration": map[string]any{"recipeRevision": declarationRevision, "fields": declarations}, "fields": fields}
					if tc.ID == "planned-research-zero-records" {
						frame["research"] = map[string]any{"recordRecipeRevision": revision, "declaration": nil, "fields": map[string]any{}}
					}
				}
				encoded := draftJSON(t, frame)
				r, code := ValidateRetainedColumnDraftFrame("published-corpus", encoded, selection)
				if code != 0 || r.Document != string(encoded) || r.Digest != retainedDraftDigest(string(encoded)) || r.Frame == nil || r.FrameSummary == nil {
					t.Fatal("published frame structure/preservation failed", r.Diagnostics)
				}
				if tc.InputJSON != nil && origin == "generated" && !tc.FieldEntryAbsent {
					if got := r.Frame.Research.Fields[column].ValueJSON; got == nil || *got != *tc.InputJSON {
						t.Fatal("opaque typed provenance lost")
					}
				}
			})
		}
	}
	for _, kind := range []string{"text", "decimal", "number", "boolean", "research-value"} {
		if !kinds[kind] {
			t.Fatal("conversion kind lost", kind)
		}
	}
	for _, typ := range []string{"text", "decimal", "number", "boolean", "integer", "text-list", "range", "string"} {
		if !types[typ] {
			t.Fatal("original type lost", typ)
		}
	}
}

func TestRetainedColumnRC2PublishedRawValueBounds(t *testing.T) {
	raw, err := fs.ReadFile(retainedColumnArtifacts, "retained-column/official/conformance/retained-column/1/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Bounds []struct {
			ID            string `json:"id"`
			Origin        string `json:"origin"`
			Unit          string `json:"unit"`
			Repeat        int    `json:"repeat"`
			Suffix        string `json:"suffix"`
			ListItems     int    `json:"listItems"`
			ItemBytes     int    `json:"itemBytes"`
			ExpectedBytes int    `json:"expectedBytes"`
			Profile       string `json:"profile"`
			Expected      struct {
				Refusal string `json:"refusal"`
			} `json:"expected"`
		} `json:"rawValueBounds"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Bounds) != 11 {
		t.Fatal("published bound inventory changed")
	}
	for _, tc := range corpus.Bounds {
		for _, profile := range []string{"base", "extended"} {
			t.Run(tc.ID+"/"+profile, func(t *testing.T) {
				value := strings.Repeat(tc.Unit, tc.Repeat) + tc.Suffix
				if tc.ListItems != 0 {
					items := make([]string, tc.ListItems)
					for i := range items {
						items[i] = strings.Repeat("x", tc.ItemBytes)
					}
					value = string(draftJSON(t, items))
				}
				if tc.ExpectedBytes != 0 && len(value) != tc.ExpectedBytes {
					t.Fatal("published byte recipe changed", len(value))
				}
				frame := map[string]any{"contractVersion": "1", "origin": tc.Origin, "values": map[string]any{"Column": value}, "research": nil}
				if tc.Origin == "generated" {
					frame["research"] = map[string]any{"recordRecipeRevision": "r", "declaration": nil, "fields": map[string]any{"Opaque": map[string]any{"status": "supported", "valueJson": "{invalid typed raw"}}}
				}
				selection := RetainedColumnDraftBase()
				selection.Profile = profile
				encoded := draftJSON(t, frame)
				r, code := ValidateRetainedColumnDraftFrame("published-bounds", encoded, selection)
				if tc.Expected.Refusal != "" {
					if code == 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != tc.Expected.Refusal || r.Diagnostics[0].InstancePath != "/values/Column" {
						t.Fatal("bound not enforced", code, r.Diagnostics)
					}
				} else if code != 0 || r.Frame == nil || *r.Frame.Values["Column"] != value || r.Document != string(encoded) || r.Digest != retainedDraftDigest(string(encoded)) {
					t.Fatal("admitted value not preserved", code, r.Diagnostics)
				}
			})
		}
	}
}

func TestRetainedColumnRC2Identity(t *testing.T) {
	r, code := ContractRetainedColumnDraft("test", RetainedColumnDraftBase())
	if code != 0 || r.Spec.Version != "0.3.0-rc.2" || r.Maturity != "draft" || r.Specification != fmt.Sprintf("https://github.com/Judgment-Pack/judgment-pack-spec/blob/%s/spec/retained-column.md", RetainedColumnDraftRevision) || r.Role != "document-validation" {
		t.Fatal("wrong installed role identity", r)
	}
	if _, _, code := SelectRetainedColumnDraft("test", "retained-column/1@ade3914b0b81ef299e5b9a8d6147f37ff3f0976c", "base"); code == 0 {
		t.Fatal("historical selector accepted as installed RC2")
	}
}

func TestRetainedColumnRC2PublishedCapabilityRole(t *testing.T) {
	raw, err := fs.ReadFile(retainedColumnArtifacts, "retained-column/official/conformance/retained-column/1/capabilities.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Contract     string                    `json:"contract"`
		Distribution string                    `json:"distribution"`
		Maturity     string                    `json:"maturity"`
		Profiles     map[string]map[string]int `json:"profiles"`
		Roles        map[string][]string       `json:"roles"`
	}
	if err = json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	role := table.Roles["document-validation"]
	if table.Contract != "retained-column/1" || table.Distribution != "v0.3.0-rc.2" || table.Maturity != "draft" || len(role) != 2 || role[0] != "documentBytes" || role[1] != "frameBytes" {
		t.Fatal("published role table changed")
	}
	for _, profile := range []string{"base", "extended"} {
		selection := RetainedColumnDraftBase()
		selection.Profile = profile
		r, code := ContractRetainedColumnDraft("published-role", selection)
		if code != 0 || r.DraftCapabilities == nil || len(r.DraftCapabilities.ConversionContracts) != 1 {
			t.Fatal("closed published role missing")
		}
		capability := r.DraftCapabilities.ConversionContracts[0]
		if capability.Contract != table.Contract || capability.Spec != r.Spec || capability.Profile != profile || capability.Role != "document-validation" || capability.Limits.DocumentBytes != table.Profiles[profile]["documentBytes"] || capability.Limits.FrameBytes != table.Profiles[profile]["frameBytes"] {
			t.Fatal("reported role differs from installed table", capability)
		}
	}
}
