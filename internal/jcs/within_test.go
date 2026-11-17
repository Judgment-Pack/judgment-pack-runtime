package jcs

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
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
		if len(got) > maxNumberBytes {
			t.Fatalf("%v: %d bytes, more than maxNumberBytes", number, len(got))
		}
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
