# Decision records

`sdk.Pack.EvaluateWithRecord` and `sdk.Pack.EvaluateWithKeyedRecord` evaluate a pack as
`Pack.Evaluate` does and return, beside the evaluation, a decision record of package `sdk/record`:
one canonical JSON text naming the pack, the facts and evidence documents, the disposition and the
evaluator, with its own `sha256:` digest
([ADR-0079](adr/0079-a-decision-record-for-an-evaluation.md),
[ADR-0087](adr/0087-a-caller-keyed-digest-on-the-decision-record.md)). Nothing is written: the
record is the caller's to keep. It is not an audit-trail entry and it is unsigned. Everything on
this page is EXPERIMENTAL, as the rest of the Go API is.

This page states the record's two forms, the canonical text its document digests are taken over,
with vectors a second implementation can reproduce, how a caller keys those digests, and what a
record says about the facts in each form.

## The two forms

A record names the facts and evidence documents in one of two forms, and its `recordVersion` says
which. The major part of the version names the form.

| member | plain form, `recordVersion` `"1.0.0"` | keyed form, `recordVersion` `"2.0.0"` |
|---|---|---|
| `binding` | the caller's names and `sha256:` digests, as given; absent when none was given | the same |
| `disposition` | the §8.3 portable disposition, the bytes `CanonicalDisposition` returns | the same |
| `evaluator` | `runtimeVersion` (absent when the build records none) and `specVersion` | the same |
| `evidenceDigest` | `sha256:` and the SHA-256 of the evidence document's canonical text | absent |
| `factsDigest` | `sha256:` and the SHA-256 of the facts document's canonical text | absent |
| `keyedDigests` | absent | `algorithm`, `evidence`, `facts` and `keyId`: the caller's keyed digests of the same two canonical texts, each as lower-case hexadecimal digits, the algorithm's label and the caller's key identifier |
| `pack` | `digest`, `id`, `specVersion` and `version`, as `Pack.Identity` returns them | the same |
| `recordVersion` | `"1.0.0"` | `"2.0.0"` |

`EvaluateWithRecord` writes the plain form, whose bytes are the ones it wrote before the keyed form
existed. `EvaluateWithKeyedRecord` writes the keyed form. The record itself is written in the
canonical form below, and holds no number.

The two forms share no digest member. A keyed record has no `factsDigest` or `evidenceDigest`, so a
reader that looks for a plain digest finds none rather than finding a keyed one; and a plain record
has no `keyedDigests`. `record.Read` admits exactly `"1.0.0"` with the plain form's members and
`"2.0.0"` with the keyed form's, and refuses anything else: a record with both forms' members, a
member of no form, or a later version, which it never reads as one it knows. A reader written to
the 1.0.0 form alone refuses a keyed record by its major version, and, if it ignores the version, by
the missing `factsDigest`.

## The canonical text

A record's facts and evidence digests are taken over each document's canonical text, not over the
bytes the caller passed. `record.CanonicalFacts(facts)` and `record.CanonicalEvidence(evidence)`
return that text. For every document `Pack.Evaluate` admits, the SHA-256 of the bytes they return is
the plain record's `factsDigest` or `evidenceDigest`, and those bytes are what a caller keys for the
keyed form. An omitted evidence document (`nil`) is the empty object JPS Core §8.2 evaluates it as,
and its text is `{}`.

### Reading

The document is read as the evaluation reads it, by the carrier decoder under its default limits,
and refused, with an error wrapping `record.ErrDocument`, when it is:

- empty (a nil or zero-length facts document; a zero-length supplied evidence document);
- longer than 10,485,760 bytes (`JPS-RESOURCE-INPUT-BYTE-LIMIT`);
- not exactly one JSON text (RFC 8259) in valid UTF-8, with nothing after it but white space: a
  byte order mark, a raw control character inside a string, a number such as `01` or `NaN`, a
  second value, and a surrogate code point written in UTF-8 are all refused
  (`JPS-CARRIER-INVALID-JSON`);
