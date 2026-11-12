package view

// The internals that the tests of package view_test, which load packs
// through package sdk, read directly.
var (
	CanonicalText  = canonicalText
	EscapeMarkdown = escapeMarkdown
	AnnotationText = annotationText
	BuildView      = buildView
	RenderMarkdown = renderMarkdown
)

const (
	ViewDraftVersion   = viewDraftVersion
	ViewCoreVersion    = viewCoreVersion
	ViewCore050Version = viewCore050Version
)
