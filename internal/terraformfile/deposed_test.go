package terraformfile

import (
	"reflect"
	"strings"
	"testing"
)

// Deposed objects (dialect §4.5 step 4): an object an apply replaced but has
// not yet destroyed shares its resource's address, and is told apart by its
// deposed key, `deposed` in a plan and `deposed_key` in a state. Every key
// here is made up.

func TestADeposedPlanObjectCarriesItsKey(t *testing.T) {
	data := plan(t,
		change{address: "synthetic_db.main", actions: []string{"delete"}, before: object("size", 1), deposed: "0a1b2c3d"},
		change{address: "synthetic_db.main", actions: []string{"no-op"}, before: object("size", 2), after: object("size", 2)},
	)
	acquisition := mustAccept(t, data, planSource("synthetic_db"))
	if got, want := ids(acquisition), []string{"synthetic_db.main", "synthetic_db.main deposed 0a1b2c3d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ids %q", got)
	}
	if got := addresses(acquisition); !reflect.DeepEqual(got, []string{"synthetic_db.main", "synthetic_db.main"}) {
		t.Fatalf("addresses %q", got)
	}
	// deposed comes after before and before index, in canonical order.
	want := `[{"actions":["no-op"],"address":"synthetic_db.main","after":{"size":"2"},"afterUnknown":[],"before":{"size":"2"},"mode":"managed","name":"main","providerName":"` + syntheticProvider + `","type":"synthetic_db"},` +
		`{"actions":["delete"],"address":"synthetic_db.main","after":null,"afterUnknown":[],"before":{"size":"1"},"deposed":"0a1b2c3d","mode":"managed","name":"main","providerName":"` + syntheticProvider + `","type":"synthetic_db"}]`
	if string(acquisition.Value()) != want {
		t.Fatalf("value\n got %s\nwant %s", acquisition.Value(), want)
	}
}

func TestADeposedStateObjectCarriesItsKey(t *testing.T) {
	data := state(t, module{
		resources: []stateResource{
			{address: "synthetic_db.main", values: object("size", 1), deposedKey: "ffff0001"},
			{address: "synthetic_db.main", values: object("size", 2)},
			{address: "synthetic_db.main", values: object("size", 0), deposedKey: "0000aaaa"},
		},
		children: []module{{address: "module.app", resources: []stateResource{
			{address: "module.app.synthetic_db.main[0]", index: 0, values: object("size", 3), deposedKey: "12345678"},
			{address: "module.app.synthetic_db.main[0]", index: 0, values: object("size", 4)},
		}}},
	})
	acquisition := mustAccept(t, data, stateSource("synthetic_db"))
	wantIDs := []string{
		"module.app.synthetic_db.main[0]",
		"module.app.synthetic_db.main[0] deposed 12345678",
		"synthetic_db.main",
		"synthetic_db.main deposed 0000aaaa",
		"synthetic_db.main deposed ffff0001",
	}
	if got := ids(acquisition); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("ids %q", got)
	}
	// deposed comes after address and before index, in canonical order.
	if got, want := string(acquisition.Resources[1].JSON), `{"address":"module.app.synthetic_db.main[0]","deposed":"12345678","index":"0","mode":"managed","name":"main","providerName":"`+syntheticProvider+`","type":"synthetic_db","values":{"size":"3"}}`; got != want {
		t.Fatalf("deposed object\n got %s\nwant %s", got, want)
	}
	for i, size := range []string{"4", "3", "2", "0", "1"} {
		object := resources(t, acquisition)[i]
		if object["values"].(map[string]any)["size"] != size {
			t.Fatalf("%s: values %v", wantIDs[i], object["values"])
		}
		if _, ok := object["deposed"]; ok != strings.Contains(wantIDs[i], " deposed ") {
			t.Fatalf("%s: deposed member %v", wantIDs[i], object["deposed"])
		}
	}
}

