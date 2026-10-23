package graph

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func assertUnsupportedGraphProfile(t *testing.T, failure *evaluation.Failure) {
	t.Helper()
	if failure == nil || failure.Code != "profile-unsupported" || failure.Profile != "extended" || failure.Message != "" || failure.Class != "" || failure.SpecVersion != "" || failure.ExitCode != result.ExitUnsupported {
		t.Fatalf("expected sole unsupported-profile refusal, got %+v", failure)
	}
}

func TestCore030GraphProfileNegotiationPrecedesInputAdmission(t *testing.T) {
	// The refused pack intentionally lacks schema-required members. Negotiation
	// needs its complete root declaration, never its schema or graph inputs.
	base := coreGraphPack("0.3.0", graphRoutes, "")
	loaded, doc := coreGraphProject(t, base, `{"specVersion":"0.3.0","profile":"extended"}`)
	doc.Edges = []Edge{{From: "a", To: "b", Fact: "/owned"}}
	tests := []struct{ name, inputs string }{
		{"empty", ""},
		{"malformed JSON", `{"b":`},
		{"non-object", `[]`},
		{"unknown node", `{"undeclared":{}}`},
		{"fact collision", `{"b":{"facts":{"owned":"caller"}}}`},
		{"non-object facts", `{"b":{"facts":[]}}`},
		{"malformed evidence shape", `{"b":{"evidence":[]}}`},
		{"oversized", strings.Repeat(" ", int(MaxInputsBytes)+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer, dir := coreGraphAudit(t)
			calls := 0
			got, failure := Evaluate(loaded, nil, doc, "graph.json", []byte(test.inputs), true, Options{Audit: writer, LawCheck: func(string, []byte) *evaluation.Failure { calls++; return nil }})
			assertUnsupportedGraphProfile(t, failure)
			if !reflect.DeepEqual(got, result.GraphEvaluation{}) || calls != 0 {
				t.Fatalf("refused profile produced work: %+v checks=%d", got, calls)
			}
			if _, err := os.Stat(filepath.Join(dir, "audit", audit.FileName)); !os.IsNotExist(err) {
				t.Fatalf("profile refusal wrote audit: %v", err)
			}
		})
	}
}

func TestCore030GraphProfileNegotiationPrecedesEmptyRows(t *testing.T) {
	base := coreGraphPack("0.3.0", graphRoutes, "")
	loaded, doc := coreGraphProject(t, base, `{"specVersion":"0.3.0","profile":"extended"}`)
	got, failure := Test(loaded, nil, doc, "graph.json", "rows.json", Rows{}, Options{})
	assertUnsupportedGraphProfile(t, failure)
	if !reflect.DeepEqual(got, result.GraphTest{}) {
		t.Fatalf("profile refusal returned a matrix result: %+v", got)
	}
}

func TestCore030GraphSuiteNegotiatesProfileBeforeReadingRows(t *testing.T) {
	loaded := writeProject(t, map[string]string{
		"jpack.json": `{"configVersion":"2","packs":{"p":{"path":"p.json"}},"graphs":{"routes":{"path":"graph.json","rows":"missing.rows.json"}}}`,
		"p.json":     `{"specVersion":"0.3.0","profile":"extended"}`,
		"graph.json": `{"formatVersion":"1","id":"routes","version":"1.0.0","nodes":{"a":{"pack":"p"}},"edges":[],"result":"a"}`,
	})
	got, failure := TestProject(loaded, nil, "", Options{})
	assertUnsupportedGraphProfile(t, failure)
	if !reflect.DeepEqual(got, result.GraphSuite{}) {
		t.Fatalf("profile refusal returned a suite: %+v", got)
	}
}

func TestGraphProfilePreflightRequiresCompletePackJSON(t *testing.T) {
	base := coreGraphPack("0.3.0", graphRoutes, "")
	loaded, doc := coreGraphProject(t, base, `{"specVersion":"0.3.0","profile":"extended",`)
	_, failure := Evaluate(loaded, nil, doc, "graph.json", nil, false, Options{})
	if failure == nil || failure.Code == "profile-unsupported" || failure.Class != result.ClassPackNotConformant || failure.Phase != result.PhasePreflight || !strings.Contains(failure.Message, `Node "b"`) {
		t.Fatalf("incomplete pack prefix was negotiated: %+v", failure)
	}
}

func TestLegacyGraphInputRefusalsKeepPrecedence(t *testing.T) {
	legacy := coreGraphPack(result.EvaluatorSpecVersion, `{"triggers":["no-match"],"target":{"kind":"queue","name":"Review"}}`, "")
	loaded, doc := coreGraphProject(t, legacy, `{`)
	_, failure := Evaluate(loaded, nil, doc, "graph.json", []byte(`{"a":`), true, Options{})
	if failure == nil || failure.Code != "JPS-GRAPH-INPUTS-JSON" || failure.Class != "" {
		t.Fatalf("legacy input precedence changed: %+v", failure)
	}
	_, failure = Test(loaded, nil, doc, "graph.json", "rows.json", Rows{}, Options{})
	if failure == nil || failure.Code != "JPS-GRAPH-ROWS-EMPTY" {
		t.Fatalf("legacy empty matrix precedence changed: %+v", failure)
	}
	// Even when a graph cannot safely identify packs, an empty legacy matrix
	// keeps the original direct-call precondition rather than attempting reads.
	doc.Nodes["b"] = Node{Pack: "undeclared"}
	_, failure = Test(loaded, nil, doc, "graph.json", "rows.json", Rows{}, Options{})
	if failure == nil || failure.Code != "JPS-GRAPH-ROWS-EMPTY" {
		t.Fatalf("legacy empty/invalid graph precedence changed: %+v", failure)
	}
}

func TestCore030GraphInputRefusalsCarryNegotiatedVersion(t *testing.T) {
	base := coreGraphPack("0.3.0", graphRoutes, "")
	loaded, doc := coreGraphProject(t, base, base)
	_, failure := Evaluate(loaded, nil, doc, "graph.json", []byte(`{"a":`), true, Options{})
	if failure == nil || failure.Code != "JPS-GRAPH-INPUTS-JSON" || failure.SpecVersion != "0.3.0" {
		t.Fatalf("graph input refusal lost selected version: %+v", failure)
	}
	_, failure = Test(loaded, nil, doc, "graph.json", "rows.json", Rows{}, Options{})
	if failure == nil || failure.Code != "JPS-GRAPH-ROWS-EMPTY" || failure.SpecVersion != "0.3.0" {
		t.Fatalf("empty matrix refusal lost selected version: %+v", failure)
	}
}
