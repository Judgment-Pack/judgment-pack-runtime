package dialect

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestVersion3IdentitySpellingRefusesWithoutArtifacts(t *testing.T) {
	for _, tc := range []struct{ members, path string }{
		{`"provider":""`, "/provider"}, {`"facts":null`, "/facts"}, {`"evidence":null`, "/evidence"},
		{`"sources":[]`, "/sources"}, {`"unmapped":[]`, "/unmapped"}, {`"unmappedEvidence":[]`, "/unmappedEvidence"},
		{`"case":{"parameters":{}}`, "/case/parameters"}, {`"sources":[{"parameters":{}}]`, "/sources/0/parameters"},
		{`"sources":[{}, {"parameters":{}}]`, "/sources/1/parameters"},
		{`"admits":{"facts":{}}`, "/admits/facts"}, {`"admits":{"evidence":{}}`, "/admits/evidence"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			raw := `{"version":3,` + tc.members + `}`
			r, code := Encode("test", []byte(raw))
			if code != 1 || r.Document != "" || r.Digest != "" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "JPS-DIALECT-V3-SPELLING" || r.Diagnostics[0].InstancePath != tc.path {
				t.Fatalf("%s: %d %+v", raw, code, r)
			}
			// Version 2 preserves these optional members, including their identity.
			if r, code = Encode("test", []byte(strings.Replace(raw, `"version":3`, `"version":2`, 1))); code != 0 || r.Document == "" {
				t.Fatalf("version 2 changed: %d %+v", code, r)
			}
		})
	}
}

func TestVersion3OtherEmptyMembersArePreserved(t *testing.T) {
	raw := `{"admits":{},"case":{"evidence":[],"facts":[]},"sources":[{"body":{},"headers":{},"query":{}}],"version":3}`
	r, code := Encode("test", []byte(raw))
	if code != 0 || r.Document != raw || r.FullDocumentConformance {
		t.Fatalf("%d %+v", code, r)
	}
}

func TestStartersMatchReferenceRunnerSpelling(t *testing.T) {
	for version, expected := range map[int]string{
		2: `{"case":{"evidence":[],"facts":[]},"evidence":null,"facts":null,"provider":"","version":2}`,
		3: `{"case":{"evidence":[],"facts":[]},"version":3}`,
	} {
		r, code := Init("test", version)
		if code != 0 || r.Document != expected || !r.StarterTemplate {
			t.Fatalf("%d: %d %+v", version, code, r)
		}
		again, code := Encode("test", []byte(r.Document))
		if code != 0 || again.Digest != r.Digest || again.StarterTemplate {
			t.Fatalf("round trip: %d %+v", code, again)
		}
	}
}

func TestPinnedUnboundDialectIdentity(t *testing.T) {
	raw, err := os.ReadFile("testdata/unbound-bound-at-import/unbound.json")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := os.ReadFile("testdata/unbound-bound-at-import/binding.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		Dialect string `json:"dialect"`
	}
	if err = json.Unmarshal(binding, &expected); err != nil {
		t.Fatal(err)
	}
	r, code := Encode("test", raw)
	if code != 0 || r.Digest != expected.Dialect {
		t.Fatalf("%d %+v expected %s", code, r, expected.Dialect)
	}
}
