---
status: proposed
date: 2026-09-16
deciders: maintainer
---

# Refuse malformed Unicode at the stdio transport, for every string argument — and everywhere else in the request line

## Context and problem statement

`internal/mcp/server.go` admitted a JSON-RPC line on `json.Valid` alone, and `json.Valid`
does not judge UTF-8. `encoding/json` states it plainly: "When unmarshaling quoted strings,
invalid UTF-8 or invalid UTF-16 surrogate pairs are not treated as an error. Instead, they
are replaced by the Unicode replacement character U+FFFD." Every tool that unquotes a string
argument therefore received text the decoder had already repaired. Measured against
`d891b05` with a binary built from it: `validate` answered `"status":"valid"` with all three
layers passed for the bundled `valid/minimal-literal.json` pack whose title carried a lone
`\ud800` escape written at the JSON-RPC argument level, and again for one carrying a raw
`0x80` byte; `experimental_validate_expectations` answered `"status":"valid"` and returned
`canonical` as `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"allow�","reasons":[]}`
— the bytes `61 6c 6c 6f 77 EF BF BD` — for an `outcomeId` the caller wrote as `allow` plus a
lone `\ud800`. The repair is visible wherever a gate quotes what it read: the same bytes
placed in an outcome id produce `Local identifier "acc<U+FFFD>ept" has an invalid shape`, and
a truncated three-byte sequence produces two U+FFFD in that same message.

§2.1 of Core requires that "implementations MUST reject malformed or incomplete input … rather
than process only a silent prefix", and §8.3 requires that "Two conforming implementations
given the same pack, facts document, evidence-availability document, and supported-extension
set MUST produce byte-identical canonicalized dispositions." Neither is a rule about this wire,
and Core says so in as many words: "Core defines no transport, file layout, or command-line
surface for these inputs. It defines what they mean." (§8.2, l.612). What follows is therefore
this runtime's own transport policy, with §2.1 and §8.3 as its reasons rather than its mandate.
`internal/carrier` already holds
both rules one level down — `Decode` refuses non-UTF-8 bytes and refuses an unpaired surrogate
escape, so that an authored `"\ud800"` cannot canonicalize to the same bytes as a literal
U+FFFD — but nothing reaches a carrier until a tool has already unquoted its argument into a
Go string, by which time the defect is gone. The runtime also already holds the outbound half
of the same standard: `internal/mcp/tools.go:501` refuses to serve a project document whose
bytes are not valid UTF-8, rather than let the JSON encoder transcode them (ADR-0029).

## Decision drivers

- §2.1's obligation to reject malformed input must be reachable on the wire, not only inside
  a carrier a repaired string never reaches.
- A disposition this runtime canonicalizes must be the caller's bytes, or §8.3's byte-identical
  comparison is comparing something nobody sent.
- The defect is the transport's, not any one tool's: `validate`'s document
  (`internal/mcp/tools.go:290`), each expectation (`internal/mcp/expectations.go:71`), and every
  string argument of every tool added later unquote the same way.
- Inbound and outbound must hold one standard; the outbound refusal is already there.
- A refusal must cost the session nothing: one message refused, the stream continues.
- No new dependency, and no new wire or CLI surface beyond the refusal itself: the scan is
  exported within the module, not published, and nothing about what conformance checking accepts
  changes.

## Considered options

- **Hold the rule once at the stdio transport, before dispatch.** One check over the frame,
  for every tool, present and future.
- **Check at every unquote site, per tool.** `validate`'s `document`, each element of an
  expectations batch, `experimental_evaluate`'s `pack` and `facts`, and each new tool's
  strings, every one of them remembering to do it.
- **Leave it to each tool's schema.** The advertised `inputSchema`s are JSON Schema; a string
  is a string, and by the time a validator sees one the repair has happened.
- **Copy the carrier's surrogate scan into `internal/mcp`.** Two copies of one rule, drifting.
- **Hold the rule at the transport, but only over the part of the line a tool will unquote.**
  Scan the `arguments` object rather than the frame, so a defect in a method name or a string id
  keeps the answer it has today.
- **Answer such a line under the request's `id`.** The JSON is well-formed, so an id could be
  read out of it and an ordinary error returned against it.
