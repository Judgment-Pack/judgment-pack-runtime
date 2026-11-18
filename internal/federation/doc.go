// Package federation is the reader of registry format 1.1.0, proposed: the
// assembly of one registry from the registries of several sources (§§3–4),
// the assembled index and its check (§5), and the bounds a source may set on
// the values of the kinds it governs (§6); the lock (§7), its writer (§8),
// its check and coverage (§9). A writer given an assembly verifies each
// source signature as §11 does before it names it. The lock signature, the
// serial record and verification (§§10–11) are not built yet: the codes only
// they raise are listed in Codes, and nothing raises them.
//
// Format 1.0.0's rules hold unchanged (§1): each source's registry, and the
// assembled one, is validated by package registry, which this package calls
// and does not change. Every input is bounded by size and every collection by
// count, as the text states, and a repeated JSON member is refused.
//
// The format is proposed: what this package writes may change before the
// text is released.
package federation
