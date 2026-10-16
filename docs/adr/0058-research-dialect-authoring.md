# ADR 0058: Validate portable research declarations offline

Status: accepted for implementation

The published version-3 Dialect contract includes research definitions with
requested fields, capability declarations and reconciliation reads. Desk needs the
same authoritative check for imported presets, manual edits and chat proposals.

Runtime validates these declarations through the existing dialect tools. The
carrier, canonical identity, strict omissions, schema resources and pack inventory
checks stay shared with other Dialect sources. Both integer 3 and named 3.0.0 use
the same source validator. Family 2 continues to refuse research sources.

A research validation pass does not acquire observations, resolve profiles, check
model availability, bind an installation, execute a reconciliation or verify a
preparation. Runner and Gateway keep those responsibilities. Reports continue to
state these limits; full document/preparation conformance is not claimed.

The unchanged Exchange preset is pinned with its upstream revision, license and
checksums. Adversarial tests additionally cover all six field types, partial
bindings, capability order, bounded text, unknown fields, timestamp parameters,
reconciliation selection and missing-value fallbacks. A separate synthetic policy
example checks that this is independent of the research domain.

Read-only clean-room Codex review identified a timestamp-parameter restriction and
an overly broad reconciliation condition-pointer suffix. Both were corrected.
Final review found no implementation correctness issue; stale CLI/MCP capability
prose was corrected. This was a same-vendor review under the user's explicit
exception, not a cross-vendor review.
