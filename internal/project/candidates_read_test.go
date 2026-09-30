package project

import (
	"strings"
	"testing"
)

// A candidates document is read back under the shape it is written in, as
// strictly as a matrix is (ADR-0045): the version this runtime writes, the
// exact members spelled exactly, origin and rationale as the strings the writer
// states, at least one candidate, unique ids, and a facts document on each,
// null included.
func TestDecodeCandidatesHoldsTheShapeItWrites(t *testing.T) {
	good := `{"candidatesVersion":"1","candidates":[
	  {"id":"a","origin":"generated","facts":{"x":"1"},"evidenceAvailability":{"r":"present"},"rationale":"One."},
	  {"id":"b","origin":"generated","facts":{"x":"2"},"rationale":"Two."}]}`
	decoded, err := DecodeCandidates([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Candidates) != 2 || string(decoded.Candidates[0].EvidenceAvailability) != `{"r":"present"}` || decoded.Candidates[1].EvidenceAvailability != nil {
		t.Fatalf("decoded = %+v", decoded)
	}
	// No candidates is what the writer emits when it derived nothing.
	if empty, err := DecodeCandidates([]byte(`{"candidatesVersion":"1","candidates":[]}`)); err != nil || len(empty.Candidates) != 0 {
		t.Fatalf("an empty candidates array is zero inputs: %+v %v", empty, err)
	}
	// A facts document of null is a document, as a --base row may state it.
	if _, err := DecodeCandidates([]byte(`{"candidatesVersion":"1","candidates":[{"id":"a","origin":"generated","facts":null,"rationale":"r"}]}`)); err != nil {
		t.Fatalf("null facts is a supplied document: %v", err)
	}
	const one = `{"id":"a","origin":"generated","facts":{},"rationale":"r"}`
	for name, test := range map[string]struct{ document, want string }{
		"a later version":           {`{"candidatesVersion":"2","candidates":[` + one + `]}`, `candidatesVersion "1"`},
		"no version":                {`{"candidates":[` + one + `]}`, `candidatesVersion "1"`},
		"a root member":             {`{"candidatesVersion":"1","candidates":[` + one + `],"cases":[]}`, `"cases"`},
		"a member cased apart":      {`{"candidatesVersion":"1","candidates":[{"id":"a","origin":"generated","Facts":{},"rationale":"r"}]}`, `"Facts"`},
		"an expectation":            {`{"candidatesVersion":"1","candidates":[{"id":"a","origin":"generated","facts":{},"rationale":"r","expectedDisposition":{}}]}`, `"expectedDisposition"`},
		"a repeated id":             {`{"candidatesVersion":"1","candidates":[` + one + `,` + one + `]}`, "more than once"},
		"no id":                     {`{"candidatesVersion":"1","candidates":[{"origin":"generated","facts":{},"rationale":"r"}]}`, "declares no id"},
		"no facts":                  {`{"candidatesVersion":"1","candidates":[{"id":"a","origin":"generated","rationale":"r"}]}`, "declares no facts"},
		"no origin":                 {`{"candidatesVersion":"1","candidates":[{"id":"a","facts":{},"rationale":"r"}]}`, "origin as a string"},
		"a null rationale":          {`{"candidatesVersion":"1","candidates":[{"id":"a","origin":"generated","facts":{},"rationale":null}]}`, "rationale as a string"},
		"a duplicate member":        {`{"candidatesVersion":"1","candidatesVersion":"1","candidates":[` + one + `]}`, "not acceptable JSON"},
		"a candidate not an object": {`{"candidatesVersion":"1","candidates":["a"]}`, "not a JSON object"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCandidates([]byte(test.document)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want one naming %s", err, test.want)
			}
		})
	}
}
