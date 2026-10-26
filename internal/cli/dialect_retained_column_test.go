package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/dialect"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func retainedEntryDocument(kind, profile string) string {
	conversion := `{"contractVersion":"1","column":"Column","kind":"` + kind + `"}`
	if kind == "boolean" {
		conversion = `{"contractVersion":"1","column":"Column","kind":"boolean","trueValue":"YES","falseValue":"NO"}`
	}
	extra := ""
	if profile == "extended" {
		extra = `,"profile":"extended"`
	}
	return `{"version":3` + extra + `,"case":{"facts":[{"target":"/expense","source":"/retained","conversion":` + conversion + `}],"evidence":[{"requirement":"receipt","source":"/retained","availability":{"contractVersion":"1","column":"Receipt"}},{"requirement":"cost-center","source":"/retained","availability":{"contractVersion":"1","column":"Cost center"}}]}}`
}

func TestRetainedColumnEntryCLIAndMCP(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	pack, err := os.ReadFile("../artifacts/jps/0.2.0-draft/cases/valid/minimal-expense-approval.json")
	if err != nil {
		t.Fatal(err)
	}
	packFile := filepath.Join(t.TempDir(), "pack.json")
	if err = os.WriteFile(packFile, pack, 0600); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"base", "extended"} {
		for _, kind := range []string{"text", "decimal", "number", "boolean", "research-value"} {
			raw := retainedEntryDocument(kind, profile)
			for _, operation := range []string{"check", "validate", "canonicalize", "digest"} {
				t.Run(profile+"/"+kind+"/"+operation, func(t *testing.T) {
					argv := []string{"dialect", operation, "-", "--draft-contract", dialect.RetainedColumnDraftSelector, "--draft-profile", profile, "--format", "json"}
					args := map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": profile, "document": raw}
					if operation == "check" {
						argv = append(argv, "--pack", packFile)
						args["pack"] = string(pack)
					}
					code, out, stderr := runTest(t, argv, raw)
					var cliReport map[string]any
					if code != 0 || stderr != "" || json.Unmarshal([]byte(out), &cliReport) != nil {
						t.Fatal("actual CLI failed", code, out, stderr)
					}
					mcpReport, isError := dialectMCPResult(t, engine, "dialect_"+operation, args)
					delete(cliReport, "command")
					delete(mcpReport, "command")
					if isError || !reflect.DeepEqual(cliReport, mcpReport) {
						t.Fatal("actual CLI/MCP Draft mismatch", cliReport, mcpReport)
					}
					if cliReport["maturity"] != "experimental-draft" || cliReport["specRevision"] != dialect.RetainedColumnDraftRevision || cliReport["fullDocumentConformance"] != false {
						t.Fatal("wrong Draft identity/scope", cliReport)
					}
					if operation == "check" {
						coverage := cliReport["compatibility"].(map[string]any)
						if coverage["status"] != "complete" || coverage["executionChecked"] != false || coverage["valueTypesChecked"] != false || len(coverage["evidence"].([]any)) != 2 {
							t.Fatal("converted targets bypassed real inventory", coverage)
						}
					}
				})
			}
		}
		code, out, stderr := runTest(t, []string{"dialect", "contract", "--draft-contract", dialect.RetainedColumnDraftSelector, "--draft-profile", profile, "--format", "json"}, "")
		var cliReport map[string]any
		if code != 0 || stderr != "" || json.Unmarshal([]byte(out), &cliReport) != nil {
			t.Fatal(code, out, stderr)
		}
		mcpReport, isError := dialectMCPResult(t, engine, "get_dialect_contract", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": profile})
		delete(cliReport, "command")
		delete(mcpReport, "command")
		if isError || !reflect.DeepEqual(cliReport, mcpReport) {
			t.Fatal("contract transport mismatch", cliReport, mcpReport)
		}
	}
}

