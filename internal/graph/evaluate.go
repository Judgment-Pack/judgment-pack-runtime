package graph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Options carries one graph evaluation's caller choices, applied identically
// to every node: the reporting surface's command name, and the consumer's
// extension capability set. There is no per-node option, deliberately — a
// graph whose nodes ran under different capability sets would be a composite
// nobody could reason about.
type Options struct {
	Command             string
	SupportedExtensions []string

	// Audit is the trail this run's records are appended to, or nil for a run
	// that records nothing. It is a field rather than something derived from
	// the project, because the project is live on both graph paths and only
	// one of them is a decision: experimental graph evaluate sets it and
	// experimental graph test does not, so a matrix run over the same graph,
	// with the same configuration, records nothing.
	Audit *audit.Writer

	// LawCheck is a caller-supplied check on each node's pack, applied to the
	// exact bytes this run is about to evaluate, at the one point they are in
	// hand. It is a function rather than anything this package understands:
	// what it enforces is the reviewed-set lock (ADR-0019), which is the
	// caller's concern, and a check that re-read the file to answer would be
	// answering about a different read than the one that decides. nil is a run
	// that checks nothing — experimental graph test leaves it so, for the same
	// reason it records nothing.
	LawCheck func(decisionID string, document []byte) *evaluation.Failure

	// ReportBudget bounds the bytes a whole-suite run may accumulate, or 0 for
	// no bound. A graph matrix multiplies where a pack matrix does not -- up to
	// 10,000 rows, each re-evaluating up to 64 nodes and retaining each node's
	// canonical disposition -- so a report can reach gigabytes long before any
	// check on the marshaled response could see it. ADR-0025 names that
	// "build gigabytes, then check the MCP size" shape as a defect, so the
	// budget is enforced AS the suite accumulates rather than after it exists.
	//
	// The CLI leaves this 0: it streams to a terminal an operator can interrupt.
	// A long-lived stdio server handing one frame to a model client cannot be
	// interrupted, so that surface sets it.
	ReportBudget int

	// injectionBudget overrides the per-node injected-bytes budget, whose
	// default is the carrier's hard byte limit. It exists so a test can trip
	// the budget without megabytes of fixture; no surface sets it.
	injectionBudget int64
}

// auditWriteFailure is the refusal a record that could not be written
// produces. It refuses the run rather than dropping the record: a project that
// asked to be told what its packs decided is not served by a disposition it
// was never told about, and the evaluation itself is untouched either way —
// every node had already been evaluated when this is reached.
func auditWriteFailure() *evaluation.Failure {
	return &evaluation.Failure{
		Code:     audit.FailureCode,
		Message:  audit.FailureMessage,
		ExitCode: result.ExitIO,
	}
}

// budget is the per-node injected-bytes budget in force.
func (o Options) budget() int64 {
	if o.injectionBudget > 0 {
		return o.injectionBudget
	}
	return carrier.HardMaxBytes
}

// nodeInputs is one node's caller-supplied documents, decoded. The two
// supplied markers keep "an empty document" apart from "no document", exactly
// as the engine's EvidenceSupplied option does and for the same §8.2 reason.
type nodeInputs struct {
	facts            any
	factsSupplied    bool
	evidence         map[string]any
	evidenceSupplied bool
}

