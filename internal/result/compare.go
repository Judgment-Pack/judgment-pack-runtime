package result

// ComparisonLabel labels every comparison of two pack versions (ADR-0045). A
// difference says the two documents decide an input differently and nothing
// about which of them is right, so the label says so wherever the payload goes.
const ComparisonLabel = "pack comparison: an experimental rehearsal of two pack documents over one set of inputs; a difference says the two versions decide an input differently, not which is right, and nothing here is a decision or an expectation"

// PackComparison is one experimental compare run (ADR-0045): every input
// evaluated under both packs, and the inputs whose results differ.
//
// Rehearsal is always true. The command opens no project, so it appends no
// audit record and consults no reviewed set, and the payload says so in the
// member ADR-0028 gave that statement. Inputs that are the same are counted
// and not listed; every difference is listed, in input order.
type PackComparison struct {
	OutputVersion             string            `json:"outputVersion"`
	Tool                      Tool              `json:"tool"`
	Command                   string            `json:"command"`
	Status                    string            `json:"status"`
	Experimental              bool              `json:"experimental"`
	Rehearsal                 bool              `json:"rehearsal"`
	ConformanceClaimReference string            `json:"conformanceClaimReference"`
	Label                     string            `json:"label"`
	EvaluatorSpecVersion      string            `json:"evaluatorSpecVersion"`
	Old                       ComparedPack      `json:"old"`
	New                       ComparedPack      `json:"new"`
	Inputs                    ComparedInputs    `json:"inputs"`
	Differences               []InputDifference `json:"differences"`
}

// ComparedPack names one of the two documents: the path it was read from, its
// own id and version as an evaluation read them (empty when every evaluation of
// it was refused before its identity could be read), and the digest of its
// exact bytes.
type ComparedPack struct {
	Path        string `json:"path"`
	PackID      string `json:"packId,omitempty"`
	PackVersion string `json:"packVersion,omitempty"`
	Digest      string `json:"digest"`
}

// ComparedInputs says what the inputs were and how they compared: kind is
// "matrix" (expectations unread) or "candidates" (a packs suggest document).
type ComparedInputs struct {
	Kind      string `json:"kind"`
	Count     int    `json:"count"`
	Same      int    `json:"same"`
	Different int    `json:"different"`
}

// InputDifference is one input the two packs decide differently. Changed names
// what differs, in a fixed order: kind, outcomeId, reasons, handoff, value,
// handoffTarget, or refusal. The names describe the difference; what decides
// that there is one is the §8.3 canonical bytes, the handoff target, and the
// refusal.
type InputDifference struct {
	ID      string       `json:"id"`
	Changed []string     `json:"changed"`
	Old     ComparedSide `json:"old"`
	New     ComparedSide `json:"new"`
}

// ComparedSide is one pack's result for one input: a disposition, with the
// handoff target when one was requested, or the §8.4 refusal.
type ComparedSide struct {
	Disposition     *Disposition     `json:"disposition,omitempty"`
	HandoffTarget   *HandoffTarget   `json:"handoffTarget,omitempty"`
	EvaluationError *ComparedRefusal `json:"evaluationError,omitempty"`
}

// ComparedRefusal is a refused evaluation's class, phase and this runtime's
// code. Two refusals are the same when all three are.
type ComparedRefusal struct {
	Class string `json:"class,omitempty"`
	Phase string `json:"phase,omitempty"`
	Code  string `json:"code"`
}
