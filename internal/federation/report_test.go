package federation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// §2.1: source sorts right after document, and keyId right after source, an
// absent member as "".
func TestTheOrderPlacesSourceAndKeyAfterDocument(t *testing.T) {
	ordered := []Location{
		{Document: "bounds", Pointer: "/z"},
		{Document: "bounds", Source: "a", Pointer: "/z"},
		{Document: "bounds", Source: "a", KeyID: "k", Pointer: "/a"},
		{Document: "bounds", Source: "b", Pointer: "/a"},
		{Document: "registry", Pointer: "/a"},
		{Document: "registry", Source: "a", Entry: &registry.EntryName{Kind: "k", ID: "a"}, Pointer: ""},
		{Document: "registry", Source: "a", Entry: &registry.EntryName{Kind: "k", ID: "a"}, Environment: "dev", Pointer: ""},
		{Document: "registry", Source: "a", Kind: "k", Pointer: ""},
	}
	for i := range ordered[1:] {
		a := Diagnostic{Code: "FED-MANIFEST", Location: ordered[i]}
		b := Diagnostic{Code: "FED-MANIFEST", Location: ordered[i+1]}
		if compareDiagnostics(a, b) >= 0 || compareDiagnostics(b, a) <= 0 {
			t.Fatalf("%+v does not sort before %+v", ordered[i], ordered[i+1])
		}
	}
}

func TestTheReportFormOmitsAbsentMembers(t *testing.T) {
	collected := &collector{}
	collected.add("FED-SOURCE-MISSING", Location{Document: DocumentFederation, Pointer: "/sources/0/registry"})
	collected.add("FED-BOUND-WIDENS", Location{Document: DocumentBounds, Source: "billing", Pointer: "/fields/0/bound/maximum"})
	got, err := collected.report("").Canonical()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"diagnostics":[{"code":"FED-BOUND-WIDENS","location":{"document":"bounds","pointer":"/fields/0/bound/maximum","source":"billing"},"message":"` + Codes["FED-BOUND-WIDENS"].message + `","severity":"error"},{"code":"FED-SOURCE-MISSING","location":{"document":"federation","pointer":"/sources/0/registry"},"message":"` + Codes["FED-SOURCE-MISSING"].message + `","severity":"error"}],"reportFormatVersion":"1.1.0","valid":false}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	empty, err := (&collector{}).report("sha256:" + strings.Repeat("0", 64)).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"diagnostics":[],"registry":{"digest":"sha256:` + strings.Repeat("0", 64) + `"},"reportFormatVersion":"1.1.0","valid":true}`; string(empty) != want {
		t.Fatalf("got %s", empty)
	}
	keyed := &collector{}
	keyed.add("FED-MANIFEST", Location{Document: DocumentSignature, Source: "a", KeyID: "abc", Pointer: ""})
	if out, _ := keyed.report("").Canonical(); !strings.Contains(string(out), `"keyId":"abc"`) {
		t.Fatalf("the key identifier is not written: %s", out)
	}
}

// The report keeps the first 1000 distinct diagnostics in its order, whatever
// order they were found in, then REG-REPORT-TRUNCATED; the validity counts
// every error found.
func TestTheCollectorKeepsTheFirstThousand(t *testing.T) {
	collected := &collector{}
	for i := 1200; i > 0; i-- {
		collected.add("FED-MANIFEST", Location{Document: DocumentFederation, Pointer: fmt.Sprintf("/sources/%04d", i)})
		collected.add("FED-MANIFEST", Location{Document: DocumentFederation, Pointer: fmt.Sprintf("/sources/%04d", i)})
	}
	report := collected.report("")
	if len(report.Diagnostics) != MaxReportDiagnostics+1 || report.Valid {
		t.Fatalf("%d diagnostics, valid %v", len(report.Diagnostics), report.Valid)
	}
	if first, last := report.Diagnostics[0].Location.Pointer, report.Diagnostics[MaxReportDiagnostics-1].Location.Pointer; first != "/sources/0001" || last != "/sources/1000" {
		t.Fatalf("kept %s to %s", first, last)
	}
	if end := report.Diagnostics[MaxReportDiagnostics]; end.Code != "REG-REPORT-TRUNCATED" || end.Severity != SeverityWarning || end.Location != (Location{Document: registry.DocumentRegistry}) {
		t.Fatalf("the report ends with %+v", end)
	}
	exact := &collector{}
	for i := 0; i < MaxReportDiagnostics; i++ {
		exact.add("FED-MANIFEST", Location{Document: DocumentFederation, Pointer: fmt.Sprintf("/sources/%04d", i)})
	}
	if report := exact.report(""); len(report.Diagnostics) != MaxReportDiagnostics || exact.dropped {
		t.Fatalf("1000 diagnostics are truncated: %d", len(report.Diagnostics))
	}
}

// A validation inside an operation that lists only its first 1000 says the
// operation's report is truncated too, and fails it when the validation did:
// its uncounted errors are not taken for none.
func TestATruncatedValidationTruncatesTheReport(t *testing.T) {
	collected := &collector{}
	collected.insert(Diagnostic{Code: "REG-REPORT-TRUNCATED", Severity: SeverityWarning, Location: Location{Document: registry.DocumentRegistry}, Message: messageTruncated})
	if report := collected.report(""); len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "REG-REPORT-TRUNCATED" || !report.Valid {
		t.Fatalf("%+v", report)
	}
	collected.failed()
	if report := collected.report(""); report.Valid || len(report.Diagnostics) != 1 {
		t.Fatalf("an invalid validation whose errors were not listed passes: %+v", report)
	}
}

func TestAnUnlistedCodePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an unlisted code was recorded")
		}
	}()
	(&collector{}).add("FED-UNLISTED", Location{Document: DocumentFederation})
}
