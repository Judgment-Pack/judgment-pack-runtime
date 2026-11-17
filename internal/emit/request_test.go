package emit

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// topMembers is a request's top-level member names in the order its bytes
// hold them.
func topMembers(t *testing.T, data []byte) []string {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		t.Fatalf("not an object: %v", err)
	}
	var names []string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, token.(string))
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return names
}

func decodeRequest(t *testing.T, data []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

// declarationsOf checks a resolved-form request against registry companion
// §11.2 and returns its declarations' kind names: every pin of every
// environment has its kind's declaration, the SHA-256 of whose RFC 8785 form
// is the pin's digest, and every declaration has a pin.
func declarationsOf(t *testing.T, data []byte) []string {
	t.Helper()
	request := decodeRequest(t, data)
	declarations, ok := request["declarations"].([]any)
	if !ok {
		t.Fatalf("no declarations: %s", data)
	}
	digests := map[string]string{}
	var kinds []string
	for _, item := range declarations {
		declaration, _ := item.(map[string]any)
		kind, _ := declaration["kind"].(string)
		canonical, err := jcs.EncodeDocument(declaration)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, canonical) {
			t.Fatalf("the %s declaration is not in the request in its RFC 8785 form", kind)
		}
		if _, twice := digests[kind]; twice {
			t.Fatalf("the %s declaration is there twice", kind)
		}
		digests[kind] = digestOf(canonical)
		kinds = append(kinds, kind)
	}
	resolved, _ := request["resolved"].([]any)
	for _, document := range resolved {
		pins, _ := document.(map[string]any)["kinds"].([]any)
		if len(pins) != len(digests) {
			t.Fatalf("%d pins, %d declarations", len(pins), len(digests))
		}
		for _, item := range pins {
			pin, _ := item.(map[string]any)
			kind, _ := pin["kind"].(string)
			if digests[kind] == "" || digests[kind] != pin["digest"] {
				t.Fatalf("the %s pin's digest %v, its declaration's %q", kind, pin["digest"], digests[kind])
			}
		}
	}
	return kinds
}

func TestEmitResolvedRequestCarriesTheReadKindsDeclarations(t *testing.T) {
	m := mustManifest(t, manifestDoc([]string{"exit", "0"}, nil))
	requests, diagnostics := FormRequests(m, Inputs{Registry: testRegistry(t)})
	if diagnostics != nil || len(requests) != 2 {
		t.Fatalf("requests: %d %v", len(requests), codes(diagnostics))
	}
	queue := decodeRequest(t, []byte(queueKind))
	want, err := jcs.EncodeDocument(queue)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		// The five members, in RFC 8785's order, and the bytes are that form.
		if got := topMembers(t, request.Bytes); !slices.Equal(got, []string{"declarations", "emitRequestVersion", "emitter", "options", "resolved"}) {
			t.Fatalf("the %s request's members: %v", request.Environment, got)
		}
		if canonical, err := jcs.EncodeDocument(decodeRequest(t, request.Bytes)); err != nil || !bytes.Equal(canonical, request.Bytes) {
			t.Fatalf("the %s request is not in its RFC 8785 form: %v", request.Environment, err)
		}
		// Only the kind read: topic is pinned but not read.
		if kinds := declarationsOf(t, request.Bytes); !slices.Equal(kinds, []string{"queue"}) {
			t.Fatalf("the %s request declares %v", request.Environment, kinds)
		}
		if strings.Contains(string(request.Bytes), "topic") || strings.Contains(string(request.Bytes), "partitions") {
			t.Fatalf("the %s request holds the kind not read: %s", request.Environment, request.Bytes)
		}
		// The declaration is the one admitted, and its digest is the
		// registry's pin of it.
		declarations := decodeRequest(t, request.Bytes)["declarations"].([]any)
		got, err := jcs.EncodeDocument(declarations[0])
		if err != nil || !bytes.Equal(got, want) || digestOf(got) != kindDigest(t, queueKind) {
			t.Fatalf("the declaration: %s, want %s", got, want)
		}
	}
}

