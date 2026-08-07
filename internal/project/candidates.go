package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The test-row input generator of ADR-0024.
//
// It reads a pack's own conditions and emits candidate *inputs* — facts
// documents at the values the pack's literals imply — and never a row. The
// distinction is the whole design: a row is a facts document plus an
// expectation, the expectation is the only member that says what the pack
// should decide, and deriving one from the pack would be the circular oracle
// ADR-0014 names. So a candidate carries neither expectedDisposition nor
// expectedErrorClass, and their ABSENCE rather than a placeholder is what
// enforces it: LoadMatrix already refuses a row that declares neither
// (matrix.go, "must declare exactly one of expectedDisposition and
// expectedErrorClass"), so a candidate pasted into a cases array fails closed
// with a message naming the missing work, through machinery no edit here can
// relax. A sentinel would instead be a slot inviting a fill, and the fill is
// one token; absence makes an author write four members from the policy text.
//
// The emitted document is not a matrix either. Its root members are
// candidatesVersion and candidates, so a jpack.json matrix path aimed at a raw
// candidate file is refused twice over by the loader's strict decode and by its
// zero-rows check. Neither of those refusals is code this file adds; both were
// already there, which is exactly why the document was shaped to trip them.

const (
	// CandidatesVersion is the one candidatesVersion this runtime emits, on the
	// same single-integer footing as MatrixVersion and ConfigVersion: a document
	// a program reads either has a shape that program knows, or does not.
	CandidatesVersion = "1"
	// CandidateOrigin marks every candidate this generator emits, and is carried
	// through into a matrix row's own origin member and into the packs test
	// report as a count. It is provenance and never a gate (ADR-0024): a gate on
	// it would be defeated by deleting one member, which is a worse affordance
	// than the sentinel this design already refuses, and it would teach the
	// deletion that destroys the only signal measuring how much of a suite a
	// generator supplied.
	CandidateOrigin = "generated"
	// DefaultMaxCandidates bounds one run by default, far under the matrix's own
	// MaxMatrixCases. The binding constraint here is not the loader's, it is a
	// reviewer's: volume is what turns review into rubber-stamping, so the
	// default is a number a person can read in a sitting and the run refuses
	// rather than truncates past it.
	DefaultMaxCandidates = 500
	// MaxCandidatesUnset is what SuggestOptions.Max carries when a caller states
	// no bound of its own and accepts DefaultMaxCandidates. It is deliberately
	// not zero: zero is a bound a caller can mean — emit nothing — and a run that
	// read it as five hundred would emit five hundred candidates somebody asked
	// not to have. The command line never passes it, because an absent --max
	// arrives already carrying the default.
	MaxCandidatesUnset = -1
	// DefaultMaxOutputBytes bounds the candidate document one run composes, on
	// the same footing as MaxMatrixBytes and for the same reason that number
	// exists. --max bounds a *count*, and the document's size is that count
	// times a base row: a reviewed row is bounded only by MaxMatrixBytes, so a
	// three-megabyte base under the default count is gigabytes of output nobody
	// asked for and gigabytes of memory to compose it in.
	//
	// It bounds the WRITTEN form — the bytes EncodeCandidates emits, indentation
	// and all — and not a compact measure of the same values. That distinction
	// is the bound rather than a detail of it: indentation is not a constant
	// factor but multiplies with nesting depth, so a base row a hundred
	// containers deep wrapping very many tiny tokens is under a megabyte compact
	// and hundreds of megabytes written, passes every carrier bound on the way
	// in, and would sail through a budget charged compact. The budget is charged
	// as each candidate is composed rather than measured at the end, so the
	// bytes are never held in the first place and the refusal fires before any
	// such document exists.
	DefaultMaxOutputBytes = 16 << 20
	// maxStepPrecision bounds the exponent of the one-unit step. A literal
	// authored at finer precision than this steps at 10^-6 instead, and the
	// candidate's rationale says so rather than implying the value is the pack's
	// own next representable one.
	maxStepPrecision = 6
	// maxCandidateLiteralBytes bounds a compared literal this generator will
	// derive values around. §2.1's carrier bounds an authored string only at a
	// megabyte, and ADR-0023 already established that a report repeating such a
	// string is a size defect; a *candidate* would repeat it in an id, in a
	// rationale, and in a facts document. A value no reviewer can read is a
	// candidate no reviewer can review, which is this surface's whole purpose,
	// so the literal is reported as skipped rather than rendered.
	maxCandidateLiteralBytes = 128
	// suggestSkipCap is how many subjects one skipped-dimension sentence names
	// before counting the rest, in the house style joinCapped states.
	suggestSkipCap = 6
	// candidateEnvelopeBytes is what one candidate costs the written document
	// beyond its own encoded form: the array element's leading indentation, the
	// comma that separates it from the next, and the newline after — and, since
	// it is charged nowhere else, the document's own root framing. Those come to
	// a handful of bytes and this charges rather more than a handful, on
	// purpose: a byte budget that under-charges is the only kind that fails, and
	// the parts that actually multiply with a base row are measured rather than
	// estimated (candidateWrittenBytes).
	candidateEnvelopeBytes = 128
	// candidateIndentStep is the one indent EncodeCandidates writes with, and
	// candidateIndentPrefix is where a candidate sits inside that document: the
	// candidates array is one step inside the root object and its elements one
	// step inside the array, so every line of a candidate past its first is
	// written two steps in. The charge and the writer read these same two
	// constants, because a budget measured at a different indentation than the
	// file is written at is not a bound on the file.
	candidateIndentStep   = "  "
	candidateIndentPrefix = candidateIndentStep + candidateIndentStep
	// candidateRationaleClose is the sentence every candidate ends with. It is
	// repeated per candidate rather than stated once at the document root
	// because the file is read one candidate at a time, and the one thing a
	// reader must not carry away from a candidate is that anything about it has
	// been decided.
	candidateRationaleClose = " No expectation is stated: write one from the policy text, or delete this candidate."
)

// Candidates is the document jpack packs suggest emits. It is deliberately NOT
// a Matrix: the root members differ, so LoadMatrix refuses it, and its rows
// carry no expectation, so LoadMatrix refuses those too if they are lifted into
// a cases array unchanged. Both refusals already existed (ADR-0024).
type Candidates struct {
	CandidatesVersion string      `json:"candidatesVersion"`
	Candidates        []Candidate `json:"candidates"`
}

// Candidate is one suggested test-row input: an id, the provenance marker, the
// facts document, the evidence availability where the candidate varies that
// axis, and one sentence saying what the input places and why the pack's own
// declarations imply it.
//
// There is no expectation member and there must not be one, not even an empty
// or sentinel one. The rationale is prose about the *pack*, never about what an
// evaluation would produce: nothing in this file runs the evaluator, so no
// member here can carry an evaluator's product.
type Candidate struct {
	ID                   string          `json:"id"`
	Origin               string          `json:"origin"`
	Facts                json.RawMessage `json:"facts"`
	EvidenceAvailability json.RawMessage `json:"evidenceAvailability,omitempty"`
	Rationale            string          `json:"rationale"`
}

