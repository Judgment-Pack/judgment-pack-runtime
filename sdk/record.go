package sdk

import (
	"context"
	"errors"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/recordhook"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/record"
)

// runtimeVersion is the module version a decision record names, read once
// from the program's build information as Describe reads it. A test
// replaces it.
var runtimeVersion = sync.OnceValue(moduleVersion)

// recordFaultCode is the code of every error for a decision record this
// runtime could not build after the evaluation completed.
const recordFaultCode = "JPS-RECORD-INTERNAL"

// EvaluateWithRecord evaluates facts and evidence exactly as Evaluate does,
// and returns, beside the Evaluation, the decision record of that evaluation
// (ADR-0079): the pack's identity, a digest of the facts and of the evidence
// documents as evaluated, the disposition as CanonicalDisposition returns it,
// this runtime's version and the evaluator contract applied, and binding, the
// caller's own named digests, carried as given. Experimental.
//
// It writes nothing: no audit trail, file or other output. The record holds
// no time and no random value, so the same pack bytes, documents and binding
// under the same runtime version give the same record bytes and digest; a
// timestamp, where the caller wants one, is the caller's and outside the
// record. Package sdk/record states the record's members, the canonical form
// each document is digested in, and what the record does not establish: it
// is not an audit-trail entry, it is unsigned, its binding is the caller's
// assertion, and it names the facts and evidence by digest only.
//
// binding is checked before anything is evaluated. A nil or empty binding is
// no binding; one not of the shape record.Binding states is refused with an
// error wrapping record.ErrBinding, which is not a *Refusal: the binding is
// not an input of the evaluator. Every other error is one Evaluate returns
// for the same inputs, or, for a record this runtime cannot build, an error
// wrapping ErrInternal. The context is checked on entry, as Evaluate checks
// it.
func (p *Pack) EvaluateWithRecord(ctx context.Context, facts, evidence []byte, binding record.Binding) (*Evaluation, record.DecisionRecord, error) {
	if ctx == nil {
		return nil, record.DecisionRecord{}, errNilContext
	}
	if p == nil || p.admitted == nil {
		return nil, record.DecisionRecord{}, errors.New("sdk: EvaluateWithRecord on a Pack that Load did not return")
	}
	if err := ctx.Err(); err != nil {
		return nil, record.DecisionRecord{}, err
	}
	if recordhook.Check == nil || recordhook.Build == nil {
		return nil, record.DecisionRecord{}, internalFault(recordFaultCode, "The decision record builder is not initialised.")
	}
	if err := recordhook.Check(binding); err != nil {
		return nil, record.DecisionRecord{}, err
	}
	evaluated, err := p.Evaluate(ctx, facts, evidence)
	if err != nil {
		return nil, record.DecisionRecord{}, err
	}
	// The documents are read again as the engine read them for the
	// evaluation that has just completed: the same decoder under the same
	// limits, so the values are the ones evaluated, and a document the
	// engine admitted cannot fail here. An omitted evidence document is the
	// empty object JPS Core §8.2 evaluates it as.
	factsValue, failure := carrier.Decode(facts, carrier.DefaultLimits())
	if failure != nil {
		return nil, record.DecisionRecord{}, internalFault(recordFaultCode, "The evaluated facts document did not decode again.")
	}
	var evidenceValue any = map[string]any{}
	if evidence != nil {
		if evidenceValue, failure = carrier.Decode(evidence, carrier.DefaultLimits()); failure != nil {
			return nil, record.DecisionRecord{}, internalFault(recordFaultCode, "The evaluated evidence document did not decode again.")
		}
	}
	built, err := recordhook.Build(recordhook.Input{
		PackID:               p.identity.ID,
		PackVersion:          p.identity.Version,
		PackSpecVersion:      p.identity.SpecVersion,
		PackDigest:           p.identity.Digest,
		Facts:                factsValue,
		Evidence:             evidenceValue,
		Disposition:          evaluated.CanonicalDisposition(),
		RuntimeVersion:       runtimeVersion(),
		EvaluatorSpecVersion: evaluated.EvaluatorSpecVersion,
		Binding:              binding,
	})
	if err != nil {
		return nil, record.DecisionRecord{}, internalFault(recordFaultCode, "The decision record could not be built: "+err.Error()+".")
	}
	decided, ok := built.(record.DecisionRecord)
	if !ok {
		return nil, record.DecisionRecord{}, internalFault(recordFaultCode, "The decision record builder returned no record.")
	}
	return evaluated, decided, nil
}