// Evaluate runs one graph: every node, in the deterministic topological
// order, each as one ordinary engine evaluation of its resolved pack against
// its own facts and evidence documents plus whatever the in-edges fed.
//
// The graph is held to its Structural checks first, completely, before any
// node runs — and deliberately not to the pack-reading checks of Semantic:
// when a node runs, the engine reads the pack itself, so a malformed or
// non-conformant pack is refused with the §8.4 class and phase it deserves
// instead of an unclassed graph refusal, and an evidence feed naming an
// undeclared requirement is the engine's own malformed-input error in §8.2's
// order, with the node named. An invalid graph is refused before anything
// evaluates, a refused node refuses the whole run with that node named and
// its class intact, and no disposition of any kind accompanies either:
// errors are not dispositions, at the composite level as much as at the node
// level.
//
// inputsSupplied says whether the caller supplied an inputs document at all,
// so empty bytes can be refused as the malformed input they are rather than
// read as "no inputs".
func Evaluate(loaded *project.Project, engine *evaluation.Engine, doc Document, graphPath string, inputs []byte, inputsSupplied bool, options Options) (result.GraphEvaluation, *evaluation.Failure) {
	if diagnostics := doc.Structural(loaded); len(diagnostics) > 0 {
		first := diagnostics[0]
		location := first.InstancePath
		if location == "" {
			location = "<root>"
		}
		return result.GraphEvaluation{}, &evaluation.Failure{
			Code:     "JPS-GRAPH-INVALID",
			Message:  fmt.Sprintf("The graph is not evaluated because its document and reference checks found %d problem(s); run experimental graph validate for the complete report. First diagnostic: %s at %s: %s", len(diagnostics), first.Code, display.Sanitize(location), display.Sanitize(first.Message)),
			ExitCode: result.ExitInvalid,
		}
	}
	perNode, failure := parseInputs(inputs, inputsSupplied, doc)
	if failure != nil {
		return result.GraphEvaluation{}, failure
	}
	// Every collision between a fact edge and the caller's own inputs is
	// decided here, before anything evaluates. The check needs nothing but the
	// declared pointers and the supplied documents, so making it wait for an
	// upstream disposition would make one invocation's acceptance depend on a
	// mid-run evaluation result — and, worse, would let a caller's value stand
	// in, unreported, for an outcome an unresolved upstream never produced, at
	// the exact pointer the edge declares it owns.
	for _, edge := range doc.Edges {
		if edge.Fact == "" {
			continue
		}
		in := perNode[edge.To]
		if !in.factsSupplied {
			continue
		}
		root, ok := in.facts.(map[string]any)
		if !ok {
			return result.GraphEvaluation{}, &evaluation.Failure{
				Code:     "JPS-GRAPH-FACT-CONFLICT",
				Message:  fmt.Sprintf("Node %q: the fact pointer %q cannot be written because the node's facts document is not a JSON object.", display.Sanitize(edge.To), display.Sanitize(edge.Fact)),
				ExitCode: result.ExitInvocation,
			}
		}
		if collision := callerCollision(root, edge.Fact); collision != "" {
			return result.GraphEvaluation{}, &evaluation.Failure{
				Code:     "JPS-GRAPH-FACT-CONFLICT",
				Message:  fmt.Sprintf("Node %q: %s", display.Sanitize(edge.To), collision),
				ExitCode: result.ExitInvocation,
			}
		}
	}

	order, _ := doc.Order()
	dispositions := map[string]result.Disposition{}
	nodes := make([]result.GraphNodeEvaluation, 0, len(order))
	// The run's audit records, held until it has a composite. Empty for a run
	// that records nothing at all.
	records := []audit.Record{}
	for _, nodeID := range order {
		packID := doc.Nodes[nodeID].Pack
		entry, _ := loaded.Entry(packID)
		packBytes, oversized, readFailure := readNodePack(loaded, nodeID, packID, entry)
		if readFailure != nil {
			return result.GraphEvaluation{}, readFailure
		}
		// The caller's check on the bytes just read, before the node they
		// belong to evaluates. Bytes the read could not present in full carry
		// the oversized marker instead, and the engine refuses them in the
		// preflight's own order — there is nothing to check about a document
		// that was never presented.
		if options.LawCheck != nil && packBytes != nil {
			if failure := options.LawCheck(packID, packBytes); failure != nil {
				return result.GraphEvaluation{}, failure
			}
		}

		in := perNode[nodeID]
		facts := in.facts
		if !in.factsSupplied {
			facts = map[string]any{}
		}
		evidence := in.evidence
		factFeeds := []result.GraphFactFeed{}
		evidenceFeeds := []result.GraphEvidenceFeed{}
		// A collision between an edge's feed and the caller's own documents is
		// an invocation refusal, and it precedes the engine's §8.2 preflight
		// exactly as every other invocation guard on this surface does — dual
		// stdin, a missing document, a malformed inputs object: a caller who
		// supplied colliding inputs has not yet posed a well-formed evaluation
		// for §8.4 to class.
		//
		// The injected-bytes budget is different: it is a resource condition,
		// and a resource condition never outranks the pack's own admission.
		// When one node's injected outcome ids pass the budget, injection
		// stops — that is what bounds the allocation — and the facts input is
		// handed to the engine as oversized instead of refused here, so §8.2
		// reaches it at the facts document's own place in the preflight and a
		// non-conformant pack still gets its own class first, exactly as the
		// CLI's bounded reads hand the same condition over. The pointer-depth
		// cap bounds injected depth the same way this bounds injected width.
		injectedBytes := int64(0)
		factsOversized := false
		for _, edge := range doc.Edges {
			if edge.To != nodeID {
				continue
			}
			upstream := dispositions[edge.From]
			if edge.Fact != "" {
				feed := result.GraphFactFeed{From: edge.From, Pointer: edge.Fact}
				inject := upstream.Kind == "outcome" && !factsOversized
				if inject {
					injectedBytes += int64(len(upstream.OutcomeID))
					if overInjectionBudget(injectedBytes, options.budget()) {
						factsOversized = true
						inject = false
					}
				}
				if inject {
					root, ok := facts.(map[string]any)
					if !ok {
						return result.GraphEvaluation{}, &evaluation.Failure{
							Code:     "JPS-GRAPH-FACT-CONFLICT",
							Message:  fmt.Sprintf("Node %q: the fact pointer %q cannot be written because the node's facts document is not a JSON object.", display.Sanitize(nodeID), display.Sanitize(edge.Fact)),
							ExitCode: result.ExitInvocation,
						}
					}
					if err := injectFact(root, edge.Fact, upstream.OutcomeID); err != nil {
						return result.GraphEvaluation{}, &evaluation.Failure{
							Code:     "JPS-GRAPH-FACT-CONFLICT",
							Message:  fmt.Sprintf("Node %q: %s", display.Sanitize(nodeID), err.Error()),
							ExitCode: result.ExitInvocation,
						}
					}
					facts = root
					feed.Injected = true
					feed.Value = upstream.OutcomeID
				}
				factFeeds = append(factFeeds, feed)
			}
			if edge.Evidence != nil {
				state := edge.Evidence.State(upstream.Kind)
				if evidence == nil {
					evidence = map[string]any{}
				}
				if _, taken := evidence[edge.Evidence.ID]; taken {
					return result.GraphEvaluation{}, &evaluation.Failure{
						Code:     "JPS-GRAPH-EVIDENCE-CONFLICT",
						Message:  fmt.Sprintf("Node %q: the caller's evidence document already sets %q, which the edge from %q also feeds. One requirement has one source; remove the caller's entry or the edge.", display.Sanitize(nodeID), display.Sanitize(edge.Evidence.ID), display.Sanitize(edge.From)),
						ExitCode: result.ExitInvocation,
					}
				}
				evidence[edge.Evidence.ID] = state
				evidenceFeeds = append(evidenceFeeds, result.GraphEvidenceFeed{From: edge.From, Requirement: edge.Evidence.ID, State: state})
			}
		}

		var factsBytes []byte
		var err error
		if !factsOversized {
			factsBytes, err = marshalDocument(facts)
		}
		if err != nil {
			return result.GraphEvaluation{}, &evaluation.Failure{
				Code:     "JPS-GRAPH-INTERNAL",
				Message:  fmt.Sprintf("Node %q: the assembled facts document could not be encoded.", display.Sanitize(nodeID)),
				ExitCode: result.ExitInternal,
			}
		}
		// An assembled document past the carrier limit is handed to the engine
		// as oversized rather than carried: the bytes exist — the inputs
		// document's own limit bounds them — but nothing downstream should
		// hold a buffer the preflight is going to refuse.
		if int64(len(factsBytes)) > carrier.HardMaxBytes {
			factsBytes = nil
			factsOversized = true
		}
		evidenceSupplied := in.evidenceSupplied || len(evidenceFeeds) > 0
		var evidenceBytes []byte
		if evidenceSupplied {
			if evidence == nil {
				evidence = map[string]any{}
			}
			evidenceBytes, err = marshalDocument(evidence)
			if err != nil {
				return result.GraphEvaluation{}, &evaluation.Failure{
					Code:     "JPS-GRAPH-INTERNAL",
					Message:  fmt.Sprintf("Node %q: the assembled evidence document could not be encoded.", display.Sanitize(nodeID)),
					ExitCode: result.ExitInternal,
				}
			}
		}

		if factsOversized {
			oversized = append(oversized, "facts")
		}
		evaluated, engineFailure := engine.EvaluateWith(packBytes, factsBytes, evidenceBytes, evaluation.Options{
			Command:             options.Command,
			SupportedExtensions: options.SupportedExtensions,
			EvidenceSupplied:    evidenceSupplied,
			OversizedInputs:     oversized,
		})
		if engineFailure != nil {
			return result.GraphEvaluation{}, &evaluation.Failure{
				Class:    engineFailure.Class,
				Phase:    engineFailure.Phase,
				Code:     engineFailure.Code,
				Message:  fmt.Sprintf("Node %q (pack %q): %s", display.Sanitize(nodeID), display.Sanitize(packID), engineFailure.Message),
				ExitCode: engineFailure.ExitCode,
			}
		}
		// The node's record is composed now, while what it describes is in
		// hand, and held: nothing is appended until the whole run has a
		// composite, so a node refused later leaves no trace of the nodes that
		// ran before it. The recorded facts are the assembled document, not the
		// caller's — it is what this node was evaluated against, and on this
		// surface those are two different documents.
		if options.Audit != nil {
			record, err := audit.EvaluationRecord(evaluated, audit.Inputs{
				Facts:            factsBytes,
				Evidence:         evidenceBytes,
				EvidenceSupplied: evidenceSupplied,
			}, packBytes, &audit.Graph{
				ID:            doc.ID,
				Version:       doc.Version,
				FormatVersion: doc.FormatVersion,
				Digest:        doc.Digest,
				Node:          nodeID,
			})
			if err != nil {
				return result.GraphEvaluation{}, auditWriteFailure()
			}
			records = append(records, record)
		}
		dispositions[nodeID] = evaluated.Disposition
		nodes = append(nodes, result.GraphNodeEvaluation{
			Node:          nodeID,
			Pack:          packID,
			PackID:        evaluated.PackID,
			PackVersion:   evaluated.PackVersion,
			SpecVersion:   evaluated.SpecVersion,
			FactFeeds:     factFeeds,
			EvidenceFeeds: evidenceFeeds,
			Disposition:   evaluated.Disposition,
			HandoffTarget: evaluated.HandoffTarget,
			Trace:         evaluated.Trace,
		})
	}

	output := result.GraphEvaluation{
		OutputVersion:             result.OutputVersion,
		Tool:                      result.CurrentTool(),
		Command:                   options.Command,
		Status:                    "evaluated",
		Experimental:              true,
		ConformanceClaimReference: result.EvaluationClaimReference,
		Label:                     result.GraphCompositeLabel,
		Kind:                      result.ProjectKind,
		EvaluatorSpecVersion:      result.EvaluatorSpecVersion,
		FormatVersion:             FormatVersion,
		ConfigPath:                loaded.ConfigPath,
		GraphPath:                 graphPath,
		GraphID:                   doc.ID,
		GraphVersion:              doc.Version,
		ResultNode:                doc.Result,
		Handoffs:                  []result.GraphHandoff{},
		Nodes:                     nodes,
	}
	for _, node := range nodes {
		if node.Node == doc.Result {
			output.Disposition = node.Disposition
			output.HandoffTarget = node.HandoffTarget
		}
		if node.Disposition.Handoff.State == "requested" {
			output.Handoffs = append(output.Handoffs, result.GraphHandoff{
				Node:        node.Node,
				TriggeredBy: node.Disposition.Handoff.TriggeredBy,
				Target:      node.HandoffTarget,
			})
		}
	}
	if set, err := artifacts.Load(result.EvaluatorSpecVersion); err == nil {
		output.Artifact = &result.Artifact{
			SpecVersion:  result.EvaluatorSpecVersion,
			BundleDigest: set.Lock().BundleDigest.Value,
			Provenance:   set.Lock().Source.Kind,
		}
	}
	// The whole run is appended here, in one write, or not at all: the node
	// records held above plus the composite, which is the one thing none of them
	// holds — the headline this run produced, and which node the graph declared
	// it comes from.
	if options.Audit != nil {
		composite, err := audit.CompositeRecord(output, doc.Digest)
		if err != nil {
			return result.GraphEvaluation{}, auditWriteFailure()
		}
		if err := options.Audit.AppendAll(append(records, composite)); err != nil {
			return result.GraphEvaluation{}, auditWriteFailure()
		}
	}
	return output, nil
}