- holding a `\uD800`–`\uDFFF` escape that is not one half of a well-formed pair: an unpaired
  surrogate is refused, not replaced by U+FFFD (`JPS-CARRIER-INVALID-JSON`, ADR-0038);
- holding an object with one member name twice, compared after escapes are decoded, so `"a"` and
  `"a"` are one name (`JPS-CARRIER-DUPLICATE-MEMBER`);
- nested more than 128 arrays and objects deep (`JPS-RESOURCE-DEPTH-LIMIT`), of more than 250,000
  values (`JPS-RESOURCE-NODE-LIMIT`), or holding a string, member name or number token longer than
  1,048,576 bytes (`JPS-RESOURCE-STRING-LIMIT`, `JPS-RESOURCE-NUMBER-LIMIT`).

The error names the document and the rule, with the code, in a fixed sentence: it repeats no member
name, value or position of the document. A number is not refused for its size or precision. The
calls do not hold a document to a pack: that a facts document is an object, or an evidence document
names declared requirements, is the evaluation's to judge.

### Writing

The text is UTF-8, with no byte order mark, no white space outside strings and no trailing newline.

- **Objects** are written `{`, then each member as its name, `:`, its value, separated by `,`, then
  `}`. Members are ordered by the UTF-16 code units of their names, as decoded, compared unit by
  unit, a shorter name before a longer one it begins. This is not the order of the UTF-8 bytes or of
  the code points: a name beginning with a code point above U+FFFF (written as a surrogate pair in
  UTF-16, `D800`–`DBFF` first) sorts before one beginning with U+E000 to U+FFFF.
- **Arrays** are written `[`, the values in document order separated by `,`, then `]`.
- **Strings**, member names included, are written between `"` with these escapes and no others:
  `\"` for U+0022, `\\` for U+005C, `\b` `\t` `\n` `\f` `\r` for U+0008, U+0009, U+000A, U+000C and
  U+000D, and `\u00` followed by two lower-case hexadecimal digits for every other code point below
  U+0020. Every other code point is written as itself in UTF-8: `/`, U+007F, U+2028, U+2029, `<`,
  `>`, `&`, `'` and every code point above U+FFFF included. An escape in the document is decoded
  first, so `"A\/"` is written `"A/"`.
- **Numbers** are written as the token the document holds, byte for byte: `1.50`, `-0`, `1E+2`,
  `12345678901234567890` and `1e400` are written so.
- **Literals** are written `true`, `false` and `null`.

### Where it is and is not RFC 8785

The form is RFC 8785 (the JSON Canonicalization Scheme) in everything but numbers.

- **The same:** member order by UTF-16 code units (RFC 8785 §3.2.3), the string escapes, which are
  ECMAScript's `JSON.stringify` (§3.2.2.2), the literals, no white space, and UTF-8 output (§3.2.4).
  The refusals the RFC takes from I-JSON (RFC 7493) are made here too: a duplicate member name, and
  a string that is not well-formed Unicode, an unpaired surrogate escape included.
- **Not the same:** a number. RFC 8785 §3.2.2.3 writes a number as ECMAScript writes the IEEE 754
  double nearest it, and refuses one no double holds. This form writes the token as the document
  holds it, because the evaluator compares numbers as written, and rewriting them would give two
  different documents one digest: RFC 8785 writes both `12345678901234567890` and
  `12345678901234567891` as `12345678901234567000` (ADR-0079, option E). So RFC 8785 writes `1.50`
  as `1.5`, `-0` as `0`, `0.0` as `0`, `1e2` and `1E+2` as `100`, and refuses `1e400`; this form
  writes each as given.
- **The limits** above are this runtime's, not the RFC's.

A document with no number, or whose every number is already written as ECMAScript writes it (an
integer of magnitude below 2^53 with no fraction, exponent, `+` or leading zero, and not `-0`, for
example), has a canonical text equal byte for byte to the RFC's output. For any other document an
RFC 8785 library does not reproduce the text; the vectors below hold what this form writes.

## Vectors

Each line of the block below is one JSON object, written by hand, which a test of this repository
reads from this page and holds `record.CanonicalFacts` and `record.CanonicalEvidence` to. A line
holds `name`, and then either:

