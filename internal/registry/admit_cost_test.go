package registry

import (
	"encoding/json"
	"strings"
	"testing"
)

// Admitting a document whose numbers canonical encoding expands costs about
// what admitting any document of its size does (#458). An exact comparison
// by big arithmetic cost hundreds of bytes a number, so a MiB of 1e20s
// allocated 223 MiB. Now each 1e20 costs the decoder's work and its
// 21-digit form alone.
//
// The factor: the decoder that every reader shares costs 50 to 130 times a
// document's bytes to read one of small values into a tree (`1,` holds a
// value every two bytes), the same whether its numbers expand or not, and
// admission adds each changed literal, about 8 times the bytes here. This
// document measures about 59 times its size; 128 is about twice that, and
// the 223 times it cost before fails it.
func TestAdmitValueOfExpandingNumbersCostsAboutItsSize(t *testing.T) {
	const size = 1 << 20
	data := []byte("[" + strings.TrimSuffix(strings.Repeat("1e20,", size/5), ",") + "]")
	allocatesWithin(t, 128*size, "a MiB of 1e20s", func() {
		value, code, pointer := AdmitValue(data, len(data))
		items, _ := value.([]any)
		if code != "" || len(items) != size/5 || items[0] != json.Number("100000000000000000000") {
			t.Fatalf("admitted %d items, %s at %q", len(items), code, pointer)
		}
	})
}

// A literal is checked without big arithmetic: one kept as written allocates
// nothing, and one admission rewrites allocates its new form alone.
func TestAdmittedLiteralAllocatesOnlyARewrittenForm(t *testing.T) {
	for _, row := range []struct {
		literal, kept string
		allocations   float64
	}{
		{"1.5", "1.5", 0}, {"123", "123", 0}, {"-9007199254740991", "-9007199254740991", 0}, {"1e-7", "1e-7", 0},
		{"0.000001", "0.000001", 0}, {"1.7976931348623157e+308", "1.7976931348623157e+308", 0},
		{"1e20", "100000000000000000000", 1}, {"1.50", "1.5", 1}, {"-2.5E-3", "-0.0025", 1}, {"5e-324", "5e-324", 0},
	} {
		literal, ok := admittedLiteral(json.Number(row.literal))
		if !ok || string(literal) != row.kept {
			t.Fatalf("%s: got %q %v, want %q", row.literal, literal, ok, row.kept)
		}
		number := json.Number(row.literal)
		if got := testing.AllocsPerRun(100, func() { admittedLiteral(number) }); got != row.allocations {
			t.Errorf("%s: %v allocations, want %v", row.literal, got, row.allocations)
		}
	}
}
