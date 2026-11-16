package registry

import (
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// tokenKind holds one secret and one integer; a registry with no secret
// scopes gets one REF-SECRET-UNSCOPED warning per entry and environment.
const tokenKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/token","version":"1.0.0","kind":"token","owner":"team-a",
"schema":{"type":"object","additionalProperties":false,"properties":{"secret":{"type":"string"},"n":{"type":"integer"}}},
"secrets":["/secret"]}`

// spareKind is supplied but not pinned: one REG-PIN-UNPINNED warning.
const spareKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/spare","version":"1.0.0","kind":"spare","owner":"team-a",
"schema":{"type":"object"}}`

// boundRegistry is a registry of one environment with no secret scopes,
// pinning tokenKind, with the entries given.
func boundRegistry(t *testing.T, entries []string) string {
	t.Helper()
	return `{"registryFormatVersion":"1.0.0","id":"https://example.invalid/registries/bound","version":"1.0.0",
"environments":["dev"],"owners":["team-a"],
"kinds":[{"kind":"token","id":"https://example.invalid/kinds/token","version":"1.0.0","digest":"` + digestOf(t, tokenKind) + `"}],
"entries":[` + strings.Join(entries, ",") + `]}`
}

func tokenEntry(id, value string) string {
	return `{"kind":"token","id":"` + id + `","owner":"team-a","value":` + value + `}`
}

const unscopedSecret = `{"secret":"secret:gcp-sm:projects/example-dev/secrets/x/versions/1"}`

// withLimit runs call with the report bound set to limit; 0 is no bound.
func withLimit(limit int, call func()) {
	saved := maxReportDiagnostics
	maxReportDiagnostics = limit
	defer func() { maxReportDiagnostics = saved }()
	call()
}

// checkCapped checks a capped report against the uncapped one: exactly its
// first limit diagnostics, in order, then one REG-REPORT-TRUNCATED.
func checkCapped(t *testing.T, name string, capped, uncapped []Diagnostic, limit int) {
	t.Helper()
	if len(uncapped) <= limit {
		if !reflect.DeepEqual(capped, uncapped) {
			t.Fatalf("%s: a report within the bound changed", name)
		}
		return
	}
	if len(capped) != limit+1 {
		t.Fatalf("%s: %d diagnostics of %d, want %d plus the truncation code", name, len(capped), len(uncapped), limit)
	}
	if !reflect.DeepEqual(capped[:limit], uncapped[:limit]) {
		t.Fatalf("%s: the capped report is not the first %d of the uncapped one", name, limit)
	}
	last := capped[limit]
	if last.Code != "REG-REPORT-TRUNCATED" || last.Severity != SeverityWarning || !reflect.DeepEqual(last.Location, Location{Document: DocumentRegistry}) {
		t.Fatalf("%s: last %+v", name, last)
	}
}

