package project

import (
	"testing"
)

// Coverage is derived from what the evaluator reads: a staged draft pack's
// root is its evaluation form, and one with no form yields nothing.
func TestPackRootIsTheStagedEvaluationForm(t *testing.T) {
	pack := []byte(`{"specVersion":"0.4.0-draft","valueSets":[{"id":"s","entries":[{"value":"a"}]}],"extensions":{"org.judgmentpack.annotations":{"k":"v"}},"rules":[{"id":"r","when":{"op":"fact","path":"/x","operator":"in","valueSet":"s"}}]}`)
	root := PackRoot(pack)
	if _, has := root["valueSets"]; has {
		t.Fatal("value sets survived")
	}
	if _, has := root["extensions"]; has {
		t.Fatal("annotations survived")
	}
	when := root["rules"].([]any)[0].(map[string]any)["when"].(map[string]any)
	if values, ok := when["value"].([]any); !ok || len(values) != 1 || values[0] != "a" {
		t.Fatalf("reference not expanded: %v", when)
	}
	if PackRoot([]byte(`{"specVersion":"0.4.0-draft","rules":[{"id":"r","when":{"op":"fact","path":"/x","operator":"in","valueSet":"missing"}}]}`)) != nil {
		t.Fatal("a pack with no evaluation form yielded a root")
	}
	released := PackRoot([]byte(`{"specVersion":"0.3.0","valueSets":[]}`))
	if _, has := released["valueSets"]; !has {
		t.Fatal("a released pack's root was rewritten")
	}
}
