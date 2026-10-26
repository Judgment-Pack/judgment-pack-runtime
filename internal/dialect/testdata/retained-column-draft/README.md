# Experimental retained-column Draft document reader

`contract.schema.json` and `cases.json` are byte-exact test inputs from Spec commit
`ade3914b0b81ef299e5b9a8d6147f37ff3f0976c`, under
`rfcs/retained-column-conversion/`. They are Draft artifacts, never embedded or
served by the released Dialect schema/resource tools. Source:
https://github.com/Judgment-Pack/judgment-pack-spec/tree/ade3914b0b81ef299e5b9a8d6147f37ff3f0976c/rfcs/retained-column-conversion

SHA-256:

- Schema: `996cec17473987eebf1aa553536df0ce5a740971532567f411282ee70036879f`
- Corpus: `e2ac53f56cd5ab779e032d2847b49fa04438ffb9df57d456f75f2ba058dc12d2`

The explicit internal `ValidateRetainedColumnDraft` and
`ValidateRetainedColumnDraftFrame` entries require the exact trusted Draft selector.
They check document syntax/identity, copy variants, declarations, target ownership,
ordinary references and retained-frame structure. The trusted selection must match
the document's explicit `profile`; absence still means base. The conditional Draft
limits are base document 1048576/frame 2097152 and extended document 2097152/frame
4194304. These are experimental admission limits, not adopted combined-carrier
accounting or a production capability report.

Base parser depth 64, document node and individual-string budgets 131072 remain
independent and unchanged. Extended uses depth 128, nodes 147456 and individual
strings 262144. The carrier counts values and containers, not member keys. An old
262144-byte extended document needs at most 131072 such nodes; each new boolean
conversion adds at most six, each availability adds three. The 512/256 target
ceilings add at most 3840 nodes and the profile adds one. A separate 16384-node
reserve covers that growth without deriving work from the enlarged byte cap.
The string ceiling preserves any string in the original extended byte envelope.

All applicable Core extended document rows are checked: sources, parameters,
clauses, conditions, unwrap pointers, fact/evidence targets, research declaration
counts and string limits, reconciliation inputs and artifact IDs. Closed grammar,
canonical numeric domains and fixed conversion spelling/type limits are retained.
Acquisition results, answers, profiles/bindings, preparation/record sizes and
combined-carrier accounting remain outside these document/frame entries; no full
profile conformance or public extended capability is claimed.

Closed frames need at most 7011 nodes under either profile: root and container
scaffolding, 1000 values, 1000 three-node declaration entries and 1000 three-node
field entries. Their reader permits 8192. Opaque `valueJson` strings may occupy
the selected frame envelope and are never recursively parsed or converted. Each
`fieldJson` stays bounded at 16384 UTF-8 bytes before parsing, with depth 32 and
at most 16384 nodes. This preserves original numbers and exact declaration text.

The 72 pinned conversion cases exercise actual document and standalone frame
admission only. Their preview/claim outputs are not evaluated by Runtime. Empty
or unknown original types, unavailable declarations, mismatched revisions,
malformed typed value JSON and legacy escaped surrogates remain inspectable;
Runner must apply the ordered per-record conversion diagnostics. The inspected
frame distinguishes absent, historical null and exact raw strings. Its report
retains the complete original frame text and hashes those supplied UTF-8 bytes;
`fieldJson` identity separately hashes the exact original declaration string.
No original source, receipt or declaration-inventory authenticity is inferred.

Default Contract/Validate/CLI/MCP and released schemas remain unchanged. The entry
is not capability negotiation: in particular it does not resolve the Draft
reader's omitted `capabilities.conversionContracts` diagnostic mismatch. It cannot
authorize acquisition, conversion, preview, preparation, provenance verification,
producer restoration or full-envelope compatibility. Those owners and normative
adoption remain prerequisites for Agent #171.

This is an experimental, unreviewed Draft candidate pin, not an adopted or released dependency. Final integration requires the reviewed immutable identity and matching artifacts.

The selected Draft MCP report text uses literal HTML JSON encoding, matching the
outer structured encoder. Both `structuredContent` and `content[].text` carry the
same report, including the exact original frame text and digest. Applying an
optional second HTML escape pass to the text report can amplify an admitted Base
frame beyond the client line budget; ordinary tool serialization is unchanged.
The transport tests consume the complete escaped request and both report forms.
They distinguish handwritten projected old-source families from independent
full-frame grammar stress cases, and from whitespace used only to reach an exact
byte ceiling. The pinned Desk/Agent pack domain is 1 MiB; Runtime's ordinary
10 MiB decoded pack cap and 16 MiB incoming MCP line cap remain separate limits.

Standalone frame wire reports retain all exact original data once per report in
`document`, with its exact digest and bounded `frameSummary` counts. Parsing that
original text reconstructs every value, status, declaration and escape spelling.
The complete typed Frame remains available internally in Go and is not repeated
in JSON. Both MCP report representations remain equivalent. With Base frame F,
JSON quoting of valid original JSON costs at most 2F; quoting that report text
again costs at most 4F. The complete dual response therefore fits 6F plus bounded
fixed report/summary metadata, below 16 MiB for F=2 MiB. This does not establish a
full Extended response guarantee or change ordinary transport/report defaults.
