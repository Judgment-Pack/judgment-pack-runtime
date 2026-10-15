package dialect

import (
	"encoding/json"
	"unicode/utf8"
)

// Charge the encoded coverage while constructing it, before retaining each
// component. Counting strings avoids allocating an amplified JSON buffer merely
// to discover that it exceeds the limit. String accounting matches encoding/json
// with its default HTML escaping; empty typed envelopes keep field costs in sync.
type coverageBudget struct {
	used, limit int
}

var (
	coverageEnvelopeBytes = encodedEmptySize(Compatibility{Scope: "declared-inventory-coverage", Status: "review-required", InventoryOverApproximates: true, Facts: []CoverageEntry{}, Evidence: []CoverageEntry{}, Notes: []CoverageNote{}})
	entryEnvelopeBytes    = encodedEmptySize(CoverageEntry{Producers: []string{}, AcknowledgedBy: []string{}})
	noteEnvelopeBytes     = encodedEmptySize(CoverageNote{})
)

// Called only for fixed, empty envelopes, never for supplied strings or reports.
func encodedEmptySize(v any) int {
	data, _ := json.Marshal(v)
	return len(data)
}

func newCoverageBudget(c *Compatibility, limit int) *coverageBudget {
	b := &coverageBudget{used: coverageEnvelopeBytes, limit: limit}
	for _, s := range []string{c.Pack.ID, c.Pack.Version, c.Pack.SpecVersion, c.Pack.SHA256} {
		if !b.stringContent(s) {
			break
		}
	}
	return b
}

func (b *coverageBudget) exceeded() bool { return b.used > b.limit }

func (b *coverageBudget) charge(n int) bool {
	if b.exceeded() {
		return false
	}
	b.used += n
	return !b.exceeded()
}

// The enclosing shape already accounts for a string's two quote bytes.
func (b *coverageBudget) stringContent(s string) bool {
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		n := size
		switch {
		case r == '"' || r == '\\' || r == '\b' || r == '\t' || r == '\n' || r == '\f' || r == '\r':
			n = 2
		case r < 0x20 || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029' || (r == utf8.RuneError && size == 1):
			n = 6
		}
		if !b.charge(n) {
			return false
		}
		s = s[size:]
	}
	return !b.exceeded()
}

func (b *coverageBudget) entry(e CoverageEntry) bool {
	// One separator per entry is a conservative bound (the first needs none).
	if !b.charge(entryEnvelopeBytes+1) || !b.stringContent(e.Target) || !b.stringContent(e.Status) {
		return false
	}
	for _, values := range [][]string{e.Producers, e.AcknowledgedBy} {
		for i, value := range values {
			n := 2 // string quotes
			if i > 0 {
				n++ // separator
			}
			if !b.charge(n) || !b.stringContent(value) {
				return false
			}
		}
	}
	return true
}

func (b *coverageBudget) note(n CoverageNote) bool {
	return b.charge(noteEnvelopeBytes+1) && b.stringContent(n.Code) && b.stringContent(n.Kind) && b.stringContent(n.Target) && b.stringContent(n.Severity)
}
