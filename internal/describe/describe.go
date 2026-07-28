// Package describe composes machine-readable descriptions of this runtime and
// the specification artifacts embedded in it.
//
// It holds no CLI, transport, or presentation concerns, so every surface that
// reports these values -- the CLI today, other adapters later -- produces the
// same payload rather than a per-surface reimplementation of it.
package describe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Runtime composes the runtime-description payload for a loaded artifact set.
//
// command names the operation reporting the payload so a surface can identify
// itself without forking the shape of the result.
func Runtime(set *artifacts.Set, command string) result.Version {
	return result.Version{
		OutputVersion:      result.OutputVersion,
		Tool:               result.CurrentTool(),
		Command:            command,
		Status:             "valid",
		SupportedSpecs:     artifacts.SupportedVersions(),
		ArtifactProvenance: bundledProvenance(set),
	}
}

// bundledProvenance describes every bundled version rather than only the one
// passed. The payload carries one provenance string for a list of supported
// versions, so reporting the given set's kind alone would hide a development
// snapshot bundled for another version. When the locks disagree, the
// least-trusted kind is reported, which is the one a release gate acts on.
func bundledProvenance(set *artifacts.Set) string {
	provenance := set.Lock().Source.Kind
	for _, version := range artifacts.SupportedVersions() {
		other, err := artifacts.Load(version)
		if err != nil {
			// Unreachable in practice: every surface reaching here has already
			// loaded and verified every bundled version. A version that cannot be
			// loaded at all is not evidence about provenance either way.
			continue
		}
		if kind := other.Lock().Source.Kind; provenanceRank(kind) > provenanceRank(provenance) {
			provenance = kind
		}
	}
	return provenance
}

// provenanceRank orders the artifact source kinds the importer mints, most
// trusted first, so bundledProvenance reports the least-trusted kind among the
// bundles rather than whichever one it happened to visit last. A kind this
// runtime does not know is ranked below both: an unrecognized provenance is not
// evidence of a trustworthy one, and a release gate should see it.
func provenanceRank(kind string) int {
	switch kind {
	case "immutable-git-ref":
		return 0
	case "unreleased-local-snapshot":
		return 1
	default:
		return 2
	}
}

// Schema composes metadata describing the exact bundled schema bytes of
// specVersion: the schema's declared $id, its size, and its SHA-256.
//
// schemaBytes must be the original bundled bytes, not a re-encoding, because
// the reported size and digest describe those exact bytes. An error is returned
// only when they do not decode as a JSON object. Callers that write the schema
// out set WrittenTo on the returned value themselves; it is a property of the
// caller's action, not of the artifact.
func Schema(set *artifacts.Set, specVersion, command string, schemaBytes []byte) (result.Schema, error) {
	var document map[string]any
	if err := json.Unmarshal(schemaBytes, &document); err != nil {
		return result.Schema{}, err
	}
	sum := sha256.Sum256(schemaBytes)
	return result.Schema{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "valid",
		SpecVersion:   specVersion,
		SchemaID:      stringFrom(document["$id"]),
		Bytes:         len(schemaBytes),
		SHA256:        hex.EncodeToString(sum[:]),
		Provenance:    set.Lock().Source.Kind,
	}, nil
}

// Examples composes the catalog of bundled valid examples: the digest-locked
// conformance fixtures the set embeds, offered read-only. They are
// version-pinned corpus fixtures, not authored templates; the Kind field says
// so in-band.
func Examples(set *artifacts.Set, command string) (result.Examples, error) {
	infos, err := set.Examples()
	if err != nil {
		return result.Examples{}, err
	}
	summaries := make([]result.ExampleSummary, 0, len(infos))
	for _, info := range infos {
		summaries = append(summaries, result.ExampleSummary{
			Name:        info.Name,
			Focus:       info.Focus,
			SpecSection: info.SpecSection,
		})
	}
	return result.Examples{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "valid",
		SpecVersion:   set.Lock().SpecVersion,
		Provenance:    set.Lock().Source.Kind,
		Kind:          result.ExampleKind,
		Examples:      summaries,
	}, nil
}

// Example composes metadata for one bundled valid example and returns that
// example's exact bundled document bytes alongside it. The reported size and
// digest describe those exact bytes. An unknown name yields
// *artifacts.UnknownExampleError from the underlying set.
func Example(set *artifacts.Set, name, command string) (result.Example, []byte, error) {
	data, info, err := set.Example(name)
	if err != nil {
		return result.Example{}, nil, err
	}
	sum := sha256.Sum256(data)
	return result.Example{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "valid",
		SpecVersion:   set.Lock().SpecVersion,
		Name:          info.Name,
		Focus:         info.Focus,
		SpecSection:   info.SpecSection,
		Bytes:         len(data),
		SHA256:        hex.EncodeToString(sum[:]),
		Provenance:    set.Lock().Source.Kind,
		Kind:          result.ExampleKind,
	}, data, nil
}

func stringFrom(value any) string {
	text, _ := value.(string)
	return text
}
