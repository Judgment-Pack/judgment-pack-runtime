package result

// CanonicalDispositionShape declares the members Disposition.CanonicalWith
// writes. CanonicalWith builds the disposition from maps so that RFC 8785
// orders it, so the persisted shape has no Go type of its own; this is that
// type. It is never encoded. The audit-formats check (#327) fingerprints it as
// the disposition of every evaluations.jsonl record and holds the bytes
// CanonicalWith actually produces to it: a member the builder writes that is
// not declared here, or one declared here that no exemplar shows, fails the
// check. Members are listed in the canonical (code-point) order.
type CanonicalDispositionShape struct {
	Handoff struct {
		Routes      []HandoffTarget `json:"routes,omitempty"`
		State       string          `json:"state"`
		TriggeredBy []string        `json:"triggeredBy,omitempty"`
	} `json:"handoff"`
	Kind      string         `json:"kind"`
	OutcomeID string         `json:"outcomeId,omitempty"`
	Reasons   []string       `json:"reasons"`
	Value     map[string]any `json:"value,omitempty"`
}
