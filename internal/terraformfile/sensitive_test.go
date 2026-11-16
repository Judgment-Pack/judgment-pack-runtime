package terraformfile

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// after reads one plan resource's after member, as written in the value.
func afterOf(t *testing.T, c change) (string, *Acquisition) {
	t.Helper()
	c.address = "synthetic_db.main"
	acquisition := mustAccept(t, plan(t, c), planSource("synthetic_db"))
	got := resources(t, acquisition)[0]
	noSecret(t, acquisition.Value(), acquisition.Record())
	value, ok := got["after"]
	if !ok {
		return "<absent>", acquisition
	}
	return string(encode(t, value)), acquisition
}

func TestSensitivePositionsAreRemoved(t *testing.T) {
	cases := []struct {
		name    string
		after   any
		mirror  any
		want    string
		removed int
	}{
		{"an attribute", object("password", "SECRET-A", "name", "db"), object("password", true), `{"name":"db"}`, 1},
		{"a nested block", object("conf", object("key", "SECRET-B", "x", 1), "name", "db"), object("conf", true), `{"name":"db"}`, 1},
		{"inside a nested block", object("conf", object("key", "SECRET-B", "x", 1)), object("conf", object("key", true)), `{"conf":{"x":"1"}}`, 1},
		{"an element of a list removes the list", object("tags", []any{"a", "SECRET-C"}, "name", "db"), object("tags", []any{false, true}), `{"name":"db"}`, 1},
		{"an element of a list in a list", object("rules", []any{[]any{"a"}, []any{"b", "SECRET-D"}}, "name", "db"), object("rules", []any{[]any{}, []any{false, true}}), `{"name":"db"}`, 1},
		{"a member of a list's element", object("blocks", []any{object("k", "SECRET-E", "v", 1), object("v", 2)}), object("blocks", []any{object("k", true)}), `{"blocks":[{"v":"1"},{"v":"2"}]}`, 1},
		{"the whole value", object("password", "SECRET-F"), true, `<absent>`, 1},
		{"an array value's element", []any{"a", "SECRET-G"}, []any{false, true}, `<absent>`, 1},
		{"a null at a marked position", object("password", nil, "name", "db"), object("password", true), `{"name":"db"}`, 1},
		{"a position the value lacks", object("name", "db"), object("password", true, "conf", object("key", true), "list", []any{true}), `{"name":"db"}`, 0},
		{"two members count two", object("a", "SECRET-H", "b", "SECRET-I", "c", 1), object("a", true, "b", true), `{"c":"1"}`, 2},
		{"absent positions count nothing", object("conf", object("a", "SECRET-J", "b", "SECRET-K")), object("conf", object("a", true, "b", true, "c", true)), `{"conf":{}}`, 2},
		{"an escalation counts once", object("list", []any{object("a", "SECRET-L", "b", 1), "SECRET-M"}), object("list", []any{object("a", true), true}), `{}`, 1},
		{"a mirror member names an element", object("list", []any{"a", "SECRET-N"}), object("list", object("1", true)), `{}`, 1},
		{"a mirror member that is no index", object("list", []any{"a", "b"}), object("list", object("01", true, "-", true, "2", true, "x", true)), `{"list":["a","b"]}`, 0},
		{"a mirror element names a member", object("obj", object("0", "a", "1", "SECRET-O")), object("obj", []any{false, true}), `{"obj":{"0":"a"}}`, 1},
		{"an escaped mirror name", object("password", "SECRET-P"), json.RawMessage(`{"password":true}`), `{}`, 1},
		{"a mirror on a scalar", object("name", "db"), object("name", object("x", true)), `{"name":"db"}`, 0},
		{"false and empty mirrors", object("a", []any{1}, "b", object("c", 1)), object("a", []any{false}, "b", object()), `{"a":["1"],"b":{"c":"1"}}`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, acquisition := afterOf(t, change{after: c.after, afterSens: c.mirror})
			if got != c.want {
				t.Fatalf("after %s, want %s", got, c.want)
			}
			if acquisition.RemovedSensitive != c.removed {
				t.Fatalf("removedSensitive %d, want %d", acquisition.RemovedSensitive, c.removed)
			}
		})
	}
}

