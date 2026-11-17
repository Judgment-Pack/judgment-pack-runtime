package conformance

import (
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// The comparison of check 4, on requests crafted by hand.
func TestFilteredRequestComparison(t *testing.T) {
	const id = "https://example.invalid/jpack-harness/kinds/jpack-harness-undeclared"
	original := `{"emitRequestVersion":"1.0.0","emitter":{"id":"https://example.invalid/e","version":"1.0.0"},"options":{},` +
		`"resolved":[{"entries":[],"environment":"dev","kinds":[{"digest":"sha256:aa","id":"https://example.invalid/kinds/queue","kind":"queue","version":"1.2.0"}],` +
		`"registry":{"digest":"sha256:0001","id":"https://example.invalid/r","version":"1.0.0"},"resolvedFormatVersion":"1.0.0"}]}`
	cases := map[string]struct {
		changed string
		reason  string
	}{
		"the same bytes":                   {original, ""},
		"only the registry digest differs": {strings.Replace(original, "sha256:0001", "sha256:0002", 1), ""},
		"the synthetic kind's pin is in it": {strings.Replace(original, `"kinds":[`,
			`"kinds":[{"digest":"sha256:bb","id":"`+id+`","kind":"jpack-harness-undeclared","version":"1.0.0"},`, 1), reasonFilterHolds},
		"the synthetic kind's pin, and the digest": {strings.Replace(strings.Replace(original, "sha256:0001", "sha256:0002", 1), `"kinds":[`,
			`"kinds":[{"digest":"sha256:bb","id":"`+id+`","kind":"jpack-harness-undeclared","version":"1.0.0"},`, 1), reasonFilterHolds},
		"a kind's digest differs":    {strings.Replace(original, "sha256:aa", "sha256:ab", 1), reasonFilterBytes},
		"the registry id differs":    {strings.Replace(original, "invalid/r", "invalid/s", 1), reasonFilterBytes},
		"another member differs":     {strings.Replace(original, `"options":{}`, `"options":{"a":1}`, 1), reasonFilterBytes},
		"not a request":              {"[]", reasonFilterBytes},
		"a digest where none was":    {strings.Replace(original, `"registry":{"digest":"sha256:0001",`, `"registry":{`, 1), reasonFilterBytes},
		"an environment more than 1": {strings.Replace(original, `"resolved":[{`, `"resolved":[{},{`, 1), reasonFilterBytes},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			restored, reason := filteredRequest([]byte(original), []byte(c.changed), id)
			if reason != c.reason {
				t.Fatalf("reason %q, want %q", reason, c.reason)
			}
			if reason == "" && string(restored) != original {
				t.Fatalf("restored: %s", restored)
			}
		})
	}
}

// The facts form's restoration, where a contract's source carries the
// registry digest.
func TestFilteredRequestFactsForm(t *testing.T) {
	original := `{"factsContracts":[{"source":{"registry":{"digest":"sha256:0001"}}}]}`
	changed := strings.Replace(original, "0001", "0002", 1)
	if _, reason := filteredRequest([]byte(original), []byte(changed), "x"); reason != "" {
		t.Fatalf("reason %q", reason)
	}
}