func TestEmitAllEnvironmentsRequestCarriesEachDeclarationOnce(t *testing.T) {
	// The manifest reads both kinds, topic first: the declarations are
	// sorted by kind, not by the order of the reads.
	m := mustManifest(t, manifestDoc([]string{"exit", "0"}, map[string]any{"environments": "all", "reads": []any{
		map[string]any{"kind": "topic", "id": "https://example.invalid/kinds/topic", "versions": "^2.0.0"},
		map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "^1.0.0"},
	}}))
	requests, diagnostics := FormRequests(m, Inputs{Registry: testRegistry(t)})
	if diagnostics != nil || len(requests) != 1 {
		t.Fatalf("requests: %d %v", len(requests), codes(diagnostics))
	}
	data := requests[0].Bytes
	if resolved, _ := decodeRequest(t, data)["resolved"].([]any); len(resolved) != 2 {
		t.Fatalf("environments: %d", len(resolved))
	}
	if kinds := declarationsOf(t, data); !slices.Equal(kinds, []string{"queue", "topic"}) {
		t.Fatalf("declarations: %v", kinds)
	}
	if n := strings.Count(string(data), `"kindFormatVersion"`); n != 2 {
		t.Fatalf("%d declarations in the request, want 2", n)
	}
}

func TestEmitFactsContractsRequestHasNoDeclarations(t *testing.T) {
	m := mustManifest(t, manifestDoc([]string{"exit", "0"}, map[string]any{"input": "factsContracts", "environments": nil}))
	requests, diagnostics := FormRequests(m, Inputs{Registry: testRegistry(t)})
	if diagnostics != nil || len(requests) != 1 {
		t.Fatalf("requests: %d %v", len(requests), codes(diagnostics))
	}
	if got := topMembers(t, requests[0].Bytes); !slices.Equal(got, []string{"emitRequestVersion", "emitter", "factsContracts", "options"}) {
		t.Fatalf("members: %v", got)
	}
}

// The request is refused, not written, unless its pins and declarations
// are of the same kinds one to one, each pin's digest its declaration's.
func TestDeclaresPins(t *testing.T) {
	pins := func(items ...string) map[string]any {
		var kinds []any
		for _, item := range items {
			kind, digest, _ := strings.Cut(item, "=")
			kinds = append(kinds, map[string]any{"kind": kind, "digest": digest})
		}
		return map[string]any{"kinds": kinds}
	}
	declared := map[string]string{"queue": "sha256:aa", "topic": "sha256:bb"}
	cases := map[string]struct {
		resolved map[string]any
		want     bool
	}{
		"each pin declared":          {pins("queue=sha256:aa", "topic=sha256:bb"), true},
		"a pin with no declaration":  {pins("queue=sha256:aa", "other=sha256:bb"), false},
		"a digest not the pin's":     {pins("queue=sha256:aa", "topic=sha256:ab"), false},
		"a declaration with no pin":  {pins("queue=sha256:aa"), false},
		"a pin twice":                {pins("queue=sha256:aa", "queue=sha256:aa"), false},
		"no pins and declarations":   {map[string]any{}, false},
		"a pin that is not a member": {map[string]any{"kinds": []any{"queue", "topic"}}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := declaresPins(c.resolved, declared); got != c.want {
				t.Fatalf("declaresPins: %v, want %v", got, c.want)
			}
		})
	}
	if !declaresPins(map[string]any{"kinds": []any{}}, map[string]string{}) {
		t.Fatal("no pins and no declarations")
	}
}

// A registry that is not valid has no declarations to give.
func TestReadDeclarationsRefusesAnInvalidRegistry(t *testing.T) {
	invalid := registry.Validate([]byte(`{"registryFormatVersion":"1.0.0"}`), nil)
	if _, _, ok := readDeclarations(invalid, nil); ok {
		t.Fatal("an invalid registry's declarations were read")
	}
}

