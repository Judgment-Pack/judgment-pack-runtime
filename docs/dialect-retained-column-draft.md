# Experimental retained-column Draft reader

Runtime has an explicit experimental entry for RFC 0029 at Spec revision
`ade3914b0b81ef299e5b9a8d6147f37ff3f0976c`. This is a Draft experiment, with no
normative adoption, preparation verification, source authenticity or execution claim.
The existing dialect operations retain their default contract and limits.

Select exactly
`--draft-contract retained-column/1@ade3914b0b81ef299e5b9a8d6147f37ff3f0976c`
and `--draft-profile base` or `--draft-profile extended` on `dialect contract`,
`canonicalize`, `digest`, `validate` or `check`. Both flags are required for the
experiment. Unsupported selection refuses before opening input files. A document
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
The unpublished `frameSummary` contains only contract version, origin and value,
declaration and field counts. The full typed inspection remains available to
internal Go callers; its duplicate raw strings are not repeated on the wire.
Both MCP report forms contain the same complete report. This keeps the Base frame
response bounded without reducing accepted input or changing ordinary reports.

The selected contract response reports only role `document-validation` in
`capabilities.conversionContracts`, with fixed trusted identity:

- Version label: `Draft RFC0029`.
- Revision: `ade3914b0b81ef299e5b9a8d6147f37ff3f0976c`.
- Schema: `sha256:996cec17473987eebf1aa553536df0ce5a740971532567f411282ee70036879f`.
- Corpus: `sha256:e2ac53f56cd5ab779e032d2847b49fa04438ffb9df57d456f75f2ba058dc12d2`.

| Profile | Document UTF-8 bytes | Standalone frame UTF-8 bytes |
| --- | ---: | ---: |
| base | 1048576 | 2097152 |
| extended | 2097152 | 4194304 |

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

Released artifacts, including Runtime 0.39.0-alpha.1, gain no support from this
experiment. Accepted immutable upstream contracts, reviewed source/artifacts and
actual Runner/Agent interoperability remain release prerequisites.
