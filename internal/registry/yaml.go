package registry

import (
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// MaxImportBytes bounds a YAML file given to import: the registry's bound,
// the largest a companion document has.
const MaxImportBytes = MaxRegistryBytes

// Import converts one YAML file in RFC 0034 §7's authoring subset to the
// canonical JSON of the registry or kind declaration it holds, which document
// it is being said by its format selector member (ADR-0094).
//
// The file is refused, with a report and no bytes, when it is outside the
// subset or when the document it holds fails step 1's admission as JSON
// would. A refusal is located by line and column in the file, and the report
// carries no value from it. A registry's kinds and entries are written in
// canonical order (§3). The same file always gives the same bytes. Nothing
// past step 1 is checked: that is validation's.
func Import(source Document) ([]byte, Report) {
	diagnostics := &collector{}
	canonical := importYAML(source, diagnostics)
	report := Report{ReportFormatVersion: ReportFormatVersion, Valid: diagnostics.errors() == 0, Diagnostics: diagnostics.sorted()}
	if !report.Valid {
		canonical = nil
	}
	return canonical, report
}

func importYAML(source Document, diagnostics *collector) []byte {
	if source.Oversize || len(source.Data) > MaxImportBytes {
		diagnostics.add("REG-DOC-SIZE", Location{Document: DocumentYAML})
		return nil
	}
	text, ok := decodeYAMLText(source.Data, diagnostics)
	if !ok {
		return nil
	}
	parser := newYAMLParser(text, diagnostics)
	root := parser.parse()
	if root == nil || diagnostics.errors() > 0 {
		return nil
	}
	var member, selector string
	bound := 0
	registryMember, kindMember := root.kind == yamlMapping && root.member("registryFormatVersion") != nil, root.kind == yamlMapping && root.member("kindFormatVersion") != nil
	switch {
	case registryMember && !kindMember:
		member, selector, bound = "registryFormatVersion", RegistryFormatVersion, MaxRegistryBytes
		canonicalOrder(root)
	case kindMember && !registryMember:
		member, selector, bound = "kindFormatVersion", KindFormatVersion, MaxKindBytes
	default:
		// Neither selector, or both: the file does not say which document
		// it is, and import does not guess.
		diagnostics.add("REG-DOC-SELECTOR", parser.location(root.at))
		return nil
	}
	canonical, err := jcs.EncodeDocument(root.toValue())
	if err != nil {
		// Every number was held to §1's rules as it was read.
		diagnostics.add("REG-DOC-NUMBER", parser.location(root.at))
		return nil
	}
	// Step 1 on the bytes written, as validation will run it. The YAML
	// reading already refused what admission refuses inside the document, so
	// what is left to find here is the size bound and the selector's value.
	// A finding is located by line and column, never by a pointer, which
	// could carry a name the file holds.
	admission := &collector{}
	if admit(canonical, bound, member, selector, Location{Document: DocumentYAML}, admission) == nil {
		for _, diagnostic := range admission.diagnostics {
			location := parser.location(nodeAt(root, diagnostic.Location.Pointer).at)
			diagnostics.add(diagnostic.Code, location)
		}
		return nil
	}
	return canonical
}

// nodeAt returns the node a pointer into the document addresses, or the
// deepest node on its way that exists.
func nodeAt(root *yamlNode, pointer string) *yamlNode {
	tokens, err := parsePointer(pointer)
	if err != nil {
		return root
	}
	node := root
	for _, token := range tokens {
		var next *yamlNode
		switch node.kind {
		case yamlMapping:
			next = node.member(token)
		case yamlSequence:
			if index, ok := arrayIndex(token, len(node.children)); ok {
				next = node.children[index]
			}
		}
		if next == nil {
			return node
		}
		node = next
	}
	return node
}

// canonicalOrder sorts a registry's kinds by kind and its entries by kind
// then id, in code-point order (§3), when each item has those members as
// strings. A list that does not is left as written, for validation to report.
// The sort is stable, so the result depends only on the file.
func canonicalOrder(root *yamlNode) {
	sortList(root.member("kinds"), "kind")
	sortList(root.member("entries"), "kind", "id")
}

func sortList(list *yamlNode, members ...string) {
	if list == nil || list.kind != yamlSequence {
		return
	}
	type item struct {
		key  []string
		node *yamlNode
	}
	items := make([]item, len(list.children))
	for i, child := range list.children {
		if child.kind != yamlMapping {
			return
		}
		for _, name := range members {
			value := child.member(name)
			if value == nil || value.kind != yamlScalar {
				return
			}
			text, isString := value.value.(string)
			if !isString {
				return
			}
			items[i].key = append(items[i].key, text)
		}
		items[i].node = child
	}
	slices.SortStableFunc(items, func(a, b item) int { return slices.Compare(a.key, b.key) })
	for i := range items {
		list.children[i] = items[i].node
	}
}
