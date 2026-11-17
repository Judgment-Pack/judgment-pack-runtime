package conformance

import (
	"cmp"
	"maps"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// The comparison of check 4, on requests crafted by hand.
func TestFilteredRequestComparison(t *testing.T) {
	const id = "https://example.invalid/jpack-harness/kinds/jpack-harness-undeclared"
	const declaration = `{"id":"https://example.invalid/kinds/queue","kind":"queue","kindFormatVersion":"1.0.0","owner":"platform","schema":{},"version":"1.2.0"}`
	const synthetic = `{"id":"` + id + `","kind":"jpack-harness-undeclared","kindFormatVersion":"1.0.0","owner":"platform","schema":{},"version":"1.0.0"}`
	original := `{"declarations":[` + declaration + `],"emitRequestVersion":"1.0.0","emitter":{"id":"https://example.invalid/e","version":"1.0.0"},"options":{},` +
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
		// The declarations are compared too, and nothing in them is restored.
		"the synthetic kind's declaration is in it": {strings.Replace(original, `"declarations":[`, `"declarations":[`+synthetic+`,`, 1), reasonFilterHolds},
		"a declaration differs":                     {strings.Replace(original, `"schema":{}`, `"schema":{"type":"object"}`, 1), reasonFilterBytes},
		"a declaration is not there":                {strings.Replace(original, declaration, "", 1), reasonFilterBytes},
		"a kind's digest differs":                   {strings.Replace(original, "sha256:aa", "sha256:ab", 1), reasonFilterBytes},
		"the registry id differs":                   {strings.Replace(original, "invalid/r", "invalid/s", 1), reasonFilterBytes},
		"another member differs":                    {strings.Replace(original, `"options":{}`, `"options":{"a":1}`, 1), reasonFilterBytes},
		"not a request":                             {"[]", reasonFilterBytes},
		"a digest where none was":                   {strings.Replace(original, `"registry":{"digest":"sha256:0001",`, `"registry":{`, 1), reasonFilterBytes},
		"an environment more than 1":                {strings.Replace(original, `"resolved":[{`, `"resolved":[{},{`, 1), reasonFilterBytes},
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
		// The synthetic kind is pinned and declared, and the declarations
		// are the original's byte for byte: the queue's alone.
		if a, b := declarationsBytes(t, original[i].Bytes), declarationsBytes(t, changed[i].Bytes); a != b || !strings.Contains(a, `"kind":"queue"`) || strings.Contains(b, added.id) {
			t.Fatalf("%s: declarations %s, want %s", changed[i].Environment, b, a)
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

// Check 5 as ADR-0099 decides it: a run of a string passes when it lies
// inside a complete occurrence of any string at a secret position, not only
// of its own. Two references of one project share
// secret:gcp-sm:projects/<p>/secrets/, 43 characters, so under the text's
// "of R" a file holding both whole would fail.
func TestReferenceScanAnyString(t *testing.T) {
	a := "secret:gcp-sm:projects/example-dev/secrets/orders-queue/versions/3"
	b := "secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/1"
	cases := map[string]struct {
		file    string
		values  []string
		failing []int32
	}{
		"both whole":                     {a + "\n" + b + "\n", []string{a, b}, nil},
		"both whole, one line":           {a + b + "\n", []string{a, b}, nil},
		"one whole, the shared prefix":   {b + " " + a[:43] + "\n", []string{a, b}, []int32{0, 1}},
		"one whole, the other's middle":  {b + " " + a[20:60] + "\n", []string{a, b}, []int32{0}},
		"the other's middle, then whole": {a[20:60] + " " + b + "\n", []string{a, b}, []int32{0}},
		"inside a string of exactly 32":  {"x" + a[:32] + "x", []string{a, a[:32]}, nil},
		"one past a string of 32":        {"x" + a[:33] + "x", []string{a, a[:32]}, []int32{0}},
		"spanning two occurrences":       {a[:50] + "\n", []string{a, a[:40], a[10:50]}, []int32{0}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if failing := scanFails(t, c.file, c.values...); !slices.Equal(failing, c.failing) {
				t.Fatalf("failing %v, want %v", failing, c.failing)
			}
		})
	}
}

// A target's automaton is built once for a request: not again for each file
// or for the second pass, and not at all for a string no file holds.
func TestReferenceScanBuildsEachAutomatonOnce(t *testing.T) {
	a := "secret:gcp-sm:projects/example-dev/secrets/orders-queue/versions/3"
	b := "secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/1"
	s, err := newScanner([]target{{value: []byte(a)}, {value: []byte(b)}, {value: []byte("secret:gcp-sm:projects/example-prod/secrets/x/versions/9")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{a + "\n" + b + "\n", b + " " + a[20:60] + "\n", a + "\n"} {
		if _, err := s.scan([]byte(file)); err != nil {
			t.Fatal(err)
		}
	}
	if s.builds != 2 || len(s.automata) != 2 {
		t.Fatalf("builds: %d, automata: %d", s.builds, len(s.automata))
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

// declarationsBytes is the RFC 8785 form of a request's declarations member.
func declarationsBytes(t *testing.T, request []byte) string {
	t.Helper()
	value, code, _ := registry.AdmitValue(request, MaxRequestBytes)
	root, ok := value.(map[string]any)
	if code != "" || !ok {
		t.Fatalf("request refused: %s", code)
	}
	declarations, ok := root["declarations"].([]any)
	if !ok {
		t.Fatalf("no declarations: %s", request)
	}
	data, err := jcs.EncodeDocument(declarations)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A given request is held to the envelope the host writes: in the resolved
// form, the five members with declarations an array; in the facts form, the
// four, with no declarations. Its selector is the one the host writes for
// the manifest, its options are valid against the manifest's schema, and a
// manifest that runs each environment is given exactly one resolved
// document, whose environment the request is for.
func TestGivenRequestEnvelope(t *testing.T) {
	f := newFixture(t)
	resolved := f.manifest(requestMode, map[string]any{"environments": "all"})
	each := f.manifest(requestMode, nil)
	facts := f.manifest(requestMode, map[string]any{"input": "factsContracts", "environments": nil})
	unwritten := f.manifest(requestMode, map[string]any{"environments": "all", "requestVersions": []any{"9.0.0"}})
	const emitter = `"emitter":{"id":"https://example.invalid/emitters/test","version":"1.0.0"}`
	const envelope = `"emitRequestVersion":"1.0.0",` + emitter + `,"options":{}`
	const dev = `{"environment":"dev"}`
	cases := map[string]struct {
		m        *emit.Manifest
		request  string
		pointer  string // "-" for none refused
		code     string // EMIT-REQUEST when empty
		document string // the request when empty
	}{
		"not admitted, a repeated member": {resolved, `{"declarations":[],"declarations":[],` + envelope + `,"resolved":[]}`, "/declarations", "", ""},
		"another request version": {resolved, `{"declarations":[],"emitRequestVersion":"1.1.0",` + emitter + `,"options":{},"resolved":[]}`,
			"/emitRequestVersion", "EMIT-REQUEST-VERSION", ""},
		"a manifest of no version the host writes": {unwritten, `{"declarations":[],` + envelope + `,"resolved":[]}`,
			"/requestVersions", "EMIT-REQUEST-VERSION", emit.DocumentManifest},
		"no options, a member in place": {resolved, `{"declarations":[],"emitRequestVersion":"1.0.0",` + emitter + `,"other":{},"resolved":[]}`, "/options", "", ""},
		"options the schema refuses": {resolved, `{"declarations":[],"emitRequestVersion":"1.0.0",` + emitter + `,"options":{"other":true},"resolved":[]}`,
			"/other", "EMIT-OPTIONS", emit.DocumentOptions},
		"resolved {}":                  {resolved, `{"declarations":[],` + envelope + `,"resolved":{}}`, "/resolved", "", ""},
		"each, one document":           {each, `{"declarations":[],` + envelope + `,"resolved":[` + dev + `]}`, "-", "", ""},
		"each, no document":            {each, `{"declarations":[],` + envelope + `,"resolved":[]}`, "/resolved", "", ""},
		"each, two documents":          {each, `{"declarations":[],` + envelope + `,"resolved":[` + dev + `,` + dev + `]}`, "/resolved", "", ""},
		"each, no environment":         {each, `{"declarations":[],` + envelope + `,"resolved":[{}]}`, "/resolved", "", ""},
		"each, a document not object":  {each, `{"declarations":[],` + envelope + `,"resolved":["dev"]}`, "/resolved", "", ""},
		"resolved, declared":           {resolved, `{"declarations":[],` + envelope + `,"resolved":[]}`, "-", "", ""},
		"resolved, no declarations":    {resolved, `{` + envelope + `,"resolved":[]}`, "", "", ""},
		"resolved, declarations {}":    {resolved, `{"declarations":{},` + envelope + `,"resolved":[]}`, "/declarations", "", ""},
		"resolved, a member in place":  {resolved, `{"factsContracts":[],` + envelope + `,"resolved":[]}`, "/declarations", "", ""},
		"facts":                        {facts, `{` + envelope + `,"factsContracts":[]}`, "-", "", ""},
		"facts, with declarations":     {facts, `{"declarations":[],` + envelope + `,"factsContracts":[]}`, "", "", ""},
		"resolved, declared, unsorted": {resolved, `{"resolved":[],` + envelope + `,"declarations":[]}`, "-", "", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			request, refused := givenRequest([]byte(c.request), c.m)
			if c.pointer == "-" {
				if len(refused) != 0 || !strings.HasPrefix(string(request.Bytes), `{"declarations":[],`) && c.m != facts {
					t.Fatalf("refused %+v, bytes %s", refused, request.Bytes)
				}
				if want := map[*emit.Manifest]string{each: "dev"}[c.m]; request.Environment != want {
					t.Fatalf("environment %q, want %q", request.Environment, want)
				}
				return
			}
			code, document := cmp.Or(c.code, "EMIT-REQUEST"), cmp.Or(c.document, emit.DocumentRequest)
			if len(refused) != 1 || refused[0].Code != code || refused[0].Location.Document != document || refused[0].Location.Pointer != c.pointer {
				t.Fatalf("refused: %+v, want %s in %s at %q", refused, code, document, c.pointer)
			}
		})
	}
}

// Check 2 on Windows moves the variables a program finds its home by there
// with HOME, except one the manifest's env names, which is held; elsewhere
// it changes only HOME, TZ, LANG and SOURCE_DATE_EPOCH.
func TestDeterminismOverrideOnWindows(t *testing.T) {
	base := map[string]string{"HOME": "h", "TZ": "z", "LANG": "l", "SOURCE_DATE_EPOCH": "1"}
	if got := determinismOverride("linux", nil, "h", "z", "l", "1"); !maps.Equal(got, base) {
		t.Fatalf("linux: %v", got)
	}
	want := maps.Clone(base)
	want["USERPROFILE"], want["LOCALAPPDATA"] = "h", "h"
	if got := determinismOverride("windows", []string{"APPDATA"}, "h", "z", "l", "1"); !maps.Equal(got, want) {
		t.Fatalf("windows: %v", got)
	}
}

// A given request is held to the bound in its canonical form, the bytes the
// emitter reads, which can be longer than the file: 1e20 is written as 21
// digits.
func TestGivenRequestIsBoundedAsCanonical(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(requestMode, map[string]any{"environments": "all"})
	const envelope = `"emitRequestVersion":"1.0.0","emitter":{"id":"https://example.invalid/emitters/test","version":"1.0.0"},"options":{}`
	short := `{"declarations":[{"n":100}],` + envelope + `,"resolved":[]}`
	long := `{"declarations":[{"n":1e20}],` + envelope + `,"resolved":[]}`
	saved := maxGivenRequestBytes
	defer func() { maxGivenRequestBytes = saved }()
	maxGivenRequestBytes = len(long) + 5
	if _, refused := givenRequest([]byte(short), m); len(refused) != 0 {
		t.Fatalf("a request within the bound in both forms: %+v", refused)
	}
	request, refused := givenRequest([]byte(long), m)
	if len(refused) != 1 || refused[0].Code != "EMIT-REQUEST" || refused[0].Location.Document != emit.DocumentRequest {
		t.Fatalf("a request within the bound as written, over it canonical: %+v, %d bytes", refused, len(request.Bytes))
	}
}

// A given request whose canonical form passes the bound is refused as before,
// and the form is never built (#458): past admission, refusing it allocates
// less than the bound. Two are refused, at the bound's scale: one whose form
// is one 1e20 over it, and one whose file is at the bound and whose form is
// more than four times it, the shape of a 63 MiB file that cost 16.7 GB
// before. Just under the bound, the request is admitted with the bytes it
// had.
func TestGivenRequestRefusesAnExpandingFormWithoutBuildingIt(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(requestMode, map[string]any{"environments": "all"})
	const envelope = `"emitRequestVersion":"1.0.0","emitter":{"id":"https://example.invalid/emitters/test","version":"1.0.0"},"options":{}`
	text := func(numbers int, number string) string {
		return `{"declarations":[[` + strings.TrimSuffix(strings.Repeat(number+",", numbers), ",") + `]],` + envelope + `,"resolved":[]}`
	}
	const bound = 1 << 20
	saved := maxGivenRequestBytes
	defer func() { maxGivenRequestBytes = saved }()
	maxGivenRequestBytes = bound

	// 1e20 is written as 21 digits, and each number adds 22 bytes to the form.
	under := (bound - len(text(0, "")) + 1) / 22
	canonical := text(under, "100000000000000000000")
	if len(canonical) > bound || len(text(under+1, "100000000000000000000")) <= bound {
		t.Fatalf("the form of %d numbers is %d bytes", under, len(canonical))
	}
	request, refused := givenRequest([]byte(text(under, "1e20")), m)
	if len(refused) != 0 || string(request.Bytes) != canonical {
		t.Fatalf("just under the bound: %+v, %d bytes", refused, len(request.Bytes))
	}

	allocated := func(f func()) uint64 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		f()
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	atBound := (bound - len(text(0, "")) + 1) / 5
	for name, data := range map[string][]byte{
		"one number over":     []byte(text(under+1, "1e20")),
		"a file at the bound": []byte(text(atBound, "1e20")),
	} {
		if len(data) > bound {
			t.Fatalf("%s: the file is %d bytes", name, len(data))
		}
		admission := allocated(func() {
			if _, code, _ := registry.AdmitValue(data, bound); code != "" {
				t.Fatalf("%s: not admitted: %s", name, code)
			}
		})
		total := allocated(func() {
			request, refused := givenRequest(data, m)
			if len(refused) != 1 || refused[0].Code != "EMIT-REQUEST" || refused[0].Location != (emit.Location{Document: emit.DocumentRequest}) {
				t.Fatalf("%s: %+v, %d bytes", name, refused, len(request.Bytes))
			}
		})
		t.Logf("%s: %d bytes, admission %d, in all %d", name, len(data), admission, total)
		if total > admission+bound {
			t.Errorf("%s: refusing allocated %d bytes past the %d of admission, more than the bound of %d", name, total-admission, admission, bound)
		}
		// The factor: the decoder every reader shares costs 10 to 51 times
		// a document's bytes to read one of small values into a tree, the
		// same whether its numbers expand or not. This request measures
		// about 30 times the bound; 48 leaves room, and the 75 times it cost
		// while the decoder read every value through the standard decoder's
		// tokens fails it, as do the 276 times before that.
		if total > 48*bound {
			t.Errorf("%s: %d bytes allocated, more than 48 times the bound", name, total)
		}
	}

	// A request refused for its envelope is refused for it as before, its
	// form over the bound or not.
	wrong := strings.Replace(text(atBound, "1e20"), `"emitRequestVersion":"1.0.0"`, `"emitRequestVersion":"9.9.9"`, 1)
	if _, refused := givenRequest([]byte(wrong), m); len(refused) != 1 || refused[0].Code != "EMIT-REQUEST-VERSION" || refused[0].Location.Pointer != "/emitRequestVersion" {
		t.Fatalf("a request of another version: %+v", refused)
	}
}
