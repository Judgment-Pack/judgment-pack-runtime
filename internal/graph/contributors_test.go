package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
)

func TestGraphNodesAndAuditRetainTheirOwnContributors(t *testing.T) {
	pack := strings.Replace(coreGraphPack("0.3.0", graphRoutes, ""), `"value":false`, `"value":true`, 1)
	loaded, doc := coreGraphProject(t, pack, pack)
	writer, dir := coreGraphAudit(t)
	evaluated, failure := Evaluate(loaded, newEngine(t), doc, "graph.json", nil, false, Options{Audit: writer})
	if failure != nil {
		t.Fatal(failure)
	}
	const want = `[{"kind":"rule","id":"one"}]`
	for _, node := range evaluated.Nodes {
		encoded, err := json.Marshal(node.Contributors)
		if err != nil || string(encoded) != want {
			t.Fatalf("node %s contributors = %s (%v), want %s", node.Node, encoded, err, want)
		}
	}
	encoded, err := json.Marshal(evaluated)
	if err != nil {
		t.Fatal(err)
	}
	var headline map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &headline); err != nil {
		t.Fatal(err)
	}
	if _, present := headline["contributors"]; present {
		t.Fatal("aggregate graph invented pack attribution")
	}
	data, err := os.ReadFile(filepath.Join(dir, "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("node and composite records: %d", len(lines))
	}
	for index, line := range lines {
		var record map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if index < 2 && string(record["contributors"]) != want {
			t.Fatalf("node audit lost attribution: %s", line)
		}
		if index == 2 && len(record["contributors"]) != 0 {
			t.Fatal("composite audit invented attribution")
		}
	}
}
