package result

// --- the experimental graph surface (ADR-0015; spec RFC 0002, Draft) ---
//
// A graph composes packs this runtime's project convention already names; the
// Judgment Pack Specification defines none of it. JPS Core 0.2.0-draft has no
// graph, no composition, and no composite result — composition is the
// specification's RFC 0002, a draft proposal — so every payload here is labeled
// a non-normative runtime convention exactly as the jpack.json payloads are,
// and the composite payload additionally carries GraphCompositeLabel so no
// consumer reads it as a specification artifact. The §8.3 dispositions inside it are per-node
// facts produced by the experimental evaluator, unchanged; the envelope around
// them is this runtime's own.

// GraphCompositeLabel labels every graph evaluation this runtime reports. The
// composite is an envelope of per-node results, not a specification artifact:
// no JPS version defines a composition, and the one thing the label may say
// about the evaluator that produced the node dispositions is where its
// conformance claim is stated.
const GraphCompositeLabel = "graph composite: an experimental composition of this runtime (spec RFC 0002 is a draft proposal; no JPS version defines a graph or a composite result); each node's disposition is produced by the experimental evaluator, whose conformance claim is stated, in full and only, in CONFORMANCE.md"

// GraphFactFeed records one outcome-as-fact edge as it applied to one node's
// facts document. Injected says whether a value was actually written: an
// upstream node that produced no outcome — an unresolved or not-applicable
// disposition — injects nothing, and the pointer is simply absent from the
// downstream facts document, which is how §7 already treats a fact nobody
// established. Value carries the injected outcome id exactly when Injected is
// true.
type GraphFactFeed struct {
	From     string `json:"from"`
	Pointer  string `json:"pointer"`
	Injected bool   `json:"injected"`
	Value    string `json:"value,omitempty"`
}

// GraphEvidenceFeed records one outcome-as-evidence edge as it applied to one
// node's evidence-availability document: the upstream node, the downstream
// requirement id, and the tri-state value the edge contributed. An upstream
// outcome contributes "present"; anything else contributes the edge's declared
// onUnresolved value, "unknown" unless the edge says "absent".
type GraphEvidenceFeed struct {
	From        string `json:"from"`
	Requirement string `json:"requirement"`
	State       string `json:"state"`
}

// GraphNodeEvaluation is one node's evaluation inside a graph run: the graph's
// node id, the project decision id it names, the evaluated pack's own identity
// echoes, the feeds that arrived over in-edges, and the same disposition,
// handoff target, and trace a standalone evaluation reports. Nothing is
// summarized away: the composite is these results side by side, not a
// replacement for them.
type GraphNodeEvaluation struct {
	Node          string              `json:"node"`
	Pack          string              `json:"pack"`
	PackID        string              `json:"packId"`
	PackVersion   string              `json:"packVersion"`
	SpecVersion   string              `json:"specVersion"`
	FactFeeds     []GraphFactFeed     `json:"factFeeds"`
	EvidenceFeeds []GraphEvidenceFeed `json:"evidenceFeeds"`
	Disposition   Disposition         `json:"disposition"`
	HandoffTarget *HandoffTarget      `json:"handoffTarget,omitempty"`
	Trace         []TraceEntry        `json:"trace"`
}

// GraphHandoff is one node's requested handoff, aggregated so a reader of the
// composite cannot miss an escalation a non-result node requested. It is an
// aggregation of what the node dispositions already say, never a new fact.
type GraphHandoff struct {
	Node        string         `json:"node"`
	TriggeredBy []string       `json:"triggeredBy"`
	Target      *HandoffTarget `json:"target,omitempty"`
}