func TestWithSyntheticKind(t *testing.T) {
	f := newFixture(t)
	v := &Vector{Name: "v", Registry: registry.Document{Data: []byte(registryText(t))},
		Kinds: []registry.Document{{Data: []byte(queueKind)}, {Data: []byte(topicKind)}}}
	// The manifest reads a kind of the synthetic name, so another is taken.
	m := f.manifest(requestMode, map[string]any{"reads": []any{
		map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "^1.0.0"},
		map[string]any{"kind": "jpack-harness-undeclared", "id": "https://example.invalid/kinds/x", "versions": "^1.0.0"},
	}})
	added, err := withSyntheticKind(v, m)
	if err != nil {
		t.Fatal(err)
	}
	if added.id != syntheticKindBase+"jpack-harness-undeclared-2" {
		t.Fatalf("id: %s", added.id)
	}
	result := registry.ValidateDocuments(added.registry, added.kinds)
	if !result.Report.Valid {
		t.Fatalf("the registry with the synthetic kind is not valid: %+v", result.Report.Diagnostics)
	}
	if pins := result.Pins(); len(pins) != 3 || pins[0].Kind != "jpack-harness-undeclared-2" {
		t.Fatalf("pins: %+v", pins)
	}
	original, refused := emit.FormRequests(m, emit.Inputs{Registry: registry.ValidateDocuments(v.Registry, v.Kinds)})
	if len(refused) > 0 {
		t.Fatal(refused)
	}
	changed, refused := emit.FormRequests(m, emit.Inputs{Registry: result})
	if len(refused) > 0 || len(changed) != len(original) {
		t.Fatal(refused)
	}
	for i := range changed {
		if string(changed[i].Bytes) == string(original[i].Bytes) {
			t.Fatal("the registry digest did not change; the test does not show its restoration")
		}
		if _, reason := filteredRequest(original[i].Bytes, changed[i].Bytes, added.id); reason != "" {
			t.Fatalf("%s: %s", changed[i].Environment, reason)
		}
	}
}

// scanFails runs check 5's scan of one file for strings.
func scanFails(t *testing.T, file string, values ...string) []int32 {
	t.Helper()
	var targets []target
	for _, value := range values {
		targets = append(targets, target{value: []byte(value)})
	}
	s, err := newScanner(targets)
	if err != nil {
		t.Fatal(err)
	}
	failing, err := s.scan([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	return failing
}

func TestReferenceScan(t *testing.T) {
	r := "secret:gcp-sm:projects/example-dev/secrets/orders-queue/versions/3"
	cases := map[string]struct {
		file  string
		fails bool
	}{
		"absent":                    {"nothing here\n", false},
		"whole":                     {"a " + r + " b\n", false},
		"whole, twice, adjacent":    {r + r + "\n", false},
		"whole, then cut":           {r + " " + r[:40] + "\n", true},
		"cut, then whole":           {r[:40] + " " + r + "\n", true},
		"cut to 32":                 {"x" + r[:32] + "x", true},
		"cut to 31":                 {"x" + r[:31] + "x", false},
		"its last 32":               {"x" + r[len(r)-32:] + "x", true},
		"its middle 33":             {"x" + r[10:43] + "x", true},
		"rewritten around its name": {"secret:gcp-sm:projects/example-dev/secrets/" + "\n" + "orders-queue/versions/3", true},
		"the project id on its own": {"example-dev orders-queue\n", false},
		"far apart, whole then cut": {r + strings.Repeat(" ", 1000) + r[:33] + "\n", true},
		"whole, far apart":          {r + strings.Repeat(" ", 1000) + r + "\n", false},
		"whole, then its first 31":  {r + r[:31] + "\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := len(scanFails(t, c.file, r)) > 0; got != c.fails {
				t.Fatalf("fails %v, want %v", got, c.fails)
			}
		})
	}
	if failing := scanFails(t, "x"+strings.Repeat("a", 31)+"x", strings.Repeat("a", 31)); len(failing) != 0 {
		t.Fatal("a string of at most 32 characters failed")
	}
}

// RFC 0036 §5 as written: a run of R outside a complete occurrence of R
// fails, even when it lies inside a complete occurrence of another string at
// a secret position. Two references of one project share more than 32
// characters, so a file holding both whole fails. ADR-0099 records this as an
// open question for the companion; the harness applies the RFC's text.
func TestReferenceScanSharedPrefixIsTheRFCsRule(t *testing.T) {
	a := "secret:gcp-sm:projects/example-dev/secrets/orders-queue/versions/3"
	b := "secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/1"
	if failing := scanFails(t, a+"\n"+b+"\n", a, b); len(failing) != 2 {
		t.Fatalf("failing: %v", failing)
	}
}

func TestReferenceScanBounds(t *testing.T) {
	saved := maxReferenceBytes
	maxReferenceBytes = 64
	defer func() { maxReferenceBytes = saved }()
	if _, err := newScanner([]target{{value: []byte(strings.Repeat("a", 40))}, {value: []byte(strings.Repeat("b", 40))}}); err != errReferenceBound {
		t.Fatalf("err: %v", err)
	}
}
