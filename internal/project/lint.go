package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

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

// Producers is the explicit manifest a caller may hand packs lint: the fact
// pointers an application can actually produce and the evidence ids it
// supplies. Absent, the lint reads the configuration's own hints as the
// producer declaration — a hint is the project saying "this is where that
// answer lives", which is a producer claim in the project's own words.
type Producers struct {
	ProducersVersion string   `json:"producersVersion"`
	Facts            []string `json:"facts"`
	Evidence         []string `json:"evidence"`
}

// DecodeProducers reads one manifest strictly: unknown members are refused
// the way every closed input here refuses them, and the version is held to
// the one shape this runtime knows.
func DecodeProducers(data []byte) (*Producers, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Producers
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("the producer manifest is not the documented shape: %v", err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("the producer manifest carries trailing content after the document")
	}
	if manifest.ProducersVersion != ProducersVersion {
		return nil, fmt.Errorf("producersVersion %q is not %q, the one shape this runtime reads", manifest.ProducersVersion, ProducersVersion)
	}
	if manifest.Facts == nil {
		manifest.Facts = []string{}
	}
	if manifest.Evidence == nil {
		manifest.Evidence = []string{}
	}
	return &manifest, nil
}

// producesPointer is readsPointer's inverse: whether some producer supplies
// this consulted pointer — the producer itself, or an ancestor of it, since
// a producer that writes a subtree answers every pointer beneath it.
func producesPointer(producers []string, consulted string) bool {
	for _, producer := range producers {
		if producer == consulted || strings.HasPrefix(consulted, producer+"/") {
			return true
		}
	}
	return false
}

// usesQuantifiers reports whether the document carries a draft-RFC collection
// quantifier: a condition-shaped node that also carries `where` or `at`. The
// flat consulted-pointer list reports such a pack's element-relative pointers
// without their element context (ADR-0020's recorded narrowness), so the lint
// must say it cannot check that pack rather than fail it on pointers no flat
// producer set could name — or pass it on a list it knows is untrustworthy.
func usesQuantifiers(document any) bool {
	switch value := document.(type) {
	case map[string]any:
		if _, isCondition := value["op"]; isCondition {
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
// producer, and whether declared and supplied evidence agree in both
// directions. It never gates what validate already gates: a document that
// cannot be read skips its checks here with the reason, because validate is
// where a broken pack is an error, and two commands failing one defect would
// report it twice in two vocabularies.
//
// The run's status follows the packs test discipline: "passed" only when a
// check passed somewhere, "failed" on any failed check, and "skipped" when
// nothing was checkable — a green lint over zero checks would say a project
// was linted when nothing was.
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
	anyPassed, anyFailed := false, false
	for _, packID := range selected {
		entry := p.lintPack(manifest, packID, p.Config.Packs[packID])
		output.Summary.Total++
		switch entry.Status {
		case "passed":
			anyPassed = true
			output.Summary.Passed++
		case "failed":
			anyFailed = true
			output.Summary.Failed++
		}
		output.Packs = append(output.Packs, entry)
	}
	switch {
	case anyFailed:
		output.Status = "failed"
	case anyPassed:
		output.Status = "passed"
	}
	return output, nil
}

func (p *Project) lintPack(manifest *Producers, id string, entry Pack) result.PackProducersLintEntry {
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
		detail := "the document could not be read, which packs validate reports; nothing here was checked: " + ReadFailureMessage(entry.Path, err)
		add(CheckFactProducers, result.PackCheckSkipped, detail)
		add(CheckEvidenceProducers, result.PackCheckSkipped, detail)
		return report
	}
	report.PackID = found.ID
	report.PackVersion = found.Version

	factProducers := sortedKeys(entry.Facts)
	evidenceProducers := sortedKeys(entry.Evidence)
	if manifest != nil {
		factProducers = slices.Clone(manifest.Facts)
		evidenceProducers = slices.Clone(manifest.Evidence)
		slices.Sort(factProducers)
		slices.Sort(evidenceProducers)
	}

	// The fact half. A pack using draft-RFC quantifiers is not checkable
	// against a flat producer set, and saying so beats either verdict.
	document, decodeErr := decodeRoot(data)
	if decodeErr == nil && usesQuantifiers(document) {
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
					strings.Join(missing, ", ")+". Declare the producer, or fix the pack.")
		} else if len(found.FactPaths) == 0 {
			add(CheckFactProducers, result.PackCheckSkipped, "The pack consults no fact pointers.")
		} else {
			add(CheckFactProducers, result.PackCheckPassed, "")
		}
	}

	// The evidence half, both directions: a declared requirement nobody
	// supplies, and a supplied id the pack never declared.
	problems := []string{}
	declared := slices.Clone(found.EvidenceRequirements)
	slices.Sort(declared)
	declared = slices.Compact(declared)
	for _, requirement := range declared {
		if !slices.Contains(evidenceProducers, requirement) {
			problems = append(problems, fmt.Sprintf("the declared evidence requirement %q has no supplier", requirement))
		}
	}
	for _, supplied := range evidenceProducers {
		if !slices.Contains(declared, supplied) {
			problems = append(problems, fmt.Sprintf("the supplied evidence id %q names no requirement the pack declares", supplied))
		}
	}
	switch {
	case len(problems) > 0:
		add(CheckEvidenceProducers, result.PackCheckFailed, strings.Join(problems, "; ")+".")
	case len(declared) == 0 && len(evidenceProducers) == 0:
		add(CheckEvidenceProducers, result.PackCheckSkipped, "The pack declares no evidence requirements and nothing supplies any.")
	default:
		add(CheckEvidenceProducers, result.PackCheckPassed, "")
	}
	return report
}

// decodeRoot re-decodes the already-read pack bytes for the quantifier scan.
// readIdentity decoded them once; holding the decoded form on identity for
// one extra caller would grow every inventory for this command's benefit.
func decodeRoot(data []byte) (any, error) {
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return document, nil
}
