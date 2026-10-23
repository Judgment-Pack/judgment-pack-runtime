package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The expected new bytes are authored by hand. The old historical trail golden
// remains unchanged and continues to test absent-member byte compatibility.
func TestEvaluationRecordRetainsCompleteContributorBytes(t *testing.T) {
	for _, kind := range []string{"rule", "forcing-exception"} {
		t.Run(kind, func(t *testing.T) {
			evaluation := evaluated()
			evaluation.Artifact = nil
			evaluation.Contributors = []result.Contributor{{Kind: kind, ID: "a"}, {Kind: kind, ID: "z"}}
			record, err := EvaluationRecord(evaluation, Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil)
			if err != nil {
				t.Fatal(err)
			}
			record.At, record.Run = "2026-10-23T00:00:00Z", "synthetic"
			record.Tool = Tool{Name: "jpack", Version: "test"}
			record.Pack.Digest = "sha256:" + strings.Repeat("a", 64)
			encoded, err := encodeLines([]Record{record})
			if err != nil {
				t.Fatal(err)
			}
			want := `{"recordVersion":"1","run":"synthetic","at":"2026-10-23T00:00:00Z","kind":"evaluation","surface":"experimental evaluate","tool":{"name":"jpack","version":"test"},"evaluatorSpecVersion":"0.2.0-draft","pack":{"id":"expense-approval","version":"1.2.0","specVersion":"0.2.0-draft","digest":"sha256:` + strings.Repeat("a", 64) + `"},"inputs":{"facts":{},"evidence":null,"evidenceSupplied":false},"contributors":[{"kind":"` + kind + `","id":"a"},{"kind":"` + kind + `","id":"z"}],"disposition":{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"auto-approve","reasons":[]}}` + "\n"
			if string(encoded) != want {
				t.Fatalf("got %s\nwant %s", encoded, want)
			}
		})
	}
}

func TestContributorChangesAreCoveredByAuditChain(t *testing.T) {
	writer, root := writerAt(t, "audit")
	evaluation := evaluated()
	evaluation.Contributors = []result.Contributor{{Kind: "rule", ID: "a"}, {Kind: "rule", ID: "z"}}
	if err := writer.Evaluation(evaluation, Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := writer.Evaluation(evaluation, Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "audit", FileName))
	if err != nil {
		t.Fatal(err)
	}
	if original := verifyBytes(t, data, nil); original.Status != "valid" {
		t.Fatalf("original chain invalid: %+v", original)
	}
	// Change only contributor membership, preserving every other byte and the
	// following record's commitment. A generic reserialization could invalidate
	// the chain for unrelated reasons and would not test this mutation.
	const removed = `,{"kind":"rule","id":"z"}`
	if !strings.Contains(string(data), removed) {
		t.Fatal("fixture has no contributor to remove")
	}
	mutated := strings.Replace(string(data), removed, "", 1)
	verified := verifyBytes(t, []byte(mutated), nil)
	if verified.Status == "valid" {
		t.Fatal("removing a contributor escaped the retained chain commitment")
	}
}
