// Package recordhook is how package sdk builds a record.DecisionRecord, whose
// cached bytes no caller can set: only sdk and sdk/record may import it.
package recordhook

// Input is what one evaluation gives its decision record, as package sdk read
// it after the evaluation completed (ADR-0079, ADR-0087).
type Input struct {
	// PackID, PackVersion, PackSpecVersion and PackDigest are the loaded
	// pack's identity, as sdk.Pack.Identity returns it.
	PackID, PackVersion, PackSpecVersion, PackDigest string
	// Facts is the facts document as the caller passed it to the evaluation
	// that completed.
	Facts []byte
	// Evidence is the evidence-availability document as the caller passed
	// it, and nil when it was omitted, which JPS Core §8.2 evaluates as the
	// empty object.
	Evidence []byte
	// Disposition is the §8.3 portable disposition in its canonical form.
	Disposition []byte
	// RuntimeVersion is sdk.Description.RuntimeVersion, empty when the build
	// records none.
	RuntimeVersion string
	// EvaluatorSpecVersion is the JPS Core evaluator contract applied.
	EvaluatorSpecVersion string
	// Binding is the caller's binding, already held to its shape by Check;
	// nil or empty when none was given.
	Binding map[string]string
	// Keyed is the caller's keyed digests, already held to their shape by
	// CheckKeyed, and nil for a record of the plain form.
	Keyed *Keyed
}

// Keyed is record.KeyedDigests, which this package cannot name.
type Keyed struct {
	Algorithm, KeyID string
	Facts, Evidence  []byte
}

// Check is set by package record when it is initialised and called by
// sdk.Pack.EvaluateWithRecord before anything is evaluated. It holds a
// binding to its shape and returns an error wrapping record.ErrBinding for
// one that is not.
var Check func(binding map[string]string) error

// CheckKeyed is set by package record when it is initialised and called by
// sdk.Pack.EvaluateWithKeyedRecord before anything is evaluated. It holds
// keyed digests to their shape and returns an error wrapping
// record.ErrKeyedDigests for ones that are not.
var CheckKeyed func(keyed Keyed) error

// Build is set by package record when it is initialised and called by
// sdk.Pack.EvaluateWithRecord after the evaluation completed. It returns a
// record.DecisionRecord as any, because this package cannot import package
// record, which imports it. On an input it cannot encode it returns nil and
// the error saying so. For a keyed record whose binding or keyed digests
// repeat a plain digest of a document the record names, that error wraps
// record.ErrBinding or record.ErrKeyedDigests: it is the caller's, not a
// fault of this runtime.
var Build func(input Input) (record any, err error)
