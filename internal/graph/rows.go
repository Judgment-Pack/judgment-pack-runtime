package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	// GraphMatrixVersion is the newest graphMatrixVersion this runtime
	// accepts. It moved to "2" for the handoff-target assertions (ADR-0032),
	// on matrixVersion's own reasoning: a matrix is a closed input, so adding
	// a member moves the version whatever the addition is, and the version
	// gate is what turns "this member is not known" into "this matrix needs
	// graphMatrixVersion 2".
	GraphMatrixVersion = "2"
	// GraphMatrixVersionDefault is what rows declaring no graphMatrixVersion
	// are read as, on the matrixVersion precedent: silence is the version
	// that existed before any member was version-gated, never the newest.
	GraphMatrixVersionDefault = "1"
	// MaxRowsBytes bounds one graph matrix document, which carries an inputs
	// document per row, on the instance-matrix precedent.
	MaxRowsBytes = int64(16 << 20)
	// MaxRowsCases bounds the rows of one graph matrix.
	MaxRowsCases = 10_000
)

// RowCase is one graph matrix row: one inputs document for the whole graph,
// and exactly one expectation — the composite headline disposition, or the
// §8.4 class (and optionally phase) of a refused run. ExpectedNodes names the
// per-node dispositions the row also pins; a node it does not name is
// unchecked, which is the row author's stated choice rather than a default
// nobody chose.
//
// A row can assert only the two stable vocabularies: canonical §8.3
// dispositions, and §8.4 classes with their phases. A refusal that carries no
// class — a graph-layer or invocation refusal, such as a feed colliding with
// the row's own inputs — is deliberately unassertable and mismatches every
// expectation: this runtime's JPS-* codes are provisional detail (§8.4 fixes
// the class, not the detail), and a row that pinned one would break on
// wording this runtime is free to change.
// ExpectedHandoffTarget asserts the composite's reported handoff target —
// the result node's own, which is the configured target exactly when the
// disposition requested a handoff and null otherwise — and
// ExpectedNodeHandoffTargets asserts named nodes' (ADR-0032,
// graphMatrixVersion "2"): each value is null or a {kind, name} object,
// exactly a pack row's expectedHandoffTarget. A node whose target a row
// asserts must also appear in ExpectedNodes: a target is asserted beside the
// node's disposition, as a pack row asserts it beside its own.
type RowCase struct {
	ID                         string                     `json:"id"`
	Inputs                     json.RawMessage            `json:"inputs"`
	ExpectedDisposition        json.RawMessage            `json:"expectedDisposition,omitempty"`
	ExpectedNodes              map[string]json.RawMessage `json:"expectedNodes,omitempty"`
	ExpectedHandoffTarget      json.RawMessage            `json:"expectedHandoffTarget,omitempty"`
	ExpectedNodeHandoffTargets map[string]json.RawMessage `json:"expectedNodeHandoffTargets,omitempty"`
	ExpectedErrorClass         string                     `json:"expectedErrorClass,omitempty"`
	ExpectedErrorPhase         string                     `json:"expectedErrorPhase,omitempty"`
}

// Rows is one graph's matrix: the rows a project wrote about its own graph.
type Rows struct {
	GraphMatrixVersion string    `json:"graphMatrixVersion,omitempty"`
	Cases              []RowCase `json:"cases"`
}

// SupportedGraphMatrixVersions names every graphMatrixVersion this runtime
// accepts, newest last, so a refusal can say what it would have taken rather
// than only what was wrong. The list is cumulative and literal — never
// derived from the newest constant, so a future bump cannot silently drop an
// intermediate version and with it the gate on the members it introduced
// (round-1 review) — and a test holds every version the introduction map
// names to a place in it.
func SupportedGraphMatrixVersions() []string {
	return []string{"1", "2"}
}

// graphRowsVersionMembers names the members each graphMatrixVersion
// introduced, so a document declaring an older version is refused with the
// version it would take rather than with a generic unknown-member message.
var graphRowsVersionMembers = map[string]string{
	"expectedHandoffTarget":      "2",
	"expectedNodeHandoffTargets": "2",
}

