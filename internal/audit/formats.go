package audit

import (
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// LineFormat is one persisted line type of a project's audit trail: the file
// it is written to, its name, the format version the writer stamps on that
// file's lines, and the Go type it is encoded from. The catalog in
// internal/auditformat fingerprints each one (#327), so a change to the shape
// of a persisted line cannot pass without a committed format decision.
//
// Embedded names a json.RawMessage member the runtime builds itself, by its
// JSON name, with the type that declares its shape; the fingerprint uses that
// type in place of an opaque JSON value. Exemplars are lines produced by the
// writer's own encoder, which the check holds to the declared shape and to
// any `format` grammar a string member declares. The fingerprint covers shape
// and those declared grammars, not every value spelling (a timestamp's, for
// one).
type LineFormat struct {
	File      string
	Line      string
	Version   string
	Type      reflect.Type
	Embedded  map[string]reflect.Type
	Exemplars func() ([][]byte, error)
}

// LineFormats lists every line type this runtime persists, in file order. A
// new persisted line type is added here, or the catalog cannot fingerprint it.
func LineFormats() []LineFormat {
	return []LineFormat{
		// One shape carries both decision kinds, evaluation and graph-composite.
		{FileName, "record", RecordVersion, reflect.TypeOf(Record{}),
			map[string]reflect.Type{"disposition": reflect.TypeOf(result.CanonicalDispositionShape{})}, recordExemplars},
		{FileName, KindDiscontinuity, RecordVersion, reflect.TypeOf(discontinuityRecord{}), nil, nil},
		{SidecarName, KindRecordSignature, SidecarVersion, reflect.TypeOf(recordSignatureLine{}), nil, nil},
		{SidecarName, KindKeyRotation, SidecarVersion, reflect.TypeOf(keyRotationLine{}), nil, nil},
		{StampsName, "stamp", StampVersion, reflect.TypeOf(stampLine{}), nil, stampExemplars},
	}
}

// recordExemplars are records whose dispositions, built by the canonical
// encoder, together carry every disposition member: an outcome with a value
// under the legacy contract, and a routed requested handoff under Core 0.3.
func recordExemplars() ([][]byte, error) {
	dispositions := []result.Disposition{
		{Contract: result.LegacyEvaluatorContract(), Kind: "outcome", OutcomeID: "approve",
			Handoff: result.Handoff{State: "none"}, Value: map[string]any{"limit": "10", "final": true}},
		{Contract: result.LegacyEvaluatorContract(), Kind: "unresolved", Reasons: []string{"conflict"},
			Handoff: result.Handoff{State: "requested", TriggeredBy: []string{"conflict"}}},
		{Contract: result.Core030EvaluatorContract(), Kind: "unresolved", Reasons: []string{"conflict"},
			Handoff: result.Handoff{State: "requested", TriggeredBy: []string{"conflict"},
				Routes: []result.HandoffTarget{{Kind: "queue", Name: "review"}}}},
	}
	var lines [][]byte
	for _, disposition := range dispositions {
		canonical, err := disposition.Canonical()
		if err != nil {
			return nil, err
		}
		line, err := encodeLines([]Record{{RecordVersion: RecordVersion, Run: "exemplar", At: "exemplar",
			Kind: KindEvaluation, Surface: "exemplar", Tool: Tool{Name: "exemplar", Version: "exemplar"},
			EvaluatorSpecVersion: disposition.Contract.Version(), Disposition: canonical}})
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// stampExemplars is one stamp line from the stamp encoder, with token bytes
// whose standard base64 spelling uses both "+" and "/" and padding.
func stampExemplars() ([][]byte, error) {
	checkpoint := result.AuditCheckpoint{CheckpointVersion: "1", RecordDigest: Digest(nil), Sequence: 1, Trail: "exemplar"}
	return [][]byte{encodeStampLine(checkpoint, []byte{0xfb, 0xff})}, nil
}