- **Change nothing and document the repair.** State in `docs/mcp-clients.md` that a client
  must send well-formed UTF-8, and keep answering about repaired text when it does not.

## Decision outcome

Chosen option: **hold the rule once at the stdio transport**, because the root is that
`json.Valid` does not check UTF-8 — not that any particular tool forgot — and because a
per-tool check is a rule every future tool has to be told about, while a frame that never
carried the defect cannot leak it into a tool that was written after the rule.

`Serve` judges each line in this order, before any dispatch:

1. `utf8.Valid(line)`. A line that is not valid UTF-8 is refused with JSON-RPC error
   `-32700` under `id: null` and the message `Message is not valid UTF-8 JSON.`
2. `json.Valid(line)` exactly as before: `-32700` under `id: null`,
   `Message is not valid JSON.`
3. Immediately after it, `carrier.UnpairedSurrogateEscape(line)`. A lone `\uD800`–`\uDFFF`
   escape is refused with `-32700` under `id: null` and the message `Message contains an
   unpaired surrogate escape at byte offset N. RFC 8785 §3.2.2.2 makes such a value invalid
   rather than replaceable, and this runtime refuses it rather than substituting U+FFFD.`
   The offset is into the message line as the transport read it, after the JSON whitespace
   trim the loop already applies.

Both checks read the whole request line, and both run before the envelope is read, so the defect
is refused wherever it is written. A tool argument is where it was found, not the limit of the
rule: a lone escape or an invalid byte in a method name, in a string `id`, in any member, or in a
line that is not a JSON object at all, is this same parse error. That is deliberate, and it is why
the narrower option above was rejected: a line carrying either defect is not a JSON-RPC message
this runtime will read, reading an id out of it to answer against would be reading part of what
was refused, and a method name or a string id repaired into U+FFFD is no more the client's text
than a repaired argument is. It widens what the parse-error branch answers, at the cost named
under Compatibility and migration below, and tests hold the wider scope so that quietly narrowing
it back fails rather than passes.

The two checks are there for different reasons, and only one of the two positions is a choice.
`utf8.Valid` has to exist because `json.Valid` does not judge encoding at all: measured on
go1.26.5, `{"a":"x\x80y"}` is `json.Valid` true and `utf8.Valid` false, so a raw `0x80` inside a
string passes the JSON check whichever side of it the encoding check sits on. What the position
decides is the diagnostic a line that is defective in both ways receives: `{"a":"x\x80`, an
unterminated string carrying a raw `0x80`, is both invalid UTF-8 and invalid JSON, and answers
`Message is not valid UTF-8 JSON.` because the encoding check runs first — the defect the JSON
diagnostic would not have named, since `Message is not valid JSON.` says nothing about encoding,
while a client that fixes its encoding and resends learns of any syntax defect that remains from
the next answer. That choice is held by a test, so moving the check fails it rather than changing
the wire quietly.

The surrogate scan's position, by contrast, is a precondition and not a preference. It
comes after JSON because the scan reads a backslash as an escape only inside a string, and
over bytes that are not JSON its string tracking is a guess: an unterminated string makes the
rest of the line look like string content, and the scan would report an escape's offset inside
bytes that have no strings at all. JSON validity is the defect this runtime can actually
locate in such a line, so it is settled first and named.

The scan is exported from `internal/carrier` rather than copied: `UnpairedSurrogateEscape`,
with its doc comment kept and extended to state the precondition its one outside caller must
meet. `Decode` keeps using it, unchanged.

Three further determinations:

- **The noun is `Message`, not `Input`.** The carrier says "Input is not valid UTF-8 JSON."
  and "Input contains an unpaired surrogate escape…" about a document handed to it. At the
  transport the thing refused is one JSON-RPC message line, and a client that got "Input…"
  beside `-32700` would have to guess whether the runtime means its line or the document
  inside it. The three parse errors now read as one family: `Message is not valid JSON.`,
  `Message is not valid UTF-8 JSON.`, `Message contains an unpaired surrogate escape at byte
  offset N…`. For the same reason the surrogate sentence says "this runtime refuses it" where
  the carrier's says "this decoder refuses it": at the transport no decoder has run. The rest
  of the sentence, the RFC citation included, is the carrier's word for word, so one grep
  finds both levels.
