package result

import (
	"reflect"
	"strings"
	"testing"
)

type declaredItem struct {
	Name string `json:"name"`
}

// declaredShape exercises each rule of encodeDeclared once.
type declaredShape struct {
	Absent string         `json:"absent,omitempty"`
	Empty  []string       `json:"empty,omitempty"`
	Items  []declaredItem `json:"items"`
	Nested struct {
		Present string `json:"present"`
	} `json:"nested"`
	NilList  []string       `json:"nilList,omitempty"`
	Required []string       `json:"required"`
	Text     string         `json:"text"`
	Value    map[string]any `json:"value"`
}

// The members written are the declared ones, by their json names: an empty
// string and a nil slice under omitempty are omitted, a non-nil empty slice is
// [], a required nil slice is [], and a map[string]any as the top-level value
// member is passed to the canonical encoder whole, or omitted under omitempty
// when nil. The expected bytes are written by hand.
func TestEncodeDeclaredWritesTheDeclaredMembers(t *testing.T) {
	value := declaredShape{Empty: []string{}, Items: []declaredItem{{Name: "b"}}, Value: map[string]any{"note": "a", "flag": true}}
	value.Nested.Present = "x"
	encoded, err := encodeDeclared(value)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"empty":[],"items":[{"name":"b"}],"nested":{"present":"x"},"required":[],"text":"","value":{"flag":true,"note":"a"}}`
	if string(encoded) != want {
		t.Fatalf("encoded:\n got %s\nwant %s", encoded, want)
	}
	nilValue := struct {
		Kind  string         `json:"kind"`
		Value map[string]any `json:"value,omitempty"`
	}{Kind: "k"}
	if encoded, err := encodeDeclared(nilValue); err != nil || string(encoded) != `{"kind":"k"}` {
		t.Fatalf("nil value member: encoded %s, err %v", encoded, err)
	}
}

// DeclaredEmbedded is exported so that only the embedding is refused.
type DeclaredEmbedded struct {
	Kind string `json:"kind"`
}

type embeddingMember struct {
	DeclaredEmbedded `json:"embedded"`
}

// unexportedTagged is a struct whose unexported field carries a json tag, which
// the audit-formats normalization skips. It is built with reflect because vet
// refuses that tag in source.
func unexportedTagged() any {
	return reflect.New(reflect.StructOf([]reflect.StructField{
		{Name: "Kind", Type: reflect.TypeOf(""), Tag: `json:"kind"`},
		{Name: "hint", PkgPath: reflect.TypeOf(declaredItem{}).PkgPath(), Type: reflect.TypeOf(""), Tag: `json:"hint"`},
	})).Elem().Interface()
}

// A field whose member name or encoding is not plainly declared, and a kind
// outside the canonical value space, are refused rather than written.
func TestEncodeDeclaredRefusesWhatItCannotDeclare(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"unexported field", unexportedTagged(), "field hint of"},
		{"unexported untagged field", struct {
			Kind string `json:"kind"`
			hint string
		}{"k", "h"}, "field hint of"},
		{"embedded struct", embeddingMember{}, "field DeclaredEmbedded of"},
		{"untagged field", struct{ Kind string }{"k"}, "field Kind of"},
		{"field hidden from json", struct {
			Kind string `json:"-"`
		}{"k"}, "field Kind of"},
		{"string option", struct {
			Count string `json:"count,string"`
		}{"1"}, "field Count of"},
		{"integer", struct {
			Count int `json:"count"`
		}{1}, "kind int"},
		{"Boolean", struct {
			Final bool `json:"final"`
		}{true}, "kind bool"},
		{"pointer", struct {
			Next *declaredItem `json:"next,omitempty"`
		}{&declaredItem{Name: "n"}}, "kind ptr"},
		{"interface", struct {
			Any any `json:"any"`
		}{"x"}, "kind interface"},
		{"map of strings", struct {
			Labels map[string]string `json:"labels"`
		}{map[string]string{"a": "b"}}, "kind map"},
		// A map is taken only as the top-level value member (#350).
		{"map member other than value", struct {
			Value map[string]any `json:"value"`
			Extra map[string]any `json:"extra"`
		}{map[string]any{}, map[string]any{"a": "b"}}, "disposition shape: a member of kind map at extra is taken only as the top-level value member"},
		{"nil map member other than value", struct {
			Extra map[string]any `json:"extra,omitempty"`
		}{}, "kind map at extra is taken only"},
		{"map named value below the top level", struct {
			Handoff struct {
				Value map[string]any `json:"value,omitempty"`
			} `json:"handoff"`
		}{}, "kind map at handoff.value is taken only"},
		{"map in a list", struct {
			Items []map[string]any `json:"items"`
		}{[]map[string]any{{"a": "b"}}}, "kind map at items[] is taken only"},
		{"map at the top level", map[string]any{"a": "b"}, "kind map at the top level is taken only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := encodeDeclared(tc.value)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("encoded %s, err %v; want a refusal naming %q", encoded, err, tc.want)
			}
		})
	}
}

// Disposition.CanonicalWith writes through the shape: a routed pack's empty
// route selection is "routes":[], a selected route is its kind and name, and a
// string the canonical encoder refuses is still refused. The expected bytes are
// written by hand.
func TestCanonicalWritesThroughTheDeclaredShape(t *testing.T) {
	for _, tc := range []struct {
		name        string
		disposition Disposition
		want        string
	}{
		{"routed, none selected", Disposition{Contract: Core030EvaluatorContract(), Kind: "unresolved", Reasons: []string{"exception-escalation"},
			Handoff: Handoff{State: "none", Routes: []HandoffTarget{}}},
			`{"handoff":{"routes":[],"state":"none"},"kind":"unresolved","reasons":["exception-escalation"]}`},
		{"routed, one selected", Disposition{Contract: Core030EvaluatorContract(), Kind: "unresolved", Reasons: []string{"unknown", "conflict"},
			Handoff: Handoff{State: "requested", TriggeredBy: []string{"conflict"}, Routes: []HandoffTarget{{Kind: "queue", Name: "Review"}}}},
			`{"handoff":{"routes":[{"kind":"queue","name":"Review"}],"state":"requested","triggeredBy":["conflict"]},"kind":"unresolved","reasons":["conflict","unknown"]}`},
		{"outcome with values", Disposition{Kind: "outcome", OutcomeID: "approve", Handoff: Handoff{State: "none", TriggeredBy: []string{}},
			Value: map[string]any{"limit": "10", "final": true}},
			`{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve","reasons":[],"value":{"final":true,"limit":"10"}}`},
		{"outcome id not UTF-8", Disposition{Kind: "outcome", OutcomeID: "\xff", Handoff: Handoff{State: "none"}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			canonical, err := tc.disposition.Canonical()
			if tc.want == "" {
				if err == nil {
					t.Fatalf("encoded %s; want a refusal", canonical)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(canonical) != tc.want {
				t.Fatalf("canonical:\n got %s\nwant %s", canonical, tc.want)
			}
		})
	}
}