- `input`, the document as a JSON string (decode the line, and the string's UTF-8 bytes are the
  input), `canonical`, the canonical text as a JSON string, and `sha256`, the lower-case hexadecimal
  SHA-256 of the canonical text's UTF-8 bytes, which is the plain record's digest without its
  `sha256:` prefix;
- `input`, or `inputHex` (the input's bytes in hexadecimal, for one that is not UTF-8), and
  `refused`, the code the document is refused with, or `empty`;
- `canonical`, `keyHex`, a synthetic HMAC key's bytes in hexadecimal, and `hmacSha256`, the
  HMAC-SHA-256 of the canonical text under that key: what the keyed form's digests are.

```jsonl
{"name": "an empty object", "input": "{}", "canonical": "{}", "sha256": "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"}
{"name": "white space between tokens is removed", "input": " {\n\t\"a\" : 1 ,\r\n \"b\":[ true , false ] }\n", "canonical": "{\"a\":1,\"b\":[true,false]}", "sha256": "a08bcb55555d1879a7ecc51e7116ee72c1faffa2b4f9f7600a70d5ea2efbbb71"}
{"name": "members are ordered by name at every depth", "input": "{\"b\":1,\"a\":2,\"c\":{\"z\":0,\"y\":0}}", "canonical": "{\"a\":2,\"b\":1,\"c\":{\"y\":0,\"z\":0}}", "sha256": "3a0db5e62d4f03381be47b46e589737ee24603bf994e17c6312a3f1db9815d0f"}
{"name": "the empty name sorts first and a prefix before a longer name", "input": "{\"ab\":1,\"a\":2,\"\":3}", "canonical": "{\"\":3,\"a\":2,\"ab\":1}", "sha256": "f7c822ad8e8a17f47959dac9f7e1f4368bdd1d91df2bf4775b803230904bb635"}
{"name": "names are ordered by UTF-16 code units, so U+1F600 sorts before U+E000", "input": "{\"\\ue000\":1,\"\\ud83d\\ude00\":2,\"z\":3}", "canonical": "{\"z\":3,\"\ud83d\ude00\":2,\"\ue000\":1}", "sha256": "cd0e192e95f5e545c193788e80b08ab3d6fb48a5d20fed4af175061ec107847b"}
{"name": "an escaped name sorts by the name it spells", "input": "{\"\\u0062\":1,\"a\":2}", "canonical": "{\"a\":2,\"b\":1}", "sha256": "d3626ac30a87e6f7a6428233b3c68299976865fa5508e4267c5415c76af7a772"}
{"name": "array order is kept", "input": "[3,1,2,\"b\",\"a\"]", "canonical": "[3,1,2,\"b\",\"a\"]", "sha256": "50300122e6fdfef79ecb31e0447247453c80bc9fd0c146af68c052638f73572c"}
{"name": "every number is written as the token the document holds", "input": "{\"n\":[1.50,1.5,-0,0.0,1e2,1E+2,1e-7,100]}", "canonical": "{\"n\":[1.50,1.5,-0,0.0,1e2,1E+2,1e-7,100]}", "sha256": "6010ce61fdaf690271151d0ff3b4924286600aa1afc34fd53fd15fad5d38b8a5"}
{"name": "an integer beyond 2^53 is kept as written", "input": "{\"n\":12345678901234567890}", "canonical": "{\"n\":12345678901234567890}", "sha256": "8716eed5e3ea73042324b69568de421fc67f1547923dcb85ab0d1e156d6e0588"}
{"name": "a number outside the double range is kept as written", "input": "{\"n\":1e400}", "canonical": "{\"n\":1e400}", "sha256": "99450423054b8646b3dc4c0bc12384edb68977ec153e96189686618fafdc584c"}
{"name": "the two mandatory escapes and the five short forms; an escaped solidus is written as itself", "input": "\"\\\"\\\\\\/\\b\\f\\n\\r\\t\"", "canonical": "\"\\\"\\\\/\\b\\f\\n\\r\\t\"", "sha256": "11b17be1c38ac0f40c1c36cb58861f9853f04d6702d8edf4c79e6aca6216c346"}
{"name": "every other control character is written as \\u00xx in lower case", "input": "\"\\u0000\\u001F\\u000B\"", "canonical": "\"\\u0000\\u001f\\u000b\"", "sha256": "fec099e4c758be515d3a0e6c20e61e396119f5a77a268933b449f94a7edcc292"}
{"name": "an escaped character above U+001F is written as itself, in UTF-8", "input": "\"\\u0041\\u00e9\\u20ac\"", "canonical": "\"A\u00e9\u20ac\"", "sha256": "be18de9b730e17a69d035752089f3ef760c80485ac1a41789dbdcbe78a6caa53"}
{"name": "DEL, U+2028 and U+2029 are written as themselves", "input": "\"\\u007f\\u2028\\u2029\"", "canonical": "\"\u007f\u2028\u2029\"", "sha256": "95df0b63167be5cce73c768a4dd6b0005aa196870fdc24ca85e0a03c1dd7a6c0"}
{"name": "a surrogate-pair escape is written as the one code point, in UTF-8", "input": "\"\\ud83d\\ude00\"", "canonical": "\"\ud83d\ude00\"", "sha256": "7a0c50b92434b015545fe93ab723db2d4b2cdd14a441405624a9ce8be29f1d5a"}
{"name": "characters an HTML-safe encoder escapes are written as themselves", "input": "\"<&>'\"", "canonical": "\"<&>'\"", "sha256": "e39d7f4253614b4c6210bb2f0849692be42641e091e263f81d0e2b5d1720cfc9"}
{"name": "true, false and null", "input": "{\"t\":true,\"f\":false,\"z\":null}", "canonical": "{\"f\":false,\"t\":true,\"z\":null}", "sha256": "60aa464a7b144f04dc42c7e4462acca5e4c25fef90e3c25124225b2df1791b6f"}
{"name": "a facts document spelled with white space and escapes", "input": "{ \"amount\" : \"12\\u0035\\u0030\" }", "canonical": "{\"amount\":\"1250\"}", "sha256": "570f96005d6be6ccdae183593bae4eee0a23ac0ca265dc4f2b07a6e8b6e65c5a"}
{"name": "128 nested arrays, the carrier's nesting limit", "input": "[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]", "canonical": "[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]", "sha256": "dbaec29ce2fb52a1a372e1da31b0d434d257fe11bebee2d31c6649710e3052a6"}
{"name": "a member name twice", "input": "{\"a\":1,\"a\":2}", "refused": "JPS-CARRIER-DUPLICATE-MEMBER"}
{"name": "a member name twice, once escaped", "input": "{\"a\":1,\"\\u0061\":2}", "refused": "JPS-CARRIER-DUPLICATE-MEMBER"}
{"name": "bytes that are not UTF-8", "inputHex": "22ff22", "refused": "JPS-CARRIER-INVALID-JSON"}
{"name": "a surrogate code point encoded in UTF-8", "inputHex": "22eda08022", "refused": "JPS-CARRIER-INVALID-JSON"}
{"name": "an unpaired high surrogate escape", "input": "\"\\ud800\"", "refused": "JPS-CARRIER-INVALID-JSON"}
{"name": "an unpaired low surrogate escape", "input": "\"\\udc00x\"", "refused": "JPS-CARRIER-INVALID-JSON"}
{"name": "a second JSON text after the first", "input": "{} {}", "refused": "JPS-CARRIER-INVALID-JSON"}
{"name": "a byte order mark", "inputHex": "efbbbf7b7d", "refused": "JPS-CARRIER-INVALID-JSON"}
{"name": "a raw control character inside a string", "input": "\"a\tb\"", "refused": "JPS-CARRIER-INVALID-JSON"}
{"name": "a number with a leading zero", "input": "01", "refused": "JPS-CARRIER-INVALID-JSON"}
{"name": "NaN, which is not JSON", "input": "NaN", "refused": "JPS-CARRIER-INVALID-JSON"}
{"name": "129 nested arrays", "input": "[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]", "refused": "JPS-RESOURCE-DEPTH-LIMIT"}
{"name": "an empty document", "input": "", "refused": "empty"}
{"name": "HMAC-SHA-256 of a facts text", "canonical": "{\"amount\":\"1250\",\"member\":\"M-0042\"}", "keyHex": "5245434f474e495341424c452d544553542d4b45592d3766336139632d646f2d6e6f742d757365", "hmacSha256": "e7f602ba0b2a3002a6d8c6056e00c0a98f3b198467bb3dd5fc231563e88e1e23"}
{"name": "HMAC-SHA-256 of an evidence text", "canonical": "{\"invoice\":\"present\"}", "keyHex": "5245434f474e495341424c452d544553542d4b45592d3766336139632d646f2d6e6f742d757365", "hmacSha256": "456b73db91e85562c6199148f638c3f7530a415a6a501d0f6c0b84a43eba583b"}
{"name": "HMAC-SHA-256 of the text of an omitted evidence document", "canonical": "{}", "keyHex": "5245434f474e495341424c452d544553542d4b45592d3766336139632d646f2d6e6f742d757365", "hmacSha256": "bfc86cc321a10572353a43fea41e293424347f11becafe25ff333f461d6ca20c"}
```

## The keyed form

A plain digest of a document whose values come from a small space, an identifier or an address, is
a guessable pseudonym: anyone holding the record can compute the digest of each candidate document
and compare. The evidence document is worse placed still: its members are the pack's declared
evidence requirements and its values `present`, `absent` and `unknown`, so anyone holding the pack
can try every document and recover it from its plain digest. The keyed form names both documents
instead by digests the caller computes with a key of its own, which this runtime never receives:

```go
facts, err := record.CanonicalFacts(factsBytes)
evidence, err := record.CanonicalEvidence(evidenceBytes) // nil: omitted, the text {}
mac := hmac.New(sha256.New, key)                         // the caller's key, kept by the caller
mac.Write(facts)
factsDigest := mac.Sum(nil)
mac.Reset()
mac.Write(evidence)
evidenceDigest := mac.Sum(nil)
evaluated, decided, err := pack.EvaluateWithKeyedRecord(ctx, factsBytes, evidenceBytes, binding,
    record.KeyedDigests{Algorithm: record.HMACSHA256, KeyID: "k-2f9c", Facts: factsDigest, Evidence: evidenceDigest})
```

- **The algorithm.** The list of labels is closed and holds one: `hmac-sha256`, HMAC (RFC 2104)
  over SHA-256, whose output is 32 bytes. A label outside it is refused when a record is built and
  when one is read. A second label buys nothing here: the protection is the key's secrecy, and a
  256-bit output is not the weak point; HMAC-SHA-256 is in the standard library of every common
  language and is offered by common key-management services; and each further label is
  one more way for two holders of one key to write two records of one evaluation that do not agree.
  A label is added, if one is ever needed, by a new minor version of the keyed form.
- **The key identifier** is the caller's name for the key, so that a reader can find the key a
  record was made with: 1 to 128 characters from `A`–`Z`, `a`–`z`, `0`–`9`, `.`, `_`, `-`, `:` and
  `/`. It is data: this runtime never reads meaning into it, never compares it with anything, and
  never repeats it in an error. The record carries it in clear, so it is as visible as the record.
  Use an opaque identifier, such as a random string or a version number, never one derived from the
  facts, from whoever or whatever they describe, or from the key itself, and never a name that says
  more than you would say in the record.
- **What this runtime checks.** The label is in the list, the identifier has its shape, and each
  digest is the algorithm's output size, all before anything is evaluated, with an error wrapping
  `record.ErrKeyedDigests` (or `record.ErrBinding` for the binding). After the evaluation it refuses
  a keyed digest that is the plain SHA-256 of either document, as passed or as canonical text, and a
  binding digest that is one: either would carry beside the keyed digests the plain digest they
  replace. Neither refusal is a `*Refusal`, and the evaluation is not returned with it.
- **What it cannot check.** Without the key, this runtime cannot tell whether a keyed digest is the
  digest of the documents evaluated, of other documents, or of anything at all: it carries them as
  the caller's assertion, as it carries the binding. Compute them over the bytes
  `CanonicalFacts` and `CanonicalEvidence` return for the same bytes you pass to
  `EvaluateWithKeyedRecord`. It cannot judge the key either: a short or guessable key is as weak as
  no key.
- **Linkability.** One key and one document give one keyed digest, so records made under one key
  can be matched to each other by their keyed digests, and anyone who learns the document behind one
  learns it for every record with the same digest under that key. This matters most for the
  evidence document, whose few possible values repeat across records. Use different keys, with
  different identifiers, where records must not be linkable.

## Checking a record

`record.Read(bytes)` reads a record of either form from its exact bytes, and
`DecisionRecord.CheckDocuments(facts, evidence, keyed)` compares the documents a caller holds with
the record's digests, reading the record's bytes, not its exported fields. `keyed` is a
`record.KeyedDigestFunc`, the caller's function from an algorithm label, a key identifier and a
canonical text to a keyed digest: the key stays with the caller, and the package passes the function
nothing else. The result names the form and, for each document, one of:

| outcome | meaning |
|---|---|
| `match` | the digest of the document's canonical text is the one the record carries |
| `mismatch` | it is not: a different document, or, for a keyed record, a different key |
| `not-checkable-without-key` | the record is keyed and no function was given: whether the document is the one the record names is not known; this is neither a match nor a mismatch |

A plain record is checked without a key, and the function is not called. Keyed digests are compared
in constant time. A function that fails, or returns a digest of another size, gives an error
wrapping `record.ErrKeyedDigestFunc` whose text repeats nothing the function returned; the function's
own error is reachable through `errors.Is` and `errors.As`. The check says whether the documents are
the ones the record names, and nothing of the pack, the disposition, the binding or who wrote the
record.

## What in a record follows from the facts

| member | follows from the facts? | plain form | keyed form |
|---|---|---|---|
| `factsDigest` | yes: a guessable pseudonym of the facts | present | absent |
| `evidenceDigest` | from the evidence document, whose few possible values anyone holding the pack can try | present | absent |
| `keyedDigests.facts`, `keyedDigests.evidence` | yes, under the caller's key: not computable without it | absent | present |
| `keyedDigests.algorithm` | no | absent | present |
| `keyedDigests.keyId` | no, unless the caller makes it so; see above | absent | present |
| `disposition` | yes: it is the decision, a choice among the pack's declared outcomes, reasons and handoff states, so it says what the pack decided (that an amount was at or above a threshold, say) without naming the facts it decided on; no fact value is copied into it through package `sdk`, which does not offer the draft outcome values (ADR-0039) that could copy one | present | present |
| `binding` | not by this runtime; the caller may bind a digest of anything, and in the keyed form a digest that is the plain SHA-256 of the facts or evidence document, as passed or as canonical text, is refused | as given | as given |
| `evaluator`, `pack`, `recordVersion` | no | present | present |
| the record's own digest, `Digest()` | yes, through every member above: in the plain form a guessable pseudonym of the facts as well, since anyone holding the pack can evaluate a candidate, write its record and compare; in the keyed form not computable without the key | | |
| the typed fields `FactsDigest`, `EvidenceDigest` | as their members | set | empty, so encoding the struct carries no plain digest |

The `Evaluation` returned beside a record, and the audit trail a project keeps (which holds the facts
document itself), are not the record and are not changed by its form.

## Versions

`recordVersion` follows the rule in [VERSIONING.md](../VERSIONING.md#go-api): its major part moves
when a member is removed or renamed, or a member's value is taken over other bytes or written in
another form; its minor part when a member is added; its patch part for any other change of bytes.
The keyed form removes `factsDigest` and `evidenceDigest`, so it is major version 2; the plain form
stays `"1.0.0"`, byte for byte. A later change to either form moves that form's version by the same
rule, and `record.Read` admits only the versions it knows.
