package evalform

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func decode(t *testing.T, text string) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal([]byte(text), &root); err != nil {
		t.Fatal(err)
	}
	return root
}

func same(t *testing.T, got map[string]any, want string) {
	t.Helper()
	gotText, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantText, err := json.Marshal(decode(t, want))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotText) != string(wantText) {
		t.Fatalf("evaluation form:\n got  %s\n want %s", gotText, wantText)
	}
}

// Each expectation below is the evaluation form written out by hand.
func TestOfExpandsEveryConditionPosition(t *testing.T) {
	cases := []struct{ name, pack, want string }{
		{
			"applicability",
			`{"valueSets":[{"id":"s","entries":[{"value":"a"},{"value":{"k":1}}]}],"applicability":{"op":"fact","path":"/x","operator":"in","valueSet":"s"}}`,
			`{"applicability":{"op":"fact","path":"/x","operator":"in","value":["a",{"k":1}]}}`,
		},
		{
			"rule when",
			`{"valueSets":[{"id":"s","entries":[{"value":1}]}],"rules":[{"id":"r","when":{"op":"fact","path":"/x","operator":"in","valueSet":"s"}}]}`,
			`{"rules":[{"id":"r","when":{"op":"fact","path":"/x","operator":"in","value":[1]}}]}`,
		},
		{
			"exception when",
			`{"valueSets":[{"id":"s","entries":[{"value":true}]}],"exceptions":[{"id":"e","when":{"op":"fact","path":"/x","operator":"in","valueSet":"s"}}]}`,
			`{"exceptions":[{"id":"e","when":{"op":"fact","path":"/x","operator":"in","value":[true]}}]}`,
		},
		{
			"under all, any and not",
			`{"valueSets":[{"id":"s","entries":[{"value":"a"}]},{"id":"t","entries":[{"value":"b"},{"value":"c"}]}],"rules":[{"id":"r","when":{"op":"all","conditions":[{"op":"literal","value":true},{"op":"any","conditions":[{"op":"not","condition":{"op":"fact","path":"/x","operator":"in","valueSet":"t"}}]},{"op":"fact","path":"/y","operator":"in","valueSet":"s"}]}}]}`,
			`{"rules":[{"id":"r","when":{"op":"all","conditions":[{"op":"literal","value":true},{"op":"any","conditions":[{"op":"not","condition":{"op":"fact","path":"/x","operator":"in","value":["b","c"]}}]},{"op":"fact","path":"/y","operator":"in","value":["a"]}]}}]}`,
		},
		{
			"an empty valueSets member is removed",
			`{"valueSets":[],"rules":[{"id":"r","when":{"op":"literal","value":true}}]}`,
			`{"rules":[{"id":"r","when":{"op":"literal","value":true}}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form, err := Of(decode(t, tc.pack))
			if err != nil {
				t.Fatal(err)
			}
			same(t, form, tc.want)
		})
	}
}

func TestOfRemovesOnlyTheAnnotations(t *testing.T) {
	pack := `{"extensions":{"org.judgmentpack.annotations":{"a":"b"}},` +
		`"outcomes":[{"id":"o","extensions":{"org.judgmentpack.annotations":{"a":"b"},"com.example.keep":1}},{"id":"p","extensions":{}}],` +
		`"rules":[{"id":"r","extensions":{"org.judgmentpack.annotations":{"a":["b","c"]}}},{"id":"s"}],` +
		`"decision":{"extensions":{"com.example.keep":2}},` +
		`"valueSets":[{"id":"v","entries":[{"value":"x","extensions":{"org.judgmentpack.annotations":{"a":"b"}}}]}],` +
		`"applicability":{"op":"fact","path":"/x","operator":"in","valueSet":"v"}}`
	want := `{"outcomes":[{"id":"o","extensions":{"com.example.keep":1}},{"id":"p","extensions":{}}],` +
		`"rules":[{"id":"r"},{"id":"s"}],` +
		`"decision":{"extensions":{"com.example.keep":2}},` +
		`"applicability":{"op":"fact","path":"/x","operator":"in","value":["x"]}}`
	form, err := Of(decode(t, pack))
	if err != nil {
		t.Fatal(err)
	}
	same(t, form, want)
}

// The steps act on positions, never on content: data that looks like a
// reference or an annotations object is left as it is, and nothing outside a
// condition position is expanded.
func TestOfNeverReadsData(t *testing.T) {
	pack := `{"valueSets":[{"id":"s","entries":[{"value":{"op":"fact","path":"/x","operator":"in","valueSet":"s","extensions":{"org.judgmentpack.annotations":{"a":"b"}}}}]}],` +
		`"rules":[{"id":"r","when":{"op":"fact","path":"/x","operator":"equals","value":{"op":"fact","valueSet":"missing","extensions":{"org.judgmentpack.annotations":{"a":"b"}}}}}],` +
		`"evidenceRequirements":[{"id":"e","when":{"op":"fact","path":"/x","operator":"in","valueSet":"missing"}}],` +
		`"metadata":{"extensions":{"org.judgmentpack.annotations":{"a":"b"}}}}`
	want := `{"rules":[{"id":"r","when":{"op":"fact","path":"/x","operator":"equals","value":{"op":"fact","valueSet":"missing","extensions":{"org.judgmentpack.annotations":{"a":"b"}}}}}],` +
		`"evidenceRequirements":[{"id":"e","when":{"op":"fact","path":"/x","operator":"in","valueSet":"missing"}}],` +
		`"metadata":{"extensions":{"org.judgmentpack.annotations":{"a":"b"}}}}`
	form, err := Of(decode(t, pack))
	if err != nil {
		t.Fatal(err)
	}
	same(t, form, want)
}

func TestOfRefusesAnUnresolvedReference(t *testing.T) {
	_, err := Of(decode(t, `{"valueSets":[{"id":"s","entries":[{"value":1}]}],"exceptions":[{"id":"e","when":{"op":"not","condition":{"op":"fact","path":"/x","operator":"in","valueSet":"t"}}}]}`))
	var unresolved *UnresolvedError
	if !errors.As(err, &unresolved) || unresolved.Pointer != "/exceptions/0/when/condition/valueSet" || unresolved.ID != "t" {
		t.Fatalf("err = %v", err)
	}
}

func TestOfLeavesItsInputUnchanged(t *testing.T) {
	text := `{"extensions":{"org.judgmentpack.annotations":{"a":"b"}},"valueSets":[{"id":"s","entries":[{"value":1}]}],"rules":[{"id":"r","when":{"op":"any","conditions":[{"op":"fact","path":"/x","operator":"in","valueSet":"s"}]},"extensions":{"org.judgmentpack.annotations":{"a":"b"}}}]}`
	root := decode(t, text)
	if _, err := Of(root); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(root, decode(t, text)) {
		t.Fatal("Of modified the pack it was given")
	}
}
