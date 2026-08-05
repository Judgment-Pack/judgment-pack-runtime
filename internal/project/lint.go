package project

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The producer-lint checks (ADR-0022). Lint is the inverse of packs
// validate's hint-key check: validate holds every declared hint to the
// document, and lint holds every consulted pointer to a producer — the
// defect it catches otherwise presents as a system that looks conservative
// rather than broken, because a pointer no source feeds makes every rule
// touching it escalate without a single error.
const (
	CheckFactProducers     = "fact-producers"
	CheckEvidenceProducers = "evidence-producers"
	CheckQuantifierScope   = "quantifier-scope"
)

// ProducersVersion is the manifest shape this runtime reads. The manifest is
// a closed input, so its version moves on any member change, the same rule
// configVersion follows (VERSIONING.md).
const ProducersVersion = "1"

// MaxProducersBytes bounds one producer manifest. Like the configuration, it
// is an index — pointers and ids — not a document.
const MaxProducersBytes = MaxConfigBytes

// The manifest's value domains, held to the same patterns the configuration
// schema holds hint keys to: an entry that could never name a real pointer or
// a declarable requirement is a defect in the manifest, not a producer.
var (
	pointerPattern = regexp.MustCompile(`^(?:/(?:[^~/]|~0|~1)*)*$`)
	localIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
)

// Producers is the explicit manifest a caller may hand packs lint: the fact
// pointers an application can actually produce and the evidence ids it
// supplies. Absent, the lint reads the configuration's own hints as the
// producer declaration — a hint is the project saying "this is where that
// answer lives", which is a producer claim in the project's own words.
//
// A facts entry declares the pointer AND the subtree beneath it: "I produce
// the value at this pointer, whole." That subtree reading is the declared
// contract, not a runtime fact — the lint checks declarations, never systems.
type Producers struct {
	ProducersVersion string
	Facts            []string
	Evidence         []string
}

