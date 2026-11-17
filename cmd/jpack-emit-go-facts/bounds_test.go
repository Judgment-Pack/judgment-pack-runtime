package main

import (
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Tests of the bounds held before generation and before go/format (ADR-0100).

// requestOf is a request of the given contracts' texts.
func requestOf(contracts []string) string {
	return strings.Replace(base(``), contractText("https://example.invalid/c/thing", ``), strings.Join(contracts, ","), 1)
}

// enumRow is one string row whose closed list holds n values, each of the
// letter run "a" repeated width times and a number.
func enumRow(n, width int) string {
	values := make([]string, n)
	for i := range values {
		values[i] = `"` + strings.Repeat("a", width) + strconv.Itoa(i) + `"`
	}
	return `{"pointer":"/a","type":"string","present":"always","values":[` + strings.Join(values, ",") + `]}`
}

// TestTooManyContractsAreRefusedBeforeGeneration: a request of maxFiles
// contracts would need maxFiles+1 files, and is refused before any contract
// is read. The contracts are maxFiles copies of one, whose files would
// collide, so a request that reached generation would be errCollide.
func TestTooManyContractsAreRefusedBeforeGeneration(t *testing.T) {
	one := contractText("https://example.invalid/c/thing", ``)
	out, err := emit([]byte(requestOf(slices.Repeat([]string{one}, maxFiles))))
	if err != errBounds || out != nil {
		t.Fatalf("emit = %d bytes, %v; want %v", len(out), err, errBounds)
	}
}

// TestMostContractsAreGenerated: maxFiles-1 contracts are maxFiles files,
// which the bound admits.
func TestMostContractsAreGenerated(t *testing.T) {
	if testing.Short() {
		t.Skip("generates 10000 files")
	}
	contracts := make([]string, maxFiles-1)
	for i := range contracts {
		contracts[i] = contractText("https://example.invalid/c/t"+strconv.Itoa(i), ``)
	}
	out, err := emit([]byte(requestOf(contracts)))
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if paths, _ := readResponse(t, out); len(paths) != maxFiles {
		t.Fatalf("%d files, want %d", len(paths), maxFiles)
	}
}

// TestOversizedFileIsRefusedBeforeFormatting: one contract whose source is
// over maxFileBytes is refused, and with a fraction of the memory that
// formatting it would take: the allocations counted, not the peak, under a
// loose bound.
func TestOversizedFileIsRefusedBeforeFormatting(t *testing.T) {
	request := []byte(base(enumRow(500000, 1)))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	out, err := emit(request)
	runtime.ReadMemStats(&after)
	if err != errBounds || out != nil {
		t.Fatalf("emit = %d bytes, %v; want %v", len(out), err, errBounds)
	}
	// Measured: about 420 MB refused before formatting, about 1250 MB
	// formatted.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 800<<20 {
		t.Fatalf("emit allocated %d MB for a %d MB request; want the refusal before formatting", allocated>>20, len(request)>>20)
	}
}

// TestOversizedResponseIsRefusedBeforeFormatting: contracts each under
// maxFileBytes, whose sources together are over maxResponseBytes, are
// refused, and the last ones are not formatted.
func TestOversizedResponseIsRefusedBeforeFormatting(t *testing.T) {
	contracts := make([]string, 5)
	for i := range contracts {
		contracts[i] = contractText("https://example.invalid/c/t"+strconv.Itoa(i), enumRow(5000, 1000))
	}
	out, err := emit([]byte(requestOf(contracts)))
	if err != errBounds || out != nil {
		t.Fatalf("emit = %d bytes, %v; want %v", len(out), err, errBounds)
	}
}

// TestFormattingNeverShortensTheSource: the bounds held before go/format
// hold because the formatted file is never shorter than the source written.
// Checked over every vector and a request of a closed list.
func TestFormattingNeverShortensTheSource(t *testing.T) {
	requests := [][]byte{[]byte(base(enumRow(3, 1)))}
	for _, v := range readVectors(t) {
		if v.expected != nil {
			requests = append(requests, v.request)
		}
	}
	for _, request := range requests {
		value, err := decodeStrict(request)
		if err != nil {
			t.Fatal(err)
		}
		root := value.(map[string]any)
		opts, err := readOptions(root["options"])
		if err != nil {
			t.Fatal(err)
		}
		n := newNamer()
		for _, item := range root["factsContracts"].([]any) {
			c, err := readContract(item)
			if err != nil {
				t.Fatal(err)
			}
			if err := n.nameContract(c); err != nil {
				t.Fatal(err)
			}
			g := &writer{}
			g.contract(c, opts.pkg)
			text, err := formatted(g.b.String())
			if err != nil {
				t.Fatal(err)
			}
			if len(text) < g.b.Len() {
				t.Errorf("%s: formatted %d bytes, from a source of %d", c.id, len(text), g.b.Len())
			}
		}
		source := header + "\npackage " + opts.pkg + "\n" + supportSource
		if text, err := formatted(source); err != nil || len(text) < len(source) {
			t.Errorf("support file: formatted %d bytes, from a source of %d, %v", len(text), len(source), err)
		}
	}
}