// readNodePack reads one node's pack through the project's directory handle.
// An oversized pack is reported to the engine rather than refused here, so the
// byte limit keeps its place in the §8.2 preflight and §8.4 assigns its class,
// exactly as the CLI's --pack-id resolution does. Every other failure is an
// operational refusal naming the node.
func readNodePack(loaded *project.Project, nodeID, packID string, entry project.Pack) ([]byte, []string, *evaluation.Failure) {
	data, err := loaded.ReadPack(entry)
	if err == nil {
		return data, nil, nil
	}
	if errors.Is(err, fssecure.ErrTooLarge) {
		return nil, []string{"pack"}, nil
	}
	return nil, nil, &evaluation.Failure{
		Code:     "JPS-GRAPH-NODE-PACK-READ",
		Message:  fmt.Sprintf("Node %q (pack %q): %s", display.Sanitize(nodeID), display.Sanitize(packID), project.ReadFailureMessage(entry.Path, err)),
		ExitCode: result.ExitIO,
	}
}

// parseInputs decodes the caller's inputs document: a JSON object keyed by
// declared node id, each value an object with optional facts and evidence
// members. The facts member may be any JSON document, exactly as a standalone
// evaluation's facts input may; the evidence member must be a JSON object,
// because feeds merge into it. Unknown node ids and unknown members are
// refused rather than ignored — a misspelled key is an intention, not noise.
//
// Member names are walked in sorted order so a document with two problems
// reports the same one on every run.
func parseInputs(data []byte, supplied bool, doc Document) (map[string]nodeInputs, *evaluation.Failure) {
	if !supplied {
		return map[string]nodeInputs{}, nil
	}
	if int64(len(data)) > MaxInputsBytes {
		return nil, &evaluation.Failure{
			Code:     "JPS-RESOURCE-GRAPH-INPUTS-BYTE-LIMIT",
			Message:  fmt.Sprintf("The inputs document exceeds the %d-byte limit.", MaxInputsBytes),
			ExitCode: result.ExitIO,
		}
	}
	if len(data) == 0 {
		return nil, &evaluation.Failure{
			Code:     "JPS-GRAPH-INPUTS-JSON",
			Message:  "The inputs document is empty; empty bytes are not a carrier-conforming JSON text. Supply a JSON object keyed by node id, or omit --inputs entirely.",
			ExitCode: result.ExitInvocation,
		}
	}
	decoded, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		// A reached carrier resource limit keeps its own JPS-RESOURCE-* code
		// and the IO exit class, exactly as the engine's decodeFailure keeps
		// them: a document refused for its size is not a document refused for
		// its syntax, and the two call for different fixes.
		if carrierFailure.Resource {
			return nil, &evaluation.Failure{
				Code:     carrierFailure.Diagnostic.Code,
				Message:  fmt.Sprintf("The inputs document exceeds a resource limit: %s", display.Sanitize(carrierFailure.Diagnostic.Message)),
				ExitCode: result.ExitIO,
			}
		}
		return nil, &evaluation.Failure{
			Code:     "JPS-GRAPH-INPUTS-JSON",
			Message:  fmt.Sprintf("The inputs document is not acceptable JSON: %s", display.Sanitize(carrierFailure.Diagnostic.Message)),
			ExitCode: result.ExitInvocation,
		}
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return nil, &evaluation.Failure{
			Code:     "JPS-GRAPH-INPUTS-SHAPE",
			Message:  "The inputs document must be a JSON object keyed by node id, each value an object with optional facts and evidence members.",
			ExitCode: result.ExitInvocation,
		}
	}
	declared := make([]string, 0, len(doc.Nodes))
	for id := range doc.Nodes {
		declared = append(declared, id)
	}
	sort.Strings(declared)
	names := make([]string, 0, len(root))
	for name := range root {
		names = append(names, name)
	}
	sort.Strings(names)
	perNode := map[string]nodeInputs{}
	for _, name := range names {
		if !doc.declares(name) {
			return nil, &evaluation.Failure{
				Code:     "JPS-GRAPH-INPUTS-NODE",
				Message:  fmt.Sprintf("The inputs document names node %q, which the graph does not declare. Declared nodes: %s.", display.Sanitize(name), strings.Join(declared, ", ")),
				ExitCode: result.ExitInvocation,
			}
		}
		entry, ok := root[name].(map[string]any)
		if !ok {
			return nil, &evaluation.Failure{
				Code:     "JPS-GRAPH-INPUTS-SHAPE",
				Message:  fmt.Sprintf("The inputs for node %q must be a JSON object with optional facts and evidence members.", display.Sanitize(name)),
				ExitCode: result.ExitInvocation,
			}
		}
		members := make([]string, 0, len(entry))
		for member := range entry {
			members = append(members, member)
		}
		sort.Strings(members)
		in := nodeInputs{}
		for _, member := range members {
			switch member {
			case "facts":
				in.facts = entry["facts"]
				in.factsSupplied = true
			case "evidence":
				evidence, ok := entry["evidence"].(map[string]any)
				if !ok {
					return nil, &evaluation.Failure{
						Code:     "JPS-GRAPH-INPUTS-EVIDENCE",
						Message:  fmt.Sprintf("The evidence for node %q must be a JSON object mapping declared evidence-requirement ids to \"present\", \"absent\", or \"unknown\".", display.Sanitize(name)),
						ExitCode: result.ExitInvocation,
					}
				}
				in.evidence = evidence
				in.evidenceSupplied = true
			default:
				return nil, &evaluation.Failure{
					Code:     "JPS-GRAPH-INPUTS-MEMBER",
					Message:  fmt.Sprintf("The inputs for node %q carry member %q; only facts and evidence are read, and an unread member is an intention this refusal preserves.", display.Sanitize(name), display.Sanitize(member)),
					ExitCode: result.ExitInvocation,
				}
			}
		}
		perNode[name] = in
	}
	return perNode, nil
}

