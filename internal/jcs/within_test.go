package jcs

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// formatNumberByStrings is FormatNumber as it was written before
// AppendNumber, through strconv.FormatFloat and string operations: the
// reference the allocation-free layout must agree with.
func formatNumberByStrings(number float64) string {
	if number == 0 {
		return "0"
	}
	var builder strings.Builder
	if number < 0 {
		builder.WriteByte('-')
		number = -number
	}
	scientific := strconv.FormatFloat(number, 'e', -1, 64)
	mantissa, exponentText, _ := strings.Cut(scientific, "e")
	digits := strings.Replace(mantissa, ".", "", 1)
	exponent, _ := strconv.Atoi(exponentText)
	k, n := len(digits), exponent+1
	switch {
	case k <= n && n <= 21:
		builder.WriteString(digits + strings.Repeat("0", n-k))
	case 0 < n && n <= 21:
		builder.WriteString(digits[:n] + "." + digits[n:])
	case -6 < n && n <= 0:
		builder.WriteString("0." + strings.Repeat("0", -n) + digits)
	default:
		builder.WriteByte(digits[0])
		if k > 1 {
			builder.WriteString("." + digits[1:])
		}
		builder.WriteByte('e')
		if n-1 >= 0 {
			builder.WriteByte('+')
		}
		builder.WriteString(strconv.Itoa(n - 1))
	}
	return builder.String()
}

// AppendNumber writes what the string-built layout wrote, for every kind of
// double: random bit patterns, the layout's boundaries at 1e21 and 1e-6, and
// integers around 2^53.
func TestAppendNumberKeepsTheLayout(t *testing.T) {
	numbers := []float64{1, -1, 0.5, 1e20, 1e21, 1e-6, 1e-7, 123e18, 9.999999999999999e20, 0.000001234, 5e-324, math.MaxFloat64, -math.SmallestNonzeroFloat64, 1 << 53, 1<<53 + 2}
	random := rand.New(rand.NewPCG(458, 8785))
	for range 20000 {
		numbers = append(numbers, math.Float64frombits(random.Uint64()), float64(random.Int64N(1<<60)), random.Float64()*math.Pow(10, float64(random.IntN(50)-25)))
	}
	for _, number := range numbers {
		if math.IsNaN(number) || math.IsInf(number, 0) {
			continue
		}
		got, err := FormatNumber(number)
		if want := formatNumberByStrings(number); err != nil || got != want {
			t.Fatalf("%v (%016x): got %q %v, want %q", number, math.Float64bits(number), got, err, want)
		}
		if len(got) > maxNumberFormBytes {
			t.Fatalf("%v: %d bytes, more than maxNumberFormBytes", number, len(got))
		}
	}
	// The longest layout is reached.
	if got, err := FormatNumber(-0.0000033333333333333333); err != nil || len(got) != maxNumberFormBytes {
		t.Fatalf("the longest layout: %q %v, want %d bytes", got, err, maxNumberFormBytes)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		var scratch [maxNumberBytes]byte
		if _, err := AppendNumber(scratch[:0], -1.7976931348623157e308); err != nil {
			t.Fatal(err)
		}
	}); allocations != 0 {
		t.Fatalf("AppendNumber allocated %v times", allocations)
	}
}

// documentVectors are values whose forms exercise every case the measure
// counts: each escape, other control characters, multi-byte characters,
// numbers in every layout, null, Booleans, []string, and nesting.
func documentVectors() []any {
	return []any{
		nil, true, false, "", "plain", "\"\\\b\t\n\f\r", "\x00\x01\x1f\x7f", "é😀 ", json.Number("1e20"), json.Number("-0"),
		json.Number("1.50"), json.Number("5e-324"), json.Number("0.0000033333333333333333"), 1e21, -1e-7, 0.0,
		[]string{}, []string{"a", "\n"}, []any{}, []any{nil, json.Number("1"), []any{"x"}}, map[string]any{},
		map[string]any{"b": []any{json.Number("1e20"), nil}, "a\n": map[string]any{"\U0001F600": "after", "�": "before"}, "": false},
	}
}

