package registry

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

// Admitting a document whose numbers canonical encoding expands costs about
// what admitting any document of its size does (#458). An exact comparison
// by big arithmetic cost hundreds of bytes a number, so a MiB of 1e20s
// allocated 223 MiB. Now each 1e20 costs the decoder's work and its
// 21-digit form alone.
//
// The factor: the decoder that every reader shares costs 10 to 51 times a
// document's bytes to read one of small values into a tree (`1,` holds a
// value every two bytes), the same whether its numbers expand or not, and
// admission adds each changed literal, about 8 times the bytes here. This
// document measures about 29 times its size; 48 leaves room, and the 59
// times it cost while the decoder read every value through the standard
// decoder's tokens fails it, as do the 223 times before that.
func TestAdmitValueOfExpandingNumbersCostsAboutItsSize(t *testing.T) {
	const size = 1 << 20
	data := []byte("[" + strings.TrimSuffix(strings.Repeat("1e20,", size/5), ",") + "]")
	allocatesWithin(t, 48*size, "a MiB of 1e20s", func() {
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

// A YAML kind whose canonical form passes its bound, though its text is well
// within it, is refused as before, with REG-DOC-SIZE at the root, and its
// form is never built or read back (#458): past the reading of the YAML,
// refusing it allocates less than the bound. The escape \a is written as
// \u0007, three times its text. At the bound exactly, the kind is imported
// with the same bytes.
func TestImportRefusesAnExpandingKindWithoutBuildingIt(t *testing.T) {
	const escapes = 40000
	head, end := `{"description":"`, `","kindFormatVersion":"1.0.0"}`
	pad := MaxKindBytes - len(head) - 6*escapes - len(end)
	source := func(selector string, pad int) string {
		return selector + ": \"1.0.0\"\ndescription: \"" + strings.Repeat(`\a`, escapes) + strings.Repeat("x", pad) + "\"\n"
	}
	want := head + strings.Repeat(`\u0007`, escapes) + strings.Repeat("x", pad) + end
	if len(want) != MaxKindBytes || len(source("kindFormatVersion", pad)) > MaxKindBytes/2 {
		t.Fatalf("form %d bytes, text %d", len(want), len(source("kindFormatVersion", pad)))
	}
	if document, report := importText(source("kindFormatVersion", pad)); !report.Valid || string(document) != want {
		t.Fatalf("at the bound: %d bytes, %+v", len(document), report.Diagnostics)
	}
	allocated := func(f func()) uint64 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		f()
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	// The same text without a selector is refused before its form is
	// written: what reading the YAML costs.
	read := allocated(func() {
		if _, report := importText(source("neither", pad+1)); len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "REG-DOC-SELECTOR" {
			t.Fatalf("no selector: %+v", report.Diagnostics)
		}
	})
	refused := allocated(func() {
		document, report := importText(source("kindFormatVersion", pad+1))
		if document != nil || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "REG-DOC-SIZE" ||
			report.Diagnostics[0].Location != (Location{Document: DocumentYAML, Line: 1, Column: 1}) {
			t.Fatalf("one byte over the bound: %+v", report.Diagnostics)
		}
	})
	t.Logf("reading %d bytes, refusing %d bytes, bound %d", read, refused, MaxKindBytes)
	if refused > read+MaxKindBytes {
		t.Fatalf("refusing allocated %d bytes past the %d of reading the YAML, more than the bound of %d", refused-read, read, MaxKindBytes)
	}
}
