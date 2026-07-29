// Package graph implements the experimental judgment-graph surface (ADR-0015):
// a runtime-owned composition of packs a project's jpack.json already names,
// prototyping the composition the Judgment Pack Specification's RFC 0002
// (Draft) proposes.
//
// Nothing here is specified. No JPS version defines a graph, an edge, or a
// composite result, so this whole package is a non-normative convention of
// this runtime, labeled as such in every payload, on the same footing as the
// project convention (ADR-0012) and as removable as any experimental surface
// (ADR-0007). What is specified is each node's own evaluation: a node is one
// ordinary §§7-8 evaluation of one conformant pack, performed by the same
// engine every other surface uses, and this package never reaches inside it.
//
// The composition rules are deliberately small, and each one leans on a
// decision the runtime already made rather than inventing a new one:
//
//   - A graph references packs by project decision id and never contains one.
//     Identity stays in the pack document (ADR-0012), resolution goes through
//     the project's own jailed directory handle, and a graph cannot change
//     what any pack means.
//   - Edges feed dispositions forward in two declared ways: an outcome id
//     written at a facts pointer, and a tri-state contribution to evidence
//     availability. An upstream that produced no outcome injects no fact —
//     the pointer is simply absent, which §7 already reads as unknown — and
//     contributes the edge's declared onUnresolved tri-state, "unknown"
//     unless the edge says "absent". Unresolved upstreams therefore reach a
//     downstream pack only through that pack's own declared unknown handling.
//   - The edges must form a DAG, evaluation order is the deterministic
//     topological order with node ids breaking ties, and every node always
//     evaluates: there is no conditional execution to reason about, and a
//     side judgment's escalation is as visible as the result node's.
//   - Conflicts are refused, never merged: two edges feeding the same fact
//     pointer, overlapping fact pointers (one inside the other), or the same
//     evidence requirement of one node are each a validation error, and a
//     feed that collides with a caller-supplied input is an evaluation
//     refusal. A merge rule would be a semantics nobody declared.
//   - Assembly is bounded: a fact pointer is at most 64 tokens deep, one
//     node's injected outcome ids are charged against the carrier's byte
//     limit before they are written, an assembled document past what the
//     preflight admits is handed to the engine as oversized rather than
//     carried, and encoding adds no escaping inflation — so composing inputs
//     allocates no more than the inputs document's own limit already admitted.
package graph

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

const (
	// FormatVersion is the one formatVersion value this runtime accepts, a
	// single integer as a string on the jpack.json configVersion precedent:
	// this file describes a shape a program reads, and a shape either is the
	// one this program knows or is not. It is deliberately not the document's
	// own version member — a graph has an identity version like a pack does,
	// and the two must never share a name in a payload.
	FormatVersion = "1"
	// SchemaID is the embedded schema's own $id.
	SchemaID = "urn:judgmentpack:runtime:jgraph:1"
	// MaxGraphBytes bounds one graph document. It is an index of nodes and
	// edges, not a pack.
	MaxGraphBytes = int64(1 << 20)
	// MaxInputsBytes bounds one inputs document, which carries facts and
	// evidence documents for every node and is read whole before anything runs,
	// on the instance-matrix precedent.
	MaxInputsBytes = int64(16 << 20)
	// MaxFactPointerDepth bounds one fact pointer's reference-token count. The
	// carrier admits documents 128 levels deep, so a pointer bounded at half
	// that keeps every assembled facts document decodable by the engine's own
	// nesting limit instead of exhausting the assembler first; no honest fact
	// path approaches either number.
	MaxFactPointerDepth = 64
)

//go:embed graph.schema.json
var schemaBytes []byte

// SupportedFormatVersions names every formatVersion this runtime accepts, so a
// refusal can say what would have been accepted instead of only what was not.
func SupportedFormatVersions() []string { return []string{FormatVersion} }

// Schema returns the exact embedded graph schema bytes.
func Schema() []byte { return schemaBytes }

