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
		ArtifactProvenance: set.Lock().Source.Kind,
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
