package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Every fixture is synthetic: example.invalid ids and made-up projects.

// customRegistry pins the given kinds, in the order given, and holds the
// given entries in two environments.
func customRegistry(t *testing.T, entries string, kinds ...string) (string, []string) {
	t.Helper()
	var pins []string
	for _, kind := range kinds {
		root := decode(t, kind)
		pins = append(pins, `{"kind":"`+root["kind"].(string)+`","id":"`+root["id"].(string)+`","version":"`+root["version"].(string)+`","digest":"`+digestOf(t, kind)+`"}`)
	}
	return `{"registryFormatVersion":"1.0.0","id":"https://example.invalid/registries/facts","version":"1.0.0",
"environments":["dev","prod"],"owners":["team-a"],
"secretScopes":{"dev":{"gcp-sm":["example-dev"]},"prod":{"gcp-sm":["example-prod"]}},
"kinds":[` + strings.Join(pins, ",") + `],"entries":[` + entries + `]}`, kinds
}

func facts(t *testing.T, result *Result, kind string, environments []string, qualified bool) []string {
	t.Helper()
	var out bytes.Buffer
	diagnostics, err := result.WriteFacts(kind, environments, qualified, &out, nil)
	if err != nil || len(diagnostics) > 0 {
		t.Fatalf("WriteFacts: %v %+v", err, diagnostics)
	}
	text := out.String()
	if text == "" {
		return nil
	}
	if !strings.HasSuffix(text, "\n") {
		t.Fatalf("a batch ends with a newline: %q", text)
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func TestNormalDecimal(t *testing.T) {
	cases := map[string]string{
		"120": "120", "1.50": "1.5", "1.0": "1", "100": "100", "1e-7": "0.0000001", "0.0000001": "0.0000001",
		"1e2": "100", "1E+2": "100", "0": "0", "-0.5": "-0.5", "0.10": "0.1", "2.5e-3": "0.0025", "-12.340": "-12.34",
		"9007199254740991": "9007199254740991", "-9007199254740991": "-9007199254740991",
	}
	for literal, want := range cases {
		got, ok := normalDecimal(json.Number(literal))
		if !ok || got != want {
			t.Errorf("%s: got %q %v, want %q", literal, got, ok, want)
		}
		if !decimalGrammar.MatchString(got) {
			t.Errorf("%s: %q is not a Core decimal", literal, got)
		}
	}
}

func TestNegativeZeroIsRefusedAtAdmission(t *testing.T) {
	registry, kinds := fixture(t)
	registry = strings.Replace(registry, `"connectionLimit": 20`, `"connectionLimit": -0`, 1)
	result := run(registry, kinds)
	if result.Report.Valid || !hasDiagnostic(result.Report.Diagnostics, "REG-DOC-NUMBER") {
		t.Fatalf("negative zero must be refused: %+v", result.Report.Diagnostics)
	}
	if _, err := result.WriteFacts("db-role", []string{"dev"}, false, &bytes.Buffer{}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an invalid registry gives no facts: %v", err)
	}
}

func hasDiagnostic(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestFactsBatchIsTheMappedEntries(t *testing.T) {
	registry, kinds := fixture(t)
	result := run(registry, kinds)
	digest := result.Report.Registry.Digest
	lines := facts(t, result, "db-role", []string{"prod"}, false)
	want := `{"facts":{"entry":{"id":"billing-api","kind":"db-role","owner":"team-billing"},"environment":"prod",` +
		`"references":{"/database":{"id":"billing","kind":"database","value":{"engine":"postgres"}}},` +
		`"registry":{"digest":"` + digest + `","id":"https://example.invalid/registries/platform","version":"4.0.0"},` +
		`"value":{"connectionLimit":"120","database":"entry:database/billing","password":{"scope":"example-prod","store":"gcp-sm"},"privileges":["read","write"]}},` +
		`"id":"db-role/billing-api"}`
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("prod batch:\n got %q\nwant %q", lines, want)
	}

	all := facts(t, result, "db-role", []string{"dev", "staging", "prod"}, true)
	var ids []string
	for _, line := range all {
		var item struct {
			ID    string         `json:"id"`
			Facts map[string]any `json:"facts"`
		}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, item.ID)
		if strings.Contains(line, "secret:") {
			t.Errorf("a facts document carries a secret reference: %s", line)
		}
		if item.ID == "dev:db-role/billing-backfill" {
			// A reference with a pointer carries the addressed value.
			reference := item.Facts["references"].(map[string]any)["/database"].(map[string]any)
			if reference["value"] != "postgres" || item.Facts["value"].(map[string]any)["database"] != "entry:database/billing#/engine" {
				t.Errorf("a pointer reference: %s", line)
			}
		}
	}
	if strings.Join(ids, " ") != "dev:db-role/billing-api dev:db-role/billing-backfill staging:db-role/billing-api staging:db-role/billing-backfill prod:db-role/billing-api" {
		t.Errorf("ids and order: %v", ids)
	}

	// The batch depends on the registry and kinds, not on the order the
	// kinds are supplied in.
	again := facts(t, run(registry, []string{kinds[1], kinds[0]}), "db-role", []string{"dev", "staging", "prod"}, true)
	if strings.Join(again, "\n") != strings.Join(all, "\n") {
		t.Error("the batch depends on the order of the kinds")
	}
	if lines := facts(t, result, "database", []string{"prod"}, false); len(lines) != 1 || !strings.HasSuffix(lines[0], `"references":{},"registry":{"digest":"`+digest+`","id":"https://example.invalid/registries/platform","version":"4.0.0"},"value":{"engine":"postgres"}},"id":"database/billing"}`) {
		t.Errorf("a kind with no references: %q", lines)
	}
}

func TestFactsRequestRefusals(t *testing.T) {
	registry, kinds := fixture(t)
	result := run(registry, kinds)
	called := false
	ready := func() error { called = true; return nil }
	for name, test := range map[string]struct {
		kind         string
		environments []string
		want         error
	}{
		"an unpinned kind":         {"cache", []string{"prod"}, ErrNoKind},
		"an undeclared env":        {"db-role", []string{"qa"}, ErrNoEnvironment},
		"no environment":           {"db-role", nil, ErrNoEnvironment},
		"one undeclared among two": {"db-role", []string{"prod", "qa"}, ErrNoEnvironment},
	} {
		var out bytes.Buffer
		if _, err := result.WriteFacts(test.kind, test.environments, false, &out, ready); !errors.Is(err, test.want) || out.Len() > 0 || called {
			t.Errorf("%s: %v, %d bytes written", name, err, out.Len())
		}
	}
	// A registry that reads but does not validate gives no facts and no
	// contract, whatever was resolved before the finding.
	invalid := run(strings.Replace(registry, `"connectionLimit": 20`, `"connectionLimit": 999`, 1), kinds)
	if invalid.Report.Valid || invalid.registry == nil {
		t.Fatalf("want a registry read and refused: %+v", invalid.Report)
	}
	var out bytes.Buffer
	if _, err := invalid.WriteFacts("db-role", []string{"dev"}, false, &out, ready); !errors.Is(err, ErrInvalid) || out.Len() > 0 || called {
		t.Errorf("an invalid registry: %v, %d bytes", err, out.Len())
	}
	if _, _, err := invalid.FactsContract("db-role"); !errors.Is(err, ErrInvalid) {
		t.Errorf("an invalid registry's contract: %v", err)
	}
}

const nodeKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/node","version":"1.0.0","kind":"node","owner":"team-a",
"schema":{"type":"object","additionalProperties":false,"properties":{"next":{"type":"string"},"size":{"type":"number"}}},
"references":[{"pointer":"/next","kinds":["node"]}]}`

func TestFactsReferencesGoOneLevel(t *testing.T) {
	registry, kinds := customRegistry(t, `{"kind":"node","id":"a","owner":"team-a","value":{"next":"entry:node/b","size":1.50}},
{"kind":"node","id":"b","owner":"team-a","value":{"next":"entry:node/a","size":2e1}}`, nodeKind)
	result := run(registry, kinds)
	if !result.Report.Valid {
		t.Fatalf("%+v", result.Report.Diagnostics)
	}
	lines := facts(t, result, "node", []string{"dev"}, false)
	if len(lines) != 2 {
		t.Fatalf("%q", lines)
	}
	// The cycle a -> b -> a is not followed: b's own reference stays the
	// string it is.
	if !strings.Contains(lines[0], `"references":{"/next":{"id":"b","kind":"node","value":{"next":"entry:node/a","size":"20"}}}`) ||
		!strings.Contains(lines[0], `"value":{"next":"entry:node/b","size":"1.5"}`) || strings.Count(lines[0], `"references"`) != 1 {
		t.Errorf("one level: %s", lines[0])
	}
}

const blobKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/blob","version":"1.0.0","kind":"blob","owner":"team-a",
"schema":{"type":"object","properties":{"data":{"type":"string"}}},"bounds":{"maxEntryBytes":2097152}}`

