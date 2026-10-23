package mcp

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/packadvice"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func TestPackAdviceMCPParityAndStrictArguments(t *testing.T) {
	pack, err := os.ReadFile("../validation/testdata/rules-overlap-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	domain := `{"kind":"finite-cases","inputs":[{"facts":{"score":"80"},"evidence":{}}]}`
	v, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	want, f := packadvice.Analyze(evaluation.NewEngine(v), pack, []byte(domain), packadvice.DefaultOptions())
	if f != nil {
		t.Fatal(f)
	}
	b, _ := json.Marshal(want)
	var plain map[string]any
	_ = json.Unmarshal(b, &plain)
	result := runServer(t, toolCall(t, 1, packAdviceTool, map[string]any{"pack": string(pack), "domain": domain}))[0]["result"].(map[string]any)
	if result["isError"] != false || !reflect.DeepEqual(result["structuredContent"], plain) {
		t.Fatal(result)
	}
	for _, args := range []map[string]any{{"pack": string(pack), "domain": "{}"}, {"pack": "{}", "domain": domain}, {"pack": string(pack), "domain": domain, "max_work": 0}} {
		r := runServer(t, toolCall(t, 1, packAdviceTool, args))[0]["result"].(map[string]any)
		var refusal packadvice.Refusal
		decodeStructured(t, r, &refusal)
		if refusal.Status != "unavailable" || refusal.Code == "" || r["isError"] != false {
			t.Fatal(r)
		}
	}
	for _, args := range []string{`null`, `[]`, `{}`, `{"pack":"{}","domain":null}`, `{"pack":"{}","domain":"{}","max_work":null}`, `{"pack":"{}","domain":"{}","max_findings":1.5}`, `{"pack":"{}","domain":"{}","intentional_fallback":null}`, `{"pack":"{}","domain":"{}","expectedDisposition":{}}`, `{"pack":"{}","pack":"{}","domain":"{}"}`} {
		r := runServer(t, rawToolCall(t, 1, packAdviceTool, args))[0]
		if tool, ok := r["result"].(map[string]any); ok && tool["isError"] != true {
			t.Fatal(args, r)
		}
		if r["result"] == nil && r["error"] == nil {
			t.Fatal(args, r)
		}
	}
}
func TestPackAdviceMCPDoesNotWriteAudit(t *testing.T) {
	pack, trail := comparableServerProject(t, `"configVersion":"5","requireComparableFacts":true`)
	// This existing project fixture selects the older contract, which the analyzer
	// refuses without consulting project policy or writing an audit record.
	r := runServer(t, toolCall(t, 1, packAdviceTool, map[string]any{"pack": pack, "domain": `{"kind":"finite-cases","inputs":[{"facts":{},"evidence":{}}]}`}))[0]["result"].(map[string]any)
	var f packadvice.Refusal
	decodeStructured(t, r, &f)
	if f.Code != "pack-unsupported" {
		t.Fatal(r)
	}
	core, err := os.ReadFile("../validation/testdata/rules-overlap-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	success := runServer(t, toolCall(t, 2, packAdviceTool, map[string]any{"pack": string(core), "domain": `{"kind":"finite-cases","inputs":[{"facts":{"score":80},"evidence":{}}]}`}))[0]["result"].(map[string]any)
	var report map[string]any
	decodeStructured(t, success, &report)
	if report["formatVersion"] != float64(1) || success["isError"] != false {
		t.Fatal(success)
	}
	if _, err := os.Stat(trail); !os.IsNotExist(err) {
		t.Fatal("analysis wrote a trail", err)
	}
}
