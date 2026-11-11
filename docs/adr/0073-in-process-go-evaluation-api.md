---
status: proposed
date: 2026-11-10
deciders: maintainer
---

# Expose loading and evaluating a pack as an experimental in-process Go API

## Context and problem statement

A Go program that evaluates packs today has to start the `jpack` binary for each decision, or the
MCP server, because every Go package of this module is `internal` and package `sdk` was an empty
placeholder (#368). Starting a process for each decision costs a fork, a schema compile and a
re-validation of the same pack every time. Without a public package, each host also writes its own
wrapper around the CLI's JSON, which is a second reader of the evaluator's output.

Material-decision impact: public-surface, documented-claim.

## Decision drivers

- One evaluator: the package must call the shared engine with the options a CLI surface already
  uses, and add no evaluation semantics. Its results must be the engine's, byte for byte.
- No ambient I/O: inputs are bytes the caller holds. The package reads no project, file,
  environment variable or network.
- A loaded pack is validated once and reused safely across goroutines.
- The §8.3 disposition and the §8.4 class and phase are typed values, and no internal type appears
  in an exported signature.
- The surface stays small and experimental, so it can change before 1.0 without a migration.

## Considered options

- A small package `sdk`: `Load` admits a pack once, `Pack.Evaluate` evaluates facts and evidence,
  and `Describe` names the versions. The contract selection and options are those of
  `experimental evaluate` outside a project.
- Export `internal/evaluation`'s engine and options as they are.
- Keep the binary and its JSON output as the only integration boundary.

## Decision outcome

Chosen option: the small package `sdk`, because it gives an in-process caller the engine's results
without exposing the engine's option set, which includes draft-RFC prototype opt-ins, work-budget
overrides and project-only checks that this surface must not offer. Exporting the engine would make
those options public and freeze internal types. Keeping only the binary leaves the per-decision cost
and the per-host JSON readers.

`Load(ctx, pack, LoadOptions{SupportedExtensions})` selects the evaluator contract from the pack's
declared `specVersion` with `evaluation.SelectContract`, as the CLI and the MCP server do. It then
admits the pack with `AdmitPack` and runs the pack half of the §8.2 preflight once, through the
new `AdmittedPack.PackFailure`. It runs the same step `EvaluateAdmitted` now runs for that half
(the draft-pair check, the pack byte limit and the memoized admission), so the two cannot diverge. The admission is memoized under the options `Evaluate` uses, so `Evaluate` never
validates the pack again. A refusal at `Load` carries the engine's class, phase, code and message.
When the pack failed validation, it also carries the diagnostics `jpack spec validate` reports for
the same bytes.

`SupportedExtensions` is a `Load` option, not an `Evaluate` one, because admission and its memo key
depend on it. The unsupported-required-extension refusal it can cause is still returned by
`Evaluate`, after the facts and evidence documents, where §8.2 places it and §8.4 orders it. A nil
evidence slice is an omitted document; a non-nil slice is a supplied one, so empty bytes are the
malformed-input error, as on the CLI.

Errors are of three kinds: a `*Refusal` for a refusal of the caller's input, an error wrapping
`ErrInternal` for a fault of this runtime, and `ctx.Err()`. Every `*Refusal` carries its §8.4 class
except `profile-unsupported`, which Core `0.3.0` refuses before §8.4 applies and which the CLI also
reports without a class. The context is checked on entry and, in `Load`, again before validation.
A started validation or evaluation is not interrupted; the documented limits bound it.

`Describe` reports the bundled specification versions and evaluator contracts from
`describe.Runtime`, the source `jpack describe` uses. For each contract it gives the `suiteVersion`
of the bundled evaluation corpus, from the new `evaluation.CorpusSuiteVersion`. That function reads
the same schema-checked manifest `RunCorpus` reads.

The package is an eleventh surface that reaches the evaluator. CONFORMANCE.md and README.md name it beside the CLI and MCP surfaces, and the
enumeration test counts its constructor site.

### Consequences

- Good, because a Go program evaluates in-process with the engine's results and refusals, and a
  test runs both bundled corpora through the public path and compares each row with the corpus
  expectation, the corpus runner and the engine's envelope.
- Good, because no package of this module in the package's import closure imports anything that
  reaches files, the environment or the network, and the only third-party packages in it are the
  schema validator's, whose schema loading the validation package keeps offline. Tests hold this:
  one reads the import closure from source, and one evaluates from a child process with an empty
  environment and an unreadable project file in its working directory.
- Bad, because the API is a new public surface: changing it is a release-note item, and from 1.0 a
  compatibility promise.
- Bad, because `Evaluation.JSON` reports the module's compiled-in tool version, which an importing
  program does not set unless it links one in; `Describe` reports the module version from build
  information instead.
- Revisit when a caller needs a project feature (configuration, reviewed-set locks, audit records),
  a work-budget override, cancellation inside a running evaluation, or a stable API at 1.0.

## More information

- Package documentation: `sdk/doc.go`; the runnable example: `sdk/example_test.go`.
- [VERSIONING.md](../../VERSIONING.md#go-api) states the stability level.
- [ADR-0007](0007-experimental-evaluator.md) (the shared evaluator) and the admission memo of
  issue #78, which `Load` reuses.
