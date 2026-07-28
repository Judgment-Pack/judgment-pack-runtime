# Test fixtures

data-request-intake-triage.json is copied verbatim from
Judgment-Pack/judgment-pack-spec examples/ (commit 839488c). It is the pack
that spec RFC 0006's appendix instances walk; the evaluation tests execute
those instances against this engine.

rfc0008-condition-schema.json is the depth-indexed condition grammar of the
specification's RFC 0008 (Draft), written out as the RFC describes it: three
condition definitions by remaining aggregate depth, with `all`/`any`/`not`
recursing within their own tier and the depth-zero tier carrying no aggregate
branch, so aggregate depth three is unrepresentable rather than merely
forbidden. Its Core branches are the bundled JPS 0.1.0-draft schema's
`$defs/condition` branches, re-targeted per tier. It is a prototype artifact of
an open proposal: it is not a JPS schema, and no document validated against it
is valid under any published JPS version.

rfc0008/ holds the equivalence-check fixtures RFC 0008's Conformance section
asks any implementation to run, on the two census shapes a bare quantifier
reaches. Each pair is one prepared-boolean pack and one quantifier twin over
the same policy: airline-cancellation-* re-encodes
`A6:/reservation/anySegmentCancelledByAirline`, and item-availability-*
re-encodes the availability conjunct of `R3:/modification/allNewItemsAvailable`
(and its `R5` twin). The prepared packs are valid JPS 0.1.0-draft documents;
the quantifier packs are deliberately not, and the tests assert both facts. All
content is invented for specification testing and authorizes nothing.
