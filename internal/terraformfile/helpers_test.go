package terraformfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// Synthetic plans and states, written in code. Every provider, type and value
// here is made up: no real provider, account, project or credential.

const syntheticProvider = "example.test/synthetic/cloud"

// absent marks a member a fixture leaves out.
type absentValue struct{}

var absent = absentValue{}

// object builds a JSON object from name-value pairs, leaving out any member
// whose value is absent.
func object(pairs ...any) map[string]any {
	out := map[string]any{}
	for i := 0; i < len(pairs); i += 2 {
		if _, skip := pairs[i+1].(absentValue); skip {
			continue
		}
		out[pairs[i].(string)] = pairs[i+1]
	}
	return out
}

func encode(t testing.TB, value any) []byte {
	t.Helper()
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	return bytes.TrimRight(out.Bytes(), "\n")
}

func nameOfAddress(address string) string {
	trimmed := address
	if at := strings.IndexByte(trimmed, '['); at >= 0 {
		trimmed = trimmed[:at]
	}
	return trimmed[strings.LastIndexByte(trimmed, '.')+1:]
}

func typeOfAddress(address string) string {
	if at := strings.IndexByte(address, '['); at >= 0 {
		address = address[:at]
	}
	parts := strings.Split(address, ".")
	return parts[len(parts)-2]
}

// change is a resource_changes item. before_sensitive and after_sensitive
// default to false; pass absent to leave one out.
type change struct {
	address, mode         string
	index                 any
	actions               []string
	before, after         any
	beforeSens, afterSens any
	afterUnknown          any
	typ                   string
	extra                 map[string]any
	deposed               string
	providerName          any
}

func (c change) json() map[string]any {
	mode := c.mode
	if mode == "" {
		mode = "managed"
	}
	typ := c.typ
	if typ == "" {
		typ = typeOfAddress(c.address)
	}
	actions := c.actions
	if actions == nil {
		actions = []string{"no-op"}
	}
	beforeSens, afterSens := c.beforeSens, c.afterSens
	if beforeSens == nil {
		beforeSens = false
	}
	if afterSens == nil {
		afterSens = false
	}
	afterUnknown := c.afterUnknown
	if afterUnknown == nil {
		afterUnknown = map[string]any{}
	}
	index := c.index
	if index == nil {
		index = absent
	}
	provider := c.providerName
	if provider == nil {
		provider = syntheticProvider
	}
	deposed := any(absent)
	if c.deposed != "" {
		deposed = c.deposed
	}
	changeObject := object("actions", actions, "before", c.before, "after", c.after,
		"after_unknown", afterUnknown, "before_sensitive", beforeSens, "after_sensitive", afterSens)
	for name, value := range c.extra {
		changeObject[name] = value
	}
	return object("address", c.address, "mode", mode, "type", typ, "name", nameOfAddress(c.address),
		"index", index, "provider_name", provider, "deposed", deposed, "change", changeObject)
}

// plan writes a plan document with these resource changes. It also carries
// the sections the reader never reads, each holding a secret marker.
func plan(t testing.TB, changes ...change) []byte {
	t.Helper()
	items := make([]any, 0, len(changes))
	for _, c := range changes {
		items = append(items, c.json())
	}
	return encode(t, planWith(items))
}

func planWith(changes any) map[string]any {
	return object(
		"format_version", "1.2",
		"terraform_version", "0.0.0-synthetic",
		"planned_values", object("root_module", object()),
		"resource_changes", changes,
		"variables", object("db_password", object("value", "SECRET-IN-VARIABLES")),
		"configuration", object("root_module", object("variables", object("db_password", object("default", "SECRET-IN-CONFIGURATION")))),
		"output_changes", object("token", object("actions", []string{"create"}, "after", "SECRET-IN-OUTPUT", "after_sensitive", true)),
		"prior_state", object("format_version", "1.0", "values", object("root_module", object("resources", []any{object("values", object("password", "SECRET-IN-PRIOR-STATE"))}))),
		"relevant_attributes", []any{object("resource", "x", "attribute", []string{"SECRET-IN-RELEVANT"})},
		"checks", []any{object("status", "SECRET-IN-CHECKS")},
	)
}

// stateResource is a state resource.
type stateResource struct {
	address, mode string
	index         any
	values        any
	sensitive     any
	typ           string
}

