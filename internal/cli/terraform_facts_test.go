package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/terraformfile"
)

// Synthetic plans and states, written by the tests. Every provider, type,
// address and value is made up: nothing here was recorded from Terraform.

const tfProvider = "example.test/synthetic/cloud"

// tfChange is one resource_changes item of a synthetic plan.
type tfChange struct {
	address, typ, name, mode string
	index                    any
	actions                  []string
	before, after            any
	beforeSens, afterSens    any
}

func (c tfChange) json() map[string]any {
	item := map[string]any{"address": c.address, "type": c.typ, "name": c.name, "mode": c.mode, "provider_name": tfProvider}
	if item["mode"] == "" {
		item["mode"] = "managed"
	}
	if c.index != nil {
		item["index"] = c.index
	}
	change := map[string]any{"actions": c.actions, "before": c.before, "after": c.after, "after_unknown": map[string]any{},
		"before_sensitive": c.beforeSens, "after_sensitive": c.afterSens}
	for _, mirror := range []string{"before_sensitive", "after_sensitive"} {
		if change[mirror] == nil {
			change[mirror] = false
		}
	}
	item["change"] = change
	return item
}

func tfPlan(t *testing.T, changes ...tfChange) []byte {
	t.Helper()
	items := []any{}
	for _, c := range changes {
		items = append(items, c.json())
	}
	return tfEncode(t, map[string]any{
		"format_version":    "1.2",
		"terraform_version": "0.0.0-synthetic",
		"planned_values":    map[string]any{"root_module": map[string]any{}},
		"resource_changes":  items,
		"variables":         map[string]any{"db_password": map[string]any{"value": "SECRET-IN-VARIABLES"}},
	})
}

// tfResource is one resource of a synthetic state.
type tfResource struct {
	address, typ, name, mode string
	index                    any
	values, sensitive        any
}

func (r tfResource) json() map[string]any {
	item := map[string]any{"address": r.address, "type": r.typ, "name": r.name, "mode": r.mode, "provider_name": tfProvider,
		"schema_version": 0, "values": r.values, "sensitive_values": r.sensitive}
	if item["mode"] == "" {
		item["mode"] = "managed"
	}
	if r.index != nil {
		item["index"] = r.index
	}
	if r.values == nil {
		item["values"] = map[string]any{}
	}
	if r.sensitive == nil {
		item["sensitive_values"] = map[string]any{}
	}
	return item
}

func tfModule(address string, resources []tfResource, children ...map[string]any) map[string]any {
	module := map[string]any{}
	if address != "" {
		module["address"] = address
	}
	list := []any{}
	for _, r := range resources {
		list = append(list, r.json())
	}
	module["resources"] = list
	if len(children) > 0 {
		module["child_modules"] = children
	}
	return module
}

func tfState(t *testing.T, root map[string]any) []byte {
	t.Helper()
	return tfEncode(t, map[string]any{
		"format_version":    "1.0",
		"terraform_version": "0.0.0-synthetic",
		"values": map[string]any{"root_module": root,
			"outputs": map[string]any{"token": map[string]any{"sensitive": true, "value": "SECRET-IN-OUTPUT"}}},
	})
}

func tfEncode(t *testing.T, value any) []byte {
	t.Helper()
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	return bytes.TrimRight(out.Bytes(), "\n")
}

// tfLine is one decoded batch line.
type tfLine struct {
	ID    string `json:"id"`
	Facts struct {
		Terraform struct {
			Document      string `json:"document"`
			FormatVersion string `json:"formatVersion"`
			SHA256        string `json:"sha256"`
		} `json:"terraform"`
		Resource map[string]any `json:"resource"`
	} `json:"facts"`
}

// tfLines decodes a batch, and holds every line to RFC 8785: each is its own
// canonical form, and ends with one newline.
func tfLines(t *testing.T, out string) []tfLine {
	t.Helper()
	if out == "" {
		return nil
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("the batch does not end with a newline: %q", out)
	}
	var lines []tfLine
	for _, text := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			t.Fatalf("line %q: %v", text, err)
		}
		canonical, err := jcs.EncodeDocument(decoded)
		if err != nil || string(canonical) != text {
			t.Fatalf("line is not canonical:\n got %s\njcs  %s (%v)", text, canonical, err)
		}
		var line tfLine
		if err := json.Unmarshal([]byte(text), &line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	return lines
}

