package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/conformance"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/dialect"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestRetainedColumnEntryMCPSchemaSelectionAndLimits(t *testing.T) {
	for _, operation := range []string{"contract", "canonicalize", "digest", "validate", "check"} {
		definition := dialectDefinition(operation)
		raw, err := json.Marshal(definition["inputSchema"])
		if err != nil {
			t.Fatal(err)
		}
		schema, err := validation.CompileSchema(raw, "urn:judgmentpack:test:draft-"+operation)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name    string
			options map[string]any
			size    int
			valid   bool
		}{
			{"legacy", nil, dialect.MaxDocumentBytes, true},
			{"legacy bound", nil, dialect.MaxDocumentBytes + 1, false},
			{"selector only", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector}, 0, false},
			{"profile only", map[string]any{"draftProfile": "base"}, 0, false},
			{"foreign revision", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector + "x", "draftProfile": "base"}, 0, false},
			{"unknown profile", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": "unknown"}, 0, false},
			{"base exact", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": "base"}, dialect.RetainedColumnDraftDocumentBytes, true},
			{"base one over", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": "base"}, dialect.RetainedColumnDraftDocumentBytes + 1, false},
			{"extended exact", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": "extended"}, dialect.RetainedColumnDraftExtendedDocumentBytes, true},
			{"extended one over", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": "extended"}, dialect.RetainedColumnDraftExtendedDocumentBytes + 1, false},
		}
		if operation == "validate" {
			cases = append(cases,
				struct {
					name    string
					options map[string]any
					size    int
					valid   bool
				}{"frame without selector", map[string]any{"retainedFrame": true}, 0, false},
				struct {
					name    string
					options map[string]any
					size    int
					valid   bool
				}{"base frame exact", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": "base", "retainedFrame": true}, dialect.RetainedColumnDraftFrameBytes, true},
				struct {
					name    string
					options map[string]any
					size    int
					valid   bool
				}{"base frame over", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": "base", "retainedFrame": true}, dialect.RetainedColumnDraftFrameBytes + 1, false},
				struct {
					name    string
					options map[string]any
					size    int
					valid   bool
				}{"extended frame exact", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": "extended", "retainedFrame": true}, dialect.RetainedColumnDraftExtendedFrameBytes, true},
				struct {
					name    string
					options map[string]any
					size    int
					valid   bool
				}{"extended frame over", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": "extended", "retainedFrame": true}, dialect.RetainedColumnDraftExtendedFrameBytes + 1, false},
			)
		}
		for _, tc := range cases {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				if operation == "contract" && tc.size > 0 && !strings.Contains(tc.name, "exact") && tc.name != "legacy" {
					return
				}
				args := map[string]any{}
				for k, v := range tc.options {
					args[k] = v
				}
				if operation != "contract" {
					args["document"] = strings.Repeat(" ", tc.size)
				}
				if operation == "check" {
					args["pack"] = "{}"
				}
				encoded, _ := json.Marshal(args)
				instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
				if err != nil {
					t.Fatal(err)
				}
				admitted := schema.Validate(instance) == nil
				if admitted != tc.valid {
					t.Fatalf("conditional schema admitted=%v, want=%v: %s", admitted, tc.valid, tc.name)
				}
			})
		}
	}
}

func TestRetainedColumnEntryMCPRefusesMalformedAndUnsupportedSelection(t *testing.T) {
	selector := dialect.RetainedColumnDraftSelector
	for _, args := range []string{
		`{"draftContract":null,"draftProfile":"base","document":"{"}`,
		`{"draftContract":"` + selector + `","draftProfile":null,"document":"{"}`,
		`{"draftContract":"` + selector + `","draftProfile":"base","retainedFrame":null,"document":"{"}`,
		`{"draftContract":"` + selector + `","draftProfile":"base","draftProfile":"extended","document":"{"}`,
		`{"draftContract":"` + selector + `","draftProfile":"base","spec":{"revision":"caller-proof"},"document":"{"}`,
	} {
		r := runServer(t, rawToolCall(t, 1, "dialect_validate", args))[0]["result"].(map[string]any)
		if r["isError"] != true {
			t.Fatal("malformed selection admitted", r)
		}
	}
	for _, tc := range []struct{ args, want string }{
		{`{"draftContract":"foreign","draftProfile":"base","document":"{"}`, "capability-unsupported"},
		{`{"draftProfile":"base","document":"{"}`, "capability-unsupported"},
		{`{"draftContract":"` + selector + `","document":"{"}`, "input-malformed"},
		{`{"draftContract":"` + selector + `","draftProfile":"future","document":"{"}`, "input-malformed"},
	} {
		r := runServer(t, rawToolCall(t, 1, "dialect_validate", tc.args))[0]["result"].(map[string]any)
		report := r["structuredContent"].(map[string]any)
		diagnostics := report["diagnostics"].([]any)
		if r["isError"] != true || report["status"] != "refused" || diagnostics[0].(map[string]any)["code"] != tc.want || report["document"] != nil {
			t.Fatal("selection did not precede malformed artifact", r)
		}
	}
	// Frame mode is confined to selected validate, never silently accepted elsewhere.
	for _, name := range []string{"dialect_check", "dialect_digest", "get_dialect_contract"} {
		r := runServer(t, toolCall(t, 1, name, map[string]any{"draftContract": selector, "draftProfile": "base", "retainedFrame": true, "document": "{}", "pack": "{}"}))[0]["result"].(map[string]any)
		if r["isError"] != true {
			t.Fatal("extra selection admitted", name, r)
		}
	}
}

func TestRetainedColumnEntryMCPTransportEnvelope(t *testing.T) {
	// This valid decoded extended frame uses 3MiB of opaque retained text. The
	// actual JSON-RPC encoder's HTML escaping expands it beyond stdio's 16MiB line.
	frame := `{"contractVersion":"1","origin":"generated","values":{},"research":{"recordRecipeRevision":"r","declaration":null,"fields":{"Column":{"status":"supported","valueJson":"` + strings.Repeat("<", 3*1024*1024) + `"}}}}`
	selection := dialect.RetainedColumnDraftBase()
	selection.Profile = "extended"
	if r, code := dialect.ValidateRetainedColumnDraftFrame("test", []byte(frame), selection); code != 0 {
		t.Fatal("frame witness is not valid", r.Diagnostics)
	}
	request := toolCall(t, 1, "dialect_validate", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": "extended", "retainedFrame": true, "document": frame})
	if len(frame) >= dialect.RetainedColumnDraftExtendedFrameBytes || len(request) <= maxMessageBytes {
		t.Fatal("witness did not cross transport envelope", len(frame), len(request))
	}
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(engine, conformance.NewRunner(engine))
	var out, logs bytes.Buffer
	err = server.Serve(strings.NewReader(request), &out, &logs)
	if !errors.Is(err, bufio.ErrTooLong) || out.Len() != 0 {
		t.Fatal("MCP envelope was bypassed", err, out.Len())
	}
}