func TestRemovalCoversTheWholeReadSection(t *testing.T) {
	data := plan(t,
		change{address: "synthetic_db.main", actions: []string{"update"},
			before: object("password", "SECRET-BEFORE", "n", 1), beforeSens: object("password", true),
			after: object("password", "SECRET-AFTER", "n", 2), afterSens: object("password", true)},
		change{address: "synthetic_other.unselected", after: object("token", "SECRET-UNSELECTED"), afterSens: object("token", true)},
	)
	acquisition := mustAccept(t, data, planSource("synthetic_db"))
	if acquisition.RemovedSensitive != 3 || acquisition.Selected != 1 {
		t.Fatalf("record %s", acquisition.Record())
	}
	got := resources(t, acquisition)[0]
	if string(encode(t, got["before"])) != `{"n":"1"}` || string(encode(t, got["after"])) != `{"n":"2"}` {
		t.Fatalf("resource %v", got)
	}
	noSecret(t, acquisition.Value(), acquisition.Record())

	stateData := state(t, module{
		resources: []stateResource{{address: "synthetic_db.main", values: object("password", "SECRET-STATE", "keys", []any{"k", "SECRET-KEY"}, "n", 1), sensitive: object("password", true, "keys", []any{false, true})}},
		children:  []module{{address: "module.m", resources: []stateResource{{address: "module.m.synthetic_db.all", values: object("a", "SECRET-ALL"), sensitive: true}}}},
	})
	stateAcquisition := mustAccept(t, stateData, stateSource("synthetic_db"))
	if stateAcquisition.RemovedSensitive != 3 {
		t.Fatalf("state record %s", stateAcquisition.Record())
	}
	want := `[{"address":"module.m.synthetic_db.all","mode":"managed","name":"all","providerName":"example.test/synthetic/cloud","type":"synthetic_db"},` +
		`{"address":"synthetic_db.main","mode":"managed","name":"main","providerName":"example.test/synthetic/cloud","type":"synthetic_db","values":{"n":"1"}}]`
	if string(stateAcquisition.Value()) != want {
		t.Fatalf("state value %s", stateAcquisition.Value())
	}
	noSecret(t, stateAcquisition.Value(), stateAcquisition.Record())
}

func TestASecretAProviderLeftUnmarkedIsRead(t *testing.T) {
	// The residual risk RFC 0038 states plainly: Terraform's marker is the
	// only signal, so a value its provider did not mark is read.
	c := change{address: "synthetic_db.main", after: object("unmarked_key", "provider-unmarked-value")}
	got := string(mustAccept(t, plan(t, c), planSource("synthetic_db")).Value())
	if !json.Valid([]byte(got)) || !strings.Contains(got, "provider-unmarked-value") {
		t.Fatalf("value %s", got)
	}
}

func TestAfterUnknownPointers(t *testing.T) {
	cases := []struct {
		name                   string
		after, mirror, unknown any
		want                   []string
	}{
		{"sorted and escaped", object("name", "x"), false,
			object("id", true, "tags", []any{false, true}, "nested", object("a", true, "b", false), "we/ird~", true, "list", []any{}),
			[]string{"/id", "/nested/a", "/tags/1", "/we~1ird~0"}},
		{"under a removed member", object("conf", object("x", 1), "name", "x"), object("conf", true),
			object("conf", object("y", true), "id", true), []string{"/id"}},
		{"under a removed array", object("list", []any{"a", "SECRET-U"}), object("list", []any{false, true}),
			object("list", []any{false, false, true}), []string{}},
		{"at a removed member", object("token", "SECRET-T"), object("token", true),
			object("token", true), []string{}},
		{"under a removed whole value", object("a", 1), true, object("id", true), []string{}},
		{"under a member kept beside a removed one", object("conf", object("key", "SECRET-K")), object("conf", object("key", true)),
			object("conf", object("id", true)), []string{"/conf/id"}},
		{"an unknown value is absent from after", nil, false, object("id", true, "arn", true), []string{"/arn", "/id"}},
		{"the whole value unknown", nil, false, true, []string{""}},
		{"no after_unknown", object("a", 1), false, absent, []string{}},
		{"string order, not numeric", object(), false, object("l", []any{false, false, true, false, false, false, false, false, false, false, true}), []string{"/l/10", "/l/2"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := change{address: "synthetic_db.main", after: c.after, afterSens: c.mirror, afterUnknown: c.unknown}
			item := ch.json()
			if _, drop := c.unknown.(absentValue); drop {
				delete(item["change"].(map[string]any), "after_unknown")
			}
			acquisition := mustAccept(t, encode(t, planWith([]any{item})), planSource("synthetic_db"))
			var got []string
			raw := resources(t, acquisition)[0]["afterUnknown"].([]any)
			got = []string{}
			for _, p := range raw {
				got = append(got, p.(string))
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("afterUnknown %q, want %q", got, c.want)
			}
			noSecret(t, acquisition.Value())
		})
	}
}
