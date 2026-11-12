package view

// The internals that the tests of package view_test, which load packs
// through package sdk, read directly.
var (
	CanonicalText  = canonicalText
	EscapeMarkdown = escapeMarkdown
	BuildView      = buildView
	RenderMarkdown = renderMarkdown
)

const (
	ViewDraftVersion  = viewDraftVersion
	ViewCoreVersion   = viewCoreVersion
	ViewStagedVersion = viewStagedVersion
)