// declaredGraphMatrixVersion settles which graphMatrixVersion the rows are
// read under, before anything version-specific looks at them. An absent
// member is the default; a member of the wrong type is refused as such rather
// than coerced; an unknown value names every version this runtime accepts.
func declaredGraphMatrixVersion(root map[string]any, name string) (string, *evaluation.Failure) {
	refuse := func(message string) (string, *evaluation.Failure) {
		return "", &evaluation.Failure{Code: "JPS-GRAPH-ROWS-VERSION", Message: message, ExitCode: result.ExitUnsupported}
	}
	value, present := root["graphMatrixVersion"]
	if !present {
		return GraphMatrixVersionDefault, nil
	}
	declared, ok := value.(string)
	if !ok {
		// A version of the wrong type is the document's shape being wrong, not
		// a version this runtime lacks — classified exactly as the strict
		// decoder classified it before this gate existed (round-1 review).
		return "", &evaluation.Failure{
			Code: "JPS-GRAPH-ROWS-SHAPE",
			Message: fmt.Sprintf("The graphMatrixVersion of the graph matrix %s must be a string; this runtime accepts: %s.",
				display.Sanitize(name), strings.Join(SupportedGraphMatrixVersions(), ", ")),
			ExitCode: result.ExitInvalid,
		}
	}
	if slices.Contains(SupportedGraphMatrixVersions(), declared) {
		return declared, nil
	}
	return refuse(fmt.Sprintf("The graph matrix %s declares graphMatrixVersion %q, which this runtime does not support; it accepts: %s.",
		display.Sanitize(name), display.Sanitize(declared), strings.Join(SupportedGraphMatrixVersions(), ", ")))
}

// exactRowMembers holds the rows document and each of its cases to the exact
// member spellings the format declares, under the declared version. Elements
// that are not objects are left to the strict decoder, whose wrong-type
// message is the better diagnosis.
func exactRowMembers(root map[string]any, declared, name string) *evaluation.Failure {
	if failure := exactMemberSet(root, []string{"graphMatrixVersion", "cases"}, declared, name, "the graph matrix"); failure != nil {
		return failure
	}
	cases, ok := root["cases"].([]any)
	if !ok {
		return nil
	}
	rowMembers := []string{"id", "inputs", "expectedDisposition", "expectedNodes", "expectedHandoffTarget", "expectedNodeHandoffTargets", "expectedErrorClass", "expectedErrorPhase"}
	for index, element := range cases {
		row, ok := element.(map[string]any)
		if !ok {
			// The decoder would silently skip a null element into a zero row
			// that then fails as "declares no id" — the wrong diagnosis for a
			// document whose defect is shape, not content.
			return &evaluation.Failure{
				Code: "JPS-GRAPH-ROWS-SHAPE",
				Message: fmt.Sprintf("In %s, row %d of the graph matrix is not a JSON object.",
					display.Sanitize(name), index),
				ExitCode: result.ExitInvalid,
			}
		}
		if failure := exactMemberSet(row, rowMembers, declared, name, fmt.Sprintf("row %d of the graph matrix", index)); failure != nil {
			return failure
		}
		// A present expectedNodeHandoffTargets must be an object: the struct
		// decoder reads an explicit null as an absent map, which would
		// silently erase an assertion the author wrote (round-1 review). The
		// row-level expectedHandoffTarget is different — its null IS an
		// assertion — and expectedNodes' long-standing tolerance predates
		// this member and is not changed by it.
		if value, present := row["expectedNodeHandoffTargets"]; present {
			if _, isObject := value.(map[string]any); !isObject {
				return &evaluation.Failure{
					Code: "JPS-GRAPH-ROWS-SHAPE",
					Message: fmt.Sprintf("In %s, row %d of the graph matrix declares an expectedNodeHandoffTargets that is not a JSON object mapping node ids to assertions; the literal null is not an absent member.",
						display.Sanitize(name), index),
					ExitCode: result.ExitInvalid,
				}
			}
		}
	}
	return nil
}

