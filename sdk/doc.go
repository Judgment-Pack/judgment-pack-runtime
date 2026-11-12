// Package sdk is the public, in-process Go API over the judgment-pack
// runtime: it lets a Go program load a pack and evaluate it with the same
// shared evaluator the jpack CLI and MCP server use, and describe the loaded
// pack for review, without invoking the binary or opening a project
// directory.
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
//   - Pack.Identity and Identity, and Pack.View, which returns the
//     view.PackView of package sdk/view;
//   - Pack.FilteredView, which returns the view.FilteredView of package
//     sdk/view (Core 0.5.0 only);
//   - the constants ClassPackNotConformant, ClassMalformedInput,
//     ClassUnsupportedRequiredExtension, ClassResourceExhaustion,
//     PhasePreflight and PhaseEvaluation.
//
// The bytes CanonicalDisposition returns are the §8.3 portable disposition,
// whose form the specification fixes. The envelope Evaluation.JSON returns
// follows the CLI's machine-output contract and its outputVersion. The bytes
// view.PackView.JSON and view.PackView.Markdown return carry their own view
// version, view.ViewVersion, which VERSIONING.md says when to move.
//
// # Describing a pack
//
// Pack.View returns the whole of the document Load admitted as typed data, a
// view.PackView, with a canonical JSON encoding and a Markdown rendering for
// review; "jpack packs describe" writes the same bytes. Package sdk/view
// holds the view's types — PackView, its member types and its own Target —
// so that their names do not occupy this package, and only Pack.View builds
// one. The view is faithful: every member
// is carried, nothing is inferred or summarised, an absent member stays
// absent and a present empty one stays empty, arrays keep document order,
// and numbers and decimal strings are carried exactly as written. It is
// deterministic: the same pack bytes give the same view bytes. It reads the
// admitted document only, never evaluates and changes nothing.
//
// The view does not explain an evaluation, cover a test matrix or summarise
// across rules; docs/adr/0074-describe-a-loaded-pack.md records what it
// leaves out and why. docs/building-with-packs.md states the JSON members,
// the Markdown layout and the escaping rule every pack string passes
// through.
//
// For a pack under Core 0.5.0, the view shows the document's value sets and
// annotations, and Pack.FilteredView returns only the outcomes and
// rules whose annotations match every criterion given, compared by exact
// code point, as a view.FilteredView that says it is not the pack, states
// the filter and the counts, and carries its own filteredViewVersion;
// "jpack packs describe --filter" writes the same bytes
// (docs/adr/0075-admit-the-staged-draft-selector-0-4-0-draft.md, which
// docs/adr/0077-admit-core-0-5-0-and-retire-the-staged-selector.md carries
// over to the released version).
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
// Pack.View refuses nothing Load admitted. Its only errors are a plain error
// for a Pack that Load did not return, and an error wrapping ErrInternal for
// an admitted construct the view cannot represent, which is a fault of this
// runtime; it never returns a partial view. Pack.FilteredView returns, in
// addition, an error wrapping view.ErrFilterCriterion for a criterion no
// annotation could hold, and one wrapping view.ErrFilterSelector for a pack
// whose specVersion defines no annotations; neither is a *Refusal.
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