func tfIDs(lines []tfLine) []string {
	out := []string{}
	for _, line := range lines {
		out = append(out, line.ID)
	}
	return out
}

func tfFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func sqlUser(address, name string, actions []string, before, after any) tfChange {
	return tfChange{address: address, typ: "synthetic_sql_user", name: name, actions: actions, before: before, after: after}
}

func TestTerraformFactsWritesAPlanBatch(t *testing.T) {
	data := tfPlan(t,
		sqlUser("synthetic_sql_user.writer", "writer", []string{"update"}, map[string]any{"role": "reader"}, map[string]any{"role": "writer", "port": 5432.0}),
		sqlUser("synthetic_sql_user.added", "added", []string{"create"}, nil, map[string]any{"role": "reader"}),
		sqlUser("synthetic_sql_user.gone", "gone", []string{"delete"}, map[string]any{"role": "admin"}, nil),
		tfChange{address: "synthetic_bucket.logs", typ: "synthetic_bucket", name: "logs", actions: []string{"delete"}, before: map[string]any{}},
	)
	plan := tfFile(t, "plan.json", data)
	code, out, stderr := runTest(t, []string{"terraform", "facts", "--plan", plan, "--type", "synthetic_sql_user", "--actions", "delete,update"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("%d %q %q", code, out, stderr)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	want := `{"facts":{"resource":{"actions":["update"],"address":"synthetic_sql_user.writer","after":{"port":"5432","role":"writer"},"afterUnknown":[],"before":{"role":"reader"},"mode":"managed","name":"writer","providerName":"` + tfProvider + `","type":"synthetic_sql_user"},` +
		`"terraform":{"document":"plan","formatVersion":"1.2","sha256":"` + digest + `"}},"id":"synthetic_sql_user.writer"}`
	lines := tfLines(t, out)
	if got := tfIDs(lines); strings.Join(got, " ") != "synthetic_sql_user.gone synthetic_sql_user.writer" {
		t.Fatalf("ids %v", got)
	}
	if last := strings.Split(strings.TrimSuffix(out, "\n"), "\n")[1]; last != want {
		t.Fatalf("line:\n got %s\nwant %s", last, want)
	}
	// Without --actions, every change of the type, in address order.
	code, out, _ = runTest(t, []string{"terraform", "facts", "--plan", plan, "--type", "synthetic_sql_user,synthetic_bucket"}, "")
	if got := tfIDs(tfLines(t, out)); code != 0 || strings.Join(got, " ") != "synthetic_bucket.logs synthetic_sql_user.added synthetic_sql_user.gone synthetic_sql_user.writer" {
		t.Fatalf("%d %v", code, got)
	}
	// The same file from standard input gives the same batch.
	code, again, _ := runTest(t, []string{"terraform", "facts", "--plan", "-", "--type", "synthetic_sql_user", "--type", "synthetic_bucket"}, string(data))
	if code != 0 || again != out {
		t.Fatalf("standard input: %d\n%s\n%s", code, again, out)
	}
	// Nothing selected is an empty batch, and exit 0.
	code, out, stderr = runTest(t, []string{"terraform", "facts", "--plan", plan, "--type", "synthetic_sql_user", "--actions", "read"}, "")
	if code != 0 || out != "" || stderr != "" {
		t.Fatalf("empty selection: %d %q %q", code, out, stderr)
	}
}

func TestTerraformFactsWritesAStateBatchWithoutSensitiveValues(t *testing.T) {
	data := tfState(t, tfModule("", []tfResource{
		{address: "synthetic_sql_user.app", typ: "synthetic_sql_user", name: "app",
			values:    map[string]any{"role": "reader", "password": "SECRET-PASSWORD", "hosts": []any{"a", "SECRET-HOST"}},
			sensitive: map[string]any{"password": true, "hosts": []any{false, true}}},
	}))
	code, out, stderr := runTest(t, []string{"terraform", "facts", "--state", tfFile(t, "state.json", data), "--type", "synthetic_sql_user"}, "")
	lines := tfLines(t, out)
	if code != 0 || stderr != "" || len(lines) != 1 || strings.Contains(out, "SECRET") {
		t.Fatalf("%d %q %q", code, out, stderr)
	}
	line := lines[0]
	if line.Facts.Terraform.Document != "state" || line.Facts.Terraform.FormatVersion != "1.0" || fmt.Sprint(line.Facts.Resource["values"]) != "map[role:reader]" {
		t.Fatalf("%+v", line)
	}
}

func TestTerraformFactsSelectsChildModulesAndInstancesByAddress(t *testing.T) {
	user := func(address, name string, index any) tfResource {
		return tfResource{address: address, typ: "synthetic_sql_user", name: name, index: index}
	}
	data := tfState(t, tfModule("", []tfResource{user("synthetic_sql_user.root", "root", nil)},
		tfModule("module.db", []tfResource{user("module.db.synthetic_sql_user.app", "app", nil)},
			tfModule("module.db.module.inner", []tfResource{user("module.db.module.inner.synthetic_sql_user.deep", "deep", nil)})),
		tfModule("module.replica[0]", []tfResource{user("module.replica[0].synthetic_sql_user.app", "app", nil)}),
		tfModule("module.pool", []tfResource{
			user("module.pool.synthetic_sql_user.member[0]", "member", 0.0),
			user(`module.pool.synthetic_sql_user.named["ro"]`, "named", "ro"),
		}),
	))
	state := tfFile(t, "state.json", data)
	for _, c := range []struct {
		pattern string
		want    string
	}{
		// * never crosses '.', '[' or ']': a counted module or instance needs [*].
		{"module.*.synthetic_sql_user.*", "module.db.synthetic_sql_user.app"},
		{"module.*.synthetic_sql_user.*[*]", `module.pool.synthetic_sql_user.member[0] module.pool.synthetic_sql_user.named["ro"]`},
		{"module.*[*].synthetic_sql_user.*", "module.replica[0].synthetic_sql_user.app"},
		{"module.*.module.*.synthetic_sql_user.*", "module.db.module.inner.synthetic_sql_user.deep"},
	} {
		code, out, stderr := runTest(t, []string{"terraform", "facts", "--state", state, "--type", "synthetic_sql_user", "--address", c.pattern}, "")
		if got := strings.Join(tfIDs(tfLines(t, out)), " "); code != 0 || got != c.want {
			t.Errorf("%s: %d %q, want %q (%s)", c.pattern, code, got, c.want, stderr)
		}
	}
	// Every resource, child modules included, without --address.
	code, out, _ := runTest(t, []string{"terraform", "facts", "--state", state, "--type", "synthetic_sql_user"}, "")
	lines := tfLines(t, out)
	if code != 0 || len(lines) != 6 || lines[2].Facts.Resource["index"] != "0" || lines[3].Facts.Resource["index"] != "ro" {
		t.Fatalf("%d %s", code, out)
	}
}

func TestTerraformFactsSelectsDataResources(t *testing.T) {
	data := tfPlan(t,
		tfChange{address: "synthetic_zone.main", typ: "synthetic_zone", name: "main", actions: []string{"no-op"}, after: map[string]any{}},
		tfChange{address: "data.synthetic_zone.main", typ: "synthetic_zone", name: "main", mode: "data", actions: []string{"read"}, after: map[string]any{"id": "z"}},
	)
	plan := tfFile(t, "plan.json", data)
	for mode, want := range map[string]string{"data": "data.synthetic_zone.main", "managed": "synthetic_zone.main", "": "synthetic_zone.main"} {
		args := []string{"terraform", "facts", "--plan", plan, "--type", "synthetic_zone"}
		if mode != "" {
			args = append(args, "--mode", mode)
		}
		code, out, _ := runTest(t, args, "")
		if got := strings.Join(tfIDs(tfLines(t, out)), " "); code != 0 || got != want {
			t.Errorf("mode %q: %d %q", mode, code, got)
		}
	}
}

// tfRefused holds a refusal's output: no line on standard output, and on
// standard error the refusal alone, with the code's fixed message.
func tfRefused(t *testing.T, code int, out, stderr string, data []byte, want string) {
	t.Helper()
	if code != 2 || out != "" {
		t.Fatalf("exit %d, stdout %q, want 2 and nothing", code, out)
	}
	sum := sha256.Sum256(data)
	refusal := `{"bytes":` + fmt.Sprint(len(data)) + `,"code":"` + want + `","message":` + string(mustJCS(t, terraformfile.Message(want))) + `,"sha256":"` + hex.EncodeToString(sum[:]) + `"}` + "\n"
	if stderr != refusal {
		t.Fatalf("stderr:\n got %q\nwant %q", stderr, refusal)
	}
}

func mustJCS(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := jcs.EncodeDocument(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestTerraformFactsHoldsMaxAndMaxBytes(t *testing.T) {
	data := tfPlan(t,
		sqlUser("synthetic_sql_user.a", "a", []string{"create"}, nil, map[string]any{}),
		sqlUser("synthetic_sql_user.b", "b", []string{"create"}, nil, map[string]any{}),
		sqlUser("synthetic_sql_user.c", "c", []string{"create"}, nil, map[string]any{}),
	)
	plan := tfFile(t, "plan.json", data)
	base := []string{"terraform", "facts", "--plan", plan, "--type", "synthetic_sql_user"}
	code, out, _ := runTest(t, append(base, "--max", "3"), "")
	if code != 0 || len(tfLines(t, out)) != 3 {
		t.Fatalf("max 3: %d %q", code, out)
	}
	code, out, stderr := runTest(t, append(base, "--max", "2"), "")
	tfRefused(t, code, out, stderr, data, terraformfile.CodeSelectBound)

	size := fmt.Sprint(len(data))
	code, out, _ = runTest(t, append(base, "--max-bytes", size), "")
	if code != 0 || len(tfLines(t, out)) != 3 {
		t.Fatalf("max-bytes at the file's size: %d %q", code, out)
	}
	code, out, stderr = runTest(t, append(base, "--max-bytes", fmt.Sprint(len(data)-1)), "")
	tfRefused(t, code, out, stderr, data, terraformfile.CodeSize)
	// From standard input, too.
	code, out, stderr = runTest(t, []string{"terraform", "facts", "--plan", "-", "--type", "synthetic_sql_user", "--max-bytes", fmt.Sprint(len(data) - 1)}, string(data))
	tfRefused(t, code, out, stderr, data, terraformfile.CodeSize)
}

func TestTerraformFactsRefusesAnUnusableCommandLine(t *testing.T) {
	plan := tfFile(t, "plan.json", tfPlan(t, sqlUser("synthetic_sql_user.a", "a", []string{"create"}, nil, map[string]any{})))
	state := tfFile(t, "state.json", tfState(t, tfModule("", nil)))
	tooMany := make([]string, terraformfile.MaxTypes+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("synthetic_t%d", i)
	}
	for name, args := range map[string][]string{
		"actions with --state":     {"--state", state, "--type", "synthetic_sql_user", "--actions", "delete"},
		"no --type":                {"--plan", plan},
		"an empty --type":          {"--plan", plan, "--type", ""},
		"an empty type in --type":  {"--plan", plan, "--type", "synthetic_a,,synthetic_b"},
		"65 types":                 {"--plan", plan, "--type", strings.Join(tooMany, ",")},
		"a type twice":             {"--plan", plan, "--type", "synthetic_a", "--type", "synthetic_a"},
		"neither file":             {"--type", "synthetic_sql_user"},
		"both files":               {"--plan", plan, "--state", state, "--type", "synthetic_sql_user"},
		"an unknown action":        {"--plan", plan, "--type", "synthetic_sql_user", "--actions", "replace"},
		"an empty --actions":       {"--plan", plan, "--type", "synthetic_sql_user", "--actions", ""},
		"an unknown mode":          {"--plan", plan, "--type", "synthetic_sql_user", "--mode", "ephemeral"},
		"an empty --address":       {"--plan", plan, "--type", "synthetic_sql_user", "--address", ""},
		"a long --address":         {"--plan", plan, "--type", "synthetic_sql_user", "--address", strings.Repeat("*", terraformfile.MaxPatternChars+1)},
		"--max 0":                  {"--plan", plan, "--type", "synthetic_sql_user", "--max", "0"},
		"--max 10001":              {"--plan", plan, "--type", "synthetic_sql_user", "--max", "10001"},
		"--max-bytes 0":            {"--plan", plan, "--type", "synthetic_sql_user", "--max-bytes", "0"},
		"--max-bytes past 256 MiB": {"--plan", plan, "--type", "synthetic_sql_user", "--max-bytes", "268435457"},
		"a remote path":            {"--plan", "https://example.invalid/plan.json", "--type", "synthetic_sql_user"},
	} {
		code, out, stderr := runTest(t, append([]string{"terraform", "facts"}, args...), "")
		if code != 3 || out != "" || !strings.HasPrefix(stderr, "error: ") || strings.Contains(stderr, "TF-") {
			t.Errorf("%s: %d %q %q", name, code, out, stderr)
		}
	}
	// At the bounds, each flag is usable.
	at := []string{"--plan", plan, "--type", strings.Join(tooMany[:terraformfile.MaxTypes], ","), "--max", "10000", "--max-bytes", "268435456",
		"--address", strings.Repeat("*", terraformfile.MaxPatternChars), "--actions", "create,update,delete,read,no-op"}
	if code, _, stderr := runTest(t, append([]string{"terraform", "facts"}, at...), ""); code != 0 {
		t.Fatalf("at the bounds: %d %q", code, stderr)
	}
	// A missing file is an input that could not be read.
	if code, out, _ := runTest(t, []string{"terraform", "facts", "--plan", filepath.Join(t.TempDir(), "absent.json"), "--type", "synthetic_sql_user"}, ""); code != 4 || out != "" {
		t.Fatalf("a missing file: %d %q", code, out)
	}
}

// A refused file whose member names are themselves secret: the diagnostic
// carries the code, the digest, the size and the fixed message, and never a
// name, a value or the path from the command line.
func TestTerraformFactsDiagnosticsNeverCarryTheFile(t *testing.T) {
	secret := "SECRET-NAME-xyzzy"
	dir := filepath.Join(t.TempDir(), "PATH-"+secret)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	missingMirror := tfPlan(t, sqlUser("synthetic_sql_user.a", "a", []string{"create"}, nil, map[string]any{secret: "SECRET-VALUE"}))
	missingMirror = bytes.Replace(missingMirror, []byte(`"after_sensitive":false,`), nil, 1)
	for code, data := range map[string][]byte{
		terraformfile.CodeJSON:        []byte(`{"format_version":"1.2","planned_values":{},"` + secret + `":1,"` + secret + `":2}`),
		terraformfile.CodeSensitivity: missingMirror,
		terraformfile.CodeNumber:      tfPlan(t, sqlUser("synthetic_sql_user.a", "a", []string{"create"}, nil, map[string]any{secret: json.Number("1e99")})),
	} {
		if !bytes.Contains(data, []byte(secret)) {
			t.Fatalf("%s: the fixture lost its secret name", code)
		}
		path := filepath.Join(dir, secret+".json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		exit, out, stderr := runTest(t, []string{"terraform", "facts", "--plan", path, "--type", "synthetic_sql_user"}, "")
		tfRefused(t, exit, out, stderr, data, code)
		if strings.Contains(stderr, "SECRET") || strings.Contains(stderr, "xyzzy") {
			t.Fatalf("%s: the diagnostic carries the file: %q", code, stderr)
		}
	}
}

func TestTerraformFactsIsDeterministic(t *testing.T) {
	data := tfPlan(t,
		sqlUser(`synthetic_sql_user.b["x"]`, "b", []string{"update"}, map[string]any{"z": 1.5, "a": []any{true, nil}}, map[string]any{"z": 2.0, "é": "\u2028"}),
		sqlUser("synthetic_sql_user.a", "a", []string{"create"}, nil, map[string]any{"n": 1e20}),
	)
	plan := tfFile(t, "plan.json", data)
	args := []string{"terraform", "facts", "--plan", plan, "--type", "synthetic_sql_user"}
	_, first, _ := runTest(t, args, "")
	_, second, _ := runTest(t, args, "")
	if first == "" || first != second {
		t.Fatalf("two runs differ:\n%s\n%s", first, second)
	}
	if got := strings.Join(tfIDs(tfLines(t, first)), " "); got != `synthetic_sql_user.a synthetic_sql_user.b["x"]` {
		t.Fatalf("ids %q", got)
	}
}

// sqlUserPack is a synthetic pack over a state resource's facts: a reader
// is allowed, any other role needs review.
const sqlUserPack = `{"specVersion":"0.2.0-draft","id":"https://example.invalid/judgment-packs/sql-user-role","version":"0.1.0","title":"Synthetic SQL user role review","description":"Synthetic content for runtime testing; it authorizes nothing.","decision":{"intent":"Synthetic.","question":"Synthetic?"},` +
	`"applicability":{"op":"fact","path":"/resource/type","operator":"equals","value":"synthetic_sql_user"},` +
	`"outcomes":[{"id":"allowed","label":"Allowed"},{"id":"needs-review","label":"Needs review"}],` +
	`"rules":[{"id":"rule-reader","description":"Synthetic rule.","onUnknown":"escalate","rationale":"Synthetic.","when":{"op":"fact","path":"/resource/values/role","operator":"equals","value":"reader"},"outcome":"allowed"},` +
	`{"id":"rule-other","description":"Synthetic rule.","onUnknown":"escalate","rationale":"Synthetic.","when":{"op":"fact","path":"/resource/values/role","operator":"not-equals","value":"reader"},"outcome":"needs-review"}],` +
	`"escalation":{"triggers":["unknown"],"target":{"kind":"human-role","name":"Reviewer"},"message":"Synthetic."}}`

// The batch terraform facts writes is a file evaluate reads unchanged, and the
// outcome gate judges it.
func TestTerraformFactsFeedsTheOutcomeGate(t *testing.T) {
	pack := writeDocument(t, "pack.json", sqlUserPack)
	user := func(name, role string) tfResource {
		return tfResource{address: "synthetic_sql_user." + name, typ: "synthetic_sql_user", name: name, values: map[string]any{"role": role}}
	}
	for name, test := range map[string]struct {
		users []tfResource
		gate  int
	}{
		"readers only, passes": {[]tfResource{user("a", "reader"), user("b", "reader")}, 0},
		"an admin, a finding":  {[]tfResource{user("a", "reader"), user("b", "admin")}, 1},
	} {
		state := tfFile(t, "state.json", tfState(t, tfModule("", test.users)))
		code, out, stderr := runTest(t, []string{"terraform", "facts", "--state", state, "--type", "synthetic_sql_user"}, "")
		if code != 0 {
			t.Fatalf("%s: facts: %d %q", name, code, stderr)
		}
		batch := writeDocument(t, "batch.jsonl", out)
		code, out, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--batch", batch, "--format", "json", "--require-outcome", "allowed"}, "")
		if code != test.gate || strings.Count(out, "\n") != len(test.users) || strings.Contains(out, `"error"`) {
			t.Errorf("%s: gate %d, want %d:\n%s\n%s", name, code, test.gate, out, stderr)
		}
	}
}

// A facts document is held to the limit registry facts holds its documents
// to: one exactly at carrier.HardMaxBytes is written, and one a byte longer
// refuses the whole run with FACTS-DOCUMENT-LIMIT, writing no line. Ten
// strings just under the carrier's 1 MiB string bound, and a pad, make the
// document; the file stays far under the default maxBytes.
func TestTerraformFactsHoldsTheFactsDocumentLimit(t *testing.T) {
	const prefix, suffix = `{"facts":`, `,"id":"synthetic_blob.big"}` + "\n"
	run := func(pad int) ([]byte, int, string, string) {
		values := map[string]any{"pad": strings.Repeat("p", pad)}
		for i := 0; i < 10; i++ {
			values[fmt.Sprintf("s%d", i)] = strings.Repeat("a", 1_000_000)
		}
		data := tfState(t, tfModule("", []tfResource{{address: "synthetic_blob.big", typ: "synthetic_blob", name: "big", values: values}}))
		code, out, stderr := runTest(t, []string{"terraform", "facts", "--state", tfFile(t, "state.json", data), "--type", "synthetic_blob"}, "")
		return data, code, out, stderr
	}
	_, code, out, stderr := run(0)
	if code != 0 || !strings.HasPrefix(out, prefix) || !strings.HasSuffix(out, suffix) {
		t.Fatalf("no pad: %d %.200q %q", code, out, stderr)
	}
	pad := int(carrier.HardMaxBytes) - (len(out) - len(prefix) - len(suffix))
	_, code, out, stderr = run(pad)
	if code != 0 || len(out)-len(prefix)-len(suffix) != int(carrier.HardMaxBytes) {
		t.Fatalf("at the limit: %d, %d bytes, %q", code, len(out)-len(prefix)-len(suffix), stderr)
	}
	data, code, out, stderr := run(pad + 1)
	if code != 2 || out != "" {
		t.Fatalf("a byte over the limit: %d %.200q", code, out)
	}
	sum := sha256.Sum256(data)
	want := `{"bytes":` + fmt.Sprint(len(data)) + `,"code":"FACTS-DOCUMENT-LIMIT","message":` + string(mustJCS(t, registry.FactsMessage(registry.CodeFactsDocumentLimit))) + `,"sha256":"` + hex.EncodeToString(sum[:]) + `"}` + "\n"
	if stderr != want {
		t.Fatalf("stderr:\n got %q\nwant %q", stderr, want)
	}
}
