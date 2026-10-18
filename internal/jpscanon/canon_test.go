package jpscanon

import (
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"strings"
	"testing"
)

func TestExistingCanonForms(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`{"😀":-0,"\ue000":2,"a":"\u0000\b\f\n\r\t\"\\/é<>&"}`, `{"a":"\u0000\b\f\n\r\t\"\\/é<>&","":2,"😀":0}`},
		{`[null,true,false,-9007199254740991,9007199254740991]`, `[null,true,false,-9007199254740991,9007199254740991]`},
	} {
		value, failure := carrier.Decode([]byte(tc.input), carrier.DefaultLimits())
		if failure != nil {
			t.Fatal(failure)
		}
		var out strings.Builder
		if path, ok := Write(&out, value, nil); !ok {
			t.Fatalf("refused at %s", path)
		}
		if out.String() != tc.want {
			t.Fatalf("got %q want %q", out.String(), tc.want)
		}
	}
}
func TestExistingCanonNumberDomain(t *testing.T) {
	for _, input := range []string{`{"a":[1.0]}`, `{"a":[1e0]}`, `{"a":[9007199254740992]}`, `{"a":[-9007199254740992]}`} {
		value, failure := carrier.Decode([]byte(input), carrier.DefaultLimits())
		if failure != nil {
			t.Fatal(failure)
		}
		var out strings.Builder
		if path, ok := Write(&out, value, nil); ok || path != "/a/0" {
			t.Fatalf("got %s %v", path, ok)
		}
	}
}
