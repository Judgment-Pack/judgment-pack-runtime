package diagram

import (
	"encoding/json"
	"os"
	"testing"
)

func TestZTmpReviewRender(t *testing.T) {
	for _, p := range []string{
		"../artifacts/jps/0.2.0-draft/evaluation/packs/partial-trigger-conflict.json",
	} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		t.Logf("\n==== %s ====\n%s", p, Mermaid(doc))
	}
}