// exactMemberSet reports the first unknown member of one object, naming the
// exact spelling where the stranger differs from a known member only in case
// — and the first known member the declared version has not introduced yet,
// naming the version it would take, because "expectedHandoffTarget is not a
// member" is a false sentence to print at someone whose only mistake was not
// moving graphMatrixVersion (the pack matrix's closedMembers precedent).
func exactMemberSet(object map[string]any, known []string, declared, name, subject string) *evaluation.Failure {
	for _, member := range slices.Sorted(maps.Keys(object)) {
		if slices.Contains(known, member) {
			// Ordered by position in the supported list rather than by the
			// version string, which compares as text and would read "10" as
			// older than "2". A version missing from the list ranks as -1,
			// which fails CLOSED here: an unrankable introduction refuses the
			// member rather than admitting it (round-1 review).
			since, versioned := graphRowsVersionMembers[member]
			if versioned && (slices.Index(SupportedGraphMatrixVersions(), since) < 0 ||
				slices.Index(SupportedGraphMatrixVersions(), declared) < slices.Index(SupportedGraphMatrixVersions(), since)) {
				return &evaluation.Failure{
					Code: "JPS-GRAPH-ROWS-VERSION",
					Message: fmt.Sprintf("In %s, %s declares %q, which graphMatrixVersion %s introduced; this matrix is read as graphMatrixVersion %s, so declare graphMatrixVersion %q to use it.",
						display.Sanitize(name), subject, member, since, declared, since),
					ExitCode: result.ExitUnsupported,
				}
			}
			continue
		}
		for _, candidate := range known {
			if strings.EqualFold(candidate, member) {
				return &evaluation.Failure{
					Code: "JPS-GRAPH-ROWS-SHAPE",
					Message: fmt.Sprintf("In %s, %s carries a member this runtime does not know: %q. The member is spelled %q, and a spelling that differs from it only in case is refused rather than read as it.",
						display.Sanitize(name), subject, display.Sanitize(member), candidate),
					ExitCode: result.ExitInvalid,
				}
			}
		}
		return &evaluation.Failure{
			Code: "JPS-GRAPH-ROWS-SHAPE",
			Message: fmt.Sprintf("In %s, %s carries a member this runtime does not know: %q.",
				display.Sanitize(name), subject, display.Sanitize(member)),
			ExitCode: result.ExitInvalid,
		}
	}
	return nil
}