// A JSON registry with more failing entries than the bound reports the first
// 1000 diagnostics of the uncapped report and one REG-REPORT-TRUNCATED, and
// keeps no more than those: the diagnostics it drops cost no memory.
func TestValidateReportIsCappedAtTheFirst1000(t *testing.T) {
	if !strings.Contains(Codes["REG-REPORT-TRUNCATED"].message, fmt.Sprint(maxReportDiagnostics)) || maxReportDiagnostics != 1000 {
		t.Fatalf("the message does not name the bound %d", maxReportDiagnostics)
	}
	var entries []string
	for i := 0; i < 50_000; i++ {
		// A kind the registry does not pin: one REG-ENTRY-KIND each.
		entries = append(entries, `{"kind":"ghost","id":"e`+fmt.Sprintf("%05d", i)+`","owner":"team-a","value":{}}`)
	}
	registry := boundRegistry(t, entries)
	if len(registry) > MaxRegistryBytes {
		t.Fatalf("the registry is %d bytes, over the bound", len(registry))
	}
	var capped, uncapped *Result
	cappedHeld := retainedBy(func() any { capped = run(registry, []string{tokenKind}); return capped })
	var uncappedHeld uint64
	withLimit(0, func() {
		uncappedHeld = retainedBy(func() any { uncapped = run(registry, []string{tokenKind}); return uncapped })
	})
	if len(uncapped.Report.Diagnostics) != 50_000 || capped.Report.Valid {
		t.Fatalf("uncapped %d diagnostics, capped valid %v", len(uncapped.Report.Diagnostics), capped.Report.Valid)
	}
	checkCapped(t, "50000 failing entries", capped.Report.Diagnostics, uncapped.Report.Diagnostics, 1000)
	cappedReport, uncappedReport := len(encode(t, capped.Report)), len(encode(t, uncapped.Report))
	t.Logf("document %d KB; report capped %d KB, uncapped %d KB; held after the call: capped %d KB, uncapped %d KB",
		len(registry)>>10, cappedReport>>10, uncappedReport>>10, cappedHeld>>10, uncappedHeld>>10)
	// The budget: the report is at most 1001 diagnostics, 512 KB encoded,
	// whatever the document holds; the uncapped one outgrows the document.
	// What the result holds is the validated registry (about 37 MB here) and
	// those 1001, within 48 MB; the uncapped report holds 4 MB more.
	if cappedReport > 512<<10 || uncappedReport < len(registry) {
		t.Fatalf("report capped %d KB, uncapped %d KB", cappedReport>>10, uncappedReport>>10)
	}
	if uncappedHeld < cappedHeld+4<<20 || cappedHeld > 48<<20 {
		t.Fatalf("held: capped %d KB, uncapped %d KB: the capped report holds more than its 1001 diagnostics", cappedHeld>>10, uncappedHeld>>10)
	}
}

// retainedBy is the heap still in use after call, with what it returns kept
// alive.
func retainedBy(call func() any) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	held := call()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(held)
	if after.HeapAlloc < before.HeapAlloc {
		return 0
	}
	return after.HeapAlloc - before.HeapAlloc
}

// mixedRegistry finds diagnostics in an order unlike the report's: entry
// indexes past 9 sort before 2 as pointers, value findings sort after the
// registry's, and a kind document's warning is found after the registry's
// shape.
func mixedRegistry(t *testing.T) (string, []string) {
	t.Helper()
	var entries []string
	for i := 0; i < 14; i++ {
		id := fmt.Sprintf("m%02d", 20-i)
		switch i % 4 {
		case 0:
			entries = append(entries, `{"kind":"ghost","id":"`+id+`","owner":"team-a","value":{}}`)
		case 1:
			entries = append(entries, `{"kind":"token","id":"`+id+`","owner":"nobody","value":{}}`)
		case 2:
			entries = append(entries, tokenEntry(id, `{"n":"x","secret":"secret:gcp-sm:projects/example-dev/secrets/x/versions/1"}`))
		default:
			entries = append(entries, tokenEntry(id, `{"n":1.5,"extra":true}`))
		}
	}
	return boundRegistry(t, entries), []string{tokenKind, spareKind}
}

// A capped report is exactly the first limit diagnostics of the uncapped
// one, whatever order validation found them in.
func TestValidateReportCapIsAPrefixOfTheUncappedReport(t *testing.T) {
	registry, kinds := mixedRegistry(t)
	var uncapped *Result
	withLimit(0, func() { uncapped = run(registry, kinds) })
	if len(uncapped.Report.Diagnostics) < 20 {
		t.Fatalf("the fixture finds only %d diagnostics", len(uncapped.Report.Diagnostics))
	}
	for _, limit := range []int{1, 2, 3, 7, 12, 19, len(uncapped.Report.Diagnostics) - 1, len(uncapped.Report.Diagnostics), 1000} {
		var capped *Result
		withLimit(limit, func() { capped = run(registry, kinds) })
		checkCapped(t, fmt.Sprintf("limit %d", limit), capped.Report.Diagnostics, uncapped.Report.Diagnostics, limit)
		if capped.Report.Valid != uncapped.Report.Valid {
			t.Fatalf("limit %d: valid %v, uncapped %v", limit, capped.Report.Valid, uncapped.Report.Valid)
		}
	}
}