- **Answered under `null`, as a parse error, not under the request's id.** An id could be read
  out of these bytes, but they are not a JSON text this runtime accepts: the same bytes one
  level down are refused by the carrier as `JPS-CARRIER-INVALID-JSON`, and answering them as
  an ordinary error against an id would say the runtime read the request and disagreed with
  its content, which is not what happened. JSON-RPC §5 answers under a null id whenever the
  request's id could not be read — for a parse error and for an invalid request alike, so a null
  id is not the mark of a parse error — and the existing parse-error branch already answers
  there.
- **A notification carrying such bytes is answered too.** A parse error is answered under
  `null` whatever the line would have been, because whether it carries an `id` is a fact about
  a request this runtime never read (JSON-RPC §5). That is the existing branch's behaviour and
  it stays consistent with it.

Two levels of the same escape remain distinct, and both are refused, by different code for
different reasons. An escape written **in the message itself** — anywhere in the JSON-RPC line,
so that `json.Unmarshal` would repair it while binding whatever it belongs to — is the
transport's, and is what this record adds. An escape written **inside an argument's own JSON**
— `\\ud800` on the wire, which unquotes to the six characters `\ud800` inside a document or
expectation text — reaches the tool intact and is refused by `carrier.Decode` where it always
was, as an indexed `JPS-EXPECTATION-INVALID` finding or a carrier diagnostic. The inner-level
fixture at `internal/mcp/testdata/expectations.json:244` is unchanged and still proves that
second path: its line goes through the transport check like any other and passes it untouched,
because a doubled backslash is not an escape opener to the scan, and the defect is then refused
one level down by the carrier.

### Consequences

