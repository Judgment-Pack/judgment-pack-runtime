// Package recordhook is how package sdk builds a record.DecisionRecord, whose
// cached bytes no caller can set: only sdk and sdk/record may import it.
package recordhook

// Input is what one evaluation gives its decision record, as package sdk read
// it after the evaluation completed (ADR-0079).
type Input struct {
	// PackID, PackVersion, PackSpecVersion and PackDigest are the loaded
	// pack's identity, as sdk.Pack.Identity returns it.
	PackID, PackVersion, PackSpecVersion, PackDigest string
	// Facts is the facts document as the carrier decoder read it for the
	// evaluation.
	Facts any
	// Evidence is the evidence-availability document as the carrier decoder
	// read it, or the empty object for an omitted document, which is what
	// JPS Core §8.2 evaluates an omitted one as.
	Evidence any
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
}

// Check is set by package record when it is initialised and called by
// sdk.Pack.EvaluateWithRecord before anything is evaluated. It holds a
// binding to its shape and returns an error wrapping record.ErrBinding for
// one that is not.
var Check func(binding map[string]string) error

// Build is set by package record when it is initialised and called by
// sdk.Pack.EvaluateWithRecord after the evaluation completed. It returns a
// record.DecisionRecord as any, because this package cannot import package
// record, which imports it; on an input it cannot encode it returns nil and
// the error saying so.
var Build func(input Input) (record any, err error)
