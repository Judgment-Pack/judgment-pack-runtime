package evaluation

import (
	"math/big"
	"slices"
	"testing"
)

// DecimalString writes the §2.2 grammar exactly, and it is the writer
// DecimalKey is not: the key renders big.Rat's canonical form, which spells 40.5
// as "81/2" — a string §7.4 declines to compare and no facts document may carry
// (ADR-0024).
func TestDecimalStringWritesTheGrammarDecimalKeyCannot(t *testing.T) {
	half, ok := DecimalValue("40.5")
	if !ok {
		t.Fatal(`"40.5" is a §2.2 decimal`)
	}
	if key, _ := DecimalKey("40.5"); key != "81/2" {
		t.Fatalf("the identity key is big.Rat's canonical form, which this test's premise depends on: %q", key)
	}
	if text, ok := DecimalString(half); !ok || text != "40.5" {
		t.Fatalf(`DecimalString must spell the value the grammar spells: %q %v`, text, ok)
	}
	if _, comparable := DecimalCompare("40.5", "81/2"); comparable {
		t.Fatal("the premise: §7.4 cannot compare big.Rat's canonical form at all")
	}
}

// Every value the generator's lattice can reach round-trips: read the text,
// do the arithmetic, write it back, and the evaluator reads the same number.
func TestDecimalStringIsExactOverTheLattice(t *testing.T) {
	for _, row := range []struct {
		literal string
		delta   *big.Rat
		want    string
	}{
		// One unit at the authored precision, which is the step rule.
		{"5000", big.NewRat(-1, 1), "4999"},
		{"5000", big.NewRat(1, 1), "5001"},
		{"70.0", big.NewRat(-1, 10), "69.9"},
		{"70.0", big.NewRat(1, 10), "70.1"},
		{"0.001", big.NewRat(-1, 1000), "0"},
		{"0.001", big.NewRat(1, 1000), "0.002"},
		// A midpoint of two terminating decimals is itself terminating, because
		// 2 divides 10 — the property that makes midpoints principled.
		{"40", big.NewRat(1, 2), "40.5"},
		{"0.1", big.NewRat(1, 20), "0.15"},
		// Signs and the integer-part rule of the grammar.
		{"-3.5", big.NewRat(-1, 10), "-3.6"},
		{"0.5", big.NewRat(-1, 1), "-0.5"},
		{"-0.5", big.NewRat(1, 2), "0"},
	} {
		value, ok := DecimalValue(row.literal)
		if !ok {
			t.Fatalf("%q must be admitted as a decimal", row.literal)
		}
		text, rendered := DecimalString(new(big.Rat).Add(value, row.delta))
		if !rendered || text != row.want {
			t.Fatalf("%s + %s: got %q (%v), want %q", row.literal, row.delta.RatString(), text, rendered, row.want)
		}
		// The written text is a value the evaluator reads back as the same
		// number: a renderer whose output §7.4 cannot compare would emit facts
		// that witness nothing.
		read, admitted := DecimalValue(text)
		if !admitted || read.Cmp(new(big.Rat).Add(value, row.delta)) != 0 {
			t.Fatalf("%q must read back as the number it was written from", text)
		}
	}
}

// A value whose expansion does not terminate is refused, never rounded. A
// rounded value is a different number, and a generator that substituted one
// would offer an input the pack's literals do not imply.
func TestDecimalStringRefusesRatherThanRounds(t *testing.T) {
	for _, unrenderable := range []*big.Rat{
		big.NewRat(1, 3),
		big.NewRat(-2, 7),
		big.NewRat(1, 6),
		new(big.Rat).SetFrac64(1, 1<<20*3),
	} {
		if text, ok := DecimalString(unrenderable); ok {
			t.Fatalf("%s has no terminating decimal expansion, and %q rounds it", unrenderable.RatString(), text)
		}
	}
	if _, ok := DecimalString(nil); ok {
		t.Fatal("no number is not a decimal")
	}
	// Zero and the integers are the boundary cases of the scale computation.
	for _, row := range []struct {
		value *big.Rat
		want  string
	}{
		{new(big.Rat), "0"},
		{big.NewRat(-7, 1), "-7"},
		{big.NewRat(1, 1024), "0.0009765625"},
		{big.NewRat(1, 100000), "0.00001"},
	} {
		text, ok := DecimalString(row.value)
		if !ok || text != row.want {
			t.Fatalf("%s: got %q (%v), want %q", row.value.RatString(), text, ok, row.want)
		}
	}
}

