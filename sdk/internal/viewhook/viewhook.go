// Package viewhook is how package sdk builds a view.PackView, whose cached
// encodings no caller can set: only sdk and sdk/view may import it.
package viewhook

// Build is set by package view when it is initialised and called by
// sdk.Pack.View. It builds the view of one admitted document root, with the
// digest of the bytes the pack was loaded from. It returns a view.PackView as
// any, because this package cannot import package view, which imports it; on
// a construct the view cannot represent it returns nil and the message saying
// so.
var Build func(root map[string]any, digest string) (view any, fault string)

// Filter is set by package view when it is initialised and called by
// sdk.Pack.FilteredView. It filters a view.PackView, which Build returned, by
// a []view.Criterion, both passed as any for the same reason, and returns a
// view.FilteredView as any, or the error that refuses the criteria or the
// pack.
var Filter func(view any, criteria any) (filtered any, err error)

// Diff is set by package view when it is initialised and called by sdk.Diff
// and sdk.ThreeWayDiff. It compares two view.PackView values Build returned,
// older then newer, as a view.Diff, or three, base then first then second,
// as a view.ThreeWayDiff, each returned as any; or it returns the error that
// refuses the packs or reports a fault.
var Diff func(views []any) (diff any, err error)
