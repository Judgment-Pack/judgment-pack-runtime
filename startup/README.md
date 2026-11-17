# startup

```
go get github.com/Judgment-Pack/judgment-pack-runtime/startup
```

Package `startup` is what a service runs at start to read its registry (spec RFCs 0034 and 0035).
It reads one environment's resolved registry, and the values of the secrets that registry refers
to, from where the platform delivered them. It never contacts a secret store, and it has no
dependency outside the Go standard library.

This is a Go module of its own, nested in the reference runtime's repository and tagged
`startup/vX.Y.Z`. Its exported identifiers are under a 1.0 compatibility promise, while `jpack`
and the runtime module stay 0.x ([VERSIONING.md](../VERSIONING.md#the-start-up-module)). The
design is recorded in [ADR-0096](../docs/adr/0096-a-start-up-module-that-reads-a-resolved-registry-and-delivered-secrets.md).

## Where values come from

A registry holds secret references, `secret:gcp-sm:projects/P/secrets/N/versions/V`, never values.
`jpack` and CI check those references and never fetch them. At deployment, the platform reads each
pinned version from the store and injects it into the service's process, as an environment
variable or a mounted file. At start, this package reads it from there:

```go
resolved, err := startup.ReadResolvedFile("registry/resolved/prod.json", "prod")
// ...
secrets := startup.Bindings{
	Env:   map[string]string{"secret:gcp-sm:projects/example-prod/secrets/billing-db/versions/7": "BILLING_DB_PASSWORD"},
	Files: map[string]string{"secret:gcp-sm:projects/example-prod/secrets/billing-api-key/versions/2": "/run/secrets/billing-api-key"},
}
password, err := secrets.ReadSecret(ctx, reference) // reference: the string at the entry's secret position
```

The bindings say where the deployment put each pinned version. A deployment emitter that writes
the platform's secret configuration can write them too. A reference is a key: it is matched
exactly, never parsed and never resolved.

## The API

**Resolved registry**
- `ReadResolved(data, environment)` and `ReadResolvedFile(path, environment)` return a `*Resolved`
  with `Registry`, `Environment`, `Kinds` and `Entries`. Each entry's `Value` is its raw JSON, with
  every reference as written.
- `Resolved.Entry(kind, id)`, `Resolved.EntriesOf(kind)` and `Resolved.Kind(kind)` look entries
  and kind pins up.
- `ResolvedFormatVersions()` lists the exact `resolvedFormatVersion` selectors this release reads:
  `1.0.0`.

**Reading secrets**
- `Reader`: `ReadSecret(ctx, reference) (Secret, error)`. Every secret reader implements it.
- `Bindings`: a reference's environment variable (`Env`) or file (`Files`).
- `ReadEnv(name)` and `ReadFile(path)`: the two readers `Bindings` uses, also usable on their own.

**Holding secrets**
- `Secret`: `Reveal()` returns a copy of the bytes, and `IsZero()` says whether it holds a value.
  `NewSecret(bytes)` makes one, for a reader written elsewhere.

## Refusals

Every refusal is an error that `errors.Is` matches against an exported sentinel. No error carries a
secret's value, a secret reference, or a value from the document.

| Refused | Error |
|---|---|
| `resolvedFormatVersion` missing, or not exactly one of `ResolvedFormatVersions()` | `ErrSelector` |
| A document resolved for another environment, or no environment named | `ErrEnvironment` |
| Over 8 MiB, or nesting deeper than 32 (RFC 0034 §1) | `ErrLimit` |
| Not one UTF-8 JSON value, a duplicate member name anywhere, a member missing, unknown or of the wrong type, entries or kinds out of canonical order | `ErrDocument` |
| A reference with no binding, or bound to both a variable and a file | `ErrNoBinding`, `ErrTwoBindings` |
| An environment variable that is not set | `ErrNotSet` |
| A value that is empty once its trailing newline is trimmed | `ErrEmpty` |
| A value over 64 KiB | `ErrLimit` |
| A path that is not a regular file (for `ReadResolvedFile` too, checked on the opened file) | `ErrNotRegular` |
| A file others may read (permission bit `0o004`) | `ErrWorldReadable` |
| A file others may write (permission bit `0o002`), under every `FileRules` | `ErrWorldWritable` |
| Any file, on an operating system that is not Unix | `ErrPermissionsUnknown` |

The selector is read before any other member, so a document of a later format is refused as a
selector this release does not read.

## The trailing newline

Both readers trim exactly one trailing `\n` or `\r\n`, and keep every other byte: `"v\n"` and
`"v\r\n"` read as `v`, `"v\n\n"` as `"v\n"`, and `"v\r"` as `"v\r"`. Leading and inner whitespace is
kept.

## Mounted files

`ReadFile` follows symbolic links, as a mounted secret volume uses them, and checks the file they
lead to. It refuses that file when others may read it, and checks again on the opened file, so the
bytes read are those of the file checked.

The common platforms mount secret files world-readable unless told otherwise:

| Platform | Default mode of a mounted secret file | Set it with |
|---|---|---|
| Cloud Run | `0444` | the secret volume's `defaultMode`, or an item's `mode` |
| Kubernetes | `0644` | the secret volume's `defaultMode`, or an item's `mode` |

Mount with `0440` or `0400`, and the strict default reads the file. Where the mode can't be set,
`ReadFileWith(path, startup.FileRules{AllowOtherRead: true})`, or `FileRules` on `Bindings`,
accepts a file others may read. Every other check still applies. Use it only where no other user
shares the file system, such as a container that runs one process.

Outside Unix the permission bits Go reports are synthesized, so `ReadFile` and `ReadFileWith` refuse
every file there, whatever the rules. Use environment variables instead.

## Printing a secret

A `Secret` prints `[secret]` under every fmt verb, `%v`, `%+v`, `%#v`, `%s`, `%q` and `%x` included.
It is also `[secret]` through `String`, `encoding/json`, `encoding.TextMarshaler` (which
`encoding/xml` uses) and `log/slog`. fmt answers `%T` with the type's name, and `%p` with a notice
that holds an address. Neither reaches the bytes, including when a `Secret` sits in an unexported
struct field that fmt prints by reflection. `encoding/gob` refuses to encode it. A `Secret` is never
decoded: `UnmarshalJSON` and `UnmarshalText` return `ErrNotDecodable`.

## Not in this release

A reader for a secret store comes later, as a sub-package of its own that implements `Reader`. No
store client is a dependency of this module.
