package federation

import (
	"slices"
	"strconv"
	"unicode/utf16"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// CheckIndex runs §5.1's index check: it assembles again from the inputs,
// which hold the bytes of each source's paths at the commits the index
// names, and holds the index that assembly would write to the given one,
// JCS for JCS. An assembly error fails the check with that error's
// diagnostics, and no FED-INDEX-MISMATCH. The error is a *KnownError for a
// known index or lock that is not admitted, with no report; otherwise it
// reports a document that could not be written in its canonical form, which
// an admitted document never is.
func CheckIndex(in Input, index registry.Document) (Report, error) {
	a := &assembly{collected: &collector{}}
	if err := a.run(in); err != nil {
		return Report{}, err
	}
	if a.collected.hasErrors() {
		return a.collected.report(a.digest()), nil
	}
	if index.Oversize {
		a.collected.addRegistry("REG-DOC-SIZE", Location{Document: DocumentIndex})
		return a.collected.report(a.digest()), nil
	}
	given, refusals := registry.AdmitDocument(index.Data, MaxIndexBytes, "indexFormatVersion", IndexFormatVersion, registry.Location{Document: DocumentIndex})
	for _, d := range refusals {
		a.collected.insert(fromRegistry(d, ""))
	}
	if given == nil {
		if len(refusals) == 0 {
			// Admission refuses with a diagnostic; this is never reached,
			// and a refusal without one would still not pass.
			a.collected.failed()
		}
		return a.collected.report(a.digest()), nil
	}
	mismatch := func(pointer string) {
		a.collected.add("FED-INDEX-MISMATCH", Location{Document: DocumentIndex, Pointer: pointer})
	}
	skip := map[string]bool{}
	builtSources, _ := a.index["sources"].([]any)
	for i, item := range builtSources {
		documents, _ := item.(map[string]any)["documents"].([]any)
		givenDocuments, _ := lookup(given, "sources", strconv.Itoa(i), "documents")
		givenList, _ := givenDocuments.([]any)
		for j, document := range documents {
			built := document.(map[string]any)
			if j >= len(givenList) {
				continue
			}
			other, isObject := givenList[j].(map[string]any)
			path, isString := other["path"].(string)
			contentDigest, present := other["contentDigest"]
			if !isObject || !isString || path != built["path"] || !present {
				continue
			}
			here := carrier.Pointer([]string{"sources", strconv.Itoa(i), "documents", strconv.Itoa(j), "contentDigest"})
			skip[here] = true
			if !registry.JSONEqual(contentDigest, built["contentDigest"]) {
				mismatch(here)
			}
		}
	}
	if digest, present := lookup(given, "federation", "digest"); present {
		skip["/federation/digest"] = true
		if !registry.JSONEqual(digest, lookupValue(a.index, "federation", "digest")) {
			mismatch("/federation/digest")
		}
	}
	if first, differs := firstDifference(a.index, given, "", skip); differs {
		mismatch(first)
	}
	return a.collected.report(a.digest()), nil
}

func lookup(value any, tokens ...string) (any, bool) {
	return registry.Lookup(value, tokens)
}

func lookupValue(value any, tokens ...string) any {
	found, _ := registry.Lookup(value, tokens)
	return found
}

// firstDifference returns the first pointer, in reading order, at which two
// JSON values differ: members by name in UTF-16 code units, as RFC 8785
// sorts them, and array items by index. A pointer in skip is compared
// already.
func firstDifference(a, b any, at string, skip map[string]bool) (string, bool) {
	if skip[at] {
		return "", false
	}
	aObject, aIsObject := a.(map[string]any)
	bObject, bIsObject := b.(map[string]any)
	if aIsObject && bIsObject {
		names := make([]string, 0, len(aObject)+len(bObject))
		for name := range aObject {
			names = append(names, name)
		}
		for name := range bObject {
			if _, shared := aObject[name]; !shared {
				names = append(names, name)
			}
		}
		slices.SortFunc(names, compareUTF16)
		for _, name := range names {
			here := at + "/" + escapeToken(name)
			aValue, inA := aObject[name]
			bValue, inB := bObject[name]
			if !inA || !inB {
				if !skip[here] {
					return here, true
				}
				continue
			}
			if found, differs := firstDifference(aValue, bValue, here, skip); differs {
				return found, true
			}
		}
		return "", false
	}
	aList, aIsList := a.([]any)
	bList, bIsList := b.([]any)
	if aIsList && bIsList {
		for i := 0; i < max(len(aList), len(bList)); i++ {
			here := at + "/" + strconv.Itoa(i)
			if i >= len(aList) || i >= len(bList) {
				return here, true
			}
			if found, differs := firstDifference(aList[i], bList[i], here, skip); differs {
				return found, true
			}
		}
		return "", false
	}
	if registry.JSONEqual(a, b) {
		return "", false
	}
	return at, true
}

func compareUTF16(a, b string) int {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
}