// SuggestOptions is one run's selection and bounds. Every member is a caller's
// explicit choice; nothing here is read from the environment.
type SuggestOptions struct {
	// ID selects one declared pack by decision id, or every declared pack when
	// empty.
	ID string
	// BaseRow names an already-reviewed row of the selected pack's matrix whose
	// facts every candidate starts from. It requires ID, because a row id is a
	// name inside one pack's matrix and nowhere else.
	BaseRow string
	// Max bounds the candidates one run emits; past it the run refuses rather
	// than truncating, naming the flag. MaxCandidatesUnset means the caller
	// states no bound and accepts DefaultMaxCandidates; every other
	// non-positive value is refused rather than normalized, because a caller
	// that asked for no candidates and silently got five hundred was not
	// answered.
	Max int
	// IncludeHugs additionally emits the pair two decimal places finer than the
	// authored precision. Default off, and the reason is evidential rather than
	// aesthetic: the authored corpus this generator exists to complement is
	// already saturated at that distance, so the hug mechanizes what authorship
	// already does and the unit step supplies what it does not.
	IncludeHugs bool
}

// EncodeCandidates renders one candidate document as the bytes written or
// printed, with a trailing newline and HTML escaping off, so the emitted file
// reads as authored JSON and two runs over an unchanged pack produce identical
// bytes. Determinism is a property this generator must have rather than happen
// to have: a re-run that reshuffled a file a reviewer is mid-way through
// reviewing would make the diff say something changed when nothing did.
func EncodeCandidates(document Candidates) ([]byte, error) {
	var rendered bytes.Buffer
	encoder := json.NewEncoder(&rendered)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", candidateIndentStep)
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	return rendered.Bytes(), nil
}

// Suggest derives candidate test-row inputs for the selected packs and reports
// what it derived and what it could not.
//
// Two artifacts come back and they are deliberately different documents. The
// report says which packs were read, at which identities, how many candidates
// each derived, and every dimension the derivation declined — a caller renders
// it in the run's own format. The candidate document is the emitted artifact
// itself, which a caller writes to a file or to stdout. Provenance lives in the
// report rather than in the document, because the document exists to be edited
// into a matrix and a provenance member inside it would either be pasted into a
// row that has no place for it or be silently dropped.
//
// Nothing here evaluates anything, and the run has no verdict: a pack that
// derives nothing is reported "skipped", never "failed", and the exit code
// stays neutral on every surface that calls this. A generator whose exit code
// moved would be a gate, and gating on the presence of machine-supplied inputs
// is exactly the pressure this design refuses to create.
func (p *Project) Suggest(options SuggestOptions, command string) (result.PackSuggestion, Candidates, *Failure) {
	maximum, failure := suggestMaximum(options.Max)
	if failure != nil {
		return result.PackSuggestion{}, Candidates{}, failure
	}
	selected, failure := p.selection(options.ID)
	if failure != nil {
		return result.PackSuggestion{}, Candidates{}, failure
	}
	base, hasBase, failure := p.resolveBase(options)
	if failure != nil {
		return result.PackSuggestion{}, Candidates{}, failure
	}
	budget := &outputBudget{limit: DefaultMaxOutputBytes}
	output := result.PackSuggestion{
		OutputVersion:     result.OutputVersion,
		Tool:              result.CurrentTool(),
		Command:           command,
		Status:            "skipped",
		Kind:              result.ProjectKind,
		Label:             result.PackSuggestionLabel,
		ConfigPath:        p.ConfigPath,
		ConfigVersion:     p.Config.ConfigVersion,
		CandidatesVersion: CandidatesVersion,
		Packs:             make([]result.PackSuggestionEntry, 0, len(selected)),
	}
	document := Candidates{CandidatesVersion: CandidatesVersion, Candidates: []Candidate{}}
	for _, packID := range selected {
		entry, candidates := p.suggestPack(options, base, hasBase, budget, packID, p.Config.Packs[packID])
		// The byte budget is answered the moment it is crossed, before the
		// candidates composed so far are carried any further: a document past it
		// is refused whole, exactly as one past --max is, because a truncated
		// candidate set looks like a complete one however it was cut.
		if budget.exceeded {
			return result.PackSuggestion{}, Candidates{}, budget.failure()
		}
		output.Summary.Total++
		if entry.Status == "suggested" {
			output.Summary.Suggested++
			output.Status = "suggested"
		} else {
			output.Summary.Skipped++
		}
		output.Summary.Candidates += entry.Candidates
		output.Packs = append(output.Packs, entry)
		document.Candidates = append(document.Candidates, candidates...)
	}
	// Refuse rather than truncate, naming the flag: a truncated candidate set
	// looks exactly like a complete one, and a reviewer cannot tell that the
	// dimensions past the cut were never offered.
	if len(document.Candidates) > maximum {
		return result.PackSuggestion{}, Candidates{}, &Failure{
			Code: "JPS-RESOURCE-SUGGEST-CANDIDATE-LIMIT",
			Message: fmt.Sprintf("The selected pack(s) derive %d candidate inputs, past the %d this run allows; nothing was written. Raise --max to accept them all, or select one pack with --id — a truncated set would look exactly like a complete one.",
				len(document.Candidates), maximum),
			ExitCode: result.ExitInvocation,
		}
	}
	return output, document, nil
}

// suggestMaximum reads one run's candidate bound, refusing a non-positive one
// rather than normalizing it.
//
// A run asked for at most zero candidates was asked for nothing, and quietly
// answering it with five hundred is the silent substitution this family refuses
// everywhere else. Only MaxCandidatesUnset means "state no bound of my own",
// and it is a value no command line produces: --max is registered carrying the
// default, so an absent flag arrives as the default and a typed 0 arrives as 0.
func suggestMaximum(stated int) (int, *Failure) {
	if stated == MaxCandidatesUnset {
		return DefaultMaxCandidates, nil
	}
	if stated <= 0 {
		return 0, &Failure{
			Code: "JPS-INVOCATION-SUGGEST-MAX",
			Message: fmt.Sprintf("--max bounds how many candidate inputs one run may emit, so it must be a positive count; %d bounds nothing this run could offer. Omit --max to accept the default of %d.",
				stated, DefaultMaxCandidates),
			ExitCode: result.ExitInvocation,
		}
	}
	return stated, nil
}

