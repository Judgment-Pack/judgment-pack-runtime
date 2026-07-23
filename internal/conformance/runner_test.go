package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func TestBundledSuitePasses(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	actual, failure := NewRunner(engine).Run("", artifacts.DraftVersion)
	if failure != nil {
		t.Fatal(failure)
	}
	if actual.Status != "valid" || actual.Summary.Total != 47 || actual.Summary.Passed != 47 || actual.Summary.Mismatched != 0 {
		t.Fatalf("unexpected suite result: %#v", actual.Summary)
	}
	if actual.Command != "spec test-conformance" || actual.Provenance != "immutable-git-ref" {
		t.Fatalf("unexpected suite metadata: %#v", actual)
	}
}

func TestLocalSuiteMismatchReturnsResult(t *testing.T) {
	root := writeLocalSuite(t)
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	invalidFixture, err := set.Case("structural/missing-decision.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "valid", "minimal-literal.json"), invalidFixture, 0o644); err != nil {
		t.Fatal(err)
	}

	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	actual, failure := NewRunner(engine).Run(root, artifacts.DraftVersion)
	if failure != nil {
		t.Fatal(failure)
	}
	if actual.Status != "mismatch" || actual.Summary.Mismatched != 1 {
		t.Fatalf("unexpected suite result: %#v", actual.Summary)
	}
}

func TestBundledAndEquivalentLocalSuiteHaveSameCorpusDigest(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(engine)
	bundled, failure := runner.Run("", artifacts.DraftVersion)
	if failure != nil {
		t.Fatal(failure)
	}
	local, failure := runner.Run(writeLocalSuite(t), artifacts.DraftVersion)
	if failure != nil {
		t.Fatal(failure)
	}
	if bundled.CorpusDigestAlgorithm != CorpusDigestAlgorithm || local.CorpusDigestAlgorithm != CorpusDigestAlgorithm || bundled.CorpusDigest != local.CorpusDigest {
		t.Fatalf("digest mismatch: bundled=%s local=%s", bundled.CorpusDigest, local.CorpusDigest)
	}
}

func TestSuiteUnderSymlinkedAncestorIsCanonicalized(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliably available on Windows CI")
	}
	root := writeLocalSuite(t)
	aliasParent := t.TempDir()
	alias := filepath.Join(aliasParent, "alias")
	if err := os.Symlink(filepath.Dir(root), alias); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	aliasedRoot := filepath.Join(alias, filepath.Base(root))
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	actual, failure := NewRunner(engine).Run(aliasedRoot, artifacts.DraftVersion)
	if failure != nil {
		t.Fatal(failure)
	}
	if actual.Status != "valid" || actual.Summary.Passed != actual.Summary.Total {
		t.Fatalf("unexpected suite result: %#v", actual.Summary)
	}
}

func TestSuiteResultCapsRetainedDiagnostics(t *testing.T) {
	root := writeLocalSuite(t)
	manifestPath := filepath.Join(root, "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	template := manifest["cases"].([]any)[0].(map[string]any)
	cases := make([]any, 0, 11)

	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	validBytes, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	var invalid map[string]any
	if err := json.Unmarshal(validBytes, &invalid); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 101; index++ {
		invalid[fmt.Sprintf("unknown%03d", index)] = true
	}
	invalidBytes, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}

	for index := 0; index < 11; index++ {
		id := fmt.Sprintf("limit-%02d", index)
		logicalPath := "structural/" + id + ".json"
		item := map[string]any{}
		for key, value := range template {
			item[key] = value
		}
		item["id"] = id
		item["path"] = logicalPath
		item["layer"] = "structural"
		item["expectedResult"] = "invalid"
		item["expectedDiagnostic"] = map[string]any{"code": "JPS-STRUCTURE-UNKNOWN-MEMBER", "path": "/unknown000"}
		cases = append(cases, item)
		target := filepath.Join(root, filepath.FromSlash(logicalPath))
		if err := os.WriteFile(target, invalidBytes, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest["cases"] = cases
	updated, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(updated, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	actual, failure := NewRunner(engine).Run(root, artifacts.DraftVersion)
	if failure != nil {
		t.Fatal(failure)
	}
	retained := 0
	for _, item := range actual.Cases {
		retained += len(item.ActualDiagnostics)
	}
	if retained != MaxResultDiagnostics || !actual.DiagnosticsTruncated {
		t.Fatalf("retained=%d truncated=%v", retained, actual.DiagnosticsTruncated)
	}
}

func writeLocalSuite(t *testing.T) string {
	t.Helper()
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	manifestBytes, err := set.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	schemaBytes, err := set.ManifestSchema()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.schema.json"), schemaBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, manifestCase := range manifest.Cases {
		data, err := set.Case(manifestCase.Path)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, filepath.FromSlash(manifestCase.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