// SchemaDescription composes the metadata payload for the embedded schema, on
// the shape packs schema reports for the configuration schema.
func SchemaDescription(command string) result.GraphSchema {
	sum := sha256.Sum256(schemaBytes)
	return result.GraphSchema{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "valid",
		Kind:          result.ProjectKind,
		FormatVersion: FormatVersion,
		SchemaID:      SchemaID,
		Bytes:         len(schemaBytes),
		SHA256:        hex.EncodeToString(sum[:]),
	}
}

// Node is one graph node: a reference to one pack by the decision id the
// project's jpack.json declares. It carries no identity of its own beyond its
// key, because identity lives in the pack document (ADR-0012).
type Node struct {
	Pack        string `json:"pack"`
	Description string `json:"description,omitempty"`
}

// EvidenceFeed is one edge's outcome-as-evidence device: the downstream
// requirement id, and the tri-state an upstream disposition that is not an
// outcome contributes.
type EvidenceFeed struct {
	ID           string `json:"id"`
	OnUnresolved string `json:"onUnresolved,omitempty"`
}

// State returns the tri-state this feed contributes for one upstream
// disposition kind: "present" for an outcome, and the declared onUnresolved —
// "unknown" unless the edge says "absent" — for anything else.
func (f EvidenceFeed) State(upstreamKind string) string {
	if upstreamKind == "outcome" {
		return "present"
	}
	if f.OnUnresolved == "absent" {
		return "absent"
	}
	return "unknown"
}

// Edge feeds one upstream node's disposition to one downstream node, as a
// fact, as evidence availability, or both. The schema requires at least one of
// the two devices: an edge that feeds nothing declares a dependency nobody can
// observe, which is an authoring mistake and not an option.
type Edge struct {
	From        string        `json:"from"`
	To          string        `json:"to"`
	Fact        string        `json:"fact,omitempty"`
	Evidence    *EvidenceFeed `json:"evidence,omitempty"`
	Description string        `json:"description,omitempty"`
}

// Document is one graph document, already past the carrier, the version gate,
// and the embedded schema. Its references are not yet checked: Structural and
// Semantic are what hold them to the graph's own rules and to the project.
type Document struct {
	FormatVersion string          `json:"formatVersion"`
	ID            string          `json:"id"`
	Version       string          `json:"version"`
	Description   string          `json:"description,omitempty"`
	Nodes         map[string]Node `json:"nodes"`
	Edges         []Edge          `json:"edges"`
	Result        string          `json:"result"`
}

// Load reads one graph document from bytes: the strict carrier decode every
// other surface uses (duplicate member names, depth, and size limits all
// enforced), the formatVersion gate before the schema so a document written
// for a later shape is told exactly that, then the embedded schema. name
// appears in messages and is the caller's label for the input — a path, or
// "-".
//
// The returned failure is operational and carries no §8.4 class: a graph is
// not an input the specification defines, so no evaluation error class could
// be honest about it.
func Load(data []byte, name string) (Document, *evaluation.Failure) {
	if int64(len(data)) > MaxGraphBytes {
		return Document{}, &evaluation.Failure{
			Code:     "JPS-RESOURCE-GRAPH-BYTE-LIMIT",
			Message:  fmt.Sprintf("The graph document %s exceeds the %d-byte limit; it is an index of nodes and edges, not a pack.", display.Sanitize(name), MaxGraphBytes),
			ExitCode: result.ExitIO,
		}
	}
	if len(data) == 0 {
		return Document{}, &evaluation.Failure{
			Code:     "JPS-GRAPH-JSON",
			Message:  fmt.Sprintf("The graph document %s is empty; empty bytes are not a carrier-conforming JSON text.", display.Sanitize(name)),
			ExitCode: result.ExitInvocation,
		}
	}
	decoded, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return Document{}, &evaluation.Failure{
			Code:     "JPS-GRAPH-JSON",
			Message:  fmt.Sprintf("The graph document %s is not acceptable JSON: %s", display.Sanitize(name), display.Sanitize(carrierFailure.Diagnostic.Message)),
			ExitCode: result.ExitInvocation,
		}
	}
	documentRoot, ok := decoded.(map[string]any)
	if !ok {
		return Document{}, &evaluation.Failure{
			Code:     "JPS-GRAPH-SCHEMA",
			Message:  fmt.Sprintf("The graph document %s must be a JSON object.", display.Sanitize(name)),
			ExitCode: result.ExitInvalid,
		}
	}
	if failure := declaredFormatVersion(name, documentRoot); failure != nil {
		return Document{}, failure
	}
	compiled, err := validation.CompileSchema(schemaBytes, SchemaID)
	if err != nil {
		return Document{}, &evaluation.Failure{
			Code:     "JPS-ARTIFACT-GRAPH-SCHEMA",
			Message:  "The embedded graph schema could not be compiled.",
			ExitCode: result.ExitInternal,
		}
	}
	if err := compiled.Validate(decoded); err != nil {
		return Document{}, &evaluation.Failure{
			Code:     "JPS-GRAPH-SCHEMA",
			Message:  fmt.Sprintf("The graph document %s does not satisfy the graph schema: %s", display.Sanitize(name), schemaDetail(err)),
			ExitCode: result.ExitInvalid,
		}
	}
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		return Document{}, &evaluation.Failure{
			Code:     "JPS-GRAPH-SCHEMA",
			Message:  fmt.Sprintf("The graph document %s satisfied its schema but could not be decoded.", display.Sanitize(name)),
			ExitCode: result.ExitInternal,
		}
	}
	return document, nil
}