// outputBudget bounds the bytes one run's candidate document composes, across
// every pack the run selected.
//
// It is the size half of what --max bounds by count, and the two are not the
// same bound: every candidate carries a whole facts document, so a run's size
// is the candidate count times the base row, and a base row is bounded only by
// MaxMatrixBytes. A three-megabyte reviewed row under the default count is
// gigabytes of output. What is charged is the WRITTEN size of each candidate —
// what EncodeCandidates will put in the file, indentation included — because
// indentation multiplies with nesting depth and a deep base row is therefore
// two orders larger written than composed. The budget is charged as each
// candidate is composed rather than measured once the document is whole, so the
// bytes are never accumulated in the first place, and it refuses rather than
// truncates for the same reason --max does.
type outputBudget struct {
	limit int
	used  int
	// exceeded latches: once a run is past the budget it is refused whole, and
	// every later candidate is declined rather than composed.
	exceeded bool
	// measured is what the document had reached when the budget was crossed —
	// the size the refusal names, so a reader can see how far past it the run
	// was rather than only that it was.
	measured int
}

// charge adds one candidate's written bytes and reports whether the document is
// still inside the budget. The comparison is written as a remainder so a
// caller's size cannot overflow the sum.
func (budget *outputBudget) charge(size int) bool {
	if budget.exceeded {
		return false
	}
	if size > budget.limit-budget.used {
		budget.exceeded = true
		budget.measured = budget.used + size
		return false
	}
	budget.used += size
	return true
}

// failure is the one refusal a crossed budget produces: the size measured, the
// budget it passed, and the two things a caller can do about it. Nothing is
// written and nothing is truncated.
func (budget *outputBudget) failure() *Failure {
	return &Failure{
		Code: "JPS-RESOURCE-SUGGEST-OUTPUT-BYTES",
		Message: fmt.Sprintf("The candidate inputs derived so far measure %d bytes as this run would write them, indentation included, past the %d this run allows; nothing was written. Every candidate carries a whole facts document, so a base row that is large — or merely deeply nested, which indentation multiplies — multiplies again by the candidate count. Lower --max, or name a narrower --base row — a truncated candidate document would look exactly like a complete one.",
			budget.measured, budget.limit),
		ExitCode: result.ExitInvocation,
	}
}

// resolveBase reads the --base row's facts, which every candidate of the run
// then starts from. Preferring an already-reviewed row is what keeps a
// candidate minimally novel: it reads as "this reviewed row, with one pointer
// moved to a value the pack's own literals imply", which is a reviewable claim
// where a synthesized full record is a policy world the generator invented.
func (p *Project) resolveBase(options SuggestOptions) (any, bool, *Failure) {
	if options.BaseRow == "" {
		return nil, false, nil
	}
	if options.ID == "" {
		return nil, false, &Failure{
			Code:     "JPS-INVOCATION-SUGGEST-BASE",
			Message:  "--base names a row inside one pack's matrix, so it requires --id to say which pack's matrix that is.",
			ExitCode: result.ExitInvocation,
		}
	}
	entry, declared := p.Entry(options.ID)
	if !declared {
		return nil, false, p.UnknownPackFailure(options.ID)
	}
	if entry.Matrix == "" {
		return nil, false, &Failure{
			Code:     "JPS-INVOCATION-SUGGEST-BASE",
			Message:  fmt.Sprintf("The pack %q declares no matrix, so it has no reviewed row to base candidates on. Omit --base to derive candidates whose facts carry only the varied pointer.", display.Sanitize(options.ID)),
			ExitCode: result.ExitInvocation,
		}
	}
	matrix, err := p.LoadMatrix(entry)
	if err != nil {
		return nil, false, &Failure{
			Code:     "JPS-SUGGEST-BASE-READ",
			Message:  "The matrix holding the base row could not be read as rows: " + display.Sanitize(err.Error()),
			ExitCode: result.ExitIO,
		}
	}
	ids := make([]string, 0, len(matrix.Cases))
	for _, row := range matrix.Cases {
		if row.ID != options.BaseRow {
			ids = append(ids, fmt.Sprintf("%q", display.Sanitize(row.ID)))
			continue
		}
		facts, carrierFailure := carrier.Decode(row.Facts, carrier.DefaultLimits())
		if carrierFailure != nil {
			return nil, false, &Failure{
				Code:     "JPS-SUGGEST-BASE-READ",
				Message:  fmt.Sprintf("The base row %q carries a facts document this runtime cannot decode: %s", display.Sanitize(options.BaseRow), display.Sanitize(carrierFailure.Diagnostic.Message)),
				ExitCode: result.ExitInvalid,
			}
		}
		return facts, true, nil
	}
	return nil, false, &Failure{
		Code: "JPS-INVOCATION-SUGGEST-BASE",
		Message: fmt.Sprintf("The matrix of %q declares no row %q. Its rows are: %s.",
			display.Sanitize(options.ID), display.Sanitize(options.BaseRow), joinCapped(ids, suggestSkipCap)),
		ExitCode: result.ExitInvocation,
	}
}

// suggestPack derives one pack's candidates.
//
// A pack this runtime cannot read derives nothing and is reported as such with
// the read failure named. It is not a failure of this run, because this run
// judges nothing: packs validate is the surface that fails on an unreadable
// declared document, and duplicating that verdict here would give a generator a
// gate's exit code.
func (p *Project) suggestPack(options SuggestOptions, base any, hasBase bool, budget *outputBudget, id string, entry Pack) (result.PackSuggestionEntry, []Candidate) {
	report := result.PackSuggestionEntry{ID: id, Path: entry.Path, Status: "skipped"}
	found, data, err := p.readIdentity(entry)
	if err != nil {
		report.Detail = "No candidate could be derived, because the pack document could not be read; packs validate diagnoses the read itself: " + ReadFailureMessage(entry.Path, err)
		return report, nil
	}
	report.PackID, report.PackVersion = found.ID, found.Version
	pack := PackRoot(data)
	if pack == nil {
		report.Detail = "No candidate could be derived, because the pack document does not decode as a JSON object under this runtime's carrier rules; packs validate diagnoses it."
		return report, nil
	}
	set := newCandidateSet(id, base, hasBase, options, budget)
	if usesQuantifiers(pack) {
		set.skip("draft-rfc-quantifiers",
			"The pack states a draft RFC 0008 collection quantifier. This derivation shares the coverage report's structure-keyed walk, which descends only through all, any, and not, so a comparison inside a where or an at derives no candidate — and an element-relative pointer has no place in a flat facts document to derive one at.",
			"")
	}
	set.deriveValues(pack)
	set.deriveMembers(pack)
	set.deriveAbsences()
	set.deriveEvidence(pack)
	report.Candidates = len(set.candidates)
	report.Skipped = set.skipped()
	if len(set.candidates) > 0 {
		report.Status = "suggested"
	} else if report.Detail == "" {
		report.Detail = "The pack's conditions state no comparison this generator derives an input from."
	}
	return report, set.candidates
}

