# Pack signatures

A pack signature is a small JSON document, kept beside a pack file, holding an Ed25519 signature
over that file's exact bytes ([ADR-0083](adr/0083-sign-a-pack-file-and-check-the-signature-before-use.md)).
It lets the holder of a key sign a pack, and lets a reader refuse the pack unless the key it
expects signed those bytes. It is a convention of this runtime, not part of the Judgment Pack
specification: the pack's format, its `sha256:` digest, lock files and the conformance claim in
[CONFORMANCE.md](../CONFORMANCE.md) are unchanged by it, and nothing is added to the pack.

The surfaces are experimental and may change in any minor release before 1.0
([VERSIONING.md](../VERSIONING.md)).

## What a valid signature establishes, and what it does not

A signature the runtime accepts establishes one thing: **these exact bytes were signed by the
holder of this key**, where "this key" is the public key the reader named as expected.

It does not establish that the pack is correct, that it was tested, that it is the current
version, or that anyone approved it. It does not say when it was signed: the document holds no
time, and a signature made long ago, by a key since lost or retired, is accepted the same way.
Which key to expect, how a reader comes to hold it, and when a key stops being trusted are the
reader's to decide: the runtime distributes no keys, keeps no list of them and knows no revocation.
A signature whose key the reader did not expect is refused, never accepted on the document's word.

The signature names the pack by digest, as a lock does. A pack that `packs verify` reports as the
reviewed set and whose signature is accepted is still only those bytes, signed by that key.

## The convention

**File name.** The signature of `<name>` is `<name>.sig.json` in the same directory: for
`expense.json`, `expense.json.sig.json`. `jpack packs sign` writes it there and
`jpack packs verify-signature` reads it there, unless `--output` or `--signature` names another
file.

**The document.** One JSON object with exactly these five string members:

```json
{"algorithm":"ed25519","packDigest":"sha256:<64 hex>","publicKey":"<64 hex>","signature":"<128 hex>","signatureVersion":"1.0.0"}
```

- `signatureVersion`: `"1.0.0"`, the form of the document and of the message it signs.
- `algorithm`: `"ed25519"`.
- `publicKey`: the signer's 32-byte Ed25519 public key in lowercase hexadecimal, the form
  `jpack audit key public` prints.
- `packDigest`: `sha256:` and the lowercase hexadecimal SHA-256 of the pack file's exact bytes, the
  digest `packs lock`, `packs covers` and `Pack.Identity()` report.
- `signature`: the 64-byte Ed25519 signature in lowercase hexadecimal.

The runtime writes the members in this order, with no white space and a final newline.

**What is signed.** Not the pack's bytes themselves, and not the bare digest, but this message:

```text
judgment-pack-runtime/pack-signature/1:{"packDigest":"sha256:<64 hex>","signatureVersion":"1.0.0"}
```

The prefix keeps a pack signature apart from every other signature the runtime makes or checks with
a key: an audit trail's record signatures and key rotations, and a checkpoint witness's statement.
None of those is a pack signature, and a pack signature is none of those, even when the same key
makes both.

**How a reader is told which key to expect.** By naming it, every time: `--public-key <file>` on the
command line (a file holding the key in lowercase hexadecimal, as `audit key public` writes it), or
the `expected` argument in Go. There is no default and no mode that trusts the key the document
carries. Without an expected key, `packs verify-signature` refuses to run and says why.

## Signing

```sh
jpack packs sign expense.json --key /secure/path/pack-signing.seed --public-key pack-signing.pub
```

The seed file is held exactly as an audit trail's signing key is: an absolute path with no
symbolic link on it, one regular file with one name, owned by the user running the command and
readable by no one else, in directories others cannot write. Every key is refused on Windows, where
those checks cannot be made. `jpack audit key generate` makes such a file. `--public-key` is
optional here: when given, a seed whose public key is another is refused before anything is
signed, so a file swapped in at the seed's path signs nothing. The environment variables that select
a project's audit key are not read.

The pack must validate as `jpack spec validate` validates it, for any spec version the runtime
bundles; a file that does not is not signed. An existing signature document is never replaced.

## Checking