// DecimalValue admits exactly what §7.4 admits. big.Rat's own SetString is far
// wider, and a generator reading literals through it would derive values from
// spellings the evaluator refuses to compare.
func TestDecimalValueAdmitsOnlyTheGrammar(t *testing.T) {
	for _, admitted := range []string{"0", "-0", "5000", "70.0", "-3.500"} {
		if _, ok := DecimalValue(admitted); !ok {
			t.Fatalf("%q is a §2.2 decimal", admitted)
		}
	}
	for _, refused := range []any{"1/3", "1e5", " 5 ", "+5", "05", "5.", ".5", "5,000", 5000, true, nil} {
		if _, ok := DecimalValue(refused); ok {
			t.Fatalf("%v is not a §2.2 decimal, and big.Rat's own parser is not the gate", refused)
		}
	}
}

// PointerTokens hands back the escape-decoded tokens ResolvePointer walks, so a
// surface that places a value at a pointer and the evaluator that reads one
// agree about what ~1 and ~0 mean.
func TestPointerTokensAgreeWithTheResolver(t *testing.T) {
	document := map[string]any{"a/b": map[string]any{"~c": "here"}}
	tokens, rooted := PointerTokens("/a~1b/~0c")
	if !rooted || !slices.Equal(tokens, []string{"a/b", "~c"}) {
		t.Fatalf("tokens must be escape-decoded: %v %v", tokens, rooted)
	}
	if value, ok := ResolvePointer(document, "/a~1b/~0c"); !ok || value != "here" {
		t.Fatalf("the resolver reads the same tokens: %v %v", value, ok)
	}
	// The empty pointer is rooted and has no tokens: there is no member to
	// place, which a caller distinguishes rather than reading as an error.
	if tokens, rooted := PointerTokens(""); !rooted || len(tokens) != 0 {
		t.Fatalf("the empty pointer selects the root: %v %v", tokens, rooted)
	}
	if _, rooted := PointerTokens("a/b"); rooted {
		t.Fatal("a pointer that is neither empty nor rooted at / resolves against nothing")
	}
}

// ArrayIndex is the resolver's own array-token rule and not a second one. A
// surface that PLACES a value has to admit exactly the tokens the resolution
// admits, or it places where nothing reads: strconv.Atoi reads "00" and "+0" as
// zero, and RFC 6901 reads neither.
func TestArrayIndexIsTheResolutionsOwnRule(t *testing.T) {
	document := map[string]any{"items": []any{"zero", "one"}}
	for _, token := range []string{"0", "1", "00", "+0", "01", "-", "", " 0", "1.0", "2"} {
		index, admitted := ArrayIndex(token, 2)
		value, resolved := ResolvePointer(document, "/items/"+token)
		if admitted != resolved {
			t.Fatalf("token %q: ArrayIndex says %v and the resolver says %v", token, admitted, resolved)
		}
		if admitted && value != document["items"].([]any)[index] {
			t.Fatalf("token %q: the index and the resolution must name one element", token)
		}
	}
}

// The contract is about the NUMBER, not about the fraction it arrived as. This
// is an exported writer, and a *big.Rat a caller assembled through Num and
// Denom need not be in lowest terms: 2/4 rendered off an unreduced denominator
// is "0.50", which states a precision the number does not have, and 3/3 is
// refused outright for a denominator that clears the moment it is reduced.
// Both are the fewest-exact-digits contract failing on a value that satisfies
// it, so the value is reduced before it is read.
func TestDecimalStringReducesBeforeItRenders(t *testing.T) {
	// An unreduced Rat, built the one way a caller can build one: a value whose
	// denominator is a live reference, then set.
	unreduced := func(numerator, denominator int64) *big.Rat {
		value := big.NewRat(1, 2)
		value.Num().SetInt64(numerator)
		value.Denom().SetInt64(denominator)
		return value
	}
	for _, row := range []struct {
		numerator   int64
		denominator int64
		want        string
	}{
		{2, 4, "0.5"},
		{3, 3, "1"},
		{-6, 4, "-1.5"},
		{50, 100, "0.5"},
		{0, 7, "0"},
	} {
		value := unreduced(row.numerator, row.denominator)
		text, rendered := DecimalString(value)
		if !rendered || text != row.want {
			t.Fatalf("%d/%d: got %q (%v), want %q", row.numerator, row.denominator, text, rendered, row.want)
		}
		// The caller's own value is untouched: reduction happens on a copy,
		// because a writer that edited its argument would be a side effect
		// nobody asked a rendering for.
		if value.Num().Int64() != row.numerator || value.Denom().Int64() != row.denominator {
			t.Fatalf("%d/%d: the caller's value was modified: %s", row.numerator, row.denominator, value.RatString())
		}
	}
	// Reducing does not soften the refusal: a value whose expansion does not
	// terminate is still refused rather than rounded, sign included.
	for _, row := range [][2]int64{{1, 3}, {-1, 3}, {-7, 6}, {2, 6}} {
		if text, rendered := DecimalString(unreduced(row[0], row[1])); rendered {
			t.Fatalf("%d/%d has no terminating expansion and must be refused, not rounded to %q", row[0], row[1], text)
		}
	}
}