// LoadRows reads and checks one graph matrix document. "Well-formed" here is
// the carrier and the shape, exactly as a pack matrix's loader draws the
// line: strict JSON with no duplicate member names, a closed member set, a
// declared version this runtime knows, at least one row, unique row ids, an
// inputs document per row, and exactly one expectation per row. Whether an
// expectation holds is what graph test answers.
func LoadRows(data []byte, name string) (Rows, *evaluation.Failure) {
	refuse := func(code, message string, exit int) (Rows, *evaluation.Failure) {
		return Rows{}, &evaluation.Failure{Code: code, Message: message, ExitCode: exit}
	}
	if int64(len(data)) > MaxRowsBytes {
		return refuse("JPS-RESOURCE-GRAPH-ROWS-BYTE-LIMIT",
			fmt.Sprintf("The graph matrix %s exceeds the %d-byte limit.", display.Sanitize(name), MaxRowsBytes), result.ExitIO)
	}
	if len(data) == 0 {
		return refuse("JPS-GRAPH-ROWS-JSON",
			fmt.Sprintf("The graph matrix %s is empty; empty bytes are not a carrier-conforming JSON text.", display.Sanitize(name)), result.ExitInvocation)
	}
	decoded, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return refuse("JPS-GRAPH-ROWS-JSON",
			fmt.Sprintf("The graph matrix %s is not acceptable JSON: %s", display.Sanitize(name), display.Sanitize(carrierFailure.Diagnostic.Message)), result.ExitInvocation)
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return refuse("JPS-GRAPH-ROWS-SHAPE",
			fmt.Sprintf("The graph matrix %s must be a JSON object with a cases array.", display.Sanitize(name)), result.ExitInvalid)
	}
	// The declared version is settled first, because the member check is
	// version-aware: a version-2 member in version-1 rows is refused with the
	// version it would take, not as a stranger.
	declared, versionFailure := declaredGraphMatrixVersion(root, name)
	if versionFailure != nil {
		return Rows{}, versionFailure
	}
	// Member names are held to their exact spelling before the decoder runs,
	// because encoding/json case-folds them: "Cases" or "ExpectedDisposition"
	// would bind and be silently read as the members they are not — the same
	// hole the pack matrix closed under ADR-0025 and the argument decoders
	// closed under ADR-0028. A matrix is a closed input; a spelling that
	// differs only in case is refused rather than read as the member it folds
	// to.
	if failure := exactRowMembers(root, declared, name); failure != nil {
		return Rows{}, failure
	}
	var rows Rows
	// Strict decoding closes the remaining shape questions: a member of the
	// wrong type must be an error, not a row that silently expects nothing.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rows); err != nil {
		return refuse("JPS-GRAPH-ROWS-SHAPE",
			fmt.Sprintf("The graph matrix %s has a member this runtime does not know, or a member of the wrong type: %s", display.Sanitize(name), display.Sanitize(err.Error())), result.ExitInvalid)
	}
	// The version itself was settled and refused, if refusable, before the
	// decoder ran — declaredGraphMatrixVersion is the one gate.
	if len(rows.Cases) == 0 {
		return refuse("JPS-GRAPH-ROWS-EMPTY",
			fmt.Sprintf("The graph matrix %s declares no rows; an empty matrix tests nothing.", display.Sanitize(name)), result.ExitInvalid)
	}
	if len(rows.Cases) > MaxRowsCases {
		return refuse("JPS-RESOURCE-GRAPH-ROWS-CASES",
			fmt.Sprintf("The graph matrix %s declares more than the %d supported rows.", display.Sanitize(name), MaxRowsCases), result.ExitIO)
	}
	seen := map[string]bool{}
	for index, row := range rows.Cases {
		if row.ID == "" {
			return refuse("JPS-GRAPH-ROWS-ROW",
				fmt.Sprintf("Row %d declares no id; every row is named so a mismatch can be pointed at.", index), result.ExitInvalid)
		}
		if seen[row.ID] {
			return refuse("JPS-GRAPH-ROWS-ROW",
				fmt.Sprintf("Row id %q appears more than once.", display.Sanitize(row.ID)), result.ExitInvalid)
		}
		seen[row.ID] = true
		if len(row.Inputs) == 0 {
			return refuse("JPS-GRAPH-ROWS-ROW",
				fmt.Sprintf("Row %q declares no inputs document; a graph run takes one, and the empty object is a statement a row makes explicitly.", display.Sanitize(row.ID)), result.ExitInvalid)
		}
		hasDisposition := row.ExpectedDisposition != nil
		hasClass := row.ExpectedErrorClass != ""
		if hasDisposition == hasClass {
			return refuse("JPS-GRAPH-ROWS-ROW",
				fmt.Sprintf("Row %q must declare exactly one of expectedDisposition and expectedErrorClass: a composite and an evaluation error are never both produced (§8.4).", display.Sanitize(row.ID)), result.ExitInvalid)
		}
		if row.ExpectedErrorPhase != "" && !hasClass {
			return refuse("JPS-GRAPH-ROWS-ROW",
				fmt.Sprintf("Row %q declares an expectedErrorPhase without an expectedErrorClass.", display.Sanitize(row.ID)), result.ExitInvalid)
		}
		if len(row.ExpectedNodes) > 0 && !hasDisposition {
			return refuse("JPS-GRAPH-ROWS-ROW",
				fmt.Sprintf("Row %q declares expectedNodes beside an expected error; a refused run produces no node dispositions to compare.", display.Sanitize(row.ID)), result.ExitInvalid)
		}
		// The target assertions ride only beside a disposition expectation, on
		// the pack matrix's own rule (ADR-0025 via ADR-0032): a refused run
		// reports no target beside a disposition it never produced, so a row
		// stating one under an expected error is unsatisfiable rather than
		// merely unmet, and is refused before it runs. Each value is checked
		// here through the one decoder the comparator uses.
		if row.ExpectedHandoffTarget != nil && !hasDisposition {
			return refuse("JPS-GRAPH-ROWS-ROW",
				fmt.Sprintf("Row %q declares an expectedHandoffTarget beside an expected error; a refused run reports no handoff target to compare.", display.Sanitize(row.ID)), result.ExitInvalid)
		}
		if row.ExpectedHandoffTarget != nil {
			if _, err := evaluation.DecodeHandoffTarget(row.ExpectedHandoffTarget); err != nil {
				return refuse("JPS-GRAPH-ROWS-ROW",
					fmt.Sprintf("Row %q declares an expectedHandoffTarget that is neither null nor a {kind, name} object: %s", display.Sanitize(row.ID), display.Sanitize(err.Error())), result.ExitInvalid)
			}
		}
		if len(row.ExpectedNodeHandoffTargets) > 0 && !hasDisposition {
			return refuse("JPS-GRAPH-ROWS-ROW",
				fmt.Sprintf("Row %q declares expectedNodeHandoffTargets beside an expected error; a refused run reports no node targets to compare.", display.Sanitize(row.ID)), result.ExitInvalid)
		}
		for _, node := range slices.Sorted(maps.Keys(row.ExpectedNodeHandoffTargets)) {
			if _, named := row.ExpectedNodes[node]; !named {
				return refuse("JPS-GRAPH-ROWS-ROW",
					fmt.Sprintf("Row %q asserts a handoff target for node %q it does not name in expectedNodes; a target is asserted beside the node's disposition, as a pack row asserts it beside its own.", display.Sanitize(row.ID), display.Sanitize(node)), result.ExitInvalid)
			}
			if _, err := evaluation.DecodeHandoffTarget(row.ExpectedNodeHandoffTargets[node]); err != nil {
				return refuse("JPS-GRAPH-ROWS-ROW",
					fmt.Sprintf("Row %q asserts a handoff target for node %q that is neither null nor a {kind, name} object: %s", display.Sanitize(row.ID), display.Sanitize(node), display.Sanitize(err.Error())), result.ExitInvalid)
			}
		}
	}
	return rows, nil
}