```sh
jpack packs verify-signature expense.json --public-key pack-signing.pub
```

The command reads the pack once and checks the signature against those bytes **before** reading
them as a pack. If the signature is refused, the run exits 1 with the refusal's code and the pack
is never parsed. If it is accepted, the same bytes are validated and both results are reported:

```text
signature: accepted: expense.json.sig.json names key <id> (public key <hex>), the expected key, over sha256:<hex>
pack: valid (specVersion 0.5.0)
```

The run exits 0 for a valid pack, and 1 or 2 when the pack is invalid or of an unsupported version,
the exits `spec validate` gives; the signature is still reported as accepted. `--format json`
reports the same as one JSON object.

| Code | Exit | Meaning |
|---|---|---|
| `JPS-INVOCATION-PACK-SIGNATURE` | 3 | `--public-key` (verify) or `--key` (sign) was not given |
| `JPS-PACK-SIGNATURE-EXPECTED-KEY` | 3 | the expected key is not a public key a verifier may trust |
| `JPS-PACK-SIGNATURE-EXPECTED-KEY-READ` | 4 | the expected key's file could not be read |
| `JPS-PACK-SIGNATURE-MISSING` | 1 | there is no signature document: the pack is unsigned |
| `JPS-PACK-SIGNATURE-READ` | 4 | the document could not be read, or is a symbolic link |
| `JPS-PACK-SIGNATURE-TOO-LARGE` | 1 | the document is over 4096 bytes |
| `JPS-PACK-SIGNATURE-MALFORMED` | 1 | the document is not one JSON object, or holds a backslash escape |
| `JPS-PACK-SIGNATURE-DUPLICATE-MEMBER` | 1 | a member is given twice |
| `JPS-PACK-SIGNATURE-VERSION` | 1 | `signatureVersion` is not `1.0.0` |
| `JPS-PACK-SIGNATURE-UNKNOWN-MEMBER` | 1 | a member `1.0.0` does not define |
| `JPS-PACK-SIGNATURE-MISSING-MEMBER` | 1 | a member `1.0.0` requires is missing |
| `JPS-PACK-SIGNATURE-ALGORITHM` | 1 | `algorithm` is not `ed25519` |
| `JPS-PACK-SIGNATURE-PUBLIC-KEY` | 1 | `publicKey` is not of its form, or not a key a verifier may trust |
| `JPS-PACK-SIGNATURE-PACK-DIGEST` | 1 | `packDigest` is not of its form |
| `JPS-PACK-SIGNATURE-VALUE` | 1 | `signature` is not of its form |
| `JPS-PACK-SIGNATURE-UNEXPECTED-KEY` | 1 | the document's key is not the expected key |
| `JPS-PACK-SIGNATURE-DIGEST-MISMATCH` | 1 | the pack's bytes are not the bytes the document names: changed, or another pack's signature |
| `JPS-PACK-SIGNATURE-INVALID` | 1 | the signature does not hold under the expected key |

A value of its form is lowercase hexadecimal of the exact length; an uppercase letter, a base64
spelling or another length is refused. White space between the document's tokens is admitted.

`packs sign` adds `JPS-AUDIT-KEY-REFUSED` (exit 1, the seed is refused as the audit commands
refuse it), `JPS-PACK-SIGNATURE-PACK-NOT-VALID` (exit 1, or 2 for an unbundled version),
`JPS-PACK-SIGNATURE-EXISTS` and `JPS-PACK-SIGNATURE-WRITE` (exit 4).

## From a Go program

Package `sdk/packsig` applies the same check without reading any file:

```go
pack, signature, err := packsig.Load(ctx, packBytes, signatureBytes, expectedKey, sdk.LoadOptions{})
// err is a *packsig.Refusal, and nothing was loaded, when the signature is refused;
// a *sdk.Refusal, beside the accepted signature, when the signed bytes are not a pack sdk.Load admits.
```

`Load` checks the signature against its own copy of `packBytes` and loads that same copy, so the
bytes loaded are the bytes checked. `packsig.Verify(packBytes, signatureBytes, expectedKey)` checks
without loading. A refusal's `Code` is one of the codes above.