func (r stateResource) json() map[string]any {
	mode := r.mode
	if mode == "" {
		mode = "managed"
	}
	typ := r.typ
	if typ == "" {
		typ = typeOfAddress(r.address)
	}
	index := r.index
	if index == nil {
		index = absent
	}
	sensitive := r.sensitive
	if sensitive == nil {
		sensitive = map[string]any{}
	}
	values := r.values
	if values == nil {
		values = map[string]any{}
	}
	return object("address", r.address, "mode", mode, "type", typ, "name", nameOfAddress(r.address),
		"index", index, "provider_name", syntheticProvider, "schema_version", 0,
		"values", values, "sensitive_values", sensitive)
}

type module struct {
	address   string
	resources []stateResource
	children  []module
}

func (m module) json(root bool) map[string]any {
	resources := any(absent)
	if len(m.resources) > 0 {
		list := []any{}
		for _, r := range m.resources {
			list = append(list, r.json())
		}
		resources = list
	}
	children := any(absent)
	if len(m.children) > 0 {
		list := []any{}
		for _, c := range m.children {
			list = append(list, c.json(false))
		}
		children = list
	}
	address := any(m.address)
	if root {
		address = absent
	}
	return object("address", address, "resources", resources, "child_modules", children)
}

func state(t testing.TB, root module) []byte {
	t.Helper()
	return encode(t, object(
		"format_version", "1.0",
		"terraform_version", "0.0.0-synthetic",
		"values", object("root_module", root.json(true),
			"outputs", object("token", object("sensitive", true, "value", "SECRET-IN-STATE-OUTPUT"))),
	))
}

func planSource(types ...string) Source {
	return Source{Document: DocumentPlan, Select: Select{Types: types}}
}

func stateSource(types ...string) Source {
	return Source{Document: DocumentState, Select: Select{Types: types}}
}

func readBytes(t testing.TB, data []byte, src Source) (*Acquisition, *Refusal) {
	t.Helper()
	acquisition, refusal, err := Read(bytes.NewReader(data), src)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return acquisition, refusal
}

func mustAccept(t testing.TB, data []byte, src Source) *Acquisition {
	t.Helper()
	acquisition, refusal := readBytes(t, data, src)
	if refusal != nil {
		t.Fatalf("refused %s", refusal.Code)
	}
	return acquisition
}

func mustRefuse(t testing.TB, data []byte, src Source, code string) *Refusal {
	t.Helper()
	acquisition, refusal := readBytes(t, data, src)
	if refusal == nil {
		t.Fatalf("accepted, want %s; value %s", code, acquisition.Value())
	}
	if refusal.Code != code {
		t.Fatalf("refused %s, want %s", refusal.Code, code)
	}
	checkRefusal(t, data, refusal)
	return refusal
}

// checkRefusal holds a refusal to its four members: the code, the whole
// input's sha256 and size, and the code's fixed message.
func checkRefusal(t testing.TB, data []byte, refusal *Refusal) {
	t.Helper()
	sum := sha256.Sum256(data)
	if refusal.SHA256 != hex.EncodeToString(sum[:]) || refusal.Bytes != int64(len(data)) {
		t.Fatalf("refusal names %s/%d, want the whole input's digest and size", refusal.SHA256, refusal.Bytes)
	}
	if refusal.Message != messages[refusal.Code] {
		t.Fatalf("message %q is not the fixed text of %s", refusal.Message, refusal.Code)
	}
	var members map[string]any
	if err := json.Unmarshal(refusal.JSON(), &members); err != nil || len(members) != 4 {
		t.Fatalf("refusal JSON %s: %v", refusal.JSON(), err)
	}
}

// resources decodes the acquired value.
func resources(t testing.TB, acquisition *Acquisition) []map[string]any {
	t.Helper()
	var out []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(acquisition.Value()))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		t.Fatalf("acquired value %s: %v", acquisition.Value(), err)
	}
	return out
}

func addresses(acquisition *Acquisition) []string {
	out := make([]string, 0, len(acquisition.Resources))
	for _, r := range acquisition.Resources {
		out = append(out, r.Address)
	}
	return out
}

// noSecret fails when any byte string holds a secret marker.
func noSecret(t testing.TB, outputs ...[]byte) {
	t.Helper()
	for _, out := range outputs {
		if bytes.Contains(out, []byte("SECRET")) {
			t.Fatalf("a value marked sensitive or never read reached the output: %s", out)
		}
	}
}

func ptr(s string) *string { return &s }

// uEscape writes a JSON escape of one UTF-16 code unit, for a literal that
// must hold the escape rather than the character.
func uEscape(hex string) string { return `\` + "u" + hex }