func TestFactsDocumentOverTheLimitIsReportedNotWritten(t *testing.T) {
	big := strings.Repeat("a", 1<<20+1)
	registry, kinds := customRegistry(t, `{"kind":"blob","id":"large","owner":"team-a","value":{"data":"`+big+`"}},
{"kind":"blob","id":"small","owner":"team-a","value":{"data":"b"}}`, blobKind)
	result := run(registry, kinds)
	if !result.Report.Valid {
		t.Fatalf("%+v", result.Report.Diagnostics)
	}
	var out bytes.Buffer
	called := false
	diagnostics, err := result.WriteFacts("blob", []string{"dev", "prod"}, true, &out, func() error { called = true; return nil })
	if err != nil || out.Len() > 0 || called {
		t.Fatalf("nothing is written when one document is over the limit: %v, %d bytes, ready called %v", err, out.Len(), called)
	}
	if len(diagnostics) != 2 {
		t.Fatalf("one diagnostic per environment: %+v", diagnostics)
	}
	for i, environment := range []string{"dev", "prod"} {
		d := diagnostics[i]
		if d.Code != "FACTS-DOCUMENT-LIMIT" || d.Severity != SeverityError || d.Location.Document != DocumentFacts ||
			d.Location.Entry == nil || *d.Location.Entry != (EntryName{Kind: "blob", ID: "large"}) || d.Location.Environment != environment {
			t.Errorf("%+v", d)
		}
	}
	if encoded := encode(t, diagnostics); strings.Contains(encoded, "aaaa") {
		t.Error("a diagnostic carries a value")
	}
}

func TestFactsReadyFailureWritesNothing(t *testing.T) {
	registry, kinds := fixture(t)
	result := run(registry, kinds)
	refused := errors.New("refused")
	var out bytes.Buffer
	if _, err := result.WriteFacts("db-role", []string{"prod"}, false, &out, func() error { return refused }); !errors.Is(err, refused) || out.Len() > 0 {
		t.Fatalf("%v, %d bytes", err, out.Len())
	}
}
