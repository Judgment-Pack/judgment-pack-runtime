package dialect

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

const MaxPackBytes = carrier.HardMaxBytes
const MaxCoverageComparisons int64 = 1000000
const MaxCoverageReportBytes = 2 * 1024 * 1024

type PackIdentity struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	SpecVersion string `json:"specVersion"`
	SHA256      string `json:"sha256"`
}

type CoverageEntry struct {
	Target         string   `json:"target"`
	Status         string   `json:"status"`
	Producers      []string `json:"producers"`
	AcknowledgedBy []string `json:"acknowledgedBy"`
}

type CoverageNote struct {
	Code     string `json:"code"`
	Kind     string `json:"kind"`
	Target   string `json:"target"`
	Severity string `json:"severity"`
}

type Compatibility struct {
	Scope                     string          `json:"scope"`
	Status                    string          `json:"status"`
	Pack                      PackIdentity    `json:"pack"`
	InventoryOverApproximates bool            `json:"inventoryOverApproximates"`
	ValueTypesChecked         bool            `json:"valueTypesChecked"`
	ExecutionChecked          bool            `json:"executionChecked"`
	Facts                     []CoverageEntry `json:"facts"`
	Evidence                  []CoverageEntry `json:"evidence"`
	Notes                     []CoverageNote  `json:"notes"`
}