// Test runs one graph's matrix: every row, each as one Evaluate of the graph
// against the row's inputs document, judged exactly as a pack matrix row is —
// the RFC 8785 canonical §8.3 disposition compared byte for byte (the
// composite headline, and every node the row names), or the §8.4 class and
// phase the row expects. What this reports is what a project's own rows did;
// it is not the specification's corpus, and no JPS version defines a graph.
//
// Beside the rows it reports coverage (ADR-0016): the probes the graph's
// declarations derive, and which of them some row's expectation witnesses.
// The derivation reads each node's pack once per run — not per row — through
// the same jailed handle the evaluation reads it through, so a pack that
// cannot be read derives no probes rather than failing the run. Coverage sits
// after the row loop because it reads expectations, which exist whether or
// not they held; it touches neither Status nor Summary, and a run with every
// probe missing still exits by its rows alone.
func Test(loaded *project.Project, engine *evaluation.Engine, doc Document, graphPath, rowsPath string, rows Rows, options Options) (result.GraphTest, *evaluation.Failure) {
	// LoadRows refuses an empty matrix, and this precondition is enforced here
	// too so no caller can obtain a clean 0/0 report over nothing.
	if len(rows.Cases) == 0 {
		return result.GraphTest{}, &evaluation.Failure{
			Code:     "JPS-GRAPH-ROWS-EMPTY",
			Message:  "The graph matrix declares no rows; an empty matrix tests nothing.",
			ExitCode: result.ExitInvalid,
		}
	}
	if diagnostics := doc.Structural(loaded); len(diagnostics) > 0 {
		first := diagnostics[0]
		location := first.InstancePath
		if location == "" {
			location = "<root>"
		}
		return result.GraphTest{}, &evaluation.Failure{
			Code:     "JPS-GRAPH-INVALID",
			Message:  fmt.Sprintf("The graph is not tested because its document and reference checks found %d problem(s); run experimental graph validate for the complete report. First diagnostic: %s at %s: %s", len(diagnostics), first.Code, display.Sanitize(location), display.Sanitize(first.Message)),
			ExitCode: result.ExitInvalid,
		}
	}
	output := result.GraphTest{
		OutputVersion:             result.OutputVersion,
		Tool:                      result.CurrentTool(),
		Command:                   options.Command,
		Status:                    "passed",
		Experimental:              true,
		ConformanceClaimReference: result.EvaluationClaimReference,
		Label:                     result.GraphMatrixLabel,
		Kind:                      result.ProjectKind,
		FormatVersion:             FormatVersion,
		EvaluatorSpecVersion:      result.EvaluatorSpecVersion,
		ConfigPath:                loaded.ConfigPath,
		GraphPath:                 graphPath,
		RowsPath:                  rowsPath,
		GraphID:                   doc.ID,
		GraphVersion:              doc.Version,
		GraphSHA256:               strings.TrimPrefix(doc.Digest, "sha256:"),
		Rows:                      make([]result.GraphTestRow, 0, len(rows.Cases)),
	}
	spent := 0
	if options.reportSpent != nil {
		spent = *options.reportSpent
	}
	startingSpent := spent
	for _, row := range rows.Cases {
		outcome := runRow(loaded, engine, doc, graphPath, row, options)
		output.Summary.Total++
		if outcome.Status == "passed" {
			output.Summary.Passed++
		} else {
			output.Summary.Mismatched++
			output.Status = "mismatch"
		}
		output.Rows = append(output.Rows, outcome)
		// The budget is enforced HERE, per row, because this is where the
		// retention happens: a graph may declare 10,000 rows and each retains
		// its node dispositions. Checking between graphs would still let one
		// graph accumulate the gigabytes the bound exists to prevent, and the
		// remaining rows are not evaluated once it trips.
		if options.ReportBudget > 0 {
			encoded, err := json.Marshal(outcome)
			if err != nil {
				return result.GraphTest{}, rowEncodingFailure(row.ID)
			}
			spent += len(encoded)
			if options.reportSpent != nil {
				*options.reportSpent = spent
			}
			if spent > options.ReportBudget {
				return result.GraphTest{}, reportBudgetFailure(doc.ID, output.Summary.Total, spent, options.ReportBudget)
			}
		}
	}
	output.Coverage = coverage(loaded, engine, doc, rows, options)
	// Coverage is retained per graph like the rows are, and it repeats
	// pack-derived probe strings across up to 64 nodes, so it is charged too.
	// Round 3 of the review found it outside the budget while the ADR claimed
	// the whole report was bounded.
	if options.ReportBudget > 0 {
		encoded, err := json.Marshal(output.Coverage)
		if err != nil {
			return result.GraphTest{}, coverageEncodingFailure(doc.ID)
		}
		spent += len(encoded)
		if spent > options.ReportBudget {
			return result.GraphTest{}, reportBudgetFailure(doc.ID, output.Summary.Total, spent, options.ReportBudget)
		}
	}
	// This report's own envelope is charged only when THIS report is what the
	// caller keeps -- a direct Test, where reportSpent is nil. Inside
	// TestProject the GraphTest envelope is discarded: testEntry copies the
	// status, summary, rows and coverage into a GraphSuiteEntry and drops the
	// rest, and TestProject charges that entry's envelope instead.
	//
	// Both directions were wrong once. Round 8 found this envelope uncharged on
	// the direct path. Round 9 found the fix charging it on the composed path
	// too, where the bytes are never retained -- so production-only bytes were
	// consuming the budget, and a positive-only remainder downstream can neither
	// subtract that nor undo a refusal it already caused. The invariant is
	// "charge what is RETAINED", and it fails if either side is over-applied.
	if options.ReportBudget > 0 && options.reportSpent == nil {
		encoded, err := json.Marshal(output)
		if err != nil {
			return result.GraphTest{}, reportEncodingFailure(doc.ID)
		}
		if envelope := len(encoded) - (spent - startingSpent); envelope > 0 {
			spent += envelope
		}
		if spent > options.ReportBudget {
			return result.GraphTest{}, reportBudgetFailure(doc.ID, output.Summary.Total, spent, options.ReportBudget)
		}
	}
	if options.reportSpent != nil {
		*options.reportSpent = spent
	}
	return output, nil
}

