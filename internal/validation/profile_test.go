package validation

import (
	"encoding/json"
	"strings"
	"testing"
)

// §4.1: negotiation precedes schema interpretation; the rest of these packs
// intentionally lacks required members so fallback into schema validation fails.
func TestCore030ProfileNegotiationBeforeSchema(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, profiles := range [][]string{nil, {"base"}, {"base", "extended"}} {
		report, operational := engine.Validate([]byte(`{"specVersion":"0.3.0","profile":"extended","outcomes":false}`), Options{SupportedProfiles: profiles})
		if operational != nil {
			t.Fatal(operational)
		}
		encoded, err := json.Marshal(report.Diagnostics)
		if err != nil {
			t.Fatal(err)
		}
		if report.Status != "unsupported" || string(encoded) != `[{"code":"profile-unsupported","profile":"extended"}]` {
			t.Fatalf("negotiation: %s %+v", encoded, report)
		}
	}
	report, operational := engine.Validate([]byte(`{"specVersion":"0.3.0"}`), Options{SupportedProfiles: []string{}})
	if operational != nil || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "profile-unsupported" || report.Diagnostics[0].Profile != "base" {
		t.Fatalf("implicit unsupported base: %+v %v", report, operational)
	}
	for _, profile := range []string{`null`, `""`, `"future"`, `3`, `true`, `{}`, `[]`} {
		report, operational := engine.Validate([]byte(`{"specVersion":"0.3.0","profile":`+profile+`}`), Options{})
		if operational != nil || report.Status != "invalid" || len(report.Diagnostics) != 1 || report.Diagnostics[0].Layer != "structural" {
			t.Fatalf("invalid profile %s: %+v %v", profile, report, operational)
		}
	}
	report, operational = engine.Validate([]byte(`{"specVersion":"0.3.0","profile":"extended",`), Options{})
	if operational != nil || report.Status != "invalid" || len(report.Diagnostics) != 1 || report.Diagnostics[0].Layer != "carrier" {
		t.Fatalf("syntax refusal: %+v %v", report, operational)
	}
}

func TestCore030BaseRouteSchemaBoundsAndClosure(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	pack := `{"specVersion":"0.3.0","id":"https://example.com/packs/routes","version":"1.0.0","title":"Routes","decision":{"intent":"Review","question":"Review?"},"outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"rules":[{"id":"one","description":"One","when":{"op":"literal","value":true},"outcome":"yes","onUnknown":"ignore"}],"escalation":{"triggers":["unknown"],"routes":[ROUTES]}}`
	route := `{"target":{"kind":"queue","name":"Review"}}`
	for _, declaration := range []string{"", `,"profile":"base"`} {
		raw := strings.Replace(pack, "ROUTES", route, 1)
		raw = strings.Replace(raw, `"specVersion":"0.3.0"`, `"specVersion":"0.3.0"`+declaration, 1)
		report, operational := engine.Validate([]byte(raw), Options{})
		if operational != nil || report.Status != "valid" {
			t.Fatalf("implicit/explicit base %s: %+v %v", declaration, report, operational)
		}
	}

	for _, count := range []int{0, 1, 8, 9, 16, 17} {
		routes := make([]string, count)
		for i := range routes {
			routes[i] = route
		}
		report, operational := engine.Validate([]byte(strings.Replace(pack, "ROUTES", strings.Join(routes, ","), 1)), Options{})
		if operational != nil || (report.Status == "valid") != (count >= 1 && count <= 8) {
			t.Fatalf("%d routes: %+v %v", count, report, operational)
		}
	}
	for _, route := range []string{
		`{"target":{"kind":"queue","name":"Review"},"triggers":["no-match"]}`,
		`{"target":{"kind":"queue","name":"Review"},"triggers":[]}`,
		`{"target":{"kind":"queue","name":"Review"},"triggers":["unknown","unknown"]}`,
		`{"target":{"kind":"queue","name":"Review","extra":true}}`,
		`{"target":{"kind":"email","name":"Review"}}`,
		`{"target":{"kind":"queue","name":""}}`,
		`{"target":{"kind":"queue","name":"Review"},"extra":true}`,
	} {
		report, operational := engine.Validate([]byte(strings.Replace(pack, "ROUTES", route, 1)), Options{})
		if operational != nil || report.Status != "invalid" {
			t.Fatalf("admitted illegal route %s: %+v %v", route, report, operational)
		}
	}
}