// GraphEvaluation is the composite envelope of one graph run. Disposition and
// HandoffTarget echo the declared result node's own members, exactly as they
// appear in Nodes — a validated echo on the ADR-0012 pattern, never a second
// truth — and Handoffs aggregates every requested handoff so an escalation
// upstream of the result is as visible as the result itself. Like every
// payload the experimental evaluator produces, it asserts nothing about the
// wisdom of acting on any disposition it carries.
type GraphEvaluation struct {
	OutputVersion             string                `json:"outputVersion"`
	Tool                      Tool                  `json:"tool"`
	Command                   string                `json:"command"`
	Status                    string                `json:"status"`
	Experimental              bool                  `json:"experimental"`
	ConformanceClaimReference string                `json:"conformanceClaimReference"`
	Label                     string                `json:"label"`
	Kind                      string                `json:"kind"`
	EvaluatorSpecVersion      string                `json:"evaluatorSpecVersion"`
	FormatVersion             string                `json:"formatVersion"`
	ConfigPath                string                `json:"configPath"`
	GraphPath                 string                `json:"graphPath"`
	GraphID                   string                `json:"graphId"`
	GraphVersion              string                `json:"graphVersion"`
	ResultNode                string                `json:"resultNode"`
	Disposition               Disposition           `json:"disposition"`
	HandoffTarget             *HandoffTarget        `json:"handoffTarget,omitempty"`
	Handoffs                  []GraphHandoff        `json:"handoffs"`
	Nodes                     []GraphNodeEvaluation `json:"nodes"`
	Artifact                  *Artifact             `json:"artifact,omitempty"`
}

// GraphPlanFeed is one edge as the plan states it, before any evaluation:
// which upstream node feeds which fact pointer or evidence requirement, and —
// for an evidence feed — the tri-state an unresolved upstream would
// contribute. OnUnresolved is stated explicitly, default included, because a
// plan whose reader must know the default has not explained anything.
type GraphPlanFeed struct {
	From         string `json:"from"`
	Fact         string `json:"fact,omitempty"`
	Evidence     string `json:"evidence,omitempty"`
	OnUnresolved string `json:"onUnresolved,omitempty"`
}

// GraphPlanStep is one node in evaluation order: its position, the project
// decision id it names, the declared path, the pack document's own identity
// when it could be read — empty with Detail set when it could not, never
// guessed — and every feed that will arrive before it runs.
type GraphPlanStep struct {
	Order       int             `json:"order"`
	Node        string          `json:"node"`
	Pack        string          `json:"pack"`
	Path        string          `json:"path"`
	PackID      string          `json:"packId"`
	PackVersion string          `json:"packVersion"`
	Detail      string          `json:"detail,omitempty"`
	Feeds       []GraphPlanFeed `json:"feeds"`
}

// GraphPlan is the evaluation plan of one graph, stated without evaluating
// anything: the deterministic node order, and per node the feeds it would
// receive. It reads pack documents only to echo their identity; no condition
// is interpreted, no disposition is produced, and the plan authorizes nothing.
type GraphPlan struct {
	OutputVersion string          `json:"outputVersion"`
	Tool          Tool            `json:"tool"`
	Command       string          `json:"command"`
	Status        string          `json:"status"`
	Kind          string          `json:"kind"`
	FormatVersion string          `json:"formatVersion"`
	ConfigPath    string          `json:"configPath"`
	GraphPath     string          `json:"graphPath"`
	GraphID       string          `json:"graphId"`
	GraphVersion  string          `json:"graphVersion"`
	ResultNode    string          `json:"resultNode"`
	Steps         []GraphPlanStep `json:"steps"`
}