// A request is at most MaxRequestBytes of RFC 8785 bytes: every request of
// "each", and the one of "all". The options pad each request by one ASCII
// byte per character, so the largest request is exactly the bound, then one
// byte over it.
func TestFormRequestsBoundsTheRequestBytes(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"pad": map[string]any{"type": "string"}}}
	for _, environments := range []string{"each", "all"} {
		t.Run(environments, func(t *testing.T) {
			m := mustManifest(t, manifestDoc([]string{"exit", "0"}, map[string]any{"options": schema, "environments": environments}))
			form := func(pad int) ([]Request, []Diagnostic) {
				return FormRequests(m, Inputs{Registry: testRegistry(t), Options: map[string]any{"pad": strings.Repeat("a", pad)}})
			}
			requests, diagnostics := form(0)
			if diagnostics != nil || len(requests) == 0 {
				t.Fatalf("requests: %d %v", len(requests), codes(diagnostics))
			}
			largest := 0
			for _, request := range requests {
				largest = max(largest, len(request.Bytes))
			}
			pad := MaxRequestBytes - largest

			requests, diagnostics = form(pad)
			if diagnostics != nil || len(requests) == 0 {
				t.Fatalf("at the bound: %d %v", len(requests), codes(diagnostics))
			}
			largest = 0
			for _, request := range requests {
				largest = max(largest, len(request.Bytes))
			}
			if largest != MaxRequestBytes {
				t.Fatalf("the largest request is %d bytes, want %d", largest, MaxRequestBytes)
			}
			requests = nil

			// With "each", prod's request is one byte longer than dev's, so
			// prod's alone passes the bound and the run is refused.
			requests, diagnostics = form(pad + 1)
			want := map[string]string{"each": "prod", "all": ""}[environments]
			if requests != nil || !slices.Equal(codes(diagnostics), []string{"EMIT-REQUEST@"}) ||
				diagnostics[0].Location.Document != DocumentRequest || diagnostics[0].Location.Environment != want {
				t.Fatalf("one byte over the bound: %d %+v", len(requests), diagnostics)
			}
		})
	}
}

// A kind supplied in non-canonical bytes (members out of order, white space,
// an exponent, trailing zeros, escapes and a surrogate pair) is in the
// request as admitted, in its RFC 8785 form, and the SHA-256 of that form is
// its pin's digest.
func TestEmitRequestHoldsANonCanonicalKindAsAdmitted(t *testing.T) {
	supplied := `{ "schema" : { "properties" : { "retention" : { "maximum" : 3.0E1 , "minimum" : 1.000 , "type" : "integer" } } ,
  "additionalProperties" : false , "type" : "object" , "required" : [ "retention" ] } ,
  "overlayable" : [ "\/retention" ] , "owner" : "platform" , "description" : "Café 😀" ,
  "kind" : "queue" , "version" : "1.2.0" , "id" : "https:\/\/example.invalid\/kinds\/queue" , "kindFormatVersion" : "1.0.0" }`
	admitted := `{"description":"Café 😀","id":"https://example.invalid/kinds/queue","kind":"queue",` +
		`"kindFormatVersion":"1.0.0","overlayable":["/retention"],"owner":"platform",` +
		`"schema":{"additionalProperties":false,"properties":{"retention":{"maximum":30,"minimum":1,"type":"integer"}},` +
		`"required":["retention"],"type":"object"},"version":"1.2.0"}`
	pin := kindDigest(t, supplied)
	if digestOf([]byte(admitted)) != pin {
		t.Fatalf("the admitted form's digest is not the pin's %s", pin)
	}
	text := `{"registryFormatVersion":"1.0.0","id":"https://example.invalid/registries/r","version":"1.0.0",
"environments":["dev","prod"],"owners":["platform"],
"kinds":[{"kind":"queue","id":"https://example.invalid/kinds/queue","version":"1.2.0","digest":"` + pin + `"}],
"entries":[{"kind":"queue","id":"orders","owner":"platform","value":{"retention":3}}],
"overlays":[{"environment":"prod","patches":[{"kind":"queue","id":"orders","set":{"/retention":14}}]}]}`
	result := registry.Validate([]byte(text), [][]byte{[]byte(supplied)})
	if !result.Report.Valid {
		t.Fatalf("the registry is not valid: %+v", result.Report.Diagnostics)
	}
	for _, environments := range []string{"each", "all"} {
		m := mustManifest(t, manifestDoc([]string{"exit", "0"}, map[string]any{"environments": environments}))
		requests, diagnostics := FormRequests(m, Inputs{Registry: result})
		if diagnostics != nil || len(requests) == 0 {
			t.Fatalf("%s: requests: %d %v", environments, len(requests), codes(diagnostics))
		}
		for _, request := range requests {
			if !bytes.Contains(request.Bytes, []byte(`"declarations":[`+admitted+`]`)) {
				t.Fatalf("%s: the declaration is not in its admitted form: %s", environments, request.Bytes)
			}
			if kinds := declarationsOf(t, request.Bytes); !slices.Equal(kinds, []string{"queue"}) {
				t.Fatalf("%s: declarations: %v", environments, kinds)
			}
		}
	}
}