// candidateSet accumulates one pack's candidates and the dimensions its
// derivation declined. Order is first-occurrence walk order throughout and no
// map is ever ranged over, so two runs over one unchanged pack emit identical
// bytes.
type candidateSet struct {
	decisionID string
	base       any
	hasBase    bool
	options    SuggestOptions
	candidates []Candidate
	ids        map[string]bool
	// pointers is every fact pointer some condition of this pack consults, in
	// first-occurrence order — the axis the absence witnesses are derived over.
	pointers []string
	skips    []skipRecord
	skipAt   map[string]int
	// budget is the run's own, shared with every other pack of the same run:
	// the bound is on one document, and the document is one run's whole output.
	budget *outputBudget
}

// newCandidateSet opens one pack's accumulator. It is the only construction
// there is, so no set can exist without the run's byte budget attached — a set
// with none would compose an unbounded document, and the bound is the property
// DefaultMaxOutputBytes exists to hold.
func newCandidateSet(decisionID string, base any, hasBase bool, options SuggestOptions, budget *outputBudget) *candidateSet {
	return &candidateSet{
		decisionID: decisionID,
		base:       base,
		hasBase:    hasBase,
		options:    options,
		ids:        map[string]bool{},
		budget:     budget,
	}
}

// skipRecord is one declined dimension: what it is, why, and the subjects it
// was declined for.
type skipRecord struct {
	name     string
	reason   string
	subjects []string
}

func (set *candidateSet) skip(name, reason, subject string) {
	if set.skipAt == nil {
		set.skipAt = map[string]int{}
	}
	at, opened := set.skipAt[name]
	if !opened {
		at = len(set.skips)
		set.skipAt[name] = at
		set.skips = append(set.skips, skipRecord{name: name, reason: reason})
	}
	if subject != "" && !slices.Contains(set.skips[at].subjects, subject) {
		set.skips[at].subjects = append(set.skips[at].subjects, subject)
	}
}

// skipped renders the declined dimensions for the report, each naming the first
// few subjects and counting the rest.
func (set *candidateSet) skipped() []result.SuggestionSkip {
	if len(set.skips) == 0 {
		return nil
	}
	rendered := make([]result.SuggestionSkip, 0, len(set.skips))
	for _, record := range set.skips {
		detail := record.reason
		if len(record.subjects) > 0 {
			detail += " Not derived for: " + joinCapped(record.subjects, suggestSkipCap) + "."
		}
		rendered = append(rendered, result.SuggestionSkip{Name: record.name, Detail: detail})
	}
	return rendered
}

// notePointer records a consulted pointer once, in walk order.
func (set *candidateSet) notePointer(path string) {
	if !slices.Contains(set.pointers, path) {
		set.pointers = append(set.pointers, path)
	}
}

// latticeValue is one derived value for one pointer: the §2.2 text a candidate
// places, the exact number behind it (for ordering and for deduplication), and
// the sentence that says why the pack's own declarations imply it.
type latticeValue struct {
	text      string
	value     *big.Rat
	rationale string
}

// deriveValues derives, for every pointer some ordered comparison reads, the
// lattice ADR-0024 settles: the literal itself, one representable step either
// side of it at the precision the literal was authored in, the interior
// midpoints between adjacent literals, and one unit outside the outermost ones.
//
// The bound is 4n+1 values for a pointer compared against n distinct literals
// — 6n+1 under --include-hugs, which adds the pair two decimal places finer
// than the authored precision to each literal — and the composition across
// pointers is one factor at a time — every candidate
// moves exactly one pointer and holds the rest at the base assignment — so a
// run's size is the sum over pointers and never their product. That is a review
// decision before it is a performance one: a cross product produces mostly
// nonsense rows, and an unreviewable pile is the direct cause of the failure
// mode this whole surface is arranged against.
func (set *candidateSet) deriveValues(pack map[string]any) {
	groups := boundaryGroups(comparisonSites(pack))
	// Group by pointer, in first-occurrence order, so a pointer compared against
	// several literals derives one lattice rather than one per literal.
	var paths []string
	byPath := map[string][]boundaryGroup{}
	for _, group := range groups {
		if !set.admitPointer(group.path) {
			continue
		}
		if len(group.literal) > maxCandidateLiteralBytes {
			set.skip("oversize-literal",
				fmt.Sprintf("A compared literal is longer than %d bytes. A value no reviewer can read is a candidate no reviewer can review, and ADR-0023 already bounds every authored string this family renders.", maxCandidateLiteralBytes),
				capRendered(group.path))
			continue
		}
		set.notePointer(group.path)
		if _, opened := byPath[group.path]; !opened {
			paths = append(paths, group.path)
		}
		byPath[group.path] = append(byPath[group.path], group)
	}
	for _, path := range paths {
		set.derivePointerLattice(path, byPath[path])
	}
}

// admittedLiteral is one boundary of a pointer's lattice together with the
// canonical value its literal spells.
//
// The two travel in one struct rather than in two slices read at a shared
// index: a group whose literal is not a §2.2 decimal is admitted by neither
// half, so parallel slices would be aligned only by a rule stated in another
// package — and a group that fell out of one and not the other would step the
// wrong literal in silence.
type admittedLiteral struct {
	group boundaryGroup
	value *big.Rat
}