// callerCollision reports why one fact pointer cannot be written into a
// caller-supplied facts document, or "" when it can: a value already at the
// pointer, or an intermediate that exists and is not a JSON object. It walks
// without creating anything, because it answers the invocation-time question
// — do the caller's documents collide with the graph's declared feeds? — for
// every fact edge, before any node runs and regardless of what any upstream
// later produces.
func callerCollision(root map[string]any, pointer string) string {
	current := root
	tokens := pointerTokens(pointer)
	for _, token := range tokens[:len(tokens)-1] {
		child, exists := current[token]
		if !exists {
			return ""
		}
		next, ok := child.(map[string]any)
		if !ok {
			return fmt.Sprintf("the fact pointer %q resolves through %q, which is not a JSON object in the node's facts document", display.Sanitize(pointer), display.Sanitize(token))
		}
		current = next
	}
	if _, exists := current[tokens[len(tokens)-1]]; exists {
		return fmt.Sprintf("the facts document already carries a value at %q; an injected outcome never overwrites a caller's fact", display.Sanitize(pointer))
	}
	return ""
}

// marshalDocument encodes one assembled document without HTML escaping, so
// the assembled bytes track the input bytes instead of tripling on every
// angle bracket, and drops the encoder's trailing newline.
func marshalDocument(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

// injectFact writes one outcome id at one RFC 6901 pointer in a facts
// document, creating intermediate objects as it goes. Its refusals mirror
// callerCollision's, which has already cleared the caller's documents by the
// time anything is injected; what this still guards is the assembly itself.
func injectFact(root map[string]any, pointer, value string) error {
	tokens := strings.Split(pointer[1:], "/")
	current := root
	for _, token := range tokens[:len(tokens)-1] {
		key := decodePointerToken(token)
		child, exists := current[key]
		if !exists {
			next := map[string]any{}
			current[key] = next
			current = next
			continue
		}
		next, ok := child.(map[string]any)
		if !ok {
			return fmt.Errorf("the fact pointer %q resolves through %q, which is not a JSON object in the node's facts document", display.Sanitize(pointer), display.Sanitize(key))
		}
		current = next
	}
	last := decodePointerToken(tokens[len(tokens)-1])
	if _, exists := current[last]; exists {
		return fmt.Errorf("the facts document already carries a value at %q; an injected outcome never overwrites a caller's fact", display.Sanitize(pointer))
	}
	current[last] = value
	return nil
}

// decodePointerToken unescapes one RFC 6901 reference token: ~1 becomes /,
// then ~0 becomes ~, in that order, exactly as the RFC requires.
func decodePointerToken(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
}

// overInjectionBudget reports whether one node's running total of injected
// outcome-id bytes has passed the budget — by default the carrier's hard byte
// limit, the point past which the assembled facts document could never be
// admitted anyway, so building it would spend memory on a document the
// engine's preflight is going to refuse in its own order.
func overInjectionBudget(total, budget int64) bool {
	return total > budget
}
