package sdk_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
)

func vocabularyRow(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "internal", "vocabulary", "testdata", "1.0.0", "packs", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCheckVocabulary(t *testing.T) {
	ctx := context.Background()
	load := func(data []byte, options sdk.LoadOptions) *sdk.Pack {
		t.Helper()
		pack, err := sdk.Load(ctx, data, options)
		if err != nil {
			t.Fatal(err)
		}
		return pack
	}
	check := func(pack *sdk.Pack) sdk.VocabularyReport {
		t.Helper()
		report, err := pack.CheckVocabulary()
		if err != nil {
			t.Fatal(err)
		}
		if report.Vocabulary != "annotation-vocabulary" || report.Version != sdk.VocabularyVersion || sdk.VocabularyVersion != "1.0.0" ||
			report.Findings == nil || report.Advisories == nil {
			t.Fatalf("the report does not state what it applied: %+v", report)
		}
		return report
	}

	if got := check(load(vocabularyRow(t, "each-level"), sdk.LoadOptions{})); got.Result != sdk.VocabularyPass || len(got.Findings) != 0 {
		t.Fatalf("four levels: %+v", got)
	}
	got := check(load(vocabularyRow(t, "wrong-case"), sdk.LoadOptions{}))
	if got.Result != sdk.VocabularyRefused || len(got.Findings) != 2 || got.Reason != "" {
		t.Fatalf("High and CRITICAL: %+v", got)
	}
	for index, path := range []string{"/outcomes/1/extensions/org.judgmentpack.annotations/severity", "/outcomes/3/extensions/org.judgmentpack.annotations/severity"} {
		finding := got.Findings[index]
		if finding.Code != sdk.CodeVocabularySeverityValue || finding.InstancePath != path || finding.Layer != "vocabulary" || !strings.Contains(finding.Message, "Outcome ") {
			t.Fatalf("finding %d: %+v", index, finding)
		}
	}
	if got := check(load(vocabularyRow(t, "identifier-disagrees"), sdk.LoadOptions{})); got.Result != sdk.VocabularyPass || len(got.Advisories) != 1 ||
		got.Advisories[0].Code != sdk.CodeVocabularySeverityIdentifier || got.Advisories[0].InstancePath != "/outcomes/2/id" {
		t.Fatalf("refer-high at low: %+v", got)
	}

	// A required extension the caller does not support does not stop the
	// check, and one it supports changes nothing.
	unsupported := vocabularyRow(t, "unsupported-extension")
	for _, options := range []sdk.LoadOptions{{}, {SupportedExtensions: []string{"com.example.review-policy"}}} {
		if got := check(load(unsupported, options)); got.Result != sdk.VocabularyRefused || len(got.Findings) != 1 {
			t.Fatalf("with %v: %+v", options, got)
		}
	}

	// A Core 0.3.0 pack is not checked, for that reason.
	legacy := []byte(`{"specVersion":"0.3.0","id":"https://example.com/p","version":"1.0.0","title":"T",` +
		`"decision":{"intent":"i","question":"q"},"outcomes":[{"id":"keep","label":"Keep"},{"id":"refer-high","label":"Refer"}],` +
		`"rules":[{"id":"r","description":"d","when":{"op":"literal","value":true},"outcome":"refer-high","onUnknown":"escalate"}]}`)
	if got := check(load(legacy, sdk.LoadOptions{})); got.Result != sdk.VocabularyNotChecked || got.Reason != sdk.VocabularyReasonSpecVersion || len(got.Findings) != 0 {
		t.Fatalf("a 0.3.0 pack: %+v", got)
	}

	var none *sdk.Pack
	if _, err := none.CheckVocabulary(); err == nil || errors.Is(err, sdk.ErrInternal) {
		t.Fatalf("a Pack Load did not return: %v", err)
	}
}