// derivePointerLattice derives and emits one pointer's values.
func (set *candidateSet) derivePointerLattice(path string, groups []boundaryGroup) {
	admitted := make([]admittedLiteral, 0, len(groups))
	var values []latticeValue
	add := func(text string, value *big.Rat, rationale string) {
		values = append(values, latticeValue{text: text, value: value, rationale: rationale})
	}
	// The literal itself, in the spelling the pack authored, which is ADR-0023's
	// own probe point: the single input where a strict and a non-strict encoding
	// of the threshold disagree.
	for _, group := range groups {
		value, decimal := evaluation.DecimalValue(group.literal)
		if !decimal {
			set.skip("non-decimal-literal", nonDecimalLiteralReason, capRendered(path))
			continue
		}
		admitted = append(admitted, admittedLiteral{group: group, value: value})
		add(group.literal, value, fmt.Sprintf("Places %s at %s, the literal compared by %s. It is the one input where a strict and a non-strict reading of that threshold differ.",
			capRendered(path), capRendered(group.literal), joinCapped(siteDescriptions(group.sites), suggestSkipCap)))
	}
	// Nothing to build a lattice from. This is an empty result and not a drop:
	// every group that reached here without a value opened its own skip record
	// on the way, so a pointer that derives nothing says so in the report.
	if len(admitted) == 0 {
		return
	}
	// One representable step either side, at the precision the value was authored
	// in: "5000" steps by 1, "70.0" by 0.1, and a value spelled both ways at one
	// pointer steps at the finer of them (groupStepPrecision). Not a fixed 0.01 —
	// the authored corpus this complements already hugs its thresholds two and
	// three orders finer than the thresholds' own precision, so mechanizing the
	// hug would supply what authorship already supplies. One unit at the
	// precision the policy wrote the number in is what it does not.
	for _, entry := range admitted {
		digits, clamped := groupStepPrecision(entry.group)
		step := new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil))
		note := ""
		if clamped {
			note = fmt.Sprintf(" The literal is authored at a finer precision than the %d digits this generator steps at, so the step is 10^-%d and this is not the pack's own next distinguishable value.", maxStepPrecision, maxStepPrecision)
		}
		for _, side := range []struct {
			word  string
			delta *big.Rat
		}{
			{"below", new(big.Rat).Neg(step)},
			{"above", step},
		} {
			stepped := new(big.Rat).Add(entry.value, side.delta)
			text, renderable := evaluation.DecimalString(stepped)
			if !renderable {
				set.skip("unrenderable-value", unrenderableReason, capRendered(path))
				continue
			}
			add(text, stepped, fmt.Sprintf("Places %s at %s, one unit %s the literal %s at the finest precision the pack authored that value in (10^-%d).%s",
				capRendered(path), text, side.word, capRendered(entry.group.literal), digits, note))
		}
		if !set.options.IncludeHugs {
			continue
		}
		hugDigits := min(digits+2, maxStepPrecision)
		if hugDigits <= digits {
			continue
		}
		hug := new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(hugDigits)), nil))
		for _, side := range []struct {
			word  string
			delta *big.Rat
		}{
			{"below", new(big.Rat).Neg(hug)},
			{"above", hug},
		} {
			hugged := new(big.Rat).Add(entry.value, side.delta)
			text, renderable := evaluation.DecimalString(hugged)
			if !renderable {
				set.skip("unrenderable-value", unrenderableReason, capRendered(path))
				continue
			}
			add(text, hugged, fmt.Sprintf("Places %s at %s, hugging the literal %s two decimal places finer than the pack authored it (10^-%d), which --include-hugs asked for.",
				capRendered(path), text, capRendered(entry.group.literal), hugDigits))
		}
	}
	// The interior midpoints of the adjacent literals, and one unit outside the
	// outermost ones. A midpoint invents no granularity: 2 divides 10, so the
	// midpoint of two terminating decimals is itself a terminating decimal and
	// therefore itself a §2.2 value — which is exactly why midpoints are
	// principled where "adjacent" (ADR-0023's refused option E granularity) is
	// not.
	sorted := make([]*big.Rat, 0, len(admitted))
	for _, entry := range admitted {
		sorted = append(sorted, entry.value)
	}
	slices.SortStableFunc(sorted, func(left, right *big.Rat) int { return left.Cmp(right) })
	sorted = slices.CompactFunc(sorted, func(left, right *big.Rat) bool { return left.Cmp(right) == 0 })
	for index := 0; index+1 < len(sorted); index++ {
		midpoint := new(big.Rat).Quo(new(big.Rat).Add(sorted[index], sorted[index+1]), big.NewRat(2, 1))
		text, renderable := evaluation.DecimalString(midpoint)
		if !renderable {
			set.skip("unrenderable-value", unrenderableReason, capRendered(path))
			continue
		}
		lower, _ := evaluation.DecimalString(sorted[index])
		upper, _ := evaluation.DecimalString(sorted[index+1])
		add(text, midpoint, fmt.Sprintf("Places %s at %s, the midpoint of the adjacent compared literals %s and %s — inside a band this pack's own literals partition and no literal names.",
			capRendered(path), text, lower, upper))
	}
	for _, edge := range []struct {
		word string
		from *big.Rat
		sign int64
	}{
		{"under", sorted[0], -1},
		{"over", sorted[len(sorted)-1], 1},
	} {
		outer := new(big.Rat).Add(edge.from, new(big.Rat).SetInt64(edge.sign))
		text, renderable := evaluation.DecimalString(outer)
		if !renderable {
			set.skip("unrenderable-value", unrenderableReason, capRendered(path))
			continue
		}
		bound, _ := evaluation.DecimalString(edge.from)
		add(text, outer, fmt.Sprintf("Places %s at %s, one unit %s the outermost literal %s this pointer is compared against — the region beyond every band the pack's literals partition.",
			capRendered(path), text, edge.word, bound))
	}
	// Deduplicate by the evaluator's own decimal identity, keeping the first
	// derivation of each value so the literal's authored spelling wins over a
	// midpoint or an edge that lands on it, then order the pointer's candidates
	// along the number line, which is how a reviewer reads them.
	seen := map[string]bool{}
	kept := values[:0]
	for _, value := range values {
		key, decimal := evaluation.DecimalKey(value.text)
		if !decimal || seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, value)
	}
	slices.SortStableFunc(kept, func(left, right latticeValue) int { return left.value.Cmp(right.value) })
	for _, value := range kept {
		set.emit(candidateSeed{
			family:    "value",
			subject:   path,
			qualifier: value.text,
			rationale: value.rationale,
			place:     &placement{path: path, value: value.text},
		})
	}
}

// unrenderableReason is the one sentence every non-terminating derivation is
// declined with. It cannot arise from the lattice as specified — halving a
// terminating decimal and adding powers of ten keeps the denominator's prime
// factors inside {2,5} — and it is checked anyway, because rounding a value the
// generator could not spell would emit an input nobody asked for.
const unrenderableReason = "A derived value has no terminating decimal expansion, so §2.2 cannot spell it. It was refused rather than rounded: a rounded value is a different number, and a generator that quietly substituted one would offer an input the pack's literals do not imply."

// nonDecimalLiteralReason is the one sentence a compared literal that is not a
// §2.2 decimal is declined with.
//
// No pack can reach it today: boundaryGroups admits only literals the ordered
// walk collected, and that walk collects only decimals. But that invariant
// lives in another function, and a bare continue here would be this file's one
// silent drop — a dimension vanishing without a record, which is exactly what
// every other decline in this file refuses. So the branch is defensive and
// reported: if the walk's admission and this derivation's ever part, the report
// says so instead of a pointer quietly deriving nothing.
const nonDecimalLiteralReason = "A compared literal reached the value derivation without being a §2.2 decimal, so no lattice could be derived around it. The ordered walk that collects these literals admits only decimals, so this reports a disagreement between that walk and this derivation rather than a shape a pack can state — and it is reported rather than dropped, because a dimension missing in silence reads as one the pack never stated (ADR-0022's skipped-not-passed discipline)."

// groupStepPrecision reports the exponent of one boundary's one-unit step and
// whether it was clamped, over EVERY spelling the group's sites authored rather
// than over the one the group carries.
//
// A pointer compared against "5000" in one rule and "5000.0" in another is one
// boundary under ADR-0023's identity, and the group carries whichever spelling
// was declared first. Stepping at that one would make reordering two rules
// change the lattice — the same policy, two different candidate sets, with
// nothing in the pack to explain the difference. The finest precision any
// spelling states is order-independent and is also the faithful reading of "the
// precision the policy wrote": a policy that wrote a tenth anywhere wrote a
// tenth. The clamp is likewise the disjunction — a value one of whose spellings
// is finer than this generator steps at is one whose step is not the pack's own
// next distinguishable value, and the rationale says so (ADR-0024).
func groupStepPrecision(group boundaryGroup) (int, bool) {
	digits, clamped := stepPrecision(group.literal)
	for _, site := range group.sites {
		siteDigits, siteClamped := stepPrecision(site.literal)
		if siteDigits > digits {
			digits = siteDigits
		}
		clamped = clamped || siteClamped
	}
	return digits, clamped
}

