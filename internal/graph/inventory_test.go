package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
)

// The inventory budget refuses amplification instead of performing it: a
// configuration is free to alias one document under many ids, and the builder
// charges each unique read once and each row's echo every time, so the
// gigabytes a hostile configuration could otherwise ask for are a refusal
// carrying the budget. The budget is shrunk here rather than the fixture
// grown to it.
func TestInventoryBudgetRefusesAmplification(t *testing.T) {
	root := t.TempDir()
	document := `{"formatVersion":"1","id":"` + strings.Repeat("a", 512) + `","version":"0.1.0","nodes":{},"edges":[],"result":"none"}`
	if err := os.WriteFile(filepath.Join(root, "one.graph.json"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	pack := `{"path":"one.graph.json"}`
	entries := []string{}
	for _, id := range []string{"alias-a", "alias-b", "alias-c", "alias-d"} {
		entries = append(entries, `"`+id+`":`+pack)
	}
	config := `{"configVersion":"2","packs":{},"graphs":{` + strings.Join(entries, ",") + `}}`
	// The packs member may not be empty under the schema; borrow the graph
	// document as an unread pack path — listing packs is not this test's job.
	config = strings.Replace(config, `"packs":{}`, `"packs":{"p":{"path":"one.graph.json"}}`, 1)
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, failure := project.Load(configPath)
	if failure != nil {
		t.Fatalf("loading the project: %+v", failure)
	}
	defer loaded.Close()

	// Within budget: one cached read, four echoing rows.
	if inventory, budgetFailure := ProjectInventory(loaded, "test"); budgetFailure != nil || len(inventory.Graphs) != 4 {
		t.Fatalf("four aliases of one small document fit the real budget: %+v %+v", inventory, budgetFailure)
	}

	shrunk := int64(len(document) + 600)
	original := inventoryBudget
	inventoryBudget = shrunk
	defer func() { inventoryBudget = original }()
	inventory, budgetFailure := ProjectInventory(loaded, "test")
	if budgetFailure == nil {
		t.Fatalf("the echoes alone must exhaust a shrunk budget: %+v", inventory)
	}
	if budgetFailure.Code != CodeInventoryBudget || !strings.Contains(budgetFailure.Message, "budget") {
		t.Fatalf("the refusal names the budget: %+v", budgetFailure)
	}
}
