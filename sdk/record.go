package sdk

import (
	"bytes"
	"context"
	"errors"
	"sync"

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
// in the plain form (ADR-0079): the pack's identity, a digest of the facts
// and of the evidence documents as evaluated, the disposition as
// CanonicalDisposition returns it, this runtime's version and the evaluator
// contract applied, and binding, the caller's own named digests, carried as
// given. Experimental.
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
	return p.evaluateWithRecord(ctx, facts, evidence, binding, nil)
}

// EvaluateWithKeyedRecord evaluates facts and evidence exactly as Evaluate
// does, and returns, beside the Evaluation, the decision record of that
// evaluation in the keyed form (ADR-0087): as EvaluateWithRecord's, with the
// facts and evidence documents named by keyed, the digests the caller
// computed with its own key over the bytes record.CanonicalFacts and
// record.CanonicalEvidence return for the same documents, in place of the
// plain digests. The record carries keyed's algorithm label, key identifier
// and digests, and no plain digest of either document. Experimental.
//
// This runtime never receives the key and cannot check keyed: a digest the
// caller computed over other bytes is carried as given, and only a reader
// holding the key finds it out, with record.DecisionRecord.CheckDocuments.
//
// binding and keyed are checked before anything is evaluated: a binding not
// of the shape record.Binding states is refused with an error wrapping
// record.ErrBinding, and keyed digests not of the shape record.KeyedDigests
// states, an algorithm outside the closed list included, with one wrapping
// record.ErrKeyedDigests. After the evaluation, a keyed digest that is the
// plain SHA-256 of the facts or evidence document, as passed or as canonical
// text, is refused with an error wrapping record.ErrKeyedDigests, and a
// binding digest that is one with an error wrapping record.ErrBinding: the
// record would carry beside the keyed digests the plain one they replace.
// Neither is a *Refusal, and the evaluation is not returned. Every other
// error is EvaluateWithRecord's for the same inputs.
func (p *Pack) EvaluateWithKeyedRecord(ctx context.Context, facts, evidence []byte, binding record.Binding, keyed record.KeyedDigests) (*Evaluation, record.DecisionRecord, error) {
	return p.evaluateWithRecord(ctx, facts, evidence, binding, &recordhook.Keyed{
		Algorithm: keyed.Algorithm,
		KeyID:     keyed.KeyID,
		Facts:     bytes.Clone(keyed.Facts),
		Evidence:  bytes.Clone(keyed.Evidence),
	})
}

func (p *Pack) evaluateWithRecord(ctx context.Context, facts, evidence []byte, binding record.Binding, keyed *recordhook.Keyed) (*Evaluation, record.DecisionRecord, error) {
	if ctx == nil {
		return nil, record.DecisionRecord{}, errNilContext
	}
	if p == nil || p.admitted == nil {
		return nil, record.DecisionRecord{}, errors.New("sdk: EvaluateWithRecord on a Pack that Load did not return")
	}
	if err := ctx.Err(); err != nil {
		return nil, record.DecisionRecord{}, err
	}
	if recordhook.Check == nil || recordhook.CheckKeyed == nil || recordhook.Build == nil {
		return nil, record.DecisionRecord{}, internalFault(recordFaultCode, "The decision record builder is not initialised.")
	}
	if err := recordhook.Check(binding); err != nil {
		return nil, record.DecisionRecord{}, err
	}
	if keyed != nil {
		if err := recordhook.CheckKeyed(*keyed); err != nil {
			return nil, record.DecisionRecord{}, err
		}
	}
	evaluated, err := p.Evaluate(ctx, facts, evidence)
	if err != nil {
		return nil, record.DecisionRecord{}, err
	}
	// The record reads the documents again as the engine read them for the
	// evaluation that has just completed: the same decoder under the same
	// limits, so the values are the ones evaluated, and a document the
	// engine admitted cannot fail there. An omitted evidence document is the
	// empty object JPS Core §8.2 evaluates it as.
	built, err := recordhook.Build(recordhook.Input{
		PackID:               p.identity.ID,
		PackVersion:          p.identity.Version,
		PackSpecVersion:      p.identity.SpecVersion,
		PackDigest:           p.identity.Digest,
		Facts:                facts,
		Evidence:             evidence,
		Disposition:          evaluated.CanonicalDisposition(),
		RuntimeVersion:       runtimeVersion(),
		EvaluatorSpecVersion: evaluated.EvaluatorSpecVersion,
		Binding:              binding,
		Keyed:                keyed,
	})
	if keyed != nil && (errors.Is(err, record.ErrBinding) || errors.Is(err, record.ErrKeyedDigests)) {
		return nil, record.DecisionRecord{}, err
	}
	if err != nil {
		return nil, record.DecisionRecord{}, internalFault(recordFaultCode, "The decision record could not be built: "+err.Error()+".")
	}
	decided, ok := built.(record.DecisionRecord)
	if !ok {
		return nil, record.DecisionRecord{}, internalFault(recordFaultCode, "The decision record builder returned no record.")
	}
	return evaluated, decided, nil
}
