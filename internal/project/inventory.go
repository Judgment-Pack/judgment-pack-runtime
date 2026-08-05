package project

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// identity is what one pack document says about itself: the two identity members
// §§3.1–3.2 require, the specification version it declares, the ids of the
// evidence it requires, and the fact pointers its conditions read. Everything an
// inventory reports about a pack comes from here rather than from the
// configuration, because the document is where a pack's identity is stated and
// the configuration only points at it.
//
// FactPaths serves two consumers from one derivation: packs validate checks the
// configuration's hint keys against it instead of taking them on trust — the
// same one-statement discipline expectedVersion follows — and the inventory
// reports it as consultedFactPaths, so an application can name the pointer an
// escalation is waiting on and check that every consulted pointer has a
// producer, without re-walking the document with a narrower walk (ADR-0020).
type identity struct {
	ID                   string
	Version              string
	SpecVersion          string
	EvidenceRequirements []string
	FactPaths            []string
}

// readIdentity reads one configured pack and pulls its own statement of identity
// out of it.
//
// It decodes with the same strict carrier rules a pack gets everywhere else —
// duplicate member names, depth, and size limits included — but it does not
// validate the document: listing a project's packs must work while one of them
// is mid-edit, and packs validate is the surface that says whether a pack is
// conformant. A document that cannot be decoded yields no identity at all rather
// than a partial one.
func (p *Project) readIdentity(entry Pack) (identity, []byte, error) {
	data, err := p.ReadPack(entry)
	if err != nil {
		return identity{}, nil, err
	}
	found, err := identityFrom(data)
	return found, data, err
}

// identityFrom pulls one pack's identity out of bytes already read, so a caller
// that holds the document does not read it a second time — and cannot report a
// verdict about one revision of a file and an identity from another.
func identityFrom(data []byte) (identity, error) {
	document, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return identity{}, fmt.Errorf("pack document is not acceptable JSON: %s", carrierFailure.Diagnostic.Message)
	}
	root, ok := document.(map[string]any)
	if !ok {
		return identity{}, fmt.Errorf("pack document root must be a JSON object")
	}
	found := identity{
		ID:                   stringMember(root, "id"),
		Version:              stringMember(root, "version"),
		SpecVersion:          stringMember(root, "specVersion"),
		EvidenceRequirements: []string{},
		FactPaths:            factPaths(root),
	}
	if requirements, ok := root["evidenceRequirements"].([]any); ok {
		for _, item := range requirements {
			if requirement, ok := item.(map[string]any); ok {
				if id := stringMember(requirement, "id"); id != "" {
					found.EvidenceRequirements = append(found.EvidenceRequirements, id)
				}
			}
		}
	}
	return found, nil
}

// factPaths collects every JSON Pointer the document's conditions read: the path
// member of every condition object, wherever it sits — applicability, a rule's
// when, an exception, or inside a nested collection quantifier.
//
// It walks the whole document rather than the members this runtime knows,
// because the point is to find every pointer a pack reads and a walk keyed to
// one grammar would miss the next one. A condition is recognized by carrying
// both op and a string path, which is what every fact-reading condition shape
// has in common.
//
// The result is sorted and deduplicated: a pointer two conditions read is one
// consulted pointer, and the one list serves both packs validate's hint check
// and the inventory's consultedFactPaths member.
func factPaths(document any) []string {
	found := []string{}
	var walk func(node any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if _, isCondition := value["op"]; isCondition {
				if path, ok := value["path"].(string); ok && path != "" {
					found = append(found, path)
				}
			}
			for _, member := range value {
				walk(member)
			}
		case []any:
			for _, item := range value {
				walk(item)
			}
		}
	}
	walk(document)
	sort.Strings(found)
	return slices.Compact(found)
}

func stringMember(root map[string]any, name string) string {
	value, _ := root[name].(string)
	return value
}

// EmptyInventory is the answer a surface gives when no configuration is at the
// resolved location. It is a result and not an error: a project that does not use
// this convention is an ordinary project, and a caller — a model, most often —
// needs to be told where the runtime looked and what would make packs appear,
// not handed a failure it will retry.
func EmptyInventory(configPath, command string) result.PackInventory {
	return result.PackInventory{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "none",
		Kind:          result.ProjectKind,
		ConfigPath:    configPath,
		Note: fmt.Sprintf("No project configuration was found at %s, so this project declares no packs. The convention is optional: a %s at the project root, or the %s environment variable, makes one. Pass pack documents directly instead.",
			display.Sanitize(configPath), DefaultConfigName, ConfigEnv),
		Packs: []result.PackSummary{},
	}
}