// Each document reads its own member name: a plan's deposed_key and a
// state's deposed are not deposed keys, of any type, and are not carried.
func TestEachDocumentReadsItsOwnDeposedMember(t *testing.T) {
	item := change{address: "synthetic_db.main", after: object()}.json()
	item["deposed_key"] = 1
	acquisition := mustAccept(t, encode(t, planWith([]any{item})), planSource("synthetic_db"))
	if got := ids(acquisition); !reflect.DeepEqual(got, []string{"synthetic_db.main"}) || strings.Contains(string(acquisition.Value()), "deposed") {
		t.Fatalf("a plan's deposed_key: ids %q, value %s", got, acquisition.Value())
	}
	resource := stateResource{address: "synthetic_db.main"}.json()
	resource["deposed"] = 1
	data := encode(t, object("format_version", "1.0", "values", object("root_module", object("resources", []any{resource}))))
	acquisition = mustAccept(t, data, stateSource("synthetic_db"))
	if got := ids(acquisition); !reflect.DeepEqual(got, []string{"synthetic_db.main"}) || strings.Contains(string(acquisition.Value()), "deposed") {
		t.Fatalf("a state's deposed: ids %q, value %s", got, acquisition.Value())
	}
}

func TestADeposedKeyIsANonEmptyString(t *testing.T) {
	for name, key := range map[string]any{
		"a number": 1, "null": nil, "empty": "", "an object": object(), "a list": []any{"0a1b2c3d"}, "true": true,
	} {
		t.Run("plan/"+name, func(t *testing.T) {
			item := change{address: "synthetic_db.main", after: object()}.json()
			item["deposed"] = key
			mustRefuse(t, encode(t, planWith([]any{item})), planSource("synthetic_db"), CodeDocument)
		})
		t.Run("state/"+name, func(t *testing.T) {
			resource := stateResource{address: "synthetic_db.main"}.json()
			resource["deposed_key"] = key
			data := encode(t, object("format_version", "1.0", "values", object("root_module", object("resources", []any{resource}))))
			mustRefuse(t, data, stateSource("synthetic_db"), CodeDocument)
		})
	}
}

// Two resources of the read section with one id are TF-DOCUMENT at step 4,
// selected or not, before the mirrors of step 5 are checked.
func TestTwoResourcesOfOneIDAreRefused(t *testing.T) {
	plans := map[string][]change{
		"one address": {{address: "synthetic_db.main"}, {address: "synthetic_db.main"}},
		"one address and key": {
			{address: "synthetic_db.main", deposed: "0a1b2c3d"},
			{address: "synthetic_db.main"},
			{address: "synthetic_db.main", deposed: "0a1b2c3d"},
		},
		// The id is not injective: an address that holds " deposed " meets a
		// deposed object's id.
		"an address that spells a deposed id": {{address: "synthetic_db.main deposed 0a1b2c3d"}, {address: "synthetic_db.main", deposed: "0a1b2c3d"}},
		"neither selected":                    {{address: "synthetic_other.main"}, {address: "synthetic_other.main"}, {address: "synthetic_db.main"}},
		"no mirrors":                          {{address: "synthetic_db.main", beforeSens: absent}, {address: "synthetic_db.main", beforeSens: absent}},
	}
	for name, changes := range plans {
		t.Run("plan/"+name, func(t *testing.T) {
			mustRefuse(t, plan(t, changes...), planSource("synthetic_db"), CodeDocument)
		})
	}
	states := map[string]module{
		"one address": {resources: []stateResource{{address: "synthetic_db.main"}, {address: "synthetic_db.main"}}},
		"one address and key": {resources: []stateResource{
			{address: "synthetic_db.main", deposedKey: "0a1b2c3d"}, {address: "synthetic_db.main", deposedKey: "0a1b2c3d"},
		}},
		"in two modules": {
			resources: []stateResource{{address: "synthetic_db.main"}},
			children:  []module{{address: "module.app", resources: []stateResource{{address: "synthetic_db.main"}}}},
		},
		"neither selected": {resources: []stateResource{{address: "synthetic_other.main"}, {address: "synthetic_other.main"}}},
	}
	for name, root := range states {
		t.Run("state/"+name, func(t *testing.T) {
			mustRefuse(t, state(t, root), stateSource("synthetic_db"), CodeDocument)
		})
	}
	// Distinct keys of one address are accepted.
	mustAccept(t, plan(t, change{address: "synthetic_db.main", deposed: "1"}, change{address: "synthetic_db.main", deposed: "2"}), planSource("synthetic_db"))
}