// GraphSchema describes the exact embedded graph schema bytes, on the shape
// packs schema reports for the configuration schema. It names no
// specification version, because the graph format is this runtime's and has a
// formatVersion of its own — which is what FormatVersion carries, never the
// version member of any graph document: a payload in which "graphVersion"
// could mean either fact would be a payload nobody could read safely, so the
// format version is "formatVersion" in every graph payload and the document
// identity echo is graphId with graphVersion, exactly parallel to packId with
// packVersion.
type GraphSchema struct {
	OutputVersion string `json:"outputVersion"`
	Tool          Tool   `json:"tool"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	Kind          string `json:"kind"`
	FormatVersion string `json:"formatVersion"`
	SchemaID      string `json:"schemaId"`
	Bytes         int    `json:"bytes"`
	SHA256        string `json:"sha256"`
	WrittenTo     string `json:"writtenTo,omitempty"`
}

// GraphValidation is one graph document's validation report: the document
// against the embedded schema and the graph's own semantic rules, and its
// references against the project — every node resolves through the
// configuration, every evidence feed names a requirement its target pack
// declares. Diagnostics carry every finding rather than the first one.
type GraphValidation struct {
	OutputVersion string       `json:"outputVersion"`
	Tool          Tool         `json:"tool"`
	Command       string       `json:"command"`
	Status        string       `json:"status"`
	Kind          string       `json:"kind"`
	FormatVersion string       `json:"formatVersion"`
	ConfigPath    string       `json:"configPath"`
	GraphPath     string       `json:"graphPath"`
	GraphID       string       `json:"graphId,omitempty"`
	GraphVersion  string       `json:"graphVersion,omitempty"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

// GraphMatrixLabel labels every graph matrix run. Like the pack matrix label
// it points at the one claim document and says nothing of its own about it,
// and like the composite label it says what no specification defines.
const GraphMatrixLabel = "graph matrix results: rows a project wrote about its own graph, run through this runtime's experimental composition (no JPS version defines a graph or a composite result); each node's disposition is produced by the experimental evaluator, whose conformance claim is stated, in full and only, in CONFORMANCE.md"

// GraphTestNode is one named node's comparison inside one graph matrix row:
// the RFC 8785 canonical §8.3 dispositions, expected and actual, compared byte
// for byte by the same canonicalizer every other comparison uses. Only the
// nodes a row names are compared; an unnamed node is unchecked, and saying so
// is the row author's choice made visible.
type GraphTestNode struct {
	Node     string `json:"node"`
	Status   string `json:"status"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// GraphTestRow is one graph matrix row's result: the composite headline
// compared canonically, the §8.4 class and phase for a row that expects a
// refusal instead, and the per-node comparisons the row asked for.
type GraphTestRow struct {
	ID                 string          `json:"id"`
	Status             string          `json:"status"`
	Expected           string          `json:"expected"`
	Actual             string          `json:"actual"`
	ExpectedErrorClass string          `json:"expectedErrorClass,omitempty"`
	ActualErrorClass   string          `json:"actualErrorClass,omitempty"`
	ExpectedErrorPhase string          `json:"expectedErrorPhase,omitempty"`
	ActualErrorPhase   string          `json:"actualErrorPhase,omitempty"`
	Nodes              []GraphTestNode `json:"nodes,omitempty"`
	Detail             string          `json:"detail,omitempty"`
}

// GraphTest is one graph matrix run. It carries Experimental and
// ConformanceClaimReference like every payload the evaluator produces, and
// the graph labels beside them, because both surfaces made it: the rows ran
// through the experimental composition, and each node's disposition through
// the experimental evaluator.
//
// Coverage reuses MatrixProbe unchanged, under a graph-owned probe grammar
// (MatrixProbe itself stays surface-agnostic): "node:<nodeId>:<packProbe>" is
// one node's pack-level probe — witnessed by a row's expectedNodes entry for
// that node, and for the declared result node also by the row's headline
// expectation, which is that node's disposition echoed — and
// "edge:<index>:resolved" / "edge:<index>:unresolved" are one edge's two
// branches, witnessed by the upstream node's expected disposition kind. Node
// ids are lowercase-kebab per the graph schema, so the colon namespacing is
// unambiguous. The member is present when some node's pack derived a probe,
// and absent when no node's pack could be read or the evaluator admitted none
// — never a statement that the rows did not load, which is a refusal carrying
// no payload at all. Mismatched rows still witness, because coverage reads
// expectations, which exist whether or not they held. It moves no status and
// no exit code (ADR-0014's invariant, inherited by ADR-0016), and it is
// additive, so outputVersion is unchanged by the VERSIONING.md machine-output
// rules.
type GraphTest struct {
	OutputVersion             string `json:"outputVersion"`
	Tool                      Tool   `json:"tool"`
	Command                   string `json:"command"`
	Status                    string `json:"status"`
	Experimental              bool   `json:"experimental"`
	ConformanceClaimReference string `json:"conformanceClaimReference"`
	Label                     string `json:"label"`
	Kind                      string `json:"kind"`
	FormatVersion             string `json:"formatVersion"`
	EvaluatorSpecVersion      string `json:"evaluatorSpecVersion"`
	ConfigPath                string `json:"configPath"`
	GraphPath                 string `json:"graphPath"`
	RowsPath                  string `json:"rowsPath"`
	GraphID                   string `json:"graphId"`
	GraphVersion              string `json:"graphVersion"`
	// GraphSHA256 binds this run to the exact document bytes it loaded
	// (ADR-0030), bare hex per the payload convention.
	GraphSHA256 string         `json:"graphSha256"`
	Summary     SuiteSummary   `json:"summary"`
	Rows        []GraphTestRow `json:"rows"`
	Coverage    []MatrixProbe  `json:"coverage,omitempty"`
}

// GraphSuiteEntry is one configured graph's matrix run inside the project walk
// (ADR-0017), on PackTestEntry's shape: the project's graph id beside the
// graph document's own identity echoes, the run's rows and coverage exactly as
// the single-graph payload carries them, and Detail set when the graph did not
// run — an unreadable or invalid document, a missing rows declaration — in
// which case the identity echoes are empty rather than guessed and Rows and
// Coverage are absent.
type GraphSuiteEntry struct {
	ID           string `json:"id"`
	Path         string `json:"path"`
	RowsPath     string `json:"rowsPath,omitempty"`
	GraphID      string `json:"graphId,omitempty"`
	GraphVersion string `json:"graphVersion,omitempty"`
	// GraphSHA256 is the bare-hex digest of the exact bytes this entry's run
	// loaded (ADR-0030), read off the one load: a consumer holding the served
	// document from another call binds these rows to that revision by equality
	// rather than by hope. Absent exactly when the document did not load,
	// beside the detail that says why.
	GraphSHA256 string         `json:"graphSha256,omitempty"`
	Status      string         `json:"status"`
	Summary     SuiteSummary   `json:"summary"`
	Rows        []GraphTestRow `json:"rows,omitempty"`
	Coverage    []MatrixProbe  `json:"coverage,omitempty"`
	Detail      string         `json:"detail,omitempty"`
}

// GraphSuite is the project graph-matrix walk: every configured graph's rows,
// or the one graph --id selected, each entry exactly one graph test. It
// carries the same labels the single-graph payload carries, because the same
// two surfaces made every entry. A walk over a configuration that declares no
// graphs reports status "skipped" over zero entries — an answer, not a clean
// pass: nothing was tested, and the exit code says so.
type GraphSuite struct {
	OutputVersion             string            `json:"outputVersion"`
	Tool                      Tool              `json:"tool"`
	Command                   string            `json:"command"`
	Status                    string            `json:"status"`
	Experimental              bool              `json:"experimental"`
	ConformanceClaimReference string            `json:"conformanceClaimReference"`
	Label                     string            `json:"label"`
	Kind                      string            `json:"kind"`
	FormatVersion             string            `json:"formatVersion"`
	EvaluatorSpecVersion      string            `json:"evaluatorSpecVersion"`
	ConfigPath                string            `json:"configPath"`
	ConfigVersion             string            `json:"configVersion"`
	Summary                   SuiteSummary      `json:"summary"`
	Graphs                    []GraphSuiteEntry `json:"graphs"`
}

// GraphValidationEntry is one configured graph's validation inside the project
// walk: the document against the embedded schema, the graph's own rules, and
// its references against the project — the same checks the single-graph verb
// applies — plus the declared rows document's containment when one is
// declared. Diagnostics carry every finding rather than the first one.
type GraphValidationEntry struct {
	ID           string `json:"id"`
	Path         string `json:"path"`
	RowsPath     string `json:"rowsPath,omitempty"`
	GraphID      string `json:"graphId,omitempty"`
	GraphVersion string `json:"graphVersion,omitempty"`
	// GraphSHA256 is the bare-hex digest of the exact bytes this walk loaded
	// (ADR-0030): the member that lets a consumer holding the document from
	// another call know these results are about the same revision, read off
	// the one load rather than a second one. Absent exactly when the document
	// did not load, beside the diagnostics that say why.
	GraphSHA256 string       `json:"graphSha256,omitempty"`
	Status      string       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// GraphValidationSuite is the project graph-validation walk, on
// PackValidation's shape: every configured graph held to the same checks, and
// a summary a CI gate can read at a glance. Like every graph payload it is a
// non-normative runtime convention, and says so in Kind.
type GraphValidationSuite struct {
	OutputVersion string                 `json:"outputVersion"`
	Tool          Tool                   `json:"tool"`
	Command       string                 `json:"command"`
	Status        string                 `json:"status"`
	Kind          string                 `json:"kind"`
	FormatVersion string                 `json:"formatVersion"`
	ConfigPath    string                 `json:"configPath"`
	ConfigVersion string                 `json:"configVersion"`
	Summary       PackCounts             `json:"summary"`
	Graphs        []GraphValidationEntry `json:"graphs"`
}

// GraphSummary is one configured graph's inventory row: the project's
// configured id beside the document's own identity, two different names
// reported as two members exactly as a PackSummary does. Detail is present
// when the document could not be read or decoded, in which case the identity
// members are empty rather than guessed — listing is not validating, and
// experimental graph validate is where a broken graph is an error. ResultNode
// echoes the document's declared result member, the one node whose disposition
// is the composite headline.
type GraphSummary struct {
	ID            string `json:"id"`
	GraphID       string `json:"graphId"`
	GraphVersion  string `json:"graphVersion"`
	FormatVersion string `json:"formatVersion"`
	ResultNode    string `json:"resultNode,omitempty"`
	Path          string `json:"path"`
	RowsPath      string `json:"rowsPath,omitempty"`
	RowsDeclared  bool   `json:"rowsDeclared"`
	Description   string `json:"description,omitempty"`
	// NodeCount and EdgeCount exist exactly when identity decoding succeeded
	// and the member has its declared shape — nodes a JSON object, edges a
	// JSON array — and are absent, not zero, otherwise, so a malformed
	// document can never look like an honest empty one. The names avoid nodes
	// and rows, which every walk payload uses for arrays.
	NodeCount *int   `json:"nodeCount,omitempty"`
	EdgeCount *int   `json:"edgeCount,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// GraphInventory is the resolved graph inventory of one project, the graph
// sibling of PackInventory and deliberately not a member of it: the pack
// inventory is a stable command's payload, and this surface is experimental,
// so folding one into the other would put a removable member inside a payload
// that must not shrink (the separation ADR-0015 drew and ADR-0017 kept).
// Status is "none" when no configuration was found, which is an answer and not
// a failure, and Note says where the runtime looked.
type GraphInventory struct {
	OutputVersion string         `json:"outputVersion"`
	Tool          Tool           `json:"tool"`
	Command       string         `json:"command"`
	Status        string         `json:"status"`
	Experimental  bool           `json:"experimental"`
	Kind          string         `json:"kind"`
	ConfigPath    string         `json:"configPath"`
	ConfigVersion string         `json:"configVersion,omitempty"`
	Note          string         `json:"note,omitempty"`
	Graphs        []GraphSummary `json:"graphs"`
}

// GraphDocument is the metadata beside one graph document served by configured
// id; the bytes travel separately, exactly as a served pack's do. Status is
// "valid" when the served bytes decoded and the identity members were read off
// them, and "undecodable" when they did not, in which case Detail says why and
// those members are empty rather than guessed. Neither value is a verdict on
// the document against the graph schema: serving a graph does not validate it,
// and experimental graph validate is what reports that. FormatVersion here is
// the version the document itself declares, read off the served bytes the way
// a PackDocument's specVersion is — not the format version a walk applied,
// which is what the member means on an evaluation payload.
type GraphDocument struct {
	OutputVersion string `json:"outputVersion"`
	Tool          Tool   `json:"tool"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	Experimental  bool   `json:"experimental"`
	Kind          string `json:"kind"`
	ConfigPath    string `json:"configPath"`
	ID            string `json:"id"`
	GraphID       string `json:"graphId"`
	GraphVersion  string `json:"graphVersion"`
	FormatVersion string `json:"formatVersion"`
	ResultNode    string `json:"resultNode,omitempty"`
	Path          string `json:"path"`
	RowsPath      string `json:"rowsPath,omitempty"`
	Description   string `json:"description,omitempty"`
	Bytes         int    `json:"bytes"`
	SHA256        string `json:"sha256"`
	Detail        string `json:"detail,omitempty"`
}
