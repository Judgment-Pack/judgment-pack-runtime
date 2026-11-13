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
// validation-only and test surfaces are not offered here. Package sdk/packsig
// checks a pack's detached signature against the key a caller expects before
// Load, and signs nothing. The decision record
// Pack.EvaluateWithRecord or Pack.EvaluateWithKeyedRecord returns is not an
// audit record: it is written nowhere and is the caller's to keep.
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
//   - Pack.EvaluateWithRecord and Pack.EvaluateWithKeyedRecord, which return
//     the record.DecisionRecord of package sdk/record, and that package's
//     DecisionRecord, with DecisionRecord.JSON, DecisionRecord.Digest,
//     DecisionRecord.Form and DecisionRecord.CheckDocuments, Pack, Evaluator,
//     Binding, RecordVersion, KeyedRecordVersion, Form, FormPlain, FormKeyed,
//     KeyedDigests, HMACSHA256, MaxKeyIDLength, MaxBindingNames,
//     MaxBindingNameLength, ErrBinding and ErrKeyedDigests; CanonicalFacts,
//     CanonicalEvidence and ErrDocument; Read and ErrRecord; KeyedDigestFunc,
//     DocumentCheck, Outcome, Match, Mismatch, NotCheckableWithoutKey and
//     ErrKeyedDigestFunc;
//   - Evaluation.OutcomeAnnotations, which returns view.Annotation values of
//     package sdk/view (Core 0.5.0 only);
//   - Diff and ThreeWayDiff, which return the view.Diff and view.ThreeWayDiff
//     of package sdk/view, with that package's DiffVersion,
//     ThreeWayDiffVersion and ErrDiffSpecVersion;
//   - package sdk/packsig: Verify and Load, which check a pack's detached
//     signature against an expected key, Load before sdk.Load reads any rule,
//     with Signature, Refusal and Refusal.Error, SignatureVersion,
//     Algorithm, FileSuffix, MaxSignatureBytes and the Code constants;
//   - the constants ClassPackNotConformant, ClassMalformedInput,
//     ClassUnsupportedRequiredExtension, ClassResourceExhaustion,
//     PhasePreflight and PhaseEvaluation.
//
// The bytes CanonicalDisposition returns are the §8.3 portable disposition,
// whose form the specification fixes. The envelope Evaluation.JSON returns
// follows the CLI's machine-output contract and its outputVersion. The bytes
// view.PackView.JSON and view.PackView.Markdown return carry their own view
// version, view.ViewVersion, which VERSIONING.md says when to move, and the
// bytes view.Diff and view.ThreeWayDiff return carry view.DiffVersion and
// view.ThreeWayDiffVersion, which move by the same rule. The bytes
// record.DecisionRecord.JSON returns carry their own record version,
// record.RecordVersion for the plain form and record.KeyedRecordVersion for
// the keyed one, which package sdk/record and VERSIONING.md say when to move.
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
// # Comparing two packs
//
// Diff compares two loaded packs of one specVersion, structurally: what the
// newer document says differently from the older, member by member, keyed
// by id, with old and new values, the annotations of Core 0.5.0 included at
// every position that carries them; "jpack packs diff" writes the same bytes.
// ThreeWayDiff compares two packs with their common base and says, for each
// member that differs anywhere, whether only the first changed it, only the
// second, both alike, or both differently, which is a conflict; it merges
// nothing. Both read views built afresh from the documents Load admitted and
// never evaluate: a structural difference says the documents differ, not
// that they decide an input differently.
// docs/adr/0086-compare-two-pack-documents-member-by-member.md records the
// choices, and docs/building-with-packs.md states the JSON members, which
// orders are reported and which are not.
//
// # Recording an evaluation
//
// Pack.EvaluateWithRecord evaluates exactly as Pack.Evaluate does and
// returns, beside the Evaluation, a record.DecisionRecord: the pack's id,
// version, specVersion and digest; a digest of the facts and of the evidence
// documents as evaluated, over their canonical JSON text in the form of the
// view's JSON; the §8.3 disposition; this runtime's version and the
// evaluator contract applied; recordVersion; and, when the caller gives one,
// a binding of the caller's own names to "sha256:" digests, carried as given.
// The record is one canonical JSON text with its own digest. It holds no
// time, no random value and nothing read from outside its inputs, so the same
// inputs under the same runtime version give the same bytes. It is written
// nowhere, is not an audit-trail entry, is unsigned, and names the facts and
// evidence by digest only; the binding is the caller's assertion and is never
// compared with anything. docs/adr/0079-a-decision-record-for-an-evaluation.md
// records what the record says and what it does not, and package sdk/record
// states its members.
//
// Pack.EvaluateWithKeyedRecord writes the record's keyed form, which names the
// facts and evidence documents not by their SHA-256 but by digests the caller
// computes with a key of its own over the texts record.CanonicalFacts and
// record.CanonicalEvidence return, so that a reader without the key cannot
// test candidate facts against the record's document digests. The
// disposition still says what the pack decided, so a reader holding the
// pack can evaluate a candidate and compare dispositions;
// docs/decision-records.md lists what in each form follows from the facts.
// The record carries the algorithm's label and the caller's key identifier,
// never the key, which this runtime never receives. record.Read reads a record of either form, and
// DecisionRecord.CheckDocuments compares documents with it, reporting a keyed
// record checked without the caller's key function as not checkable.
// docs/decision-records.md states both forms, the canonical text with its
// vectors, and what in each form follows from the facts;
// docs/adr/0087-a-caller-keyed-digest-on-the-decision-record.md records why.
//
// Evaluation.OutcomeAnnotations returns the annotations of the outcome a
// disposition chose, as Pack.View carries them on that outcome, so a caller
// acting on the outcome need not read the view itself. The disposition, the
// record and Evaluation.JSON do not carry them.
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
// Pack.EvaluateWithRecord returns the errors Pack.Evaluate returns for the
// same inputs, an error wrapping ErrInternal for a record this runtime cannot
// build, and, before anything is evaluated, an error wrapping
// record.ErrBinding for a binding not of the shape record.Binding states,
// which is not a *Refusal: the binding is not an input of the evaluator.
// Pack.EvaluateWithKeyedRecord returns the same, and also, before anything is
// evaluated, an error wrapping record.ErrKeyedDigests for keyed digests not
// of the shape record.KeyedDigests states, and, after the evaluation and
// without it, one wrapping record.ErrKeyedDigests or record.ErrBinding for a
// keyed or binding digest that is the plain SHA-256 of the facts or evidence
// document or the digest of the plain record of the same evaluation, or a
// key identifier that holds one; none is a *Refusal.
// Evaluation.OutcomeAnnotations returns a plain error for an Evaluation that
// Evaluate did not return, and otherwise only Pack.View's errors.
//
// Pack.View refuses nothing Load admitted. Its only errors are a plain error
// for a Pack that Load did not return, and an error wrapping ErrInternal for
// an admitted construct the view cannot represent, which is a fault of this
// runtime; it never returns a partial view. Pack.FilteredView returns, in
// addition, an error wrapping view.ErrFilterCriterion for a criterion no
// annotation could hold, and one wrapping view.ErrFilterSelector for a pack
// whose specVersion defines no annotations; neither is a *Refusal. Diff and
// ThreeWayDiff return Pack.View's errors, and one wrapping
// view.ErrDiffSpecVersion for packs whose specVersions differ, which is not a
// *Refusal either.
//
// # Context
//
// Load checks its context on entry and again before validation starts;
// Pack.Evaluate, Pack.EvaluateWithRecord and Pack.EvaluateWithKeyedRecord
// check it on entry. A validation or evaluation that has
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