// declaredFormatVersion holds one document to a formatVersion this runtime
// accepts, before the schema is consulted, so a document written for a later
// shape produces an actionable sentence rather than a list of unknown members.
func declaredFormatVersion(name string, root map[string]any) *evaluation.Failure {
	raw, present := root["formatVersion"]
	if !present {
		return &evaluation.Failure{
			Code:     "JPS-GRAPH-VERSION",
			Message:  fmt.Sprintf("The graph document %s declares no formatVersion. This runtime accepts: %s.", display.Sanitize(name), strings.Join(SupportedFormatVersions(), ", ")),
			ExitCode: result.ExitInvalid,
		}
	}
	declared, ok := raw.(string)
	if !ok {
		return &evaluation.Failure{
			Code:     "JPS-GRAPH-VERSION",
			Message:  fmt.Sprintf("The formatVersion of %s must be a string. This runtime accepts: %s.", display.Sanitize(name), strings.Join(SupportedFormatVersions(), ", ")),
			ExitCode: result.ExitInvalid,
		}
	}
	for _, supported := range SupportedFormatVersions() {
		if declared == supported {
			return nil
		}
	}
	return &evaluation.Failure{
		Code:     "JPS-GRAPH-VERSION",
		Message:  fmt.Sprintf("The graph document %s declares formatVersion %q, which this runtime does not support. It accepts: %s.", display.Sanitize(name), display.Sanitize(declared), strings.Join(SupportedFormatVersions(), ", ")),
		ExitCode: result.ExitUnsupported,
	}
}

// schemaDetail compresses one schema error into a single sanitized line,
// exactly as the project configuration's loader does and for the same reason:
// an author needs the first thing that went wrong on one line, and the whole
// schema is printable beside it.
func schemaDetail(err error) string {
	detail := strings.Join(strings.Fields(err.Error()), " ")
	const limit = 400
	if len(detail) > limit {
		detail = detail[:limit] + "..."
	}
	return display.Sanitize(detail)
}