// Check compares two bounded supplied snapshots. It validates each document,
// then compares declared targets to Runtime's existing pack inventory. A mapped
// path does not prove that a source will run, return that value, or return a
// comparable type. Pack digest is over its exact supplied bytes, not JCS.
func Check(command string, data, pack []byte, engine *validation.Engine) (Report, int) {
	// Bound both operands before parsing either one.
	if len(data) > MaxDocumentBytes {
		return refused(report(command), "JPS-DIALECT-BYTE-LIMIT", "", "The dialect exceeds the document byte limit.", result.ExitInvalid)
	}
	if int64(len(pack)) > MaxPackBytes {
		return refused(report(command), "JPS-DIALECT-PACK-BYTE-LIMIT", "", "The pack exceeds the document byte limit.", result.ExitInvalid)
	}
	r, document, code := validateDocument(command, data)
	if code != 0 {
		return r, code
	}
	r.ValidationScope = "version-2-document-and-declared-pack-inventory"
	checked, failure := engine.Validate(pack, validation.Options{})
	r.PackValidation = &checked
	if failure != nil {
		return refused(r, "JPS-DIALECT-PACK-READ", "", "The pack exceeds the supported carrier limits.", result.ExitInvalid)
	}
	if checked.Status != "valid" {
		code = result.ExitInvalid
		if checked.Status == "unsupported" {
			code = result.ExitUnsupported
		}
		return refused(r, "JPS-DIALECT-PACK-INVALID", "", "Pack validation did not pass; no mapping coverage was checked.", code)
	}
	inventory, err := project.InspectDocument(pack)
	if err != nil {
		return refused(r, "JPS-DIALECT-PACK-INVENTORY", "", "The pack inventory could not be read.", result.ExitInvalid)
	}
	if inventory.UsesQuantifiers {
		return refused(r, "JPS-DIALECT-PACK-QUANTIFIERS", "", "Collection-relative reads cannot be checked against a flat mapping inventory.", result.ExitUnsupported)
	}
	for _, p := range inventory.FactPaths {
		if !pointer(p) {
			return refused(r, "JPS-DIALECT-PACK-INVENTORY", "", "The over-approximated pack inventory includes a non-pointer; it cannot be checked as mapping coverage.", result.ExitInvalid)
		}
	}
	// Bound the declared comparison work before producing any coverage result.
	work := int64(len(inventory.FactPaths))*int64(2*len(document.factTargets)+len(document.unmapped)) + int64(len(inventory.EvidenceRequirements))*int64(2*(len(document.evidenceTargets)+len(document.unmappedEvidence)))
	if work > MaxCoverageComparisons {
		return refused(r, "JPS-DIALECT-COVERAGE-LIMIT", "", "The declared inventory exceeds this tool's coverage comparison limit; no partial coverage is reported.", result.ExitUnsupported)
	}
	sum := sha256.Sum256(pack)
	coverage := &Compatibility{Scope: "declared-inventory-coverage", Status: "complete", Pack: PackIdentity{inventory.ID, inventory.Version, inventory.SpecVersion, hex.EncodeToString(sum[:])}, InventoryOverApproximates: true, Facts: []CoverageEntry{}, Evidence: []CoverageEntry{}, Notes: []CoverageNote{}}
	outputLimit := func() (Report, int) {
		return refused(r, "JPS-DIALECT-COVERAGE-OUTPUT-LIMIT", "", "The declared coverage report exceeds this tool's output limit; no partial coverage is reported.", result.ExitUnsupported)
	}
	budget := newCoverageBudget(coverage, MaxCoverageReportBytes)
	if budget.exceeded() {
		return outputLimit()
	}
	add := func(code, kind, target, severity string) bool {
		note := CoverageNote{code, kind, target, severity}
		if !budget.note(note) {
			return false
		}
		coverage.Notes = append(coverage.Notes, note)
		if severity == "error" {
			coverage.Status = "incomplete"
		} else if coverage.Status != "incomplete" {
			coverage.Status = "review-required"
		}
		return true
	}
	usedOmissions := map[string]bool{}
	for _, p := range inventory.FactPaths {
		entry := CoverageEntry{Target: p, Status: "missing", Producers: []string{}, AcknowledgedBy: []string{}}
		descendants := []string{}
		for _, t := range document.factTargets {
			if project.CoversFactPath(t, p) {
				entry.Producers = append(entry.Producers, t)
			} else if project.CoversFactPath(p, t) {
				descendants = append(descendants, t)
			}
		}
		for _, u := range document.unmapped {
			if project.CoversFactPath(u, p) {
				entry.AcknowledgedBy = append(entry.AcknowledgedBy, u)
				usedOmissions[u] = true
			}
		}
		slices.Sort(entry.AcknowledgedBy)
		switch {
		case len(entry.Producers) > 0:
			entry.Status = "mapped"
			if len(entry.AcknowledgedBy) > 0 {
				if !add("redundant-omission", "fact", p, "warning") {
					return outputLimit()
				}
			}
		case p == "" || len(descendants) > 0:
			entry.Status = "partial"
			entry.Producers = descendants
			if !add("aggregate-coverage", "fact", p, "warning") {
				return outputLimit()
			}
		case len(entry.AcknowledgedBy) > 0:
			entry.Status = "acknowledged"
			if !add("acknowledged-omission", "fact", p, "warning") {
				return outputLimit()
			}
		default:
			if !add("missing-producer", "fact", p, "error") {
				return outputLimit()
			}
		}
		if !budget.entry(entry) {
			return outputLimit()
		}
		coverage.Facts = append(coverage.Facts, entry)
	}
	for _, target := range document.factTargets {
		used := false
		for _, p := range inventory.FactPaths {
			if project.CoversFactPath(target, p) || project.CoversFactPath(p, target) {
				used = true
				break
			}
		}
		if !used {
			if !add("unconsulted-target", "fact", target, "warning") {
				return outputLimit()
			}
		}
	}
	omissions := slices.Clone(document.unmapped)
	slices.Sort(omissions)
	for _, p := range omissions {
		if !usedOmissions[p] {
			if !add("unused-omission", "fact", p, "warning") {
				return outputLimit()
			}
		}
	}
	requirements := slices.Clone(inventory.EvidenceRequirements)
	slices.Sort(requirements)
	requirements = slices.Compact(requirements)
	for _, requirement := range requirements {
		entry := CoverageEntry{Target: requirement, Status: "missing", Producers: []string{}, AcknowledgedBy: []string{}}
		if slices.Contains(document.evidenceTargets, requirement) {
			entry.Producers = append(entry.Producers, requirement)
		}
		if slices.Contains(document.unmappedEvidence, requirement) {
			entry.AcknowledgedBy = append(entry.AcknowledgedBy, requirement)
		}
		switch {
		case len(entry.Producers) > 0:
			entry.Status = "mapped"
			if len(entry.AcknowledgedBy) > 0 {
				if !add("redundant-omission", "evidence", requirement, "warning") {
					return outputLimit()
				}
			}
		case len(entry.AcknowledgedBy) > 0:
			entry.Status = "acknowledged"
			if !add("acknowledged-omission", "evidence", requirement, "warning") {
				return outputLimit()
			}
		default:
			if !add("missing-producer", "evidence", requirement, "error") {
				return outputLimit()
			}
		}
		if !budget.entry(entry) {
			return outputLimit()
		}
		coverage.Evidence = append(coverage.Evidence, entry)
	}
	for _, e := range document.evidenceTargets {
		if !slices.Contains(requirements, e) {
			if !add("undeclared-evidence-target", "evidence", e, "error") {
				return outputLimit()
			}
		}
	}
	omittedEvidence := slices.Clone(document.unmappedEvidence)
	slices.Sort(omittedEvidence)
	for _, e := range omittedEvidence {
		if !slices.Contains(requirements, e) {
			if !add("undeclared-evidence-omission", "evidence", e, "error") {
				return outputLimit()
			}
		}
	}
	if len(coverage.Facts) == 0 && len(coverage.Evidence) == 0 && coverage.Status == "complete" {
		coverage.Status = "not-applicable"
	}

	r.Compatibility = coverage
	r.Status = coverage.Status
	if coverage.Status == "incomplete" || coverage.Status == "not-applicable" {
		return r, result.ExitInvalid
	}
	return r, result.ExitSuccess
}