// DocumentSize is the length of EncodeDocument's form, and
// EncodeDocumentWithin writes the same bytes up to the bound and refuses past
// it.
func TestEncodeDocumentWithinWritesEncodeDocumentsForm(t *testing.T) {
	for _, value := range documentVectors() {
		want, err := EncodeDocument(value)
		if err != nil {
			t.Fatalf("%#v: %v", value, err)
		}
		if size, err := DocumentSize(value); err != nil || size != len(want) {
			t.Errorf("%s: DocumentSize %d %v, want %d", want, size, err, len(want))
		}
		if got, err := EncodeDocumentWithin(value, len(want)); err != nil || string(got) != string(want) {
			t.Errorf("%s: within its own length, got %s %v", want, got, err)
		}
		if got, err := EncodeDocumentWithin(value, len(want)-1); !errors.Is(err, ErrTooLong) || got != nil {
			t.Errorf("%s: one byte under its length, got %s %v, want ErrTooLong", want, got, err)
		}
	}
}

// A value with no canonical form is refused with EncodeDocument's own error,
// over the bound or not, so that a caller that tells the two apart refuses
// it as before.
func TestEncodeDocumentWithinKeepsTheEncodersRefusals(t *testing.T) {
	long := strings.Repeat("a", 1000)
	for _, value := range []any{
		math.NaN(), math.Inf(-1), json.Number("1e400"), json.Number("Infinity"), json.Number("x"), "\xff", 7, []string{"a", "\xff"},
		[]any{long, math.NaN()}, map[string]any{"a": long, "\xff": 1.0}, map[string]any{"a": long, "b": []any{struct{}{}}},
	} {
		_, want := EncodeDocument(value)
		if want == nil {
			t.Fatalf("%#v: the encoder admits it", value)
		}
		if _, err := DocumentSize(value); err == nil || err.Error() != want.Error() {
			t.Errorf("%#v: DocumentSize %v, want %v", value, err, want)
		}
		for _, bound := range []int{0, 1 << 20} {
			if got, err := EncodeDocumentWithin(value, bound); err == nil || errors.Is(err, ErrTooLong) || err.Error() != want.Error() || got != nil {
				t.Errorf("%#v within %d: got %v, want %v", value, bound, err, want)
			}
		}
	}
}

// Refusing a value whose form passes the bound builds none of it: the
// measure allocates nothing, however far the form passes the bound. 1e20 is
// written as 21 digits, so this value's form is more than four times its
// literals.
func TestEncodeDocumentWithinRefusesWithoutBuilding(t *testing.T) {
	items := make([]any, 20000)
	for i := range items {
		items[i] = json.Number("1e20")
	}
	value := map[string]any{"numbers": items, "text": strings.Repeat("\n", 1000), "list": []string{"a", "b"}}
	size, err := DocumentSize(value)
	if err != nil || size < 20000*21 {
		t.Fatalf("size %d %v", size, err)
	}
	if got, err := EncodeDocumentWithin(value, size-1); !errors.Is(err, ErrTooLong) || got != nil {
		t.Fatalf("got %d bytes %v, want ErrTooLong", len(got), err)
	}
	if allocations := testing.AllocsPerRun(10, func() {
		if _, err := EncodeDocumentWithin(value, size/4); !errors.Is(err, ErrTooLong) {
			t.Fatal(err)
		}
	}); allocations != 0 {
		t.Fatalf("refusing a form of %d bytes allocated %v times", size, allocations)
	}
	if allocations := testing.AllocsPerRun(10, func() {
		if _, err := DocumentSize(value); err != nil {
			t.Fatal(err)
		}
	}); allocations != 0 {
		t.Fatalf("measuring a form of %d bytes allocated %v times", size, allocations)
	}
}

