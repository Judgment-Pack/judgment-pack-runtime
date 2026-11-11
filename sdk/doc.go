// Package sdk is the public, in-process Go API over the judgment-pack
// runtime: it lets a Go program load a pack and evaluate it with the same
// shared evaluator the jpack CLI and MCP server use, without invoking the
// binary or opening a project directory.
//
// It is a thin layer. Load and Pack.Evaluate select the evaluator contract,
// run the §8.2 preflight and the §§7–8 evaluation, and apply the §10 limits
// exactly as "jpack experimental evaluate" does outside a project; this
// package adds no evaluation semantics of its own. It reads no file, no
// environment variable and no network: every input is a byte slice the
// caller holds, and the specification artifacts are the ones embedded in the
// module.
//
// Draft-RFC prototype options, work-budget overrides, project features
// (configuration, reviewed-set locks, audit records, signing) and the
// validation-only and test surfaces are not offered here.
//
// # Stability
//
// Every exported identifier is experimental: while the module is pre-1.0 it
// may change or be removed in any minor release, with the change named in
// the release notes. VERSIONING.md states the policy. The exported
// identifiers, all at that level, are:
//
//   - Load, LoadOptions and Pack, with Pack.Evaluate;
//   - Evaluation, with Evaluation.CanonicalDisposition and Evaluation.JSON;
//   - Disposition, Handoff and Target;
//   - Refusal, with Refusal.Error, Diagnostic, and ErrInternal;
//   - Describe, Description and Evaluator;
//   - the constants ClassPackNotConformant, ClassMalformedInput,
//     ClassUnsupportedRequiredExtension, ClassResourceExhaustion,
//     PhasePreflight and PhaseEvaluation.
//
// The bytes CanonicalDisposition returns are the §8.3 portable disposition,
// whose form the specification fixes. The envelope Evaluation.JSON returns
// follows the CLI's machine-output contract and its outputVersion.
//
// # Errors
//
// Load and Pack.Evaluate return one of three kinds of error:
//
//   - a *Refusal, for a refusal of the caller's input, carrying the JPS Core
//     §8.4 class and phase (the class is empty only for a "profile-unsupported" pack) and
//     the code and message the CLI reports;
//   - an error wrapping ErrInternal, for a fault of this runtime, such as
//     bundled artifacts that fail their integrity check; it is never a
//     *Refusal;
//   - ctx.Err(), for a context that is done before the work starts.
//
// A nil context, or a Pack that Load did not return, is a plain error of none
// of these kinds. The methods of Evaluation are for a value a successful
// Evaluate returned.
//
// # Context
//
// Load checks its context on entry and again before validation starts;
// Pack.Evaluate checks it on entry. A validation or evaluation that has
// started is not interrupted: it runs to completion, bounded by the
// documented limits. A done context is returned as ctx.Err() and never as a
// Refusal.
//
// # Conformance
//
// The runtime's conformance claim is stated, in full and only, in
// CONFORMANCE.md at the module root, which names this surface among those
// that reach the shared evaluator.
package sdk