// runRow judges one row. A row's expected dispositions pass through the same
// strict gate the comparator applies everywhere (DecodeDisposition), so a row
// carrying an illegal §8.3 value is a defect in the row, reported as its own
// mismatch rather than compared loosely.
// targetAssertion decodes one target assertion and renders both sides by the
// one writer every report uses (result.HandoffTarget.Rendered); what decides
// is the decoded values through evaluation.SameHandoffTarget, never these
// renderings (ADR-0025 via ADR-0032). The expected side was shape-checked at
// load, so an error here is a row handed around the loader, or an assertion
// whose members §8.1 refuses — reported as the row defect it is.
func targetAssertion(expectedRaw json.RawMessage, actual *result.HandoffTarget) (expected *result.HandoffTarget, expectedRendering, actualRendering string, err error) {
	expected, err = evaluation.DecodeHandoffTarget(expectedRaw)
	if err != nil {
		return nil, "", "", err
	}
	if expectedRendering, err = expected.Rendered(); err != nil {
		return nil, "", "", err
	}
	if actualRendering, err = actual.Rendered(); err != nil {
		return nil, "", "", err
	}
	return expected, expectedRendering, actualRendering, nil
}

func runRow(loaded *project.Project, engine *evaluation.Engine, doc Document, graphPath string, row RowCase, options Options) result.GraphTestRow {
	outcome := result.GraphTestRow{
		ID:                 row.ID,
		Status:             "passed",
		ExpectedErrorClass: row.ExpectedErrorClass,
		ExpectedErrorPhase: row.ExpectedErrorPhase,
	}
	mismatch := func(detail string) result.GraphTestRow {
		outcome.Status = "mismatch"
		outcome.Detail = detail
		return outcome
	}
	if row.ExpectedDisposition != nil {
		expected, err := evaluation.DecodeDisposition(row.ExpectedDisposition)
		if err != nil {
			return mismatch("The row's expected disposition is not a canonicalizable §8.3 disposition, which is a defect in the row and not a result: " + display.Sanitize(err.Error()))
		}
		canonical, err := expected.Canonical()
		if err != nil {
			return mismatch("The row's expected disposition did not canonicalize: " + display.Sanitize(err.Error()))
		}
		outcome.Expected = string(canonical)
	}

	evaluated, failure := Evaluate(loaded, engine, doc, graphPath, row.Inputs, true, options)
	if failure != nil {
		outcome.ActualErrorClass, outcome.ActualErrorPhase = failure.Class, failure.Phase
		if row.ExpectedErrorClass == "" {
			// A row that also asserted the composite's target still gets the
			// pair, with the pack triple's honest third state: this report
			// cannot state a target, because the run it would have come from
			// was refused — told from the other causes by ActualErrorClass
			// being set, exactly as a pack row reports it (ADR-0025).
			if row.ExpectedHandoffTarget != nil {
				if _, rendering, _, renderErr := targetAssertion(row.ExpectedHandoffTarget, nil); renderErr == nil {
					outcome.ExpectedHandoffTarget = rendering
					outcome.ActualHandoffTarget = result.HandoffTargetUnavailable
				}
			}
			return mismatch("The run was refused where a composite was expected: " + failure.Code + ": " + display.Sanitize(failure.Message))
		}
		if failure.Class != row.ExpectedErrorClass {
			return mismatch("The evaluation error class differs: " + failure.Code + ": " + display.Sanitize(failure.Message))
		}
		if row.ExpectedErrorPhase != "" && failure.Phase != row.ExpectedErrorPhase {
			return mismatch("The evaluation error phase differs: " + failure.Code + ": " + display.Sanitize(failure.Message))
		}
		return outcome
	}
	if row.ExpectedErrorClass != "" {
		return mismatch("A composite was produced where the row expected a refusal.")
	}

	actual, err := evaluated.Disposition.Canonical()
	if err != nil {
		return mismatch("The produced composite headline did not canonicalize, which is a defect in this runtime: " + display.Sanitize(err.Error()))
	}
	outcome.Actual = string(actual)
	// The pair is reported before either comparison runs, on the pack matrix's
	// precedent: a row that mismatches on the headline still says which target
	// each side named, so a reader is never told less by a row that failed
	// earlier.
	var expectedTarget *result.HandoffTarget
	if row.ExpectedHandoffTarget != nil {
		var renderErr error
		var expectedRendering, actualRendering string
		expectedTarget, expectedRendering, actualRendering, renderErr = targetAssertion(row.ExpectedHandoffTarget, evaluated.HandoffTarget)
		if renderErr != nil {
			return mismatch("The row's expectedHandoffTarget could not be decoded or rendered, which is a defect in the row and not a result: " + display.Sanitize(renderErr.Error()))
		}
		outcome.ExpectedHandoffTarget, outcome.ActualHandoffTarget = expectedRendering, actualRendering
	}
	if outcome.Actual != outcome.Expected {
		return mismatch("The composite headline differs from the row's expectation.")
	}
	// §8.3 keeps the configured target outside the disposition, so this is the
	// one comparison that can see a target-only edit: every headline byte
	// above stayed identical (ADR-0025's defect class, closed here for the
	// composite by ADR-0032). Decided on the decoded values; the renderings
	// already set are display values beside the verdict.
	if row.ExpectedHandoffTarget != nil && !evaluation.SameHandoffTarget(expectedTarget, evaluated.HandoffTarget) {
		return mismatch("The composite's reported handoff target differs from the row's expectation; the renderings are reported beside this verdict.")
	}

	byNode := map[string]result.GraphNodeEvaluation{}
	for _, node := range evaluated.Nodes {
		byNode[node.Node] = node
	}
	for _, name := range sortedKeys(row.ExpectedNodes) {
		comparison := result.GraphTestNode{Node: name, Status: "passed"}
		expectedRaw := row.ExpectedNodes[name]
		nodeEval, known := byNode[name]
		if !known {
			comparison.Status = "mismatch"
			outcome.Nodes = append(outcome.Nodes, comparison)
			return mismatch(fmt.Sprintf("Row names node %q, which the graph does not declare.", display.Sanitize(name)))
		}
		if options.IncludeTraces {
			// Attached the moment the evaluation is in hand, so every reported
			// comparison of an evaluated node carries it — the mismatching ones
			// most of all, since "why" is what the ask is for (ADR-0031). The
			// contract's floor is [] and never null (ADR-0027 §1); the trace is
			// a pointer member, so a nil here would marshal the null the
			// contract forbids, and the floor is held at the one assignment.
			trace := nodeEval.Trace
			if trace == nil {
				trace = []result.TraceEntry{}
			}
			comparison.Trace = &trace
		}
		// The node's pair is reported before its comparisons too, for the same
		// reason the row's is: a mismatching comparison says which target each
		// side named.
		expectedNodeTarget, nodeTargetAsserted := (*result.HandoffTarget)(nil), false
		if raw, asserted := row.ExpectedNodeHandoffTargets[name]; asserted {
			nodeTargetAsserted = true
			var renderErr error
			var expectedRendering, actualRendering string
			expectedNodeTarget, expectedRendering, actualRendering, renderErr = targetAssertion(raw, nodeEval.HandoffTarget)
			if renderErr != nil {
				comparison.Status = "mismatch"
				outcome.Nodes = append(outcome.Nodes, comparison)
				return mismatch(fmt.Sprintf("Row's handoff-target assertion for node %q could not be decoded or rendered, which is a defect in the row and not a result: %s", display.Sanitize(name), display.Sanitize(renderErr.Error())))
			}
			comparison.ExpectedHandoffTarget, comparison.ActualHandoffTarget = expectedRendering, actualRendering
		}
		expected, err := evaluation.DecodeDisposition(expectedRaw)
		if err != nil {
			comparison.Status = "mismatch"
			outcome.Nodes = append(outcome.Nodes, comparison)
			return mismatch(fmt.Sprintf("Row's expectation for node %q is not a canonicalizable §8.3 disposition: %s", display.Sanitize(name), display.Sanitize(err.Error())))
		}
		expectedCanonical, _ := expected.Canonical()
		actualCanonical, err := nodeEval.Disposition.Canonical()
		if err != nil {
			comparison.Status = "mismatch"
			outcome.Nodes = append(outcome.Nodes, comparison)
			return mismatch(fmt.Sprintf("Node %q's produced disposition did not canonicalize, which is a defect in this runtime: %s", display.Sanitize(name), display.Sanitize(err.Error())))
		}
		comparison.Expected, comparison.Actual = string(expectedCanonical), string(actualCanonical)
		if comparison.Expected != comparison.Actual {
			comparison.Status = "mismatch"
			outcome.Nodes = append(outcome.Nodes, comparison)
			return mismatch(fmt.Sprintf("Node %q's disposition differs from the row's expectation.", display.Sanitize(name)))
		}
		if nodeTargetAsserted && !evaluation.SameHandoffTarget(expectedNodeTarget, nodeEval.HandoffTarget) {
			comparison.Status = "mismatch"
			outcome.Nodes = append(outcome.Nodes, comparison)
			return mismatch(fmt.Sprintf("Node %q's reported handoff target differs from the row's expectation; the renderings are reported beside this verdict.", display.Sanitize(name)))
		}
		outcome.Nodes = append(outcome.Nodes, comparison)
	}
	return outcome
}

func sortedKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