// countParses counts the parses of json.Number literals f makes.
func countParses(t *testing.T, f func()) int {
	t.Helper()
	parses := 0
	saved := parseFloat
	defer func() { parseFloat = saved }()
	parseFloat = func(text string, size int) (float64, error) {
		parses++
		return saved(text, size)
	}
	f()
	return parses
}

// A value whose form fits the bound with every number at its longest form is
// built in one pass, each number parsed once (#468): subnormal doubles take
// the standard library's slow path, and parsing each twice doubled the time
// of a bounded encode. Within the bound only exactly, the length is measured
// first and each number is parsed twice; refused, once.
func TestEncodeDocumentWithinParsesEachNumberOnce(t *testing.T) {
	items := make([]any, 1000)
	for i := range items {
		items[i] = json.Number("4.9406564584124654e-324")
	}
	value := map[string]any{"numbers": items, "text": "x"}
	want, err := EncodeDocument(value)
	if err != nil {
		t.Fatal(err)
	}
	most := len(want) + len(items)*(maxNumberFormBytes-len("5e-324"))
	for _, row := range []struct {
		bound, parses int
	}{{most, len(items)}, {1 << 30, len(items)}, {most - 1, 2 * len(items)}, {len(want), 2 * len(items)}, {len(want) - 1, len(items)}} {
		var got []byte
		parses := countParses(t, func() { got, err = EncodeDocumentWithin(value, row.bound) })
		if parses != row.parses {
			t.Errorf("within %d bytes: %d parses, want %d", row.bound, parses, row.parses)
		}
		if row.bound >= len(want) && (err != nil || string(got) != string(want)) {
			t.Errorf("within %d bytes: %v", row.bound, err)
		}
		if row.bound < len(want) && (!errors.Is(err, ErrTooLong) || got != nil) {
			t.Errorf("within %d bytes: %d bytes %v, want ErrTooLong", row.bound, len(got), err)
		}
	}
}