// Structural holds one loaded document to the graph's own rules — every check
// that reads nothing but the document and the configuration's declared ids:
// every edge endpoint and the result name declared nodes, no edge feeds a node
// from itself, no two edges feed the same fact pointer or the same evidence
// requirement of one node, no fact pointer sits inside another edge's fact
// pointer for the same node, no fact pointer is deeper than the injection
// depth this runtime admits, every node's pack is a decision id the
// configuration declares, and the edges form a DAG.
//
// This is the gate evaluation and planning run behind, deliberately without
// the checks that read pack documents: at evaluation time the engine reads
// each pack itself and assigns the §8.4 class its defects deserve, and a
// pre-read here would flatten a malformed pack into an unclassed graph
// refusal. Semantic adds those reference checks for the validate report.
//
// Every finding is reported rather than the first, in a deterministic order —
// nodes sorted by id, then edges by position, then the result, then the cycle
// — so two runs over one document disagree with nobody.
func (d Document) Structural(loaded *project.Project) []result.Diagnostic {
	diagnostics := []result.Diagnostic{}
	report := func(code, path, message string) {
		diagnostics = append(diagnostics, result.ErrorDiagnostic(code, "graph", path, message))
	}

	nodeIDs := make([]string, 0, len(d.Nodes))
	for id := range d.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	for _, id := range nodeIDs {
		pack := d.Nodes[id].Pack
		if _, ok := loaded.Entry(pack); !ok {
			known := "The configuration declares no packs."
			if len(loaded.IDs) > 0 {
				known = "Configured ids: " + strings.Join(loaded.IDs, ", ") + "."
			}
			report("JPS-GRAPH-NODE-PACK", "/nodes/"+id,
				fmt.Sprintf("The node references decision id %q, which %s does not declare. %s", display.Sanitize(pack), display.Sanitize(loaded.ConfigPath), known))
		}
	}

	factTargets := map[string]bool{}
	// factPaths carries every valid fact pointer per target node, in edge
	// order, for the overlap check: two pointers where one is a prefix of the
	// other write into each other's values, which no declared semantics covers
	// — and which raw-string equality cannot see, since two spellings can
	// decode to one path. The raw spelling rides along so an exact duplicate
	// stays the duplicate check's finding alone.
	type factPath struct {
		raw    string
		tokens []string
	}
	factPaths := map[string][]factPath{}
	evidenceTargets := map[string]bool{}
	for index, edge := range d.Edges {
		path := fmt.Sprintf("/edges/%d", index)
		fromDeclared := d.declares(edge.From)
		toDeclared := d.declares(edge.To)
		if !fromDeclared {
			report("JPS-GRAPH-EDGE-ENDPOINT", path+"/from",
				fmt.Sprintf("The edge names node %q, which the graph does not declare.", display.Sanitize(edge.From)))
		}
		if !toDeclared {
			report("JPS-GRAPH-EDGE-ENDPOINT", path+"/to",
				fmt.Sprintf("The edge names node %q, which the graph does not declare.", display.Sanitize(edge.To)))
		}
		if fromDeclared && toDeclared && edge.From == edge.To {
			report("JPS-GRAPH-EDGE-SELF", path,
				fmt.Sprintf("The edge feeds node %q from itself, which no acyclic order can run.", display.Sanitize(edge.From)))
		}
		if edge.Fact != "" && toDeclared {
			key := edge.To + "\x00" + edge.Fact
			if factTargets[key] {
				report("JPS-GRAPH-EDGE-DUPLICATE-FACT", path+"/fact",
					fmt.Sprintf("A second edge feeds the fact pointer %q of node %q; one pointer has one feeding edge, and a merge rule is a semantics nobody declared.", display.Sanitize(edge.Fact), display.Sanitize(edge.To)))
			}
			factTargets[key] = true

			tokens := pointerTokens(edge.Fact)
			if len(tokens) > MaxFactPointerDepth {
				report("JPS-GRAPH-FACT-POINTER-DEPTH", path+"/fact",
					fmt.Sprintf("The fact pointer of node %q has %d reference tokens; this runtime admits at most %d, so the assembled facts document stays inside the evaluator's own nesting limit.", display.Sanitize(edge.To), len(tokens), MaxFactPointerDepth))
			}
			for _, earlier := range factPaths[edge.To] {
				if earlier.raw != edge.Fact && pathsOverlap(earlier.tokens, tokens) {
					report("JPS-GRAPH-EDGE-FACT-OVERLAP", path+"/fact",
						fmt.Sprintf("The fact pointer %q of node %q sits inside another edge's fact pointer (or contains one); overlapping injections have no declared merge, and evaluation would refuse whichever edge runs second.", display.Sanitize(edge.Fact), display.Sanitize(edge.To)))
					break
				}
			}
			factPaths[edge.To] = append(factPaths[edge.To], factPath{raw: edge.Fact, tokens: tokens})
		}
		if edge.Evidence != nil && toDeclared {
			key := edge.To + "\x00" + edge.Evidence.ID
			if evidenceTargets[key] {
				report("JPS-GRAPH-EDGE-DUPLICATE-EVIDENCE", path+"/evidence/id",
					fmt.Sprintf("A second edge feeds the evidence requirement %q of node %q; one requirement has one feeding edge.", display.Sanitize(edge.Evidence.ID), display.Sanitize(edge.To)))
			}
			evidenceTargets[key] = true
		}
	}

	if !d.declares(d.Result) {
		report("JPS-GRAPH-RESULT-NODE", "/result",
			fmt.Sprintf("The result names node %q, which the graph does not declare.", display.Sanitize(d.Result)))
	}

	if _, cycle := d.Order(); len(cycle) > 0 {
		report("JPS-GRAPH-CYCLE", "/edges",
			fmt.Sprintf("The edges do not form a directed acyclic graph; these nodes form dependency cycles: %s.", strings.Join(cycle, ", ")))
	}
	return diagnostics
}

