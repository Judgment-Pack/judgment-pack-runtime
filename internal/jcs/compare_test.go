package jcs

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

// compareUTF16ByUnits is compareUTF16 as it was before it walked the names in
// place: both names converted to code points and then to UTF-16 code units,
// four slices a comparison. It is the order the walk must keep.
func compareUTF16ByUnits(left, right string) int {
	leftUnits, rightUnits := utf16.Encode([]rune(left)), utf16.Encode([]rune(right))
	return slices.Compare(leftUnits, rightUnits)
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// The walk orders every pair of names as the unit slices did: every name of
// up to three bytes drawn from ASCII, lead, continuation and invalid bytes
// (U+FFFD for each malformed byte, as both decode it),
// behind shared prefixes that end between code points, inside one, and
// inside a surrogate pair's code point; and random names of the code points
// at each boundary UTF-16 ordering turns on.
func TestCompareUTF16KeepsTheUnitOrder(t *testing.T) {
	alphabet := []byte{'a', 0xc3, 0x80, 0xbf, 0xe2, 0xef, 0xf0, 0x9f, 0xff}
	var names []string
	var grow func(prefix string)
	grow = func(prefix string) {
		names = append(names, prefix)
		if len(prefix) == 3 {
			return
		}
		for _, c := range alphabet {
			grow(prefix + string([]byte{c}))
		}
	}
	grow("")
	for _, shared := range []string{"", "key-", "é", "\U0001F600", "\xf0\x9f\x98", "\xe2\x82", "\xef\xbf"} {
		for _, a := range names {
			for _, b := range names {
				x, y := shared+a, shared+b
				if got, want := sign(compareUTF16(x, y)), sign(compareUTF16ByUnits(x, y)); got != want {
					t.Fatalf("%q vs %q: %d, want %d", x, y, got, want)
				}
			}
		}
	}
	points := []rune{'a', 'z', 0x7f, 0x80, 0x7ff, 0x800, 0xd7ff, 0xe000, 0xfffd, 0xfffe, 0xffff,
		0x10000, 0x10001, 0x103ff, 0x10400, 0x1f600, 0xfffff, 0x100000, 0x10fffe, 0x10ffff}
	random := rand.New(rand.NewPCG(464, 8785))
	name := func() string {
		var builder strings.Builder
		for range random.IntN(6) {
			if random.IntN(12) == 0 {
				builder.WriteByte(alphabet[random.IntN(len(alphabet))])
				continue
			}
			builder.WriteRune(points[random.IntN(len(points))])
		}
		return builder.String()
	}
	for range 200000 {
		x, y := name(), name()
		if random.IntN(3) == 0 {
			y = x + y
		}
		if got, want := sign(compareUTF16(x, y)), sign(compareUTF16ByUnits(x, y)); got != want {
			t.Fatalf("%q vs %q: %d, want %d", x, y, got, want)
		}
	}
	// A long shared prefix, then U+FFFF against U+10000: after it in UTF-16,
	// before it in code points.
	prefix := strings.Repeat("é", 4096)
	if compareUTF16(prefix+"￿", prefix+"\U00010000") <= 0 {
		t.Error("U+FFFF sorted before U+10000 behind a shared prefix")
	}
}

// An object's form is the same bytes with the walk as with the unit slices:
// its members sorted by either comparison, over names of every kind above.
func TestCanonicalMemberOrderIsUnchanged(t *testing.T) {
	random := rand.New(rand.NewPCG(464, 16))
	points := []rune{'a', 'b', 0x7f, 0x800, 0xd7ff, 0xe000, 0xfffd, 0xffff, 0x10000, 0x10400, 0x1f600, 0x10ffff}
	value := map[string]any{}
	for len(value) < 20000 {
		var builder strings.Builder
		for range 1 + random.IntN(4) {
			builder.WriteRune(points[random.IntN(len(points))])
		}
		value[builder.String()] = len(value) % 7
	}
	names := make([]string, 0, len(value))
	for name := range value {
		names = append(names, name)
		value[name] = fmt.Sprint(value[name])
	}
	slices.SortFunc(names, compareUTF16ByUnits)
	var want strings.Builder
	want.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			want.WriteByte(',')
		}
		if err := writeString(&want, name); err != nil {
			t.Fatal(err)
		}
		want.WriteByte(':')
		if err := writeString(&want, value[name].(string)); err != nil {
			t.Fatal(err)
		}
	}
	want.WriteByte('}')
	for _, encode := range []func(any) ([]byte, error){Encode, EncodeDocument} {
		got, err := encode(value)
		if err != nil || string(got) != want.String() {
			t.Fatalf("the form changed: %v", err)
		}
	}
}

// Comparing two names allocates nothing, so encoding an object of many
// members costs a small multiple of its form, the builder's growth and the
// sorted names: this object allocates about 7 times its form. Before the
// walk each comparison allocated its unit slices, and the same object
// allocated about 38 times its form.
func TestMemberOrderAllocatesNothing(t *testing.T) {
	if allocations := testing.AllocsPerRun(100, func() {
		compareUTF16("member-\U0001F600-a", "member-\U0001F600-b")
	}); allocations != 0 {
		t.Fatalf("a comparison allocated %v times", allocations)
	}
	value := map[string]any{}
	for i := range 20000 {
		value[fmt.Sprintf("m%07d", (i*7919)%10000000)] = true
	}
	form, err := EncodeDocument(value)
	if err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := EncodeDocument(value); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("an object of %d members, form %d bytes, allocated %d bytes", len(value), len(form), allocated)
	if allocated > 10*uint64(len(form)) {
		t.Fatalf("encoding allocated %d bytes, more than 10 times its %d-byte form", allocated, len(form))
	}
}