// stepPrecision reports the exponent of the one-unit step for one authored
// literal — the count of digits after its decimal point — and whether that
// count was clamped. The literal has already passed §2.2's grammar, so the
// count is a byte position and not a parse.
func stepPrecision(literal string) (int, bool) {
	point := strings.IndexByte(literal, '.')
	if point < 0 {
		return 0, false
	}
	digits := len(literal) - point - 1
	if digits > maxStepPrecision {
		return maxStepPrecision, true
	}
	return digits, false
}

// membershipSite is one point predicate a pack states: the pointer, the
// operator, the owning declaration, and the operand values a candidate can
// place. It is collected by a second leaf handler over the walk comparisonSites
// uses, never by widening that walk: the ordered walk feeds a demand, and a
// demand must not grow because a generator wanted more to offer.
type membershipSite struct {
	path     string
	operator string
	owner    string
	operands []any
}

// membershipSites collects every equals, not-equals, and in comparison the
// pack's conditions state, in the same declaration order the ordered walk
// reports.
func membershipSites(pack map[string]any) []membershipSite {
	var sites []membershipSite
	packConditions(pack, func(node any, owner string, _ siteStage) {
		walkConditions(node, func(condition map[string]any) {
			path, ok := condition["path"].(string)
			if !ok {
				return
			}
			operator, _ := condition["operator"].(string)
			switch operator {
			case "equals", "not-equals":
				operand, stated := condition["value"]
				if !stated {
					return
				}
				sites = append(sites, membershipSite{path: path, operator: operator, owner: owner, operands: []any{operand}})
			case "in":
				operands, ok := condition["value"].([]any)
				if !ok {
					return
				}
				sites = append(sites, membershipSite{path: path, operator: operator, owner: owner, operands: operands})
			}
		})
	})
	return sites
}

// deriveMembers derives one candidate per stated operand of every point
// predicate. It invents nothing: every value it places is a value the pack
// document itself carries, which is why this dimension is in scope where
// synthesizing a non-member is not.
func (set *candidateSet) deriveMembers(pack map[string]any) {
	for _, site := range membershipSites(pack) {
		if !set.admitPointer(site.path) {
			continue
		}
		set.notePointer(site.path)
		for _, operand := range site.operands {
			rendered, err := encodeCandidateJSON(operand)
			if err != nil {
				set.skip("unencodable-operand",
					"An operand a point predicate states could not be rendered back as JSON, so no candidate places it. It is reported rather than dropped: a dimension left out in silence reads as one the pack never stated (ADR-0022's skipped-not-passed discipline).",
					capRendered(site.path))
				continue
			}
			if len(rendered) > maxCandidateLiteralBytes {
				set.skip("oversize-literal",
					fmt.Sprintf("A compared operand is longer than %d bytes. A value no reviewer can read is a candidate no reviewer can review, and ADR-0023 already bounds every authored string this family renders.", maxCandidateLiteralBytes),
					capRendered(site.path))
				continue
			}
			set.emit(candidateSeed{
				family:    "member",
				subject:   site.path,
				qualifier: string(rendered),
				rationale: fmt.Sprintf("Places %s at %s, a value stated in the operand %s compares with %s.",
					capRendered(site.path), capRendered(string(rendered)), site.owner, display.Sanitize(site.operator)),
				place: &placement{path: site.path, raw: rendered},
			})
		}
	}
}

// deriveAbsences derives the negative witness as an ABSENCE — the pointer
// simply not present in the facts — rather than as an invented non-member
// value. Absence is a stated possibility that invents nothing and drives §7's
// unknown path; a synthesized non-member would be the one place this generator
// made up a value, and there is no dimension it would reach that absence does
// not.
//
// With a base row there is one absence per consulted pointer, each saying "this
// reviewed row, with that one answer withheld". Without a base the candidates
// carry only the varied pointer to begin with, so every per-pointer absence
// would be the same empty document; one candidate is emitted instead, and its
// rationale says which question it withholds.
func (set *candidateSet) deriveAbsences() {
	if len(set.pointers) == 0 {
		return
	}
	if !set.hasBase {
		set.emit(candidateSeed{
			family:    "absent",
			subject:   "",
			qualifier: "",
			rationale: fmt.Sprintf("States no facts at all, so every one of the %d pointer(s) this pack consults is unresolved — the stated possibility that drives §7's unknown path rather than either side of a comparison.", len(set.pointers)),
			place:     nil,
		})
		return
	}
	for _, path := range set.pointers {
		facts, outcome := removeFact(set.base, path)
		switch outcome {
		case removalBlocked:
			// A dimension this generator declined for a structural reason, on the
			// same footing as the unplaceable pointer emit() reports: it is named
			// rather than dropped, because a withholding missing in silence reads
			// as a pointer the pack never consults.
			set.skip("unremovable-pointer", unremovablePointerReason, capRendered(path))
			continue
		case removalAbsent:
			// The empty case, and correctly silent: the base row never stated this
			// pointer, so there is no answer to withhold and the candidate would be
			// the base row with a new id. Nothing was declined, so nothing is
			// reported.
			continue
		}
		set.emit(candidateSeed{
			family:    "absent",
			subject:   path,
			qualifier: "",
			rationale: fmt.Sprintf("Withholds %s from the base row's facts, leaving every comparison that reads it unresolved — the stated possibility that drives §7's unknown path rather than either side of a comparison.", capRendered(path)),
			facts:     facts,
		})
	}
}

// deriveEvidence derives the evidence-availability axis: three tri-states per
// declared requirement, with the facts held at the base assignment. It is a
// separate axis and is never crossed with the numeric lattice — crossing two
// one-factor axes is the cross product this design refuses.
func (set *candidateSet) deriveEvidence(pack map[string]any) {
	for _, requirement := range asObjects(pack["evidenceRequirements"]) {
		id, _ := requirement["id"].(string)
		if id == "" || len(id) > maxCandidateLiteralBytes {
			set.skip("unusable-evidence-id",
				fmt.Sprintf("A declared evidence requirement states no id, or states one longer than %d bytes — past what a reviewer can read and past the budget ADR-0023 renders authored strings under. Neither can be named in an evidenceAvailability a reviewer could check, so the axis is reported rather than silently left out.", maxCandidateLiteralBytes),
				capRendered(id))
			continue
		}
		required, _ := requirement["required"].(bool)
		noun := "an optional"
		if required {
			noun = "a required"
		}
		for _, state := range []string{"present", "absent", "unknown"} {
			availability, err := encodeCandidateJSON(map[string]string{id: state})
			if err != nil {
				set.skip("unencodable-candidate", unencodableCandidateReason, capRendered(id))
				continue
			}
			set.emit(candidateSeed{
				family:       "evidence",
				subject:      id,
				qualifier:    state,
				rationale:    fmt.Sprintf("Declares %s evidence requirement %s as %q, with the facts left at the base assignment. Evidence presence is an axis of the input beside the facts, so it is varied on its own rather than crossed with them.", noun, capRendered(id), state),
				availability: availability,
			})
		}
	}
}

