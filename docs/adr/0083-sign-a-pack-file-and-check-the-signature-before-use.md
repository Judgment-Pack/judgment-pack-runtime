---
status: proposed
date: 2026-11-12
deciders: maintainer
---

# Sign a pack file's bytes, and check the signature against an expected key before the pack is used

## Context and problem statement

A pack moves between the people who write it and the services that evaluate it. The runtime can
already say which bytes a pack is (its `sha256:` digest, [ADR-0019](0019-reviewed-set-lock.md)) and
can sign an audit trail with an Ed25519 key ([ADR-0047](0047-make-a-decision-record-defensible.md)), but nothing lets a
holder of a key sign a pack, or lets a reader refuse a pack whose signature is not by the key it
expects (issue #391). A reader holding only a pack and a digest learns which bytes it has, not whose
they are.

Material-decision impact: public-surface.

## Decision drivers

- What is signed is the pack file's exact bytes, the bytes the digest is over. No canonical form of
  a pack is introduced, and the pack format, the digest, the lock files and the conformance claim do
  not change.
- No signature this runtime makes for another purpose (a trail record, a key rotation, a checkpoint
  witness) is a pack signature under the same key, and no pack signature is one of those.
- The reader names the key it expects. A signature by any other key is refused, including one that
  holds under the key its document names.
- A private key is held exactly as a trail's signing key is held: the same file checks and the same
  expected-public-key guard (`audit.LoadSigner`).
- An embedding service can refuse an unsigned or wrongly signed pack before any rule is read, and
  loads the bytes it checked, not a second read of the file.
- The public Go API stays free of ambient IO (the closure test in `sdk/boundary_test.go`).

## Considered options

- Sign a canonical form of the pack (its parsed JSON re-encoded). Rejected: it would introduce a
  canonical form the spec does not define, and two files with one canonical form would share a
  signature although their digests differ.
- Sign the bare pack bytes, or the bare digest. Rejected: a message with no context is also a
  message of any other kind a key may sign.
- Carry the signature inside the pack. Rejected: it changes the pack format and the digest.
- A detached signature document beside the pack, over a context-prefixed message naming the
  document's version and the pack's digest. Chosen.

## Decision outcome

Chosen option: the detached signature document, because it leaves the pack's bytes, digest and
format exactly as they are, and its message cannot be read as any other message this runtime signs.

**The document.** `<pack file name>.sig.json`, in the pack's directory, is the default the two
commands write and read; `--output` and `--signature` name another file. It is one JSON object with
exactly five string members: `signatureVersion` `"1.0.0"`, `algorithm` `"ed25519"`, `publicKey` (the
full 32-byte key, 64 lowercase hexadecimal characters), `packDigest` (`sha256:` and 64 lowercase
hexadecimal characters) and `signature` (128 lowercase hexadecimal characters). The writer emits the
members in code-point order, with no white space and a final newline (360 bytes).

**Hexadecimal, not base64.** Every key and signature the audit tooling writes or reads (`audit key public`, `--public-key`, a trail's signature
sidecar) is lowercase hexadecimal, so the pack document uses the same spelling. Lowercase
hexadecimal of an exact length has one spelling per value, which is what "canonical" asked for: an
uppercase letter, a base64 spelling and a wrong length are each refused.

**The full key, not its identifier.** The document carries the public key itself, so a reader can
compare it with the expected key byte for byte; the 32-character key identifier (`KeyID`) is
reported beside it and is not part of the document.

**What is signed.** Ed25519 over the bytes
`judgment-pack-runtime/pack-signature/1:{"packDigest":"sha256:<hex>","signatureVersion":"1.0.0"}`.
The prefix is not a prefix of, and does not begin with, the record-signature, key-rotation or
witness prefixes, and each of those messages begins with its own prefix, so no message of one kind
is a message of another. A test signs each kind with one key and presents it as the other, in both
directions, including a pack whose bytes are exactly a record message.

**Reading a document.** Strict, with one code per failure: more than 4096 bytes
(`JPS-PACK-SIGNATURE-TOO-LARGE`); not one JSON object, or holding a backslash, since no name or
value needs an escape (`-MALFORMED`); a member twice (`-DUPLICATE-MEMBER`); a `signatureVersion`
other than `1.0.0`, checked before the other members so a later version is named as such
(`-VERSION`); a member not defined (`-UNKNOWN-MEMBER`); a member missing (`-MISSING-MEMBER`); an
algorithm other than `ed25519` (`-ALGORITHM`); a public key not of its form, or not one a verifier
may trust: small order, a non-canonical encoding or off the curve (`-PUBLIC-KEY`); a digest or
signature not of its form (`-PACK-DIGEST`, `-VALUE`). White space between tokens is admitted, so a
document reformatted by hand still reads; the members' values admit one spelling each.

**Checking a document.** The expected key is required: none, or one a verifier may not trust, is
`JPS-PACK-SIGNATURE-EXPECTED-KEY`, and there is no mode that trusts the key in the document. In
order: the document's key differs from the expected key (`-UNEXPECTED-KEY`); the SHA-256 of the
pack's bytes differs from the document's digest (`-DIGEST-MISMATCH`, which is also how a signature
of another pack moved beside this one is refused); the signature does not hold under the expected
key (`-INVALID`). The first two are checked before the signature so the reader learns the plainer
reason. The check is in `internal/packsig`, which reaches no file, environment or network, and is
the one check the CLI and the Go API apply.

**`jpack packs sign <pack-file> --key <seed-file> [--public-key <file>] [--output <file>]`.** The
seed is opened by `audit.LoadSigner`, so it is refused exactly as a trail's signing key is
(`JPS-AUDIT-KEY-REFUSED`: a relative or symbolically linked path, a file that is not one regular
file with one name, one others can read, one not owned by the user running the command, a directory
on its path others can write, and every key on Windows). `--public-key` is the expected-key guard
of `audit key public`: a seed whose public key is not that key is refused before anything is
signed. The `JPACK_SIGNING_KEY` and `JPACK_SIGNING_PUBLIC_KEY` environment variables, which select a
project's trail key, are not read: a pack is signed with the seed the command names. The pack is
read once and validated with `spec validate`'s engine through the semantic stage; a file that does
not validate is refused (`JPS-PACK-SIGNATURE-PACK-NOT-VALID`, exit 1, or 2 for a version this
runtime does not bundle), so nobody signs a broken pack by accident. This admits a pack of every
spec version the runtime bundles, `0.1.0-draft` included. The document is created, never replaced
(`JPS-PACK-SIGNATURE-EXISTS`, exit 4).

**`jpack packs verify-signature <pack-file> --public-key <file> [--signature <file>]`.** No
`--public-key` refuses the run with exit 3 and a message saying the expected key is required and
why. The signature document is read with the input rules every command uses: a final symbolic link
is refused (`JPS-PACK-SIGNATURE-READ`, exit 4), a document over the bound is `-TOO-LARGE` (exit 1),
and no document at all is `JPS-PACK-SIGNATURE-MISSING` (exit 1: the pack is unsigned). The pack is
read once; the signature is checked against those bytes **before** they are parsed, and a refusal
exits 1 with the pack never read as a document. Only an accepted signature lets the same bytes be
validated, and the result reports both: the accepted signature, and the pack's validation status
and `specVersion`. Status `valid` exits 0; `pack-invalid` exits 1 and `pack-unsupported` exits 2,
the exits `spec validate` gives, with the signature still reported as accepted. Validation, not
`sdk.Load`, is what this command applies, so it reads every bundled spec version.

**`sdk/packsig`.** `Verify(pack, signature []byte, expected ed25519.PublicKey) (Signature, error)`
and `Load(ctx, pack, signature []byte, expected, sdk.LoadOptions) (*sdk.Pack, Signature, error)`.
Both take bytes, so the caller reads the pack once. `Load` clones the bytes, checks the signature
against the clone and hands that same clone to `sdk.Load`: a caller that overwrites its slice in
between changes nothing that is loaded. A refusal before the load is a `*packsig.Refusal` and no
rule is read; a load refusal after an accepted signature is the `*sdk.Refusal` `sdk.Load` returns,
with the accepted `Signature` beside it. The package is experimental, like the rest of the Go API
(VERSIONING.md).

**Not added:** an MCP tool, a "trust the key in the document" mode, more than one expected key per
run, the pack read from standard input, or any time or signer metadata in the document.

### Consequences

- Good, because a reader can refuse a pack whose bytes were not signed by the key it expects,
  before any rule is read, in the CLI and in an embedding service.
- Good, because the pack, its digest, lock files and the conformance claim in CONFORMANCE.md are
  unchanged; a lock still names a pack by digest, and a signature names the same digest.
- Bad, because a signature says nothing about when it was made, and key distribution and revocation
  are left to the reader, as the documentation says.
- Bad, because `ed25519` and lowercase hexadecimal are fixed for `1.0.0`; another algorithm or
  spelling is a new major version of the document.
- Revisit when a reader needs more than one acceptable key, a signature time, or a signature over a
  set of packs rather than one file.

## More information

- [Pack signatures](../pack-signatures.md): the convention, and what a valid signature establishes
  and does not.
- `internal/packsig`, `internal/edkey` (the public-key checks moved out of `internal/audit` so the
  Go API can apply them without ambient IO), `internal/cli/packs_signature.go`, `sdk/packsig`.
