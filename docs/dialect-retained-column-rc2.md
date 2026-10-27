# Selected retained-column RC2 reader

Runtime installs the retained-column/1 leaf from the official immutable Spec
`v0.3.0-rc.2` distribution at source revision
`bcac15b7537ce21879201b5e5dfe795496225121`. Its maturity is `draft`; selected
support is limited to document validation and declared inventory coverage.
Validation makes no preparation, source authenticity or execution claim.
The existing dialect operations retain their default contract and limits.

Select exactly
`--draft-contract retained-column/1@bcac15b7537ce21879201b5e5dfe795496225121`
and `--draft-profile base` or `--draft-profile extended` on `dialect contract`,
`canonicalize`, `digest`, `validate` or `check`. Both flags are required. The `draft` flag names are compatibility adapter
spellings; their exact accepted value selects only this installed RC2 identity. Unsupported selection refuses before opening input files. A document
must declare the selected profile; omission means base. Extended requires explicit
`"profile":"extended"` in a validated document. No selection is inferred from leaves,
carrier versions or caller-supplied specification hashes.

`dialect validate <input> --retained-frame` with both selection flags checks a
standalone retained frame. Otherwise validate checks the entire dialect document,
including the five converted-copy kinds, evidence availability, ordinary target
ownership, source ordering and internal references. `dialect check <input> --pack
<pack>` adds the existing pack validation and bounded declared inventory walk.
Coverage reports keep `valueTypesChecked:false` and `executionChecked:false`.
No conversion is executed and no record value is asserted true by these tools.

Canonicalize and digest check encoding only: bounded JSON, canonical numeric domain,
carrier version and identity spelling. Their success does not validate conversion
leaves, document structure or profile compatibility. Rules, spellings and source
pointers remain in canonical bytes and affect the digest. Retained-frame validation
reports a digest of its exact supplied frame bytes; it does not canonicalize opaque
retained strings or establish original declaration provenance.

For standalone frame reports, `document` retains the complete exact original JSON
text and `digest` identifies those bytes. Parse that text to recover every value,
status, ordered declaration, absent/null distinction and original escape spelling.
The bounded `frameSummary` contains only contract version, origin and value,
declaration and field counts. The full typed inspection remains available to
internal Go callers; its duplicate raw strings are not repeated on the wire.
Both MCP report forms contain the same complete report. This keeps the Base frame
response bounded without reducing accepted input or changing ordinary reports.

The selected contract response reports only role `document-validation` in
`capabilities.conversionContracts`, with fixed trusted identity:

- Version label: `0.3.0-rc.2`.
- Revision: `bcac15b7537ce21879201b5e5dfe795496225121`.
- Schema: `sha256:0cb49f238c78391cb163a65474cc9af962df2bb1024a55c5310c2b784b2f0557`.
- Corpus: `sha256:17b16ff7a9b74fe4cc81eb6f6129a6aabdd9834621d9e756aa2d4c1a3f245863`.

| Profile | Document UTF-8 bytes | Standalone frame UTF-8 bytes |
| --- | ---: | ---: |
| base | 1048576 | 2097152 |
| extended | 2097152 | 4194304 |

Each retained value is bounded in UTF-8 bytes: asserted values at 16000 and
generated values at 524288. Both bounds stay fixed in Base and Extended; a frame
API maximum does not imply that the value fits inside the fixed source snapshot.
Opaque typed `valueJson` remains inspectable without conversion execution.

Parser depth, nodes, string budgets and fixed declaration/frame limits remain
independently bounded. A standalone frame limit grants no combined acquisition,
producer, transport, preparation or signed record capacity. Init, schema and reference
remain on the ordinary contract; the experiment advertises no support for those
operations, generated acquisition, preview or preparation roles.

MCP uses existing `get_dialect_contract`, `dialect_canonicalize`, `dialect_digest`,
`dialect_validate` and `dialect_check` tools. Supply exact `draftContract` and
`draftProfile` strings. `retainedFrame:true` is legal only on selected
`dialect_validate`; the `document` argument then contains exact frame JSON text.
Unknown/extra/null selectors and duplicate arguments refuse. Tool schemas retain
conditional legacy and selected document/frame limits.

The unchanged MCP stdio envelope permits a JSON-RPC line below 16777216 bytes,
including delimiter, field names and JSON string escaping. Decoded artifact limits
are separate: a valid extended frame of about 3 MiB of literal `<` text can exceed
that envelope when encoded with HTML escaping. Such a frame is accepted through
the CLI and refused by the MCP transport before dispatch. Not every 4 MiB frame or
maximum-size pack/document pair fits every JSON-RPC serialization. No transport
ceiling is raised by the Draft capability.

Every selected operation verifies the trusted embedded installation before artifact
admission. The independent provenance pin binds the exact release/source identity,
archive and inventory digests, original upstream paths and all imported bytes.
Caller-supplied hashes or agreeing capability reports never install support.
The maintainer importer admits only one bounded leading Git PAX source comment;
path overrides, unknown metadata, duplicates, traversal and special members refuse.
The importer establishes local byte agreement, while publication and attestation
verification remain separate release evidence.

The installed published corpus is exercised for this reader's role: all 72
conversion cases retain document/frame structure and opaque provenance in both
profiles; all 11 raw-value recipes check frame admission through Go, CLI and MCP.
Preview arithmetic, producer/history, preparation/export/verification and capability
consumer cases remain their owners' work. Synthetic published fixture identities
remain preparation-only and are never promoted into signed full-input proof.
Historical Draft test resources remain byte-exact under their original pins.

Existing Runtime releases, including 0.39.0-alpha.1, gain no support from this
source change. Stable integrated checks, CI, review and distribution gates remain
required before a new Runtime release. No final Core conformance claim follows.