// candidateSeed is one candidate before its id and its facts document are
// composed: which family it belongs to, what it is about, and how its facts
// differ from the base.
type candidateSeed struct {
	family    string
	subject   string
	qualifier string
	rationale string
	// place moves exactly one pointer, which is the one-factor-at-a-time rule
	// expressed in the type: a seed has one placement or none, never two.
	place *placement
	// facts, when set, is an already-composed document (the absence witnesses).
	facts        any
	availability json.RawMessage
}

// placement is the one pointer a candidate varies and the value it places
// there: a §2.2 decimal text, or an already-encoded JSON operand.
type placement struct {
	path  string
	value string
	raw   json.RawMessage
}

// unencodableCandidateReason is the one sentence a candidate that could not be
// rendered as JSON is declined with. It cannot arise from a value that came in
// through the carrier — every one of those is JSON that was already parsed once
// — and it is reported anyway, because a candidate dropped in silence looks
// exactly like a dimension the pack never stated (ADR-0022's skipped-not-passed
// discipline, which this record cites and therefore owes).
const unencodableCandidateReason = "A composed candidate could not be rendered as JSON, so it was not emitted. It is reported rather than dropped: a candidate missing in silence is indistinguishable from a dimension the pack never stated."

// emit composes one candidate's id, facts, and rationale and appends it.
func (set *candidateSet) emit(seed candidateSeed) {
	// Past the run's byte budget nothing more is composed. The refusal itself is
	// the caller's: this set reports what it derived, and Suggest answers the
	// crossed budget for the run as a whole.
	if set.budget.exceeded {
		return
	}
	document := set.base
	if !set.hasBase {
		document = map[string]any{}
	}
	switch {
	case seed.facts != nil:
		document = seed.facts
	case seed.place != nil:
		var value any
		if seed.place.raw != nil {
			// Read back through the carrier rather than through the plain
			// decoder: a number is kept as its authored digits, so an operand
			// the pack spells one way is placed in the facts the same way and
			// never through a float.
			decoded, failure := carrier.Decode(seed.place.raw, carrier.DefaultLimits())
			if failure != nil {
				set.skip("undecodable-operand",
					"An operand the pack states could not be read back through this runtime's carrier, so no candidate could place it in a facts document. It is reported rather than dropped, on ADR-0022's skipped-not-passed discipline.",
					capRendered(seed.place.path))
				return
			}
			value = decoded
		} else {
			value = seed.place.value
		}
		placed, ok := placeFact(document, set.hasBase, seed.place.path, value)
		if !ok {
			set.skip("unplaceable-pointer",
				"The base row's facts carry a value where the pointer needs a container, or an array position the pointer does not address. Replacing it would change the base beyond the one pointer a candidate varies, which is the property that makes a candidate reviewable.",
				capRendered(seed.place.path))
			return
		}
		document = placed
	}
	facts, err := encodeCandidateJSON(document)
	if err != nil {
		set.skip("unencodable-candidate", unencodableCandidateReason, capRendered(seed.subject))
		return
	}
	composed := Candidate{
		ID:                   set.uniqueID(seed),
		Origin:               CandidateOrigin,
		Facts:                facts,
		EvidenceAvailability: seed.availability,
		Rationale:            seed.rationale + candidateRationaleClose,
	}
	// Measured as the writer will write it rather than as it was composed. The
	// two differ by more than a constant: EncodeCandidates indents, so every
	// container level adds two spaces to every line beneath it, and a facts
	// document a hundred levels deep wrapping very many small tokens — legal on
	// every carrier bound — is under a megabyte composed and hundreds of
	// megabytes written.
	written, err := candidateWrittenBytes(composed)
	if err != nil {
		set.skip("unencodable-candidate", unencodableCandidateReason, capRendered(seed.subject))
		return
	}
	// Charged before the candidate is kept, so the run holds at most the budget
	// plus the one candidate that crossed it rather than everything a large or a
	// deep base row would multiply out to, and the refusal fires before the
	// document it is about could exist.
	if !set.budget.charge(written + candidateEnvelopeBytes) {
		return
	}
	set.candidates = append(set.candidates, composed)
}

// byteCounter counts what is written through it and keeps none of it.
type byteCounter struct{ count int }

func (counter *byteCounter) Write(data []byte) (int, error) {
	counter.count += len(data)
	return len(data), nil
}

// candidateWrittenBytes measures one candidate exactly as EncodeCandidates will
// write it: the same escaping, the same indent step, and the indentation prefix
// the candidate's own nesting inside the document carries — an element of the
// candidates array, which is itself a member of the root object, so two steps.
//
// The measurement goes through a counter rather than a buffer because the
// budget wants the number and not the bytes, and the number is what a run may
// have to reject.
func candidateWrittenBytes(candidate Candidate) (int, error) {
	counter := &byteCounter{}
	encoder := json.NewEncoder(counter)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent(candidateIndentPrefix, candidateIndentStep)
	if err := encoder.Encode(candidate); err != nil {
		return 0, err
	}
	return counter.count, nil
}

// uniqueID composes one candidate's id: the command that produced it, the
// project's decision id, the family, and what the candidate is about — each
// authored part rendered under ADR-0023's budget, so an unbounded pointer or
// decision id cannot produce an unbounded row id. A collision is broken by a
// counter rather than by dropping a candidate, because two candidates that
// collide are two different inputs and losing one silently is the failure this
// surface exists to avoid.
func (set *candidateSet) uniqueID(seed candidateSeed) string {
	parts := []string{"suggest", capRendered(set.decisionID), seed.family}
	if seed.subject != "" {
		parts = append(parts, capRendered(seed.subject))
	}
	if seed.qualifier != "" {
		parts = append(parts, capRendered(seed.qualifier))
	}
	id := strings.Join(parts, ":")
	candidate := id
	for counter := 2; set.ids[candidate]; counter++ {
		candidate = id + "#" + strconv.Itoa(counter)
	}
	set.ids[candidate] = true
	return candidate
}

