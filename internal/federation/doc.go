// Package federation is the reader of registry format 1.1.0: the
// assembly of one registry from the registries of several sources (§§3–4),
// the assembled index and its check (§5), and the bounds a source may set on
// the values of the kinds it governs (§6); the lock (§7), its writer (§8),
// its check and coverage (§9); and the signature documents (§10), with their
// verification and the serial record a verifier keeps (§11). A writer given
// an assembly verifies each source signature as §11 does before it names it.
//
// Format 1.0.0's rules hold unchanged (§1): each source's registry, and the
// assembled one, is validated by package registry, which this package calls
// and does not change. Every input is bounded by size and every collection by
// count, as the text states, and a repeated JSON member is refused.
//
// The format is released on the spec tag registry-v1.1.0 and not yet
// stable; VERSIONING.md says what is promised.
package federation