// Semantic is Structural plus the reference checks that read pack documents
// through the project's own handle: every evidence feed names a requirement
// its target pack declares, and a target pack that cannot be read or decoded
// is reported on its node. These checks belong to the validate report and not
// to the evaluation gate — when a node runs, the engine reads its pack itself
// and a defect there gets its §8.4 class instead of an unclassed refusal here.
func (d Document) Semantic(loaded *project.Project) []result.Diagnostic {
	diagnostics := d.Structural(loaded)
	report := func(code, path, message string) {
		diagnostics = append(diagnostics, result.ErrorDiagnostic(code, "graph", path, message))
	}

	// The declared evidence-requirement ids of every node some edge feeds
	// evidence to, read once per node. A pack that cannot be read or decoded
	// gets one diagnostic on the node, and its feeds are not additionally
	// reported against ids nobody could check — declaredEvidence simply has no
	// entry for that node.
	nodeIDs := make([]string, 0, len(d.Nodes))
	for id := range d.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	declaredEvidence := map[string]map[string]bool{}
	for _, id := range nodeIDs {
		entry, configured := loaded.Entry(d.Nodes[id].Pack)
		if !configured || !nodeReceivesEvidence(d.Edges, id) {
			continue
		}
		ids, err := declaredEvidenceIDs(loaded, entry)
		if err != nil {
			report("JPS-GRAPH-NODE-PACK-READ", "/nodes/"+id,
				fmt.Sprintf("The node's pack could not be checked against the edges that feed it evidence. %s", project.ReadFailureMessage(entry.Path, err)))
			continue
		}
		declaredEvidence[id] = ids
	}
	for index, edge := range d.Edges {
		if edge.Evidence == nil {
			continue
		}
		if declared, checked := declaredEvidence[edge.To]; checked && !declared[edge.Evidence.ID] {
			report("JPS-GRAPH-EDGE-EVIDENCE-UNDECLARED", fmt.Sprintf("/edges/%d/evidence/id", index),
				fmt.Sprintf("The edge feeds evidence requirement %q, which the pack of node %q does not declare.", display.Sanitize(edge.Evidence.ID), display.Sanitize(edge.To)))
		}
	}
	return diagnostics
}

// pointerTokens splits one RFC 6901 pointer into its decoded reference
// tokens. The pointer is schema-checked before anything calls this, so it
// begins with a slash.
func pointerTokens(pointer string) []string {
	raw := strings.Split(pointer[1:], "/")
	tokens := make([]string, len(raw))
	for index, token := range raw {
		tokens[index] = decodePointerToken(token)
	}
	return tokens
}

// pathsOverlap reports whether one decoded token path is a prefix of the
// other — including being equal, which reaches here only when two different
// spellings decode to one path, since identical spellings are the duplicate
// check's finding.
func pathsOverlap(a, b []string) bool {
	shorter := a
	longer := b
	if len(b) < len(a) {
		shorter, longer = b, a
	}
	for index, token := range shorter {
		if longer[index] != token {
			return false
		}
	}
	return true
}

// declares reports whether one node id is declared by the document.
func (d Document) declares(node string) bool {
	_, ok := d.Nodes[node]
	return ok
}

// nodeReceivesEvidence reports whether any edge feeds evidence to one node.
func nodeReceivesEvidence(edges []Edge, node string) bool {
	for _, edge := range edges {
		if edge.To == node && edge.Evidence != nil {
			return true
		}
	}
	return false
}

