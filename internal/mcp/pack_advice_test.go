package mcp

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
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

// The MCP tool reads a Core 0.5.0 pack as the CLI does: its evaluation form,
// with the report naming the pack's own selector, and the same refusal with
// Core's first diagnostic for a pack evaluation refuses.
func TestPackAdviceMCPCore050EvaluationForm(t *testing.T) {
	dir := "../packadvice/testdata/core050/"
	read := func(name string) string {
		b, err := os.ReadFile(dir + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	p050, p030, domain := read("pack-0.5.0.json"), read("pack-0.3.0.json"), read("domain.json")
	call := func(pack string) map[string]any {
		r := runServer(t, toolCall(t, 1, packAdviceTool, map[string]any{"pack": pack, "domain": domain}))[0]["result"].(map[string]any)
		if r["isError"] != false {
			t.Fatal(r)
		}
		return r["structuredContent"].(map[string]any)
	}
	got, twin := call(p050), call(p030)
	if got["targetSpecVersion"] != "0.5.0" || twin["targetSpecVersion"] != "0.3.0" || got["packDigest"] == twin["packDigest"] {
		t.Fatal(got["targetSpecVersion"], twin["targetSpecVersion"], got["packDigest"])
	}
	for _, member := range []string{"targetSpecVersion", "packDigest"} {
		delete(got, member)
		delete(twin, member)
	}
	if !reflect.DeepEqual(got, twin) {
		t.Fatal("the 0.5.0 report is not its evaluation form's:", got, twin)
	}
	refused := call(strings.Replace(p050, `"valueSet": "regions"`, `"valueSet": "missing"`, 1))
	if refused["code"] != "pack-unsupported" || refused["status"] != "unavailable" || !strings.Contains(refused["message"].(string), "first diagnostic JPS-SEMANTIC-UNRESOLVED-VALUE-SET") || refused["findings"] != nil {
		t.Fatal(refused)
	}
}
