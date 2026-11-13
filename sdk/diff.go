package sdk

import (
	"errors"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/viewhook"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// Diff returns the structural comparison of two loaded packs, older and
// newer, as a view.Diff (EXPERIMENTAL): what the two documents say
// differently, member by member, annotations included, with old and new
// values. It reads the documents Load admitted, through views built afresh as
// Pack.View builds them, never evaluates and changes nothing; it says nothing
// about how either pack decides an input, which "jpack experimental compare"
// shows. Two packs whose ids differ are compared all the same, and the
// comparison says so.
//
// Packs whose specVersions differ are refused with an error wrapping
// view.ErrDiffSpecVersion, which is not a *Refusal: both packs were admitted.
// A Pack that Load did not return is a plain error, and any other error wraps
// ErrInternal, as Pack.View's do.
func Diff(older, newer *Pack) (view.Diff, error) {
	compared, err := diff(older, newer)
	if err != nil {
		return view.Diff{}, err
	}
	result, ok := compared.(view.Diff)
	if !ok {
		return view.Diff{}, internalFault(viewFaultCode, "The comparison returned no diff.")
	}
	if Identity(result.Old) != older.identity || Identity(result.New) != newer.identity {
		return view.Diff{}, internalFault(viewFaultCode, "The comparison's identities are not the identities Load read from the same documents.")
	}
	return result, nil
}

// ThreeWayDiff returns the structural comparison of two loaded packs, first
// and second, with their common base, as a view.ThreeWayDiff (EXPERIMENTAL):
// for each member that differs anywhere, whether only first changed it from
// base, only second, both identically, or both differently, which is a
// conflict. It merges nothing. Its errors are Diff's, for the three packs.
func ThreeWayDiff(base, first, second *Pack) (view.ThreeWayDiff, error) {
	compared, err := diff(base, first, second)
	if err != nil {
		return view.ThreeWayDiff{}, err
	}
	result, ok := compared.(view.ThreeWayDiff)
	if !ok {
		return view.ThreeWayDiff{}, internalFault(viewFaultCode, "The comparison returned no three-way diff.")
	}
	if Identity(result.Base) != base.identity || Identity(result.First) != first.identity || Identity(result.Second) != second.identity {
		return view.ThreeWayDiff{}, internalFault(viewFaultCode, "The comparison's identities are not the identities Load read from the same documents.")
	}
	return result, nil
}

// diff builds a view of each pack and compares them.
func diff(packs ...*Pack) (any, error) {
	for _, pack := range packs {
		if pack == nil || pack.admitted == nil {
			return nil, errors.New("sdk: Diff on a Pack that Load did not return")
		}
	}
	views := make([]any, 0, len(packs))
	for _, pack := range packs {
		described, err := pack.View()
		if err != nil {
			return nil, err
		}
		views = append(views, described)
	}
	if viewhook.Diff == nil {
		return nil, internalFault(viewFaultCode, "The comparison is not initialised.")
	}
	compared, err := viewhook.Diff(views)
	if errors.Is(err, view.ErrDiffSpecVersion) {
		return nil, err
	}
	if err != nil {
		return nil, internalFault(viewFaultCode, err.Error())
	}
	return compared, nil
}
