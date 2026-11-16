package registry

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
)

// environmentsRegistry is a registry of n environments, e0 to en-1, of the
// one kind in kinds, holding entries and overlays.
func environmentsRegistry(t *testing.T, n int, entries, overlays string, kind string) (string, []string) {
	t.Helper()
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf(`"e%d"`, i)
	}
	registry, kinds := customRegistry(t, entries, kind)
	registry = strings.Replace(registry, `"environments":["dev","prod"]`, `"environments":[`+strings.Join(names, ",")+`]`, 1)
	registry = strings.Replace(registry, `"secretScopes":{"dev":{"gcp-sm":["example-dev"]},"prod":{"gcp-sm":["example-prod"]}},`, "", 1)
	if overlays != "" {
		registry = strings.TrimSuffix(registry, "}") + `,"overlays":` + overlays + `}`
	}
	return registry, kinds
}

func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// #434. Where no overlay applies, every environment shares the entry's own
// value, measured once, so validating in 32 environments costs about what
// validating in one does: before, each environment held its own copy and
// step 8 encoded each environment's document and decoded it again.
func TestResolutionCostDoesNotGrowWithEnvironments(t *testing.T) {
	kind := `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/k","version":"1.0.0","kind":"k","owner":"team-a",` +
		`"bounds":{"maxEntryBytes":4194304},"schema":{"type":"array"}}`
	entries := reviewEntry("[" + strings.TrimSuffix(strings.Repeat("0,", 200000), ",") + "]")
	cost := map[int]uint64{}
	for _, n := range []int{1, 32} {
		registry, kinds := environmentsRegistry(t, n, entries, "", kind)
		var result *Result
		cost[n] = allocated(func() {
			if result = run(registry, kinds); !result.Report.Valid {
				t.Fatalf("%d environments: %+v", n, result.Report.Diagnostics)
			}
		})
		first, last := result.resolved["e0"][0], result.resolved[fmt.Sprintf("e%d", n-1)][0]
		if &first.value.([]any)[0] != &last.value.([]any)[0] || first.measure != last.measure {
			t.Errorf("%d environments: the value or its measure is not shared", n)
		}
		t.Logf("%d environments: %d MiB allocated", n, cost[n]>>20)
	}
	if cost[32] > 2*cost[1] {
		t.Errorf("32 environments allocated %d MiB, one %d MiB", cost[32]>>20, cost[1]>>20)
	}
}

// An overlay copies only the containers on its paths. The entry's own value,
// which the other environments share, is never changed, and two operations
// in one container copy it once.
func TestAnOverlayLeavesTheSharedValueUnchanged(t *testing.T) {
	kind := `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/k","version":"1.0.0","kind":"k","owner":"team-a",` +
		`"schema":{"type":"object"},"overlayable":["/a/b","/a/d","/c/1"]}`
	entries := reviewEntry(`{"a":{"b":1,"d":4},"c":[1,2],"z":{"y":0}}`)
	overlays := `[{"environment":"e1","patches":[{"kind":"k","id":"e","set":{"/a/b":2,"/a/d":5,"/c/1":3}}]}]`
	registry, kinds := environmentsRegistry(t, 3, entries, overlays, kind)
	result := run(registry, kinds)
	if !result.Report.Valid {
		t.Fatalf("%+v", result.Report.Diagnostics)
	}
	values := map[string]string{}
	for _, environment := range []string{"e0", "e1", "e2", "e0"} {
		document, err := result.Resolve(environment)
		if err != nil {
			t.Fatal(err)
		}
		text := string(document)
		text = text[strings.Index(text, `"value":`):]
		values[environment] = text[:strings.Index(text, `,"environment"`)]
	}
	if want := `"value":{"a":{"b":1,"d":4},"c":[1,2],"z":{"y":0}}}]`; values["e0"] != want || values["e2"] != want {
		t.Errorf("e0 %s, e2 %s", values["e0"], values["e2"])
	}
	if want := `"value":{"a":{"b":2,"d":5},"c":[1,3],"z":{"y":0}}}]`; values["e1"] != want {
		t.Errorf("e1 %s", values["e1"])
	}
	// The part no operation touches is shared, not copied.
	var base, changed map[string]any
	for _, r := range result.resolved["e0"] {
		base = r.value.(map[string]any)
	}
	for _, r := range result.resolved["e1"] {
		changed = r.value.(map[string]any)
	}
	if fmt.Sprintf("%p", base["z"]) != fmt.Sprintf("%p", changed["z"]) {
		t.Error("an untouched member was copied")
	}
}

// Step 8 measures each environment's document without encoding it: the
// length and the nesting it computes are those of the document as encoded
// and as carrier admission reads it.
func TestTheMeasuredDocumentIsTheEncodedOne(t *testing.T) {
	kind := `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/k","version":"1.0.0","kind":"k","owner":"team-a",` +
		`"schema":{"type":"object"},"overlayable":["/a/b"]}`
	for _, entries := range []string{
		"",
		reviewEntry(`{"a":{"b":"é x"}}`),
		reviewEntry(`{"a":{"b":[[[[1.5e300]]]]}}`) + `,{"kind":"k","id":"f","owner":"team-a","value":{"a":{"b":[]}}}`,
	} {
		overlays := ""
		if entries != "" {
			overlays = `[{"environment":"e1","patches":[{"kind":"k","id":"e","set":{"/a/b":{"x":[{"y":{}}]}}}]}]`
		}
		registry, kinds := environmentsRegistry(t, 2, entries, overlays, kind)
		result := run(registry, kinds)
		if !result.Report.Valid {
			t.Fatalf("%s: %+v", entries, result.Report.Diagnostics)
		}
		for _, environment := range []string{"e0", "e1"} {
			document, err := result.encodeResolved(environment)
			if err != nil {
				t.Fatal(err)
			}
			size, depth, ok := result.measureResolved(environment)
			if !ok || size != len(document) {
				t.Errorf("%s %s: measured %d, encoded %d", entries, environment, size, len(document))
			}
			admitted := func(limit int) bool {
				_, failure := carrier.Decode(document, carrier.Limits{MaxDepth: limit, MaxNodes: len(document), MaxStringBytes: len(document)})
				return failure == nil
			}
			if !admitted(depth) || admitted(depth-1) {
				t.Errorf("%s %s: measured depth %d is not the document's", entries, environment, depth)
			}
		}
	}
}
