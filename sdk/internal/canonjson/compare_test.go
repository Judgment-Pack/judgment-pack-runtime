package canonjson

import (
	"slices"
	"testing"
	"unicode/utf16"
)

// encodedOrder is the order CompareUTF16 states, computed by encoding both
// names: the reference the allocation-free comparison is held to.
func encodedOrder(left, right string) int {
	return slices.Compare(utf16.Encode([]rune(left)), utf16.Encode([]rune(right)))
}

// CompareUTF16 agrees with comparing the encoded UTF-16 units for every pair
// of a corpus that crosses the surrogate range from both sides, holds
// prefixes, equal names and invalid UTF-8, and it allocates nothing.
func TestCompareUTF16AgreesWithEncodedUnits(t *testing.T) {
	corpus := []string{"", "a", "ab", "b", "\u007f", "é", "퟿", "", "￿", "�",
		"\U00010000", "\U0001f600", "\U0001f601", "\U0010ffff", "\U0001f600a", "a\U0001f600", "a￿",
		"\xff", "\xffa", "\xc3", "\xed\xa0\x80", "��", "\U00010400", "\U000103ff"}
	for _, left := range corpus {
		for _, right := range corpus {
			if got, want := CompareUTF16(left, right), encodedOrder(left, right); got != want {
				t.Fatalf("CompareUTF16(%q, %q) = %d, encoded units give %d", left, right, got, want)
			}
		}
	}
	if allocations := testing.AllocsPerRun(100, func() { CompareUTF16("name\U0001f600", "name￿") }); allocations != 0 {
		t.Fatalf("CompareUTF16 allocates %v times", allocations)
	}
}
