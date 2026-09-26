package mcp

import (
	"encoding/json"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSnapshotCasesShareMatrixComparisonsAndRejectEmpty(t *testing.T) {
	root := filepath.Dir(projectFixture(t))
	pack, err := os.ReadFile(filepath.Join(root, "packs", "intake-0.1.0.pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	matrix := `{"matrixVersion":"3","cases":[{"id":"hard-fail","facts":` + projectFacts + `,"evidenceAvailability":{"intake-form":"present","sponsor-endorsement":"present"},"expectedDisposition":{"kind":"outcome","outcomeId":"decline-redirect","reasons":[],"handoff":{"state":"none"}}},{"id":"wrong-expectation","facts":` + projectFacts + `,"evidenceAvailability":{"intake-form":"present","sponsor-endorsement":"present"},"expectedDisposition":{"kind":"outcome","outcomeId":"approve-standard","reasons":[],"handoff":{"state":"none"}}}]}`
	configPath := filepath.Join(root, project.DefaultConfigName)
	configBytes, _ := os.ReadFile(configPath)
	var config map[string]any
	json.Unmarshal(configBytes, &config)
	config["packs"].(map[string]any)["intake"].(map[string]any)["matrix"] = "cases.json"
	encoded, _ := json.Marshal(config)
	if err = os.WriteFile(configPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "cases.json"), []byte(matrix), 0600); err != nil {
		t.Fatal(err)
	}
	output := runServer(t, toolCall(t, 1, "experimental_test_packs", map[string]any{"pack_id": "intake"})+toolCall(t, 2, "experimental_test_cases", map[string]any{"pack": string(pack), "matrix": matrix}))
	var reports []map[string]any
	for _, r := range output {
		result := r["result"].(map[string]any)
		if result["isError"] == true {
			t.Fatal(toolText(t, result))
		}
		var report map[string]any
		if err = json.Unmarshal([]byte(toolText(t, result)), &report); err != nil {
			t.Fatal(err)
		}
		reports = append(reports, report)
	}
	first := reports[0]["packs"].([]any)[0].(map[string]any)
	second := reports[1]["packs"].([]any)[0].(map[string]any)
	for _, key := range []string{"rows", "coverage", "summary", "status"} {
		if !reflect.DeepEqual(first[key], second[key]) {
			t.Fatalf("snapshot changed %s", key)
		}
	}
	if second["status"] != "mismatch" {
		t.Fatalf("wrong expectation must mismatch: %v", second["status"])
	}
	// Snapshot input is independent of a project file changing during the request.
	if err = os.WriteFile(filepath.Join(root, "packs", "intake-0.1.0.pack.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range []map[string]any{map[string]any{"pack": string(pack), "matrix": `{"matrixVersion":"3","cases":[]}`}, map[string]any{"pack": string(pack), "matrix": matrix, "pack_id": "intake"}, map[string]any{"Pack": string(pack), "matrix": matrix}, nil} {
		r := runServer(t, toolCall(t, 3, "experimental_test_cases", args))[0]["result"].(map[string]any)
		if r["isError"] != true {
			t.Fatalf("invalid snapshot arguments accepted: %#v", args)
		}
	}
	r := runServer(t, toolCall(t, 4, "experimental_test_cases", map[string]any{"pack": string(pack), "matrix": matrix}))[0]["result"].(map[string]any)
	if r["isError"] == true {
		t.Fatal("snapshot unexpectedly read the changed project file", toolText(t, r))
	}
}
