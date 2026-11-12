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