// Inventory is the resolved, token-cheap description of every configured pack:
// the project's decision id, the pack document's own id and version, the
// description, whether a matrix exists, the ids of the evidence the pack
// requires, and the non-normative hints. It is what an agent reads to find out
// what a project can decide without fetching a single pack document.
//
// A pack whose document cannot be read is listed with its Detail saying why and
// its identity members empty. Listing is not validating: an inventory that
// vanished because one pack is mid-edit would be useless exactly when it is most
// needed, and packs validate is where a broken pack is an error.
func (p *Project) Inventory(command string) result.PackInventory {
	output := result.PackInventory{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "valid",
		Kind:          result.ProjectKind,
		ConfigPath:    p.ConfigPath,
		ConfigVersion: p.Config.ConfigVersion,
		Packs:         make([]result.PackSummary, 0, len(p.IDs)),
	}
	for _, id := range p.IDs {
		entry := p.Config.Packs[id]
		summary := result.PackSummary{
			ID:                    id,
			Path:                  entry.Path,
			MatrixPath:            entry.Matrix,
			Matrix:                entry.Matrix != "",
			Description:           entry.Description,
			ExpectedVersion:       entry.ExpectedVersion,
			ExpectedVersionStatus: result.PackVersionUnset,
			EvidenceRequirements:  []string{},
			ConsultedFactPaths:    []string{},
			Facts:                 hints(entry.Facts),
			Evidence:              hints(entry.Evidence),
		}
		found, _, err := p.readIdentity(entry)
		if err != nil {
			summary.Detail = ReadFailureMessage(entry.Path, err)
			if entry.ExpectedVersion != "" {
				summary.ExpectedVersionStatus = result.PackVersionUnknown
			}
			output.Packs = append(output.Packs, summary)
			continue
		}
		summary.PackID = found.ID
		summary.PackVersion = found.Version
		summary.EvidenceRequirements = found.EvidenceRequirements
		summary.ConsultedFactPaths = found.FactPaths
		summary.ExpectedVersionStatus = expectedVersionStatus(entry.ExpectedVersion, found.Version)
		output.Packs = append(output.Packs, summary)
	}
	return output
}

// expectedVersionStatus compares a configuration's pin against the pack
// document's own version member. The pin is never preferred: a difference is a
// difference, reported as one, and the document is what a pack is.
func expectedVersionStatus(expected, actual string) string {
	switch {
	case expected == "":
		return result.PackVersionUnset
	case actual == "":
		return result.PackVersionUnknown
	case expected == actual:
		return result.PackVersionMatches
	default:
		return result.PackVersionDiffers
	}
}

// hints flattens one hint map into a sorted list, so two runs over the same
// configuration produce the same payload rather than Go's map order.
func hints(declared map[string]Hint) []result.ProjectHint {
	keys := make([]string, 0, len(declared))
	for key := range declared {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	output := make([]result.ProjectHint, 0, len(keys))
	for _, key := range keys {
		output = append(output, result.ProjectHint{
			Key:    key,
			Source: declared[key].Source,
			Hint:   declared[key].Hint,
		})
	}
	return output
}

// Document returns one configured pack's exact document bytes and the metadata
// beside them. The bytes are the file's, unaltered: this runtime serves a pack a
// project already holds and never a pack of its own (ADR-0006).
//
// Bytes that were read and did not decode are still served — the caller asked
// for the project's file and gets it — but the payload says so: the status is
// "undecodable", Detail carries the reason, and the identity members stay empty
// rather than being reported as an identity the document does not state. The
// inventory reports the same condition the same way; two surfaces reading one
// file must not disagree about it.
func (p *Project) Document(id, command string) (result.PackDocument, []byte, *Failure) {
	entry, ok := p.Entry(id)
	if !ok {
		return result.PackDocument{}, nil, p.UnknownPackFailure(id)
	}
	found, data, err := p.readIdentity(entry)
	if err != nil && data == nil {
		return result.PackDocument{}, nil, &Failure{
			Code:     "JPS-PROJECT-PACK-READ",
			Message:  ReadFailureMessage(entry.Path, err),
			ExitCode: result.ExitIO,
		}
	}
	status, detail := "valid", ""
	if err != nil {
		status, detail = "undecodable", ReadFailureMessage(entry.Path, err)
	}
	sum := sha256.Sum256(data)
	return result.PackDocument{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        status,
		Kind:          result.ProjectKind,
		ConfigPath:    p.ConfigPath,
		ID:            id,
		PackID:        found.ID,
		PackVersion:   found.Version,
		SpecVersion:   found.SpecVersion,
		Path:          entry.Path,
		Description:   entry.Description,
		Bytes:         len(data),
		SHA256:        hex.EncodeToString(sum[:]),
		Detail:        detail,
	}, data, nil
}