// A truncated report's bytes do not depend on the order diagnostics are
// found in: not on the order the kinds are supplied in, nor on the order the
// checks reach them, which the entries' order sets. (Reordering the entries
// in the document itself changes the document, its digest and its pointers.)
func TestTruncatedReportBytesDoNotDependOnOrder(t *testing.T) {
	registry, kinds := mixedRegistry(t)
	kinds = append(kinds, databaseKind, roleKind)
	const limit = 9
	var want string
	random := rand.New(rand.NewSource(427))
	for i := 0; i < 8; i++ {
		order := permutation(random, len(kinds))
		shuffled := make([]string, len(kinds))
		for j, k := range order {
			shuffled[j] = kinds[k]
		}
		var capped *Result
		withLimit(limit, func() { capped = run(registry, shuffled) })
		got := encode(t, capped.Report)
		if i == 0 {
			want = got
			if !strings.Contains(got, "REG-REPORT-TRUNCATED") {
				t.Fatal("the fixture does not truncate")
			}
		} else if got != want {
			t.Fatalf("kinds in order %v: the report's bytes differ", order)
		}
	}
	var uncapped *Result
	withLimit(0, func() { uncapped = run(registry, kinds) })
	found := uncapped.Report.Diagnostics
	var first string
	for i := 0; i < 8; i++ {
		c := &collector{limit: limit}
		for _, k := range permutation(random, len(found)) {
			c.add(found[k].Code, found[k].Location)
			// Each is found twice; a duplicate is kept once.
			c.add(found[k].Code, found[k].Location)
		}
		got := encode(t, c.sorted())
		if i == 0 {
			first = got
			checkCapped(t, "shuffled", c.sorted(), found, limit)
		} else if got != first {
			t.Fatalf("permutation %d: the kept diagnostics differ", i)
		}
	}
}

func permutation(random *rand.Rand, n int) []int { return random.Perm(n) }

// Validity is judged on every diagnostic found, not on those kept: a
// registry of more than 1000 warnings and no error is valid and resolves, and
// one whose only error sorts after the 1000 kept warnings is not.
func TestValidityIsJudgedOnEveryDiagnostic(t *testing.T) {
	var entries []string
	for i := 0; i < 1100; i++ {
		entries = append(entries, tokenEntry(fmt.Sprintf("e%04d", i), unscopedSecret))
	}
	valid := run(boundRegistry(t, entries), []string{tokenKind})
	if !valid.Report.Valid || len(valid.Report.Diagnostics) != 1001 || valid.Report.Diagnostics[1000].Code != "REG-REPORT-TRUNCATED" {
		t.Fatalf("valid %v, %d diagnostics", valid.Report.Valid, len(valid.Report.Diagnostics))
	}
	if resolved, err := valid.Resolve("dev"); err != nil || len(resolved) == 0 {
		t.Fatalf("resolve: %v", err)
	}
	// One error, on the entry that sorts last.
	entries = append(entries, tokenEntry("z-last", `{"n":"x"}`))
	invalid := run(boundRegistry(t, entries), []string{tokenKind})
	if invalid.Report.Valid || len(invalid.Report.Diagnostics) != 1001 {
		t.Fatalf("valid %v, %d diagnostics", invalid.Report.Valid, len(invalid.Report.Diagnostics))
	}
	for _, d := range invalid.Report.Diagnostics {
		if d.Severity != SeverityWarning {
			t.Fatalf("the error should sort past the bound: %+v", d)
		}
	}
	if _, err := invalid.Resolve("dev"); err != ErrInvalid {
		t.Fatalf("resolve of an invalid registry: %v", err)
	}
}

// Diagnostics that tie on location are ordered by code, then message, so
// the kept diagnostics and their order are fully determined.
func TestReportOrderBreaksTiesByCodeThenMessage(t *testing.T) {
	at := Location{Document: DocumentRegistry, Pointer: "/entries/0"}
	a := Diagnostic{Code: "REG-ENTRY-KIND", Severity: SeverityError, Location: at, Message: "a"}
	b := a
	b.Message = "b"
	if compareDiagnostics(a, b) >= 0 || compareDiagnostics(b, a) <= 0 {
		t.Fatal("a tie on location and code is not broken by message")
	}
	c := a
	c.Code = "REG-ENTRY-ID"
	c.Message = "z"
	if compareDiagnostics(c, a) >= 0 {
		t.Fatal("code orders before message")
	}
}
