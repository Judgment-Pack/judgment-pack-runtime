package project

import (
	"strings"
	"testing"
)

// A candidates document is read back under the shape it is written in, as
// strictly as a matrix is (ADR-0045): the version this runtime writes, the
// exact members spelled exactly, at least one candidate, unique ids, and facts
// on each.
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
	for name, test := range map[string]struct{ document, want string }{
		"a later version":           {`{"candidatesVersion":"2","candidates":[{"id":"a","facts":{}}]}`, `candidatesVersion "1"`},
		"no version":                {`{"candidates":[{"id":"a","facts":{}}]}`, `candidatesVersion "1"`},
		"a root member":             {`{"candidatesVersion":"1","candidates":[{"id":"a","facts":{}}],"cases":[]}`, `"cases"`},
		"a member cased apart":      {`{"candidatesVersion":"1","candidates":[{"id":"a","Facts":{}}]}`, `"Facts"`},
		"an expectation":            {`{"candidatesVersion":"1","candidates":[{"id":"a","facts":{},"expectedDisposition":{}}]}`, `"expectedDisposition"`},
		"no candidates":             {`{"candidatesVersion":"1","candidates":[]}`, "declares no candidates"},
		"a repeated id":             {`{"candidatesVersion":"1","candidates":[{"id":"a","facts":{}},{"id":"a","facts":{}}]}`, "more than once"},
		"no id":                     {`{"candidatesVersion":"1","candidates":[{"facts":{}}]}`, "declares no id"},
		"no facts":                  {`{"candidatesVersion":"1","candidates":[{"id":"a"}]}`, "declares no facts"},
		"null facts":                {`{"candidatesVersion":"1","candidates":[{"id":"a","facts":null}]}`, "declares no facts"},
		"a duplicate member":        {`{"candidatesVersion":"1","candidatesVersion":"1","candidates":[{"id":"a","facts":{}}]}`, "not acceptable JSON"},
		"a candidate not an object": {`{"candidatesVersion":"1","candidates":["a"]}`, "not a JSON object"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCandidates([]byte(test.document)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want one naming %s", err, test.want)
			}
		})
	}
}
