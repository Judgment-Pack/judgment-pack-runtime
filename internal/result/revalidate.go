package result

// RevalidationLabel labels the reports of experimental revalidate (ADR-0091).
// What the command compares is what two runtimes gave for one project's own
// rows; it is not the specification's corpus, and the evaluator's
// conformance claim is stated, in full and only, in CONFORMANCE.md.
const RevalidationLabel = "runtime revalidation: a project's own matrix rows as one runtime gave them, compared with what this runtime gives; the evaluator's conformance claim is stated, in full and only, in CONFORMANCE.md"

// BaselineTaken is the report about one experimental revalidate --write run.
// The baseline document itself is written to the destination; this says what
// went into it.
type BaselineTaken struct {
	OutputVersion             string              `json:"outputVersion"`
	Tool                      Tool                `json:"tool"`
	Command                   string              `json:"command"`
	Status                    string              `json:"status"`
	Experimental              bool                `json:"experimental"`
	ConformanceClaimReference string              `json:"conformanceClaimReference"`
	Label                     string              `json:"label"`
	Kind                      string              `json:"kind"`
	ConfigPath                string              `json:"configPath"`
	BaselineVersion           string              `json:"baselineVersion"`
	WrittenTo                 string              `json:"writtenTo,omitempty"`
	Packs                     []BaselineTakenPack `json:"packs"`
	// NoMatrix lists the declared packs that have no matrix and so are not in
	// the baseline.
	NoMatrix []string `json:"noMatrix"`
}

// BaselineTakenPack is one pack the baseline holds.
type BaselineTakenPack struct {
	ID          string `json:"id"`
	PackID      string `json:"packId"`
	PackVersion string `json:"packVersion"`
	SpecVersion string `json:"specVersion"`
	Rows        int    `json:"rows"`
	// Unexpected counts the rows whose result did not match their own
	// expectation. The baseline records what this runtime gave, not what the
	// matrix expects, so they are in it as given.
	Unexpected int `json:"unexpected"`
}

// Revalidation is the report of one experimental revalidate --baseline run:
// what this runtime gives for the project's matrices, compared with one or
// more baselines.
type Revalidation struct {
	OutputVersion             string                `json:"outputVersion"`
	Tool                      Tool                  `json:"tool"`
	Command                   string                `json:"command"`
	Status                    string                `json:"status"`
	Experimental              bool                  `json:"experimental"`
	ConformanceClaimReference string                `json:"conformanceClaimReference"`
	Label                     string                `json:"label"`
	Kind                      string                `json:"kind"`
	ConfigPath                string                `json:"configPath"`
	Baselines                 []RevalidationSource  `json:"baselines"`
	Summary                   RevalidationSummary   `json:"summary"`
	Packs                     []RevalidatedPack     `json:"packs"`
	Findings                  []RevalidationFinding `json:"findings"`
}

// RevalidationSource is one baseline file as it was read.
type RevalidationSource struct {
	// Form is "baseline" for a document experimental revalidate --write
	// wrote, and "packs-test-report" for a packs test --format json report.
	Form string `json:"form"`
	// Version is the document's baselineVersion, or the report's
	// outputVersion.
	Version string `json:"version"`
	// Tool is the runtime that wrote it, as the document says.
	Tool  Tool     `json:"tool"`
	Packs []string `json:"packs"`
}

// RevalidationSummary counts one run.
type RevalidationSummary struct {
	Packs         int `json:"packs"`
	PacksSame     int `json:"packsSame"`
	RowsCompared  int `json:"rowsCompared"`
	RowsSame      int `json:"rowsSame"`
	Findings      int `json:"findings"`
	BytesCompared int `json:"bytesCompared"`
	// BytesNotRecorded counts the packs whose baseline records no digest, so
	// their pack and matrix bytes were not compared.
	BytesNotRecorded int `json:"bytesNotRecorded"`
}

// RevalidatedPack is one pack's comparison.
type RevalidatedPack struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Bytes is "same", "different", "not-recorded" when the baseline holds
	// no digest, or "not-compared" when one side has no run of the pack.
	Bytes     string               `json:"bytes"`
	Baseline  *RevalidatedPackSide `json:"baseline"`
	Candidate *RevalidatedPackSide `json:"candidate"`
	Rows      RevalidatedRowCounts `json:"rows"`
}

// RevalidatedPackSide is one side of a pack's comparison.
type RevalidatedPackSide struct {
	PackID       string `json:"packId"`
	PackVersion  string `json:"packVersion"`
	SpecVersion  string `json:"specVersion"`
	PackDigest   string `json:"packDigest,omitempty"`
	MatrixDigest string `json:"matrixDigest,omitempty"`
	Rows         int    `json:"rows"`
}

// RevalidatedRowCounts counts one pack's rows.
type RevalidatedRowCounts struct {
	Compared int `json:"compared"`
	Same     int `json:"same"`
}

// RevalidationFinding is one difference. Baseline and Candidate carry the
// compared canonical text of each side, where the kind has one.
type RevalidationFinding struct {
	Kind      string   `json:"kind"`
	Pack      string   `json:"pack"`
	Row       string   `json:"row,omitempty"`
	Members   []string `json:"members,omitempty"`
	Baseline  string   `json:"baseline,omitempty"`
	Candidate string   `json:"candidate,omitempty"`
	Detail    string   `json:"detail,omitempty"`
}
