package evaluation

// Document returns the decoded root of the admitted pack — the value the
// admission under options holds, decoded by the carrier rules from the private
// copy AdmitPack took — or the refusal PackFailure reports under the same
// options. It runs the same steps through the same memo, so asking for the
// document after PackFailure admits nothing twice.
//
// The returned map is the admission's own and is shared with every evaluation
// of this pack: a caller reads it and never modifies it. Its values are the
// carrier's: objects are map[string]any, arrays []any, numbers json.Number
// holding the literal token as written, and strings, booleans and nil as
// themselves. It is the document as written, never the evaluation form a Core
// 0.5.0 pack is evaluated as: its annotations and value sets are there, and
// its references are not expanded.
func (a *AdmittedPack) Document(options Options) (map[string]any, *Failure) {
	admission, failure := a.packHalf(options)
	if failure != nil {
		return nil, failure
	}
	return admission.document, nil
}

// EvaluationForm returns the root every evaluation of the admitted pack reads
// under options: the evaluation form EF(P) a Core 0.5.0 pack is evaluated as
// (Core §8.5), made once at admission, and the decoded document itself for
// every other version. It is the same value the evaluator walks, not a second
// lowering, and like Document it is shared and only read.
//
// It answers only for a pack an evaluation under options would go on to
// evaluate: a pack refused at any step of the preflight that concerns the pack
// alone has no form to read, and the refusal is returned instead -- the one
// PackFailure reports, or the unsupported-required-extension error that §8.2
// compares after the facts and evidence documents, which PackFailure leaves to
// EvaluateAdmitted.
func (a *AdmittedPack) EvaluationForm(options Options) (map[string]any, *Failure) {
	admission, failure := a.packHalf(options)
	if failure != nil {
		return nil, failure
	}
	if admission.unsupported != nil {
		return nil, copyFailure(admission.unsupported)
	}
	return admission.packRoot, nil
}

// Digest is the SHA-256 of the private copy of the pack AdmitPack took: the
// exact bytes it was handed, hashed after the copy, so no later edit of the
// caller's slice can make the digest name other bytes than the ones admitted.
// It is the zero array for a pack refused at the byte limit, which is never
// copied or hashed.
func (a *AdmittedPack) Digest() [32]byte {
	return a.packDigest
}
