package registry

import (
	"fmt"
	"slices"
)

// What the emitter conformance harness (spec RFC 0036 §5, ADR-0099) reuses
// from the reader: the strings at the secret positions of an environment's
// resolved entries, found by the kinds' own secrets patterns, as validation
// checks them. Nothing here changes what validate, resolve or facts write.

// SecretString is the string at one secret position of one resolved entry:
// the entry's name, the pointer into its resolved value, and the string the
// position holds, a secret reference in a valid registry. Value is for the
// harness's comparison only; it is never reported.
type SecretString struct {
	Entry   EntryName
	Pointer string
	Value   string
}

// SecretStrings returns the string at every secret position of one
// environment's resolved entries, keeping the kinds keep admits (every kind
// when keep is nil), in the resolved document's order and each entry's
// positions in document order. It requires a valid registry. A position that
// holds no string, which validation refuses (REF-SECRET-FORM), is not
// returned.
func (r *Result) SecretStrings(environment string, keep func(kind string) bool) ([]SecretString, error) {
	if !r.Report.Valid || r.registry == nil {
		return nil, ErrInvalid
	}
	if !slices.Contains(r.registry.environments, environment) {
		return nil, fmt.Errorf("registry: no environment %q", environment)
	}
	var out []SecretString
	for _, resolved := range r.resolved[environment] {
		if keep != nil && !keep(resolved.entry.name.Kind) {
			continue
		}
		for _, secret := range resolved.entry.kind.secrets {
			for _, place := range secret.positions(resolved.value) {
				if text, ok := place.value.(string); ok {
					out = append(out, SecretString{Entry: resolved.entry.name, Pointer: pointerOf(place.path), Value: text})
				}
			}
		}
	}
	return out, nil
}
