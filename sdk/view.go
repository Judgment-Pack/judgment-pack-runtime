package sdk

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/viewhook"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// Identity is what names one loaded pack: the id, version and specVersion
// members of the document, its profile member when the document states one,
// and the digest of the exact bytes passed to Load. Experimental.
type Identity struct {
	ID          string
	Version     string
	SpecVersion string
	// Profile is the document's profile member, and empty when the document
	// does not state one. It is never inferred: the schema's default is not
	// a statement the document made, and the evaluator applies that default
	// whether or not Identity shows it.
	Profile string
	// Digest is "sha256:" followed by the 64 lower-case hex digits of the
	// SHA-256 of the bytes Load was given, the form the reviewed-set lock and
	// the audit record use for a pack.
	Digest string
}

// View returns the whole of the admitted document as a view.PackView, with
// its canonical JSON and its Markdown rendering. It reads only the document
// Load admitted, never evaluates, and changes nothing: two calls return equal
// views and the same bytes. A construct of the admitted document the view
// cannot represent is a fault of this runtime: View returns an error wrapping
// ErrInternal, and never a partial view. Experimental.
func (p *Pack) View() (view.PackView, error) {
	if p == nil || p.admitted == nil {
		return view.PackView{}, errors.New("sdk: View on a Pack that Load did not return")
	}
	root, failure := p.admitted.Document(p.options(false))
	if failure != nil {
		// Load admitted this pack under the same options, through the same
		// memo, so a refusal here is not the caller's input: it is a fault.
		return view.PackView{}, internalFault(failure.Code, failure.Message)
	}
	if viewhook.Build == nil {
		return view.PackView{}, internalFault(viewFaultCode, "The view builder is not initialised.")
	}
	built, fault := viewhook.Build(root, p.identity.Digest)
	if fault != "" {
		return view.PackView{}, internalFault(viewFaultCode, fault)
	}
	described, ok := built.(view.PackView)
	if !ok {
		return view.PackView{}, internalFault(viewFaultCode, "The view builder returned no view.")
	}
	if Identity(described.Identity) != p.identity {
		return view.PackView{}, internalFault(viewFaultCode, "The view's identity is not the identity Load read from the same document.")
	}
	return described, nil
}

// FilteredView returns the outcomes and rules of the admitted document whose
// annotations match every one of criteria, with the filter, the counts and
// the pack's full identity (EXPERIMENTAL). It is not the pack, and both its
// encodings say so. It is taken from a view built afresh from the document
// Load admitted, as View builds it, never from a PackView a caller holds.
//
// Annotations exist only under Core 0.5.0. A criterion no annotation
// could match is refused with an error wrapping view.ErrFilterCriterion, and
// a pack under any other specVersion with one wrapping
// view.ErrFilterSelector; neither is a *Refusal, because the pack was
// admitted. Any other error wraps ErrInternal, as View's do.
func (p *Pack) FilteredView(criteria []view.Criterion) (view.FilteredView, error) {
	described, err := p.View()
	if err != nil {
		return view.FilteredView{}, err
	}
	if viewhook.Filter == nil {
		return view.FilteredView{}, internalFault(viewFaultCode, "The view filter is not initialised.")
	}
	filtered, err := viewhook.Filter(described, criteria)
	if errors.Is(err, view.ErrFilterCriterion) || errors.Is(err, view.ErrFilterSelector) {
		return view.FilteredView{}, err
	}
	if err != nil {
		return view.FilteredView{}, internalFault(viewFaultCode, err.Error())
	}
	result, ok := filtered.(view.FilteredView)
	if !ok {
		return view.FilteredView{}, internalFault(viewFaultCode, "The view filter returned no filtered view.")
	}
	if Identity(result.Identity) != p.identity {
		return view.FilteredView{}, internalFault(viewFaultCode, "The filtered view's identity is not the identity Load read from the same document.")
	}
	return result, nil
}

// OutcomeAnnotations returns the annotations of the outcome the disposition
// chose, as Pack.View carries them on that outcome: the annotations member of
// its extensions, ordered by key, each a string or a list (Core 0.5.0 only).
// It is nil when the disposition is not an outcome, when the outcome has no
// annotations member, and for a pack under any specVersion that defines no
// annotations. They are read from the document Load admitted, never from the
// pack's bytes again: from a view of it built once per Pack, the first time
// any of the pack's evaluations is asked, and the caller owns the returned
// slice. The disposition and the decision record do not carry them, and
// Evaluation.JSON is unchanged. An error wraps ErrInternal, as View's do, or
// is a plain error for an Evaluation that Evaluate did not return.
// Experimental.
func (e *Evaluation) OutcomeAnnotations() ([]view.Annotation, error) {
	if e == nil || e.pack == nil || e.pack.annotations == nil {
		return nil, errors.New("sdk: OutcomeAnnotations on an Evaluation that Evaluate did not return")
	}
	if e.Disposition.Kind != "outcome" {
		return nil, nil
	}
	table, err := e.pack.annotations()
	if err != nil {
		return nil, err
	}
	annotations, present := table[e.Disposition.OutcomeID]
	if !present {
		return nil, nil
	}
	copied := make([]view.Annotation, 0, len(annotations))
	for _, annotation := range annotations {
		copied = append(copied, view.Annotation{Key: annotation.Key, Value: annotation.Value, List: slices.Clone(annotation.List)})
	}
	return copied, nil
}

// outcomeAnnotations reads each outcome's annotations from a view of the
// admitted document, keyed by outcome id; an outcome with no annotations
// member has no entry.
func (p *Pack) outcomeAnnotations() (map[string][]view.Annotation, error) {
	described, err := p.View()
	if err != nil {
		return nil, err
	}
	table := map[string][]view.Annotation{}
	for _, outcome := range described.Outcomes {
		if outcome.Annotations != nil {
			table[outcome.ID] = outcome.Annotations
		}
	}
	return table, nil
}

// Identity returns the identity Load read from the admitted document and the
// digest of the bytes it was given. Experimental.
func (p *Pack) Identity() Identity {
	if p == nil {
		return Identity{}
	}
	return p.identity
}

// viewFaultCode is the code of every error for an admitted construct the view
// cannot represent.
const viewFaultCode = "JPS-VIEW-UNREPRESENTABLE"

// readIdentity reads the identity members of an admitted root. The schema
// makes id, version and specVersion required strings and profile an optional
// one, so a root that is not that shape is a fault, never a refusal.
func readIdentity(root map[string]any, digest string) (Identity, error) {
	identity := Identity{Digest: digest}
	for _, member := range []struct {
		name     string
		target   *string
		optional bool
	}{
		{"id", &identity.ID, false},
		{"version", &identity.Version, false},
		{"specVersion", &identity.SpecVersion, false},
		{"profile", &identity.Profile, true},
	} {
		value, present := root[member.name]
		if !present && member.optional {
			continue
		}
		text, ok := value.(string)
		if !ok || text == "" {
			return Identity{}, internalFault(viewFaultCode, fmt.Sprintf("The admitted pack holds a construct at /%s that this view cannot represent (%s); no partial view is returned.", member.name, "the admitted document does not hold this identity member as a non-empty string"))
		}
		*member.target = text
	}
	return identity, nil
}