// A form built in one pass allocates about what the exact measure's builder
// did, its form and the copy returned, though its numbers' forms are longer
// than their literals: 1e20 is written as 21 digits. Growing from the
// literals' lengths cost 5.6 times the form.
func TestEncodeDocumentWithinSizesItsBuilder(t *testing.T) {
	items := make([]any, 20000)
	for i := range items {
		items[i] = json.Number("1e20")
	}
	form, err := EncodeDocument(items)
	if err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got, err := EncodeDocumentWithin(items, 1<<30)
	runtime.ReadMemStats(&after)
	if err != nil || string(got) != string(form) {
		t.Fatalf("got %d bytes %v", len(got), err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 3*uint64(len(form)) {
		t.Fatalf("a form of %d bytes allocated %d", len(form), allocated)
	}
}

// EncodeDocumentWithin writes EncodeDocument's bytes or refuses as it did,
// whichever walk decides: over random documents of numbers of every layout,
// strings that need escapes, and nesting, at bounds on both sides of the
// form's length and of its longest reading. Every literal is one carrier.Decode
// could give, and some no double holds.
func TestEncodeDocumentWithinAgreesWithTheMeasure(t *testing.T) {
	random := rand.New(rand.NewPCG(468, 1))
	literals := []string{"0", "-0", "1e20", "1.50", "5e-324", "4.9406564584124654e-324", "0.0000033333333333333333", "-1e-7",
		"1.7976931348623157e308", "123456789012345678901234567890", "1e400", "-1e400", "1e-400"}
	var build func(depth int) any
	build = func(depth int) any {
		switch kind := random.IntN(8); {
		case depth > 3 || kind < 3:
			if random.IntN(2) == 0 {
				return json.Number(literals[random.IntN(len(literals))])
			}
			return json.Number(strconv.FormatFloat(math.Float64frombits(random.Uint64()&^(0x7ff<<52)|uint64(random.IntN(2046)+1)<<52), 'g', -1, 64))
		case kind == 3:
			return []string{"a", "\n\"", "\u00e9"}[random.IntN(3)]
		case kind == 4:
			return []any{nil, true, false, 1.5}[random.IntN(4)]
		case kind == 5:
			items := make([]any, random.IntN(6))
			for i := range items {
				items[i] = build(depth + 1)
			}
			return items
		default:
			object := map[string]any{}
			for range random.IntN(6) {
				object[[]string{"a", "b", "\U0001F600", "\uffff", "\x01"}[random.IntN(5)]] = build(depth + 1)
			}
			return object
		}
	}
	for range 3000 {
		value := build(0)
		want, wantErr := EncodeDocument(value)
		var span sizeSpan
		span.limit = 1 << 30
		span.add(value)
		for _, bound := range []int{len(want) - 1, len(want), len(want) + 1, span.most - 1, span.most, random.IntN(2*len(want) + 2)} {
			got, err := EncodeDocumentWithin(value, bound)
			switch {
			case wantErr != nil:
				if err == nil || err.Error() != wantErr.Error() || got != nil {
					t.Fatalf("%s within %d: %v, want %v", want, bound, err, wantErr)
				}
			case len(want) > bound:
				if !errors.Is(err, ErrTooLong) || got != nil {
					t.Fatalf("%s within %d: %v, want ErrTooLong", want, bound, err)
				}
			case err != nil || string(got) != string(want):
				t.Fatalf("%s within %d: got %s %v", want, bound, got, err)
			}
		}
	}
}

// The builder's first capacity is the form's length for literals that are
// shortest round-trip decimals, in any layout and subnormals among them, and
// for literals of at most 15 significant digits in the normal range written
// with zeros or an exponent: a bounded encode then allocates about what the
// exact measure's builder did. It is never past the longest form.
func TestNumberFormHint(t *testing.T) {
	random := rand.New(rand.NewPCG(468, 3))
	for range 20000 {
		double := math.Float64frombits(random.Uint64())
		if math.IsNaN(double) || math.IsInf(double, 0) {
			continue
		}
		form, err := FormatNumber(double)
		if err != nil {
			t.Fatal(err)
		}
		literals := []string{form, strconv.FormatFloat(double, 'g', -1, 64), strconv.FormatFloat(double, 'e', -1, 64), strconv.FormatFloat(double, 'E', -1, 64)}
		for _, literal := range literals {
			if got := numberFormHint(literal); got != len(form) {
				t.Fatalf("%s: hint %d, form %q", literal, got, form)
			}
		}
		if exponent := math.Abs(math.Log10(math.Abs(double))); double == 0 || exponent > 300 {
			continue
		}
		// 15 significant digits, written with zeros added and the point moved.
		short := strconv.FormatFloat(double, 'e', 14, 64)
		again, _ := strconv.ParseFloat(short, 64)
		if form, err = FormatNumber(again); err != nil {
			t.Fatal(err)
		}
		mantissa, power, _ := strings.Cut(short, "e")
		unsigned, negative := strings.CutPrefix(mantissa, "-")
		sign := ""
		if negative {
			sign = "-"
		}
		exponent, _ := strconv.Atoi(power)
		for _, literal := range []string{short, mantissa + "000e" + power, sign + "0.00" + strings.Replace(unsigned, ".", "", 1) + "e" + strconv.Itoa(exponent+3)} {
			if got := numberFormHint(literal); got != len(form) {
				t.Fatalf("%s: hint %d, form %q", literal, got, form)
			}
		}
	}
	for literal, want := range map[string]int{"1e20": 21, "-1e20": 22, "-0": 1, "0.000e9": 1, "1E+2": 3, ".5": 3,
		"x": 1, "": 1, "1e": 2, "1e+-2": 5, "1.5.0": 5, strings.Repeat("9", 40): 22, strings.Repeat("1", 30): 22} {
		if got := numberFormHint(literal); got != want {
			t.Errorf("%q: hint %d, want %d", literal, got, want)
		}
	}
}
