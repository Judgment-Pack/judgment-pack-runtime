package evaluation

import (
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The two §10 limits of the evaluator class on the ordinary Core path, and the
// one error reaching either of them produces.
//
// §10 requires an implementation claiming the class (§3.4) to define and
// document at least its collection-size and its evaluation-work limit, and makes
// reaching one of them during an evaluation the resource-exhaustion error of
// §8.4 rather than a disposition. Both are stated here, in the code that
// enforces them, and in the README's experimental section; the claim they are a
// precondition of is stated in full and only in CONFORMANCE.md (ADR-0011), and
// ADR-0010 recorded the same requirement as deliberately unmet before it.
//
// The phase split is §10's own. A documented limit reached while *admitting* an
// input is malformed-input, because §2.1 refuses that document rather than
// processing part of it, so §8.2's preflight never admits it; resource-exhaustion
// is reserved for a limit reached while *evaluating* an input already admitted.
// Either way the evaluation yields an explicit error and never a disposition, and
// neither limit is portable: §10 says two conforming implementations may define
// different ones, so an input above either is outside the portable claim.

// DefaultCoreWorkLimit is this runtime's §10 evaluation-work limit for one
// evaluation on the Core path, in the work units of the accounting model
// documented on evaluate. Options.WorkBudget overrides it exactly as it
// overrides the draft grammar's DefaultWorkBudget, so the limit is configurable
// per evaluation and the default is what an unconfigured caller gets.
//
// The number is derived in this expression rather than written down: it is
// exactly twice the carrier's byte cap, which is the only admission limit a work
// charge is measured against, so there is no independent constant here to drift
// from carrier.HardMaxBytes. At the current cap of 10 MiB per input it is
// 20,971,520 units.
//
// A work unit is one visited condition node, one §8 iteration over an authored
// rule, exception, or evidence requirement, one step of a pointer resolution, or
// one byte of a path, a member name, or a scalar token a comparison has to read.
// Every unit is backed by at least one byte of an admitted input, so reading one
// whole admitted document through once charges at most its length in units. Two
// documents reach an evaluation — the pack and the facts document — and each is
// admitted under the same byte cap, which is what twice that cap buys and what it
// does not:
//
//   - One full read of every admitted byte always fits. Reading the whole pack
//     once and the whole facts document once charges at most HardMaxBytes +
//     HardMaxBytes units, which is this limit, so an evaluation that reads each of
//     its inputs through once is never refused for work.
//   - A single maximal cross-document comparison sits exactly at the boundary and
//     may be refused. One comparison between a near-cap authored value and a
//     near-cap selected value charges both sides, so it approaches the same total
//     with the fixed per-node and per-iteration charges of §8 still to pay, and
//     those charges push it over. Such an input is refused as resource-exhaustion,
//     with no disposition, and §10 permits that: the limit is documented, it is not
//     portable, and an input above it is outside the portable claim.
//
// So the guarantee is the first bullet and not more than it. What this limit
// mainly refuses in practice is amplification — Core conditions have no runtime
// fan-out, and the one place a small condition can buy large work is the value its
// pointer selects, once per candidate of an in condition or once per condition
// over the same large value — but "only amplification is refused" would be false,
// and it is not claimed. Against a 100 KB facts document the limit admits about
// two hundred whole-document comparisons; against the values a hand-authored pack
// compares it is unreachable. Every row of the bundled evaluation corpus charges
// under 1,000 units, four orders of magnitude inside this limit
// (TestCoreWorkLimitNeverTripsOnTheCorpus).
const DefaultCoreWorkLimit = int(2 * carrier.HardMaxBytes)

// CoreCollectionSizeLimit is this runtime's §10 collection-size limit on the
// Core path: the carrier's parsed-node cap, which every input is admitted under.
//
// It is a determination rather than a second mechanism, and the determination is
// the substance. The cap is a budget over one whole parsed document, so no
// admitted document contains a collection with more than this many members — a
// collection of n members is n+1 parsed nodes — and the sum of every collection
// in one document is bounded by it too. Every collection a Core evaluation
// traverses comes from an admitted document: the facts value a fact condition
// selects, the candidate array an in condition names, the object members §7.4
// equality descends through, and the pack's own rules, exceptions, and
// evidenceRequirements arrays. Core constructs no collection of its own, and it
// has no operator that iterates one — the draft RFC 0008 aggregates do, behind
// their own opt-in and their own budget (ADR-0009).
//
// Because the bound is enforced while admitting each input, §10's phase split
// makes reaching it malformed-input in the preflight phase — pack-not-conformant
// for the pack, which §8.4 orders first in any case — rather than
// resource-exhaustion during evaluation. An evaluation-phase counter over the
// same bound could therefore only report a condition the preflight has already
// refused, and refused more strictly: §2.1 rejects the document whole instead of
// admitting it and then stopping partway through it. What DefaultCoreWorkLimit
// adds is the half a size bound cannot supply: the *work* that traversing an
// admitted collection costs, which the collection's size alone does not bound
// once one condition can read it many times.
const CoreCollectionSizeLimit = carrier.DefaultMaxNodes

// workLimitFailure is the explicit evaluation error a reached evaluation-work
// limit produces: §8.4's resource-exhaustion class, in the evaluation phase,
// with this runtime's finer code beside it as the detail §8.4 admits.
//
// It is an error and never a disposition. Each condition tree's charge is
// complete before any predicate in that tree runs, and §8's own iteration is
// charged before step 1, so an exhausted limit means no disposition was ever
// entitled to exist rather than that one was truncated. The message names the
// limit that was in force, which is the configured one when a caller configured
// it.
func workLimitFailure(e *evaluator) *Failure {
	detail := fmt.Sprintf(
		"The evaluation exceeds this runtime's documented JPS §10 evaluation-work limit of %d units. The charge for each condition tree is complete before any predicate in it runs, so it does not depend on evaluation order, and no disposition is produced.",
		e.budget)
	if e.quantifiers {
		detail = fmt.Sprintf(
			"The evaluation exceeds this runtime's draft RFC 0008 evaluation-work limit of %d units. The charge is computed before any element is evaluated and does not depend on element order, so no disposition is produced.",
			e.budget)
	}
	return &Failure{
		Class:    result.ClassResourceExhaustion,
		Phase:    result.PhaseEvaluation,
		Code:     "JPS-RESOURCE-EVALUATION-WORK-LIMIT",
		Message:  detail,
		ExitCode: result.ExitIO,
	}
}
