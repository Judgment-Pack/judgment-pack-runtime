package audit

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Hand-authored canonical bytes from Core 0.3 §§8.1/8.3, not a recording of
// evaluation or audit output. Signed audit storage preserves them verbatim.
const routedAuditCanonical = `{"handoff":{"routes":[{"kind":"queue","name":"Z\u001b[31m"},{"kind":"system","name":"A"},{"kind":"queue","name":"Z\u001b[31m"}],"state":"requested","triggeredBy":["no-match"]},"kind":"unresolved","reasons":["no-match"]}`

func TestCore030RoutesSurviveSignedEvaluationAndCompositeRecords(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	writer, dir := writerAt(t, "audit")
	writer.SignWith(signer)
	got := evaluated()
	got.SpecVersion, got.EvaluatorSpecVersion = "0.3.0", "0.3.0"
	got.Artifact.SpecVersion = "0.3.0"
	got.Disposition = result.Disposition{
		Contract: result.Core030EvaluatorContract(), Kind: "unresolved", Reasons: []string{"no-match"},
		Handoff: result.Handoff{State: "requested", TriggeredBy: []string{"no-match"}, Routes: []result.HandoffTarget{{Kind: "queue", Name: "Z\x1b[31m"}, {Kind: "system", Name: "A"}, {Kind: "queue", Name: "Z\x1b[31m"}}},
	}
	node, err := EvaluationRecord(got, Inputs{Facts: []byte(`{}`)}, nil, []byte(`{"specVersion":"0.3.0"}`), &Graph{ID: "routes", Node: "a"})
	if err != nil {
		t.Fatal(err)
	}
	composite, err := CompositeRecord(result.GraphEvaluation{Command: "graph evaluate", EvaluatorSpecVersion: "0.3.0", GraphID: "routes", ResultNode: "a", Disposition: got.Disposition, Artifact: got.Artifact}, Digest([]byte(`{}`)), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []Record{node, composite} {
		if string(record.Disposition) != routedAuditCanonical || record.EvaluatorSpecVersion != "0.3.0" {
			t.Fatalf("record: %+v", record)
		}
	}
	if err := writer.AppendAll([]Record{node, composite}); err != nil {
		t.Fatal(err)
	}
	trail, lines := trailLines(t, dir, "audit")
	if len(lines) != 2 {
		t.Fatalf("records=%d", len(lines))
	}
	for _, line := range lines {
		var record Record
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		if string(record.Disposition) != routedAuditCanonical || record.EvaluatorSpecVersion != "0.3.0" || record.Artifact.SpecVersion != "0.3.0" {
			t.Fatalf("stored canonical/provenance: %s", line)
		}
	}
	chain := verifySigned(t, trail, readSidecar(t, dir), SignatureOptions{Keys: keys(signer)})
	if chain.Status != "valid" || chain.FindingsTotal != 0 {
		t.Fatalf("signed verification: %+v", chain)
	}
	// Verification must not reinterpret a legacy record or rewrite its bytes.
	legacy := evaluated()
	record, err := EvaluationRecord(legacy, Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(record.Disposition) != `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"auto-approve","reasons":[]}` || record.EvaluatorSpecVersion != result.EvaluatorSpecVersion {
		t.Fatalf("legacy canonical changed: %+v", record)
	}
	if err := writer.AppendAll([]Record{record}); err != nil {
		t.Fatal(err)
	}
	extended, _ := trailLines(t, dir, "audit")
	if !bytes.HasPrefix(extended, trail) {
		t.Fatal("appending legacy record changed existing routed records")
	}
	chain = verifySigned(t, extended, readSidecar(t, dir), SignatureOptions{Keys: keys(signer)})
	if chain.Status != "valid" {
		t.Fatalf("mixed-era signed verification: %+v", chain)
	}
}
