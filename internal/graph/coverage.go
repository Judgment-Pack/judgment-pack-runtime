package graph

import (
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// coverage derives, from the graph's declarations and each node's own pack,
// the probes a graph matrix can witness through its rows' expectations, and
// reports which of them some expectation does witness (ADR-0016, on
// ADR-0014's derivation). Three rules shape it:
//
//   - One probe family per node, none for the headline. The composite
//     headline is a validated echo of the declared result node's disposition,
//     so a row's expectedDisposition and its expectedNodes entry for that node
//     are two witnesses of one node's behavior — deriving a headline family
//     beside it would count the same behavior twice.
//
//   - Two probes per edge, whatever devices the edge declares. The evidence
//     state an edge contributes and the fact-injection guard are both pure
//     functions of the upstream disposition kind, so a fact branch and an
//     evidence branch would be two probes with byte-identical witness sets.
//
//   - Witnesses are decode-gated expectations, never feeds and never results.
//     A row asserts dispositions; the factFeeds and evidenceFeeds members
//     live in an evaluation payload, not in the row grammar, and deriving
//     coverage from what ran would be witnessing the run instead of the rows.
//     What makes the kind-only read honest is the first two rules: the kind
//     determines both devices exactly.
//
// A missing probe is a fact — no row expects this — and never a failed row;
// coverage moves no status and no exit code. An edge-fed pointer or
// requirement restricts what a row can construct, so a missing probe here may
// be unconstructible more often than at pack level, which is one more reason
// it informs and never gates.
func coverage(loaded *project.Project, engine *evaluation.Engine, doc Document, rows Rows, options Options) []result.MatrixProbe {
	witnesses := map[string][]project.ProbeWitness{}
	for _, row := range rows.Cases {
		// A row expecting an error class witnesses nothing: a refused run
		// produces no disposition (ADR-0014's rule, unchanged).
		if row.ExpectedErrorClass != "" {
			continue
		}
		if row.ExpectedDisposition != nil {
			label := fmt.Sprintf("Row %q (headline)", display.Sanitize(row.ID))
			if witness, ok := project.DecodeWitness(label, row.ExpectedDisposition); ok {
				witnesses[doc.Result] = append(witnesses[doc.Result], witness)
			}
		}
		for _, name := range sortedKeys(row.ExpectedNodes) {
			label := fmt.Sprintf("Row %q", display.Sanitize(row.ID))
			if witness, ok := project.DecodeWitness(label, row.ExpectedNodes[name]); ok {
				witnesses[name] = append(witnesses[name], witness)
			}
		}
	}

	// Per node, in evaluation order: read and admit the pack once, derive its
	// probes under the reach its in-edges leave it, and remember what it can
	// do for the edge probes below. A node whose pack cannot be read, is not
	// admitted, or does not decode derives nothing — every such condition is
	// already a refusal or a mismatch on the surfaces that evaluate, and a
	// skipped-probe status would be a third coverage state ADR-0014 refused.
	type nodeAbility struct {
		outcomes []string
		reasons  []string
	}
	abilities := map[string]nodeAbility{}
	var probes []result.MatrixProbe
	order, _ := doc.Order()
	for _, name := range order {
		node := doc.Nodes[name]
		entry, ok := loaded.Entry(node.Pack)
		if !ok {
			continue
		}
		data, err := loaded.ReadPack(entry)
		if err != nil {
			continue
		}
		if !engine.Admits(data, options.SupportedExtensions) {
			continue
		}
		root := project.PackRoot(data)
		if root == nil {
			continue
		}
		reach := project.Reach{EvidenceStates: edgeFedStates(doc, name)}
		abilities[name] = nodeAbility{
			outcomes: project.ProducibleOutcomes(root),
			reasons:  project.ReachableReasons(root, reach),
		}
		for _, probe := range project.PackProbes(root, witnesses[name], reach) {
			probe.Probe = "node:" + name + ":" + probe.Probe
			probe.Detail = fmt.Sprintf("Node %q (pack %q): %s", display.Sanitize(name), display.Sanitize(node.Pack), probe.Detail)
			probes = append(probes, probe)
		}
	}

	// Per edge, in document order: a resolved branch exists while the upstream
	// can produce an outcome, an unresolved branch while it can reach any
	// non-outcome disposition — both read off the upstream's own derived
	// abilities, so an edge probe never claims a behavior the upstream's
	// declarations cannot reach. An edge whose upstream derived nothing
	// derives nothing.
	for index, edge := range doc.Edges {
		ability, known := abilities[edge.From]
		if !known {
			continue
		}
		if len(ability.outcomes) > 0 {
			addEdgeProbe(&probes, witnesses[edge.From], fmt.Sprintf("edge:%d:resolved", index), edgeResolvedMissing(edge), witnessesResolved)
		}
		if len(ability.reasons) > 0 {
			addEdgeProbe(&probes, witnesses[edge.From], fmt.Sprintf("edge:%d:unresolved", index), edgeUnresolvedMissing(edge), witnessesUnresolved)
		}
	}
	return probes
}

// witnessesResolved and witnessesUnresolved are the two edge-branch witness
// predicates, enumerated rather than negated so a disposition kind added
// later is not silently swept into a branch. A resolved upstream is exactly
// an outcome; an unresolved one is exactly the two kinds that produce none.
func witnessesResolved(witness project.ProbeWitness) bool {
	return witness.Kind == "outcome"
}

func witnessesUnresolved(witness project.ProbeWitness) bool {
	return witness.Kind == "unresolved" || witness.Kind == "not-applicable"
}

func addEdgeProbe(probes *[]result.MatrixProbe, witnesses []project.ProbeWitness, name, missingDetail string, witnessed func(project.ProbeWitness) bool) {
	for _, witness := range witnesses {
		if witnessed(witness) {
			*probes = append(*probes, result.MatrixProbe{
				Probe:  name,
				Status: result.MatrixProbeCovered,
				Detail: witness.Label + " expects it.",
			})
			return
		}
	}
	*probes = append(*probes, result.MatrixProbe{Probe: name, Status: result.MatrixProbeMissing, Detail: missingDetail})
}

// edgeResolvedMissing and edgeUnresolvedMissing compose one missing sentence
// per branch, naming exactly the devices the edge declares — what the branch
// would do to each — and, for the unresolved branch, the arbiter escape
// ADR-0014 requires of every such sentence: an unconstructible probe is a
// question for the policy text, never a row to force.
func edgeResolvedMissing(edge Edge) string {
	sentence := fmt.Sprintf("Node %q produces outcomes, and no row expects an outcome disposition for it, so nothing witnesses this edge %s.",
		display.Sanitize(edge.From), edgeDevices(edge, "resolved"))
	return sentence
}

func edgeUnresolvedMissing(edge Edge) string {
	return fmt.Sprintf("Node %q can resolve nothing, and no row expects an unresolved or not-applicable disposition for it, so nothing witnesses this edge %s. Either construct inputs that leave the upstream unresolved, or confirm against the policy text that it always resolves.",
		display.Sanitize(edge.From), edgeDevices(edge, "unresolved"))
}

// edgeDevices states what one branch does to the devices this edge declares.
// The schema requires at least one device, so the empty composition is
// unreachable.
func edgeDevices(edge Edge, branch string) string {
	var clauses []string
	if edge.Fact != "" {
		if branch == "resolved" {
			clauses = append(clauses, fmt.Sprintf("writing the outcome id at %q", display.Sanitize(edge.Fact)))
		} else {
			clauses = append(clauses, fmt.Sprintf("leaving %q absent", display.Sanitize(edge.Fact)))
		}
	}
	if edge.Evidence != nil {
		if branch == "resolved" {
			clauses = append(clauses, fmt.Sprintf("contributing %q to evidence requirement %q", "present", display.Sanitize(edge.Evidence.ID)))
		} else {
			clauses = append(clauses, fmt.Sprintf("contributing %q to evidence requirement %q", edge.Evidence.State("unresolved"), display.Sanitize(edge.Evidence.ID)))
		}
	}
	if len(clauses) == 2 {
		return clauses[0] + " and " + clauses[1]
	}
	return clauses[0]
}

// edgeFedStates narrows one node's evidence reach to what its in-edges leave
// a row able to supply: an edge-fed requirement's state set is exactly
// {"present", the edge's onUnresolved value} — a caller entry for an edge-fed
// requirement is refused rather than merged, so nothing can widen it — and a
// requirement no edge feeds keeps all three states. A node with no evidence
// in-edges is unnarrowed.
func edgeFedStates(doc Document, node string) func(string) []string {
	fed := map[string][]string{}
	for _, edge := range doc.Edges {
		if edge.To != node || edge.Evidence == nil {
			continue
		}
		fed[edge.Evidence.ID] = []string{"present", edge.Evidence.State("unresolved")}
	}
	if len(fed) == 0 {
		return nil
	}
	return func(requirementID string) []string {
		states, known := fed[requirementID]
		if !known {
			return []string{"present", "absent", "unknown"}
		}
		return states
	}
}