func TestRetainedColumnEntryCLISelectionBeforeFileRead(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	for _, flags := range [][]string{
		{"--draft-contract", dialect.RetainedColumnDraftSelector},
		{"--draft-profile", "base"}, {"--retained-frame"},
		{"--draft-contract", dialect.RetainedColumnDraftSelector + "foreign", "--draft-profile", "base"},
		{"--draft-contract", dialect.RetainedColumnDraftSelector, "--draft-profile", "future"},
	} {
		argv := append([]string{"dialect", "validate", missing, "--format", "json"}, flags...)
		code, out, stderr := runTest(t, argv, "")
		var r map[string]any
		if code == 0 || code == 4 || stderr != "" || json.Unmarshal([]byte(out), &r) != nil || r["status"] != "refused" || strings.Contains(out, "JPS-DIALECT-READ") {
			t.Fatal("selection did not precede opening nonexistent input", code, out, stderr)
		}
	}
	raw := retainedEntryDocument("text", "base")
	code, out, _ := runTest(t, []string{"dialect", "validate", "-", "--format", "json"}, raw)
	if code == 0 || strings.Contains(out, "conversionContracts") || strings.Contains(out, `"maturity":"experimental-draft"`) {
		t.Fatal("Draft auto-enabled", code, out)
	}
}

func TestRetainedColumnEntryCLIFrameAndByteBoundaries(t *testing.T) {
	for _, profile := range []string{"base", "extended"} {
		raw := retainedEntryDocument("text", profile)
		bound := dialect.RetainedColumnDraftDocumentBytes
		if profile == "extended" {
			bound = dialect.RetainedColumnDraftExtendedDocumentBytes
		}
		argv := []string{"dialect", "validate", "-", "--format", "json", "--draft-contract", dialect.RetainedColumnDraftSelector, "--draft-profile", profile}
		for _, extra := range []int{0, 1} {
			padded := raw + strings.Repeat(" ", bound-len(raw)+extra)
			code, out, stderr := runTest(t, argv, padded)
			if stderr != "" || (code == 0) != (extra == 0) || extra == 1 && !strings.Contains(out, "input-over-bound") {
				t.Fatal("CLI selected byte boundary", profile, extra, code, out, stderr)
			}
		}
		frame := `{"contractVersion":"1","origin":"asserted","values":{"Column":"-0"},"research":null}`
		frameArgs := append(append([]string{}, argv...), "--retained-frame")
		code, out, stderr := runTest(t, frameArgs, frame)
		if code != 0 || stderr != "" || !strings.Contains(out, `"status":"retained-frame-valid"`) {
			t.Fatal("CLI actual frame entry failed", code, out, stderr)
		}
		engine, _ := validation.NewEngine()
		mcpReport, isError := dialectMCPResult(t, engine, "dialect_validate", map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": profile, "retainedFrame": true, "document": frame})
		var cliReport map[string]any
		json.Unmarshal([]byte(out), &cliReport)
		delete(cliReport, "command")
		delete(mcpReport, "command")
		if isError || !reflect.DeepEqual(cliReport, mcpReport) {
			t.Fatal("frame transport mismatch", cliReport, mcpReport)
		}
	}
	// A decoded frame may fit CLI yet exceed MCP's envelope with HTML escaping.
	frame := `{"contractVersion":"1","origin":"generated","values":{},"research":{"recordRecipeRevision":"r","declaration":null,"fields":{"Column":{"status":"supported","valueJson":"` + strings.Repeat("<", 3*1024*1024) + `"}}}}`
	code, out, stderr := runTest(t, []string{"dialect", "validate", "-", "--retained-frame", "--draft-contract", dialect.RetainedColumnDraftSelector, "--draft-profile", "extended", "--format", "json"}, frame)
	if len(frame) >= dialect.RetainedColumnDraftExtendedFrameBytes || code != 0 || stderr != "" || !strings.Contains(out, `"status":"retained-frame-valid"`) {
		t.Fatal("CLI should admit decoded large opaque frame", code, stderr)
	}
}