// declaredEvidenceIDs reads one configured pack through the project's handle
// and returns the evidence-requirement ids it declares. The read is bounded
// and the decode is the strict carrier decode; nothing is validated here,
// because checking a reference needs the document's declarations and not its
// conformance verdict — the evaluator establishes that when the node runs.
func declaredEvidenceIDs(loaded *project.Project, entry project.Pack) (map[string]bool, error) {
	data, err := loaded.ReadPack(entry)
	if err != nil {
		return nil, err
	}
	decoded, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return nil, fmt.Errorf("%s", carrierFailure.Diagnostic.Message)
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("the pack document is not a JSON object")
	}
	ids := map[string]bool{}
	requirements, _ := root["evidenceRequirements"].([]any)
	for _, entry := range requirements {
		if requirement, ok := entry.(map[string]any); ok {
			if id, ok := requirement["id"].(string); ok {
				ids[id] = true
			}
		}
	}
	return ids, nil
}

// Order returns the deterministic evaluation order and, when the edges do not
// form a DAG, the sorted node ids that are actually on a cycle. The order is
// Kahn's algorithm with the smallest node id chosen first at every step, so
// one document yields one order in every process on every platform. Cycle
// membership is decided by strongly connected components rather than by
// Kahn's leftover set, because the leftover set also holds nodes that are
// merely blocked behind a cycle, and naming one of those would send an author
// hunting a cycle it is not in. Edges whose endpoints the document does not
// declare, and self-edges, are ignored here: Structural reports each of those
// as its own defect, and this order exists for the nodes the document
// actually relates.
func (d Document) Order() ([]string, []string) {
	predecessors := map[string]map[string]bool{}
	successors := map[string]map[string]bool{}
	for id := range d.Nodes {
		predecessors[id] = map[string]bool{}
		successors[id] = map[string]bool{}
	}
	for _, edge := range d.Edges {
		if edge.From == edge.To || !d.declares(edge.From) || !d.declares(edge.To) {
			continue
		}
		predecessors[edge.To][edge.From] = true
		successors[edge.From][edge.To] = true
	}
	available := []string{}
	for id, sources := range predecessors {
		if len(sources) == 0 {
			available = append(available, id)
		}
	}
	order := make([]string, 0, len(d.Nodes))
	for len(available) > 0 {
		sort.Strings(available)
		next := available[0]
		available = available[1:]
		order = append(order, next)
		for follower := range successors[next] {
			delete(predecessors[follower], next)
			if len(predecessors[follower]) == 0 {
				available = append(available, follower)
			}
		}
	}
	if len(order) == len(d.Nodes) {
		return order, nil
	}
	return order, cycleMembers(d.Nodes, successors)
}

// cycleMembers returns the sorted node ids on some cycle: the members of
// every strongly connected component larger than one node (self-edges never
// reach the successor sets, so a single-node component is never cyclic here).
// Tarjan's algorithm, with roots and neighbors visited in sorted order; the
// recursion is bounded by the schema's node cap.
func cycleMembers(nodes map[string]Node, successors map[string]map[string]bool) []string {
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	index := 0
	indices := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	stack := []string{}
	members := []string{}

	var connect func(id string)
	connect = func(id string) {
		indices[id] = index
		low[id] = index
		index++
		stack = append(stack, id)
		onStack[id] = true
		followers := make([]string, 0, len(successors[id]))
		for follower := range successors[id] {
			followers = append(followers, follower)
		}
		sort.Strings(followers)
		for _, follower := range followers {
			if _, seen := indices[follower]; !seen {
				connect(follower)
				if low[follower] < low[id] {
					low[id] = low[follower]
				}
			} else if onStack[follower] && indices[follower] < low[id] {
				low[id] = indices[follower]
			}
		}
		if low[id] == indices[id] {
			component := []string{}
			for {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[top] = false
				component = append(component, top)
				if top == id {
					break
				}
			}
			if len(component) > 1 {
				members = append(members, component...)
			}
		}
	}
	for _, id := range ids {
		if _, seen := indices[id]; !seen {
			connect(id)
		}
	}
	sort.Strings(members)
	return members
}
