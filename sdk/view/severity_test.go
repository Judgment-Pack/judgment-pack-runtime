package view_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	pv "github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// The view of a Core 0.5.0 pack lists each outcome's severity under
// annotation vocabulary 1.0.0 (spec/annotation-vocabulary.md §4, ADR-0088):
// the level where the outcome states one, exactly; nothing where it states
// none, or states a value that is not a level (another case, a one-element
// list, a look-alike letter), which is never mapped onto a level; and the
// outcomes ordered most severe first, one level in document order, with the
// unranked ones apart in document order. The value as written stays in the
// outcome's annotations, and a severity on a rule or on the root is not an
// outcome's.
func TestTheViewListsEachOutcomesSeverity(t *testing.T) {
	_, view := mustView(t, fixture(t, artifacts.Core050Version, "severity"))
	want := []struct{ id, severity string }{
		{"note", "low"}, {"refer", "high"}, {"unstated", ""}, {"wrong-case", ""}, {"hold", "high"},
		{"listed", ""}, {"look-alike", ""}, {"stop", "critical"}, {"check", "medium"},
	}
	if len(view.Outcomes) != len(want) {
		t.Fatalf("%d outcomes", len(view.Outcomes))
	}
	for index, outcome := range view.Outcomes {
		if outcome.ID != want[index].id || outcome.Severity != want[index].severity {
			t.Fatalf("outcome %d: %q at %q, want %q at %q", index, outcome.ID, outcome.Severity, want[index].id, want[index].severity)
		}
	}
	order := view.SeverityOrder
	if order == nil || order.VocabularyVersion != "1.0.0" ||
		!slices.Equal(order.MostSevereFirst, []string{"stop", "refer", "hold", "check", "note"}) ||
		!slices.Equal(order.Unranked, []string{"unstated", "wrong-case", "listed", "look-alike"}) {
		t.Fatalf("the order: %+v", order)
	}
	written := map[string]pv.Annotation{}
	for _, outcome := range view.Outcomes {
		for _, annotation := range outcome.Annotations {
			if annotation.Key == "severity" {
				written[outcome.ID] = annotation
			}
		}
	}
	if written["wrong-case"].Value != "High" || !slices.Equal(written["listed"].List, []string{"critical"}) ||
		written["look-alike"].Value != "crit\u0456cal" {
		t.Fatalf("the values as written: %+v", written)
	}
}

// Outcomes of one level keep their document order, whatever their ids:
// moving hold before refer moves it before refer in the order and in nothing
// else.
func TestOneLevelKeepsDocumentOrder(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(fixture(t, artifacts.Core050Version, "severity"), &root); err != nil {
		t.Fatal(err)
	}
	outcomes := root["outcomes"].([]any)
	outcomes[1], outcomes[4] = outcomes[4], outcomes[1]
	moved, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	_, view := mustView(t, moved)
	if order := view.SeverityOrder; !slices.Equal(order.MostSevereFirst, []string{"stop", "hold", "refer", "check", "note"}) ||
		!slices.Equal(order.Unranked, []string{"unstated", "wrong-case", "listed", "look-alike"}) {
		t.Fatalf("the order: %+v", order)
	}
}

// A view of a pack under another specVersion lists no severity: no outcome
// has one, there is no order, and the JSON holds the digest, the document
// and the view version only.
func TestAViewUnderAnotherVersionListsNoSeverity(t *testing.T) {
	checked := 0
	for _, input := range viewInputs(t) {
		if input.version == artifacts.Core050Version {
			continue
		}
		checked++
		_, view := mustView(t, input.pack)
		if view.SeverityOrder != nil {
			t.Fatalf("%s: an order %+v", input.name, view.SeverityOrder)
		}
		for _, outcome := range view.Outcomes {
			if outcome.Severity != "" {
				t.Fatalf("%s: outcome %q at %q", input.name, outcome.ID, outcome.Severity)
			}
		}
		var members map[string]json.RawMessage
		if err := json.Unmarshal(view.JSON(), &members); err != nil {
			t.Fatal(err)
		}
		if _, present := members["severity"]; present || len(members) != 3 {
			t.Fatalf("%s: members %v", input.name, members)
		}
	}
	if checked == 0 {
		t.Fatal("no view under another version was checked")
	}
}