// admitPointer reports whether a candidate can be composed for one pointer at
// all, recording why not when it cannot. The root pointer is the one refusal:
// a condition reading "" compares the whole facts document, and placing a
// decimal there would make the document itself a decimal string — an input no
// other candidate could share a base with and one no base row could carry.
func (set *candidateSet) admitPointer(path string) bool {
	tokens, rooted := evaluation.PointerTokens(path)
	if !rooted {
		set.skip("unaddressable-pointer",
			"A condition names a path that is neither empty nor rooted at \"/\", so RFC 6901 resolves it against nothing and no facts document can place a value at it.",
			capRendered(path))
		return false
	}
	if len(tokens) == 0 {
		set.skip("root-pointer",
			"A condition compares the whole facts document at the root pointer. Placing a value there would make the document itself that value, so the candidate could hold no other pointer at a base assignment and the one-factor-at-a-time rule could not be stated of it.",
			capRendered(path))
		return false
	}
	if len(path) > maxCandidateLiteralBytes {
		set.skip("oversize-pointer",
			fmt.Sprintf("A consulted pointer is longer than %d bytes, past what a reviewer can read and past the budget ADR-0023 renders authored strings under.", maxCandidateLiteralBytes),
			capRendered(path))
		return false
	}
	return true
}

// placeFact returns the base document with value placed at pointer, leaving the
// base itself untouched: every container on the path is copied rather than
// written through, so one base row's decoded facts serve every candidate of a
// run without any of them seeing another's placement.
//
// Where the path runs past what the base states, the missing containers are
// created as objects — the one structure an RFC 6901 token resolves against
// without the generator guessing at an array's length. Where the base states a
// scalar the path needs to descend through, the placement is refused rather
// than overwriting it: overwriting would change the base beyond the single
// pointer the candidate varies, which is the property that makes a candidate
// reviewable against the row it came from.
func placeFact(base any, hasBase bool, pointer string, value any) (any, bool) {
	tokens, rooted := evaluation.PointerTokens(pointer)
	if !rooted || len(tokens) == 0 {
		return nil, false
	}
	if !hasBase {
		base = map[string]any{}
	}
	return placeAt(base, true, tokens, value)
}

func placeAt(current any, present bool, tokens []string, value any) (any, bool) {
	if len(tokens) == 0 {
		return value, true
	}
	token := tokens[0]
	if !present || current == nil {
		child, ok := placeAt(nil, false, tokens[1:], value)
		if !ok {
			return nil, false
		}
		return map[string]any{token: child}, true
	}
	switch container := current.(type) {
	case map[string]any:
		existing, has := container[token]
		child, ok := placeAt(existing, has, tokens[1:], value)
		if !ok {
			return nil, false
		}
		next := make(map[string]any, len(container)+1)
		for key, member := range container {
			next[key] = member
		}
		next[token] = child
		return next, true
	case []any:
		index, err := strconv.Atoi(token)
		if err != nil || index < 0 || index >= len(container) {
			return nil, false
		}
		child, ok := placeAt(container[index], true, tokens[1:], value)
		if !ok {
			return nil, false
		}
		next := slices.Clone(container)
		next[index] = child
		return next, true
	default:
		return nil, false
	}
}

// removalOutcome says whether a withholding was composed and, when it was not,
// WHY. The two failures are different things and a single false conflated them:
// a pointer the base never states has nothing to withhold, which is an empty
// result and correctly silent, while an array on the pointer's path is a
// dimension this generator declined for a structural reason and therefore owes
// a report (ADR-0022's skipped-not-passed discipline, which ADR-0024 cites).
type removalOutcome int

const (
	// removalDone: the member was removed and the returned document is the base
	// with that one answer withheld.
	removalDone removalOutcome = iota
	// removalAbsent: the base does not state the pointer, so there is nothing to
	// withhold — a candidate here would be the base row with a new id.
	removalAbsent
	// removalBlocked: an array lies on the pointer's path, which this generator
	// declines to remove through.
	removalBlocked
)

// unremovablePointerReason is the one sentence a withholding declined by an
// array on the pointer's path is reported with. It is the absence axis's
// counterpart to emit()'s unplaceable-pointer skip: both are a base row whose
// shape this generator will not vary one factor of, and both are named in the
// report rather than left out, because a dimension missing in silence reads as
// one the pack never stated.
const unremovablePointerReason = "The base row's facts state a JSON array on the pointer's path. An array member cannot be withheld without renumbering every element past it — which changes more of the base than the one answer a withholding withholds, and the base being otherwise untouched is the property that makes a candidate reviewable — so no absence witness is composed for a pointer that runs through one."

// removeFact returns the base document with the pointer's member removed, and
// the outcome saying whether it was removed and why not when it was not.
func removeFact(base any, pointer string) (any, removalOutcome) {
	tokens, rooted := evaluation.PointerTokens(pointer)
	if !rooted || len(tokens) == 0 {
		return nil, removalAbsent
	}
	return removeAt(base, tokens)
}

func removeAt(current any, tokens []string) (any, removalOutcome) {
	container, ok := current.(map[string]any)
	if !ok {
		if _, isArray := current.([]any); isArray {
			return nil, removalBlocked
		}
		// A scalar or nothing where the pointer needs a container: the pointer
		// does not resolve in this base at all, so there is no answer to withhold.
		return nil, removalAbsent
	}
	token := tokens[0]
	existing, has := container[token]
	if !has {
		return nil, removalAbsent
	}
	next := make(map[string]any, len(container))
	for key, member := range container {
		next[key] = member
	}
	if len(tokens) == 1 {
		delete(next, token)
		return next, removalDone
	}
	child, outcome := removeAt(existing, tokens[1:])
	if outcome != removalDone {
		return nil, outcome
	}
	next[token] = child
	return next, removalDone
}

// encodeCandidateJSON renders one decoded value back to JSON bytes with HTML
// escaping off, matching every other document this runtime writes. Go's encoder
// orders object members by key, so the same value renders the same bytes on
// every run and in every process.
func encodeCandidateJSON(value any) (json.RawMessage, error) {
	var rendered bytes.Buffer
	encoder := json.NewEncoder(&rendered)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimRight(rendered.Bytes(), "\n")), nil
}

// MatrixOrigins counts one matrix's rows by the origin each declares, in sorted
// order of the origin, for the packs test report (ADR-0024). It counts and
// never gates: the count is what makes a suite mostly supplied by a generator
// visible to a reviewer and to CI, and a gate on it would be defeated by
// deleting one member — which would also destroy the count.
func MatrixOrigins(matrix Matrix) []result.OriginCount {
	counts := map[string]int{}
	for _, row := range matrix.Cases {
		if row.Origin == "" {
			continue
		}
		counts[row.Origin]++
	}
	if len(counts) == 0 {
		return nil
	}
	origins := make([]string, 0, len(counts))
	for origin := range counts {
		origins = append(origins, origin)
	}
	slices.Sort(origins)
	rendered := make([]result.OriginCount, 0, len(origins))
	for _, origin := range origins {
		rendered = append(rendered, result.OriginCount{Origin: capRendered(origin), Rows: counts[origin]})
	}
	return rendered
}