- **Compatibility and migration:** a client that sent a raw invalid byte or a lone surrogate
  escape at the argument level got an answer about repaired text and now gets
  `-32700` under `id: null`. Because the check is the whole line's, a line carrying either
  defect **outside** its arguments changes answer as well, and one of those answers changes id:
  measured against `d891b05`, `{"jsonrpc":"2.0","id":1,"method":"pi\ud800ng"}` was answered
  `-32601` `Unknown method: pi<U+FFFD>ng` under `id: 1` and is now `-32700` under `id: null`;
  `["\ud800"]` was answered `-32600` `The request is not a JSON object; batches are not
  supported.` under null and is now the same parse error; `{"jsonrpc":"2.0","id":"a\ud800b",
  "method":"ping"}` was answered as an ordinary result and is now refused; and
  `{"a":"x\x80`, defective both ways, answered `Message is not valid JSON.` and now answers
  `Message is not valid UTF-8 JSON.` A client that correlates strictly by `id` sees a null-id
  parse error where an id-bearing error used to come back, and a null-id error is uncorrelated:
  JSON-RPC §5 answers under null whenever the request's id could not be read — for a parse error
  and for an invalid request alike — and supplies no rule tying such an answer to the request the
  client wrote last. A client with one request outstanding can attribute the refusal to it; a
  client that pipelines cannot, from the `id` alone, and must not fail its most recently written
  request on the strength of a null-id error. The sequence every refusal test sends
  (`internal/mcp/server_test.go:1041`) is why: the malformed line and then a valid `ping` under
  `id: 99`, both written before anything is read, and the refusal comes back under null while the
  `ping` is answered under 99 — so failing the last-written request would fail the one request
  that succeeded and leave the refused one outstanding.
  The two refusals differ in what they ask of a client. A raw invalid byte was never valid UTF-8,
  and [MCP's stdio transport](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports)
  requires UTF-8, so *that* refusal changes nothing a conforming client sends. An unpaired
  surrogate *escape* is a different claim: RFC 8259 §8.2 admits one in JSON's grammar — it says
  the behaviour of software that receives such a value is unpredictable, not that the text is
  malformed — and the escape is plain ASCII on the wire, so it satisfies that transport's UTF-8
  requirement as well. Refusing it anywhere in the line is therefore this runtime's stricter
  transport policy, not a rule inherited from JSON or mandated by Core, and it is taken because
  the carrier already refuses the same escape one level down under RFC 8785 §3.2.2.2 — whose
  reason is §8.3's byte-identical canonical requirement, which a repaired escape defeats. A
  client that sent such an escape got a repaired answer before and gets a parse error now.
  Well-formed input is untouched either way: a well-formed surrogate pair (a high escape followed
  immediately by its low one, `\ud83d\ude00`) is a character and is admitted, and a literal
  U+FFFD the client actually authored is valid UTF-8 and is admitted. No tool schema, tool
  name, payload member, exit code, or CLI surface
  changes; the CLI paths do not go through this transport and were already covered by the
  carrier. Stored packs, matrices, corpora and fixtures are untouched: both bundled
  conformance suites and the evaluation corpus pass unchanged.
- **Automation:** an authoring client that was silently storing repaired text now sees the
  refusal at the point it sent the bytes, and must fix its own encoding rather than discover
  the damage in a canonical disposition later. A refusal is one message refused and not a
  session end, so a client's next request is answered normally; a client that retries the same
  bytes gets the same refusal, and the byte offset in the surrogate message points at the
  escape in the line it sent.
- **Authority:** nothing about what a pack means, what the bundled suites accept, or what this
  runtime states about itself changes — `spec test-conformance` reports what it reported for
  both versions, and a document's verdict is the verdict it always had. What changes is where
  §2.1's standard is held by *this implementation* — Core states no transport rule to hold, only
  what the inputs mean (§8.2, l.612) — so malformed input arriving on the MCP wire
  is now rejected rather than processed as repaired text. This decides only which byte
  sequences are a message this runtime will read; it states nothing new about the
  specification. The carrier keeps its own refusals, word for word; this record does not
  move a rule out of it, and no earlier record's determination is superseded — ADR-0029's
  outbound refusal is the same standard held on the way out, and the limits ADR-0035 records
  remain as they were.
- **Security and privacy:** input handling only. Silent lossy repair is removed from the one
  path that reached every tool, so no gate below decides about text the caller did not send.
  The check is two linear passes over a line already bounded at 16 MiB; it allocates nothing,
  opens nothing, and reaches no project, credential, network or model. A refused line is
  answered and discarded, and its bytes are never echoed back — the diagnostic carries an
  offset, not the offending text.
- **Validation:** wire tests drive `Serve` itself. A raw `0x80` and a truncated multi-byte
  sequence, in `validate`'s document and in an expectation, are each refused as
  `Message is not valid UTF-8 JSON.` under a null id with no tool result, and a following
  `ping` is still answered; a lone `\ud800` and a lone `\udc00`, in both tools, are each
  refused with the surrogate message whose offset the test computes from the line it built,
  so a wrong offset fails. Both orders are pinned by a line that is defective two ways: one
  that is not JSON and also carries a lone escape must report the JSON defect, and one that is
  neither valid UTF-8 nor valid JSON must report the encoding defect. The scope is pinned by
  four lines carrying the defect outside any argument — in a method name as an escape and as a
  raw byte, in a string `id`, and in a line that is not an object — each refused the same way,
  so narrowing either check to lines that carry tool arguments fails a test rather than
  passing silently. Negative controls hold the other half: a
  well-formed pair as an escape and as literal UTF-8, and a literal U+FFFD character, are all
  admitted and answered normally, and the pair survives into an expectation's canonical text.
  One control is U+10FFFF, the largest scalar value there is and one of the 1,024
  (U+10FC00–U+10FFFF) whose high unit is the last high surrogate `0xDBFF`, so it exercises the
  scan's final high-surrogate boundary rather than leaving it assumed: the `value >= 0xDBFF`
  mutation refuses every pair in that range, this control among them, and admits every pair
  below it. Each control also
  asserts the bytes its line must carry and the bytes it must not, so an
  escape case cannot quietly become a copy of the literal case beside it — which is a real
  failure mode, since a spelled-out pair in a source file can be folded into the character it
  names. Each guard was mutation-checked by neutralizing it and confirming that exactly the
  paired test fails.

Material impact is public-surface, conformance and security: a new refusal on the wire — and,
for a line whose defect lies outside its arguments, a `-32601` answered under the request's own
`id` becoming a `-32700` answered under `null` — §2.1's reject-malformed-input standard now
held, as this runtime's own transport policy, where repaired text used to pass, and input
handling with the silent repair removed. Cross-vendor review and maintainer dispositions are
required on the introducing PR before merge, under the repository's existing review regime.
