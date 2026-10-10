package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
)

func bareDigest(data []byte) string { return strings.TrimPrefix(audit.Digest(data), "sha256:") }

func TestEvaluationBindsLoadedBytesWithoutRereading(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"jpack.json", "onboarding.graph.json", "sanctions-screening-0.1.0.pack.json", "vendor-onboarding-0.1.0.pack.json"} {
		files[name] = string(fixtureBytes(t, name))
	}
	loaded := writeProject(t, files)
	document := fixtureDocument(t)
	dir := filepath.Dir(loaded.ConfigPath)
	// Modify files after the project and graph have been decoded. The result
	// must identify the evaluated documents, not the later filesystem state.
	for _, name := range []string{"jpack.json", "onboarding.graph.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(files[name]+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	check := func(id string, data []byte) *evaluation.Failure {
		entry, _ := loaded.Entry(id)
		// Called after the pack read, before evaluation: a second read to compute
		// provenance would incorrectly identify these replacement bytes.
		if err := os.WriteFile(filepath.Join(dir, entry.Path), append(append([]byte{}, data...), '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		return nil
	}
	output, failure := Evaluate(loaded, newEngine(t), document, "onboarding.graph.json", []byte(happyInputs), true, Options{LawCheck: check})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if output.GraphSHA256 != bareDigest([]byte(files["onboarding.graph.json"])) || output.ConfigSHA256 != bareDigest([]byte(files["jpack.json"])) {
		t.Fatalf("result does not bind the loaded graph and configuration: %+v", output)
	}
	for _, node := range output.Nodes {
		entry, _ := loaded.Entry(node.Pack)
		if node.PackSHA256 != bareDigest([]byte(files[entry.Path])) {
			t.Fatalf("node %s bound the wrong pack bytes: %s", node.Node, node.PackSHA256)
		}
	}
	wire := rawJSON(t, output)
	for _, field := range []string{"graphSha256", "configSha256", "packSha256"} {
		if !strings.Contains(wire, `"`+field+`":"`) {
			t.Fatalf("missing wire member %s", field)
		}
	}
}

func TestRepeatedPackNodesBindTheirOwnRead(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"jpack.json", "sanctions-screening-0.1.0.pack.json", "vendor-onboarding-0.1.0.pack.json"} {
		files[name] = string(fixtureBytes(t, name))
	}
	loaded := writeProject(t, files)
	graphBytes := []byte(`{"formatVersion":"1","id":"repeat","version":"1.0.0","nodes":{"a":{"pack":"sanctions-screening"},"b":{"pack":"sanctions-screening"}},"edges":[],"result":"b"}`)
	doc, fail := Load(graphBytes, "repeat.json")
	if fail != nil {
		t.Fatal(fail.Message)
	}
	var readDigests []string
	output, failure := Evaluate(loaded, newEngine(t), doc, "repeat.json", nil, false, Options{LawCheck: func(id string, data []byte) *evaluation.Failure {
		readDigests = append(readDigests, bareDigest(data))
		entry, _ := loaded.Entry(id)
		if err := os.WriteFile(filepath.Join(filepath.Dir(loaded.ConfigPath), entry.Path), append(append([]byte{}, data...), '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		return nil
	}})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if len(output.Nodes) != 2 || len(readDigests) != 2 || readDigests[0] == readDigests[1] {
		t.Fatal("test did not exercise distinct pack reads")
	}
	for i, node := range output.Nodes {
		if node.PackSHA256 != readDigests[i] {
			t.Fatalf("node %s was bound to a different read", node.Node)
		}
	}
	if output.GraphSHA256 != bareDigest(graphBytes) {
		t.Fatal("graph digest mismatch")
	}
}