// DecodeProducers reads one manifest through the same strict carrier the
// documents go through — duplicate member names refused, one JSON text only —
// then holds the shape closed by hand: unknown members, non-string entries,
// and values outside the pointer and local-id domains are refused rather than
// silently ignored. Lists come back sorted and deduplicated.
func DecodeProducers(data []byte) (*Producers, error) {
	document, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return nil, fmt.Errorf("the producer manifest is not acceptable JSON: %s", carrierFailure.Diagnostic.Message)
	}
	root, ok := document.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("the producer manifest must be a JSON object")
	}
	for member := range root {
		switch member {
		case "producersVersion", "facts", "evidence":
		default:
			return nil, fmt.Errorf("the producer manifest carries an unknown member %q; it is a closed shape", member)
		}
	}
	version, _ := root["producersVersion"].(string)
	if version != ProducersVersion {
		return nil, fmt.Errorf("producersVersion %q is not %q, the one shape this runtime reads", version, ProducersVersion)
	}
	manifest := &Producers{ProducersVersion: version, Facts: []string{}, Evidence: []string{}}
	var err error
	if manifest.Facts, err = stringList(root, "facts", func(entry string) error {
		if !pointerPattern.MatchString(entry) {
			return fmt.Errorf("the facts entry %q is not an RFC 6901 pointer", entry)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if manifest.Evidence, err = stringList(root, "evidence", func(entry string) error {
		if !localIDPattern.MatchString(entry) {
			return fmt.Errorf("the evidence entry %q is not a declarable requirement id", entry)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return manifest, nil
}

func stringList(root map[string]any, member string, check func(string) error) ([]string, error) {
	value, present := root[member]
	if !present {
		return []string{}, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("the producer manifest's %q must be an array of strings", member)
	}
	list := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("the producer manifest's %q must be an array of strings", member)
		}
		if err := check(text); err != nil {
			return nil, err
		}
		list = append(list, text)
	}
	slices.Sort(list)
	return slices.Compact(list), nil
}

// producesPointer reports whether some producer covers this consulted
// pointer, in the two directions that mean two different things:
//
//   - the producer at the pointer, or at an ancestor of it: the DECLARED
//     subtree contract — the producer claims the whole value at its pointer,
//     descendants included (the claim the lint exists to check);
//   - the producer at a descendant of the pointer: structurally true —
//     writing /request/type necessarily creates /request, so a condition
//     reading the ancestor resolves.
//
// The empty string is the root pointer: as a producer it covers everything,
// and as a consulted pointer it is covered by any producer at all.
func producesPointer(producers []string, consulted string) bool {
	for _, producer := range producers {
		if producer == consulted ||
			strings.HasPrefix(consulted, producer+"/") ||
			strings.HasPrefix(producer, consulted+"/") {
			return true
		}
		if producer == "" || (consulted == "" && strings.HasPrefix(producer, "/")) {
			return true
		}
	}
	return false
}

// quantifierOps is the draft-RFC collection-quantifier operator set. The
// detection is deliberately keyed to these names, unlike ADR-0020's
// shape-keyed walk: a data literal that merely carries `where` must not
// silently suppress the fact gate, and a future quantifier operator this
// list does not know falls into the flat check and fails visibly there —
// the failure direction that gets noticed — rather than skipping silently.
var quantifierOps = map[string]bool{"exists": true, "every": true, "uniform": true}

// usesQuantifiers reports whether the document carries a draft-RFC
// collection quantifier: a node whose op is one of the known quantifier
// operators and which carries `where` or `at`. The flat consulted-pointer
// list reports such a pack's element-relative pointers without their element
// context (ADR-0020's recorded narrowness), so the lint says it cannot check
// that pack's fact half rather than fail it on pointers no flat producer set
// could name — or pass it on a list it knows is untrustworthy.
func usesQuantifiers(document any) bool {
	switch value := document.(type) {
	case map[string]any:
		if op, ok := value["op"].(string); ok && quantifierOps[op] {
			if _, hasWhere := value["where"]; hasWhere {
				return true
			}
			if _, hasAt := value["at"]; hasAt {
				return true
			}
		}
		for _, member := range value {
			if usesQuantifiers(member) {
				return true
			}
		}
	case []any:
		for _, item := range value {
			if usesQuantifiers(item) {
				return true
			}
		}
	}
	return false
}

// Lint reports whether every pointer the declared packs consult has a
// producer, and whether declared and supplied evidence agree. In hints mode
// both evidence directions are per pack, because hints are pack-local; in
// manifest mode the per-pack direction is "every declared requirement has a
// supplier", and the reverse — every supplied id is declared somewhere — is
// one configuration-level check, because one application-wide list held to
// every pack separately would fail any project whose packs declare different
// requirements.
//
// The run's status follows the packs test discipline: an unreadable document
// is a failure here exactly as it is there — skipping it would let a broken
// pack lint clean — "passed" requires a check to have passed somewhere, and a
// run in which nothing was checkable is "skipped", never passed.
func (p *Project) Lint(manifest *Producers, id, command string) (result.PackProducersLint, *Failure) {
	selected, failure := p.selection(id)
	if failure != nil {
		return result.PackProducersLint{}, failure
	}
	output := result.PackProducersLint{
		OutputVersion:   result.OutputVersion,
		Tool:            result.CurrentTool(),
		Command:         command,
		Status:          "skipped",
		Kind:            result.ProjectKind,
		ConfigPath:      p.ConfigPath,
		ConfigVersion:   p.Config.ConfigVersion,
		ProducersSource: "hints",
		Packs:           make([]result.PackProducersLintEntry, 0, len(selected)),
	}
	if manifest != nil {
		output.ProducersSource = "manifest"
	}
	declaredAnywhere := map[string]bool{}
	anyPassed, anyFailed := false, false
	for _, packID := range selected {
		entry, declared := p.lintPack(manifest, packID, p.Config.Packs[packID])
		for _, requirement := range declared {
			declaredAnywhere[requirement] = true
		}
		output.Summary.Total++
		switch entry.Status {
		case "passed":
			anyPassed = true
			output.Summary.Passed++
		case "failed":
			anyFailed = true
			output.Summary.Failed++
		default:
			output.Summary.Skipped++
		}
		output.Packs = append(output.Packs, entry)
	}
	if manifest != nil {
		orphaned := []string{}
		for _, supplied := range manifest.Evidence {
			if !declaredAnywhere[supplied] {
				orphaned = append(orphaned, fmt.Sprintf("%q", supplied))
			}
		}
		check := result.PackCheck{Name: CheckEvidenceProducers, Status: result.PackCheckPassed}
		switch {
		case len(orphaned) > 0:
			check.Status = result.PackCheckFailed
			check.Detail = "The manifest supplies evidence no selected pack declares: " + strings.Join(orphaned, ", ") + ". Evidence ids are pack-local names; an id nothing declares supplies nothing."
			anyFailed = true
		case len(manifest.Evidence) == 0:
			check.Status = result.PackCheckSkipped
			check.Detail = "The manifest supplies no evidence."
		default:
			anyPassed = true
		}
		output.Checks = append(output.Checks, check)
	}
	switch {
	case anyFailed:
		output.Status = "failed"
	case anyPassed:
		output.Status = "passed"
	}
	return output, nil
}

func (p *Project) lintPack(manifest *Producers, id string, entry Pack) (result.PackProducersLintEntry, []string) {
	report := result.PackProducersLintEntry{ID: id, Path: entry.Path, Status: "skipped"}
	add := func(name, status, detail string) {
		report.Checks = append(report.Checks, result.PackCheck{Name: name, Status: status, Detail: detail})
		if status == result.PackCheckFailed {
			report.Status = "failed"
		}
		if status == result.PackCheckPassed && report.Status != "failed" {
			report.Status = "passed"
		}
	}

	found, data, err := p.readIdentity(entry)
	if err != nil {
		// The packs test discipline: an unreadable declared document fails
		// rather than skips — skipping it would let a broken pack lint clean.
		// packs validate is still where the read failure itself is diagnosed.
		add(CheckFactProducers, result.PackCheckFailed,
			"The document could not be read, so nothing it consults could be checked; packs validate diagnoses the read itself: "+ReadFailureMessage(entry.Path, err))
		add(CheckEvidenceProducers, result.PackCheckFailed,
			"The document could not be read, so its declared evidence could not be checked.")
		return report, nil
	}
	report.PackID = found.ID
	report.PackVersion = found.Version

	factProducers := sortedKeys(entry.Facts)
	evidenceProducers := sortedKeys(entry.Evidence)
	if manifest != nil {
		factProducers = manifest.Facts
		evidenceProducers = manifest.Evidence
	}

	// The fact half. A pack using draft-RFC quantifiers is not checkable
	// against a flat producer set, and saying so beats either verdict.
	document, decodeFailure := carrier.Decode(data, carrier.DefaultLimits())
	if decodeFailure == nil && usesQuantifiers(document) {
		add(CheckQuantifierScope, result.PackCheckSkipped,
			"The pack uses draft-RFC collection quantifiers, whose element-relative pointers a flat producer set cannot name (ADR-0020, ADR-0022); the fact half was not checked.")
	} else {
		missing := []string{}
		for _, consulted := range found.FactPaths {
			if !producesPointer(factProducers, consulted) {
				missing = append(missing, fmt.Sprintf("%q", consulted))
			}
		}
		if len(missing) > 0 {
			add(CheckFactProducers, result.PackCheckFailed,
				"A consulted pointer no producer supplies never errors at run time — the condition is unknowable, every rule touching it escalates, and the system looks conservative rather than broken. Unproduced: "+
					strings.Join(missing, ", ")+". The consulted list over-approximates by design (ADR-0020), so an entry here may be a condition-shaped value the pack carries as data: declare it as a producer to acknowledge it, restructure the value, or fix the pack.")
		} else if len(found.FactPaths) == 0 {
			add(CheckFactProducers, result.PackCheckSkipped, "The pack consults no fact pointers.")
		} else {
			add(CheckFactProducers, result.PackCheckPassed, "")
		}
	}

	// The evidence half. Every declared requirement needs a supplier in both
	// modes; the reverse direction is per pack only in hints mode, where the
	// declaration is pack-local by construction.
	problems := []string{}
	declared := slices.Clone(found.EvidenceRequirements)
	slices.Sort(declared)
	declared = slices.Compact(declared)
	for _, requirement := range declared {
		if !slices.Contains(evidenceProducers, requirement) {
			problems = append(problems, fmt.Sprintf("the declared evidence requirement %q has no supplier", requirement))
		}
	}
	if manifest == nil {
		for _, supplied := range evidenceProducers {
			if !slices.Contains(declared, supplied) {
				problems = append(problems, fmt.Sprintf("the supplied evidence id %q names no requirement the pack declares", supplied))
			}
		}
	}
	switch {
	case len(problems) > 0:
		add(CheckEvidenceProducers, result.PackCheckFailed, strings.Join(problems, "; ")+".")
	case len(declared) == 0 && (manifest != nil || len(evidenceProducers) == 0):
		add(CheckEvidenceProducers, result.PackCheckSkipped, "The pack declares no evidence requirements.")
	default:
		add(CheckEvidenceProducers, result.PackCheckPassed, "")
	}
	return report, declared
}
