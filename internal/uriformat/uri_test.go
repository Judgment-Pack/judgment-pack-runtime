package uriformat

import (
	"encoding/json"
	"os"
	"testing"
)

// Expectations are hand-authored from RFC 3986 Appendix A. In particular, a
// numeric reg-name may fail IPv4 grammar while a dotted IPv6 tail may not.
func TestURIComponentsRFC3986(t *testing.T) {
	raw, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		URI   string `json:"uri"`
		Valid bool   `json:"valid"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.URI, func(t *testing.T) {
			if got := Valid(tc.URI); got != tc.Valid {
				t.Fatalf("Valid(%q) = %v, want %v", tc.URI, got, tc.Valid)
			}
			err := Validate(tc.URI)
			if got := err == nil; got != tc.Valid {
				t.Fatal("format callback differs from predicate")
			}
			if err != nil {
				const want = "URI does not satisfy RFC 3986 component grammar"
				if err.Error() != want {
					t.Fatalf("Validate(%q) error = %q, want %q", tc.URI, err.Error(), want)
				}
			}
		})
	}
	for _, value := range []any{nil, 1, true, []any{}} {
		if err := Validate(value); err != nil {
			t.Fatalf("non-string type assertion belongs to schema: %v", err)
		}
	}
}
