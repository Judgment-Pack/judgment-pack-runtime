package terraformfile

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzTerraformJSON reads arbitrary bytes as a plan and as a state. Every
// result is one of the three outcomes; a refusal carries only its four
// members; an acquired value is valid, canonical JSON; and the strict reader
// never accepts what encoding/json refuses.
func FuzzTerraformJSON(f *testing.F) {
	f.Add([]byte(`{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"synthetic_db.main","type":"synthetic_db","name":"main","mode":"managed","provider_name":"p","change":{"actions":["update"],"before":{"a":[1,2]},"after":{"a":[1,"s"],"b":1e3},"after_unknown":{"c":true},"before_sensitive":{},"after_sensitive":{"a":[false,true]}}}]}`))
	f.Add([]byte(`{"format_version":"1.0","values":{"root_module":{"resources":[{"address":"synthetic_db.x[0]","index":0,"type":"synthetic_db","name":"x","mode":"managed","provider_name":"p","values":{"k":"v","é":-0.0},"sensitive_values":{"k":true}}],"child_modules":[{"resources":[]}]}}}`))
	f.Add([]byte(`{"format_version":"1.2","planned_values":{},"a":1,"a":2}`))
	f.Add([]byte(`{"format_version":"1.2","planned_values":{},"x":"\ud800"}`))
	f.Add([]byte("\xef\xbb\xbf{}"))
	f.Add([]byte(`[[[[[[[[[[0]]]]]]]]]]`))
	f.Add([]byte(`{"format_version":"1.2","planned_values":{},"resource_changes":[{"address":"synthetic_db.m","type":"synthetic_db","name":"m","mode":"managed","provider_name":"p","change":{"actions":[],"after":{"n":1e64},"before_sensitive":false,"after_sensitive":false}}]}`))
	sources := []Source{
		planSource("synthetic_db"),
		stateSource("synthetic_db"),
		{Document: DocumentPlan, Select: Select{Types: []string{"synthetic_db"}, Address: ptr("*.*"), Actions: []string{"update"}, Max: 1}},
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, src := range sources {
			acquisition, refusal, err := Read(bytes.NewReader(data), src)
			switch {
			case err != nil:
				t.Fatalf("Read: %v", err)
			case refusal != nil:
				if acquisition != nil {
					t.Fatal("both an acquisition and a refusal")
				}
				checkRefusal(t, data, refusal)
			default:
				if !json.Valid(data) {
					t.Fatal("accepted a file encoding/json refuses")
				}
				value := acquisition.Value()
				if !utf8.Valid(value) {
					t.Fatal("the acquired value is not UTF-8")
				}
				crossCheck(t, value)
				if !json.Valid(acquisition.Record()) || acquisition.Selected != len(acquisition.Resources) {
					t.Fatalf("record %s", acquisition.Record())
				}
			}
		}
		// The scanner alone: what it accepts, encoding/json accepts, and its
		// canonical form reads back to itself.
		doc, ok := parseStrict(data)
		if !ok {
			return
		}
		if !json.Valid(data) {
			t.Fatal("the strict scanner accepted what encoding/json refuses")
		}
		if !doc.checkNumbers(0) {
			return
		}
		first := &writer{limit: 1 << 26}
		doc.writeValue(first, 0)
		if first.over {
			return
		}
		again, ok := parseStrict(first.buf)
		if !ok {
			t.Fatalf("the canonical form %s does not read back", first.buf)
		}
		second := &writer{limit: 1 << 26}
		again.writeValue(second, 0)
		if !bytes.Equal(first.buf, second.buf) {
			t.Fatalf("canonical form not stable:\n%s\n%s", first.buf, second.buf)
		}
	})
}

// FuzzAddressPattern holds the segment matcher to a regular expression that
// states RFC 0038 §4's rule directly.
func FuzzAddressPattern(f *testing.F) {
	for _, seed := range [][2]string{
		{"module.*.synthetic_db.*", "module.app.synthetic_db.main"},
		{"a*b*c", "aXbYc"}, {"*[*]", `x["k.y"]`}, {"**", ""}, {"a.]", "a.]"},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, pattern, address string) {
		if !utf8.ValidString(pattern) || !utf8.ValidString(address) || utf8.RuneCountInString(pattern) > MaxPatternChars {
			return
		}
		var expression strings.Builder
		expression.WriteString(`^`)
		for _, r := range pattern {
			if r == '*' {
				expression.WriteString(`[^.\[\]]*`)
			} else {
				expression.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
		expression.WriteString(`$`)
		want := regexp.MustCompile(expression.String()).MatchString(address)
		if got := compilePattern(pattern).match(address); got != want {
			t.Fatalf("%q against %q: %v, the rule says %v", pattern, address, got, want)
		}
	})
}
