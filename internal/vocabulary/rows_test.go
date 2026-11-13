package vocabulary_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evalform"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/vocabulary"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
)

// The Spec's rows for the vocabulary, copied byte for byte from the Spec
// release testdata/provenance.json names. The Spec stages them: they are no
// conformance corpus and back no claim, and this runtime is the second reader
// of the vocabulary, by the same maintainer as the first, not an independent
// implementation.
const rowsDir = "testdata/1.0.0"

type pointer struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

type row struct {
	ID           string `json:"id"`
	Pack         string `json:"pack"`
	ExpectedCore struct {
		Result     string   `json:"result"`
		Diagnostic *pointer `json:"diagnostic"`
	} `json:"expectedCore"`
	ExpectedResult     string    `json:"expectedResult"`
	ExpectedReason     *string   `json:"expectedReason"`
	ExpectedFindings   []pointer `json:"expectedFindings"`
	ExpectedAdvisories []pointer `json:"expectedAdvisories"`
}

type evaluationRow struct {
	ID                   string          `json:"id"`
	Facts                json.RawMessage `json:"facts"`
	EvidenceAvailability json.RawMessage `json:"evidenceAvailability"`
	SupportedExtensions  []string        `json:"supportedExtensions"`
	ExpectedDisposition  json.RawMessage `json:"expectedDisposition"`
}

type manifest struct {
	Vocabulary        string `json:"vocabulary"`
	VocabularyVersion string `json:"vocabularyVersion"`
	CoreSpecVersion   string `json:"coreSpecVersion"`
	Rows              []row  `json:"rows"`
	Pairs             []struct {
		ID          string          `json:"id"`
		Basis       string          `json:"basis"`
		Packs       []string        `json:"packs"`
		Evaluations []evaluationRow `json:"evaluations"`
	} `json:"pairs"`
}

func readManifest(t *testing.T) manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(rowsDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func readPack(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(rowsDir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newEngine(t *testing.T) *validation.Engine {
	t.Helper()
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

// packsValidateCore is Core's result for the bytes as packs validate computes
// it: through the semantic layer, under the selector the pack declares.
func packsValidateCore(engine *validation.Engine, data []byte) *result.Validation {
	validated, operational := engine.Validate(data, validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
	if operational != nil {
		return nil
	}
	return &validated
}

func pointers(diagnostics []result.Diagnostic) []pointer {
	out := make([]pointer, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		out = append(out, pointer{Code: diagnostic.Code, Path: diagnostic.InstancePath})
	}
	return out
}

// sameSet compares findings as the companion says they compare: as a set of
// code and JSON Pointer pairs (§6.3).
func sameSet(got, want []pointer) bool {
	order := func(items []pointer) []pointer {
		items = slices.Clone(items)
		sort.Slice(items, func(i, j int) bool {
			if items[i].Code != items[j].Code {
				return items[i].Code < items[j].Code
			}
			return items[i].Path < items[j].Path
		})
		return slices.Compact(items)
	}
	return slices.Equal(order(got), order(want))
}

func TestVendoredFilesKeepTheirUpstreamDigests(t *testing.T) {
	var pin struct {
		Repository string `json:"repository"`
		Release    string `json:"release"`
		Revision   string `json:"revision"`
		Files      map[string]struct {
			Upstream string `json:"upstream"`
			SHA256   string `json:"sha256"`
		} `json:"files"`
	}
	raw, err := os.ReadFile("testdata/provenance.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &pin); err != nil {
		t.Fatal(err)
	}
	if pin.Release != "v0.5.2" || pin.Revision != "561b2a7245cf6ee13cb5be78da9c7a89f9adcdef" {
		t.Fatalf("the rows are pinned to %s at %s", pin.Release, pin.Revision)
	}
	listed := 0
	err = filepath.WalkDir("testdata", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		name := filepath.ToSlash(path[len("testdata/"):])
		if name == "provenance.json" || name == "README.md" {
			return nil
		}
		file, ok := pin.Files[name]
		if !ok {
			t.Errorf("%s is vendored without a provenance entry", name)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Errorf("%s no longer has the bytes of %s at the pinned revision", name, file.Upstream)
		}
		listed++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if listed != len(pin.Files) {
		t.Fatalf("provenance lists %d files and %d are vendored", len(pin.Files), listed)
	}
}

// The levels, codes and selector are written in the checker; they must be
// the Spec's vocabulary.json, which the checker never reads.
func TestEmbeddedVocabularyIsTheSpecVocabulary(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(rowsDir, "vocabulary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Vocabulary           string `json:"vocabulary"`
		Version              string `json:"version"`
		CoreSpecVersion      string `json:"coreSpecVersion"`
		AnnotationsExtension string `json:"annotationsExtension"`
		Keys                 []struct {
			Key                string   `json:"key"`
			Positions          []string `json:"positions"`
			ValueForm          string   `json:"valueForm"`
			Values             []string `json:"values"`
			Order              string   `json:"order"`
			Refusal            string   `json:"refusal"`
			IdentifierAdvisory string   `json:"identifierAdvisory"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Vocabulary != vocabulary.Name || spec.Version != vocabulary.Version ||
		spec.CoreSpecVersion != vocabulary.SpecVersion || spec.AnnotationsExtension != vocabulary.AnnotationsName {
		t.Fatalf("the checker's identity differs from vocabulary.json: %+v", spec)
	}
	if len(spec.Keys) != 1 {
		t.Fatalf("vocabulary.json defines %d keys; the checker knows one", len(spec.Keys))
	}
	key := spec.Keys[0]
	if key.Key != vocabulary.Key || !slices.Equal(key.Positions, []string{"outcome"}) || key.ValueForm != "string" ||
		key.Order != "ascending" || key.Refusal != vocabulary.ValueCode || key.IdentifierAdvisory != vocabulary.IdentifierCode {
		t.Fatalf("the checker's key differs from vocabulary.json: %+v", key)
	}
	if !slices.Equal(vocabulary.Levels(), key.Values) {
		t.Fatalf("the checker's levels %v are not vocabulary.json's %v", vocabulary.Levels(), key.Values)
	}
	for rank, level := range key.Values {
		if got, ok := vocabulary.Rank(level); !ok || got != rank {
			t.Fatalf("level %q ranks %d, %v; vocabulary.json orders it %d", level, got, ok, rank)
		}
	}
}

// Every row of the Spec's manifest through the checker as packs validate runs
// it: Core's result, then the vocabulary's result, reason, findings and
// advisories. A row whose expectations are a reader's that supports the
// extended profile is not run: this runtime reads base only, and the row says
// such a checker reports the vocabulary not applied, for that reason.
func TestSpecRows(t *testing.T) {
	m := readManifest(t)
	if m.Vocabulary != vocabulary.Name || m.VocabularyVersion != vocabulary.Version || m.CoreSpecVersion != vocabulary.SpecVersion {
		t.Fatalf("the manifest is for %s %s on Core %s", m.Vocabulary, m.VocabularyVersion, m.CoreSpecVersion)
	}
	engine := newEngine(t)
	ran, notRun := 0, 0
	for _, r := range m.Rows {
		t.Run(r.ID, func(t *testing.T) {
			data := readPack(t, r.Pack)
			core := packsValidateCore(engine, data)
			got := vocabulary.Check(data, core, carrier.DefaultLimits())
			if got.Vocabulary != vocabulary.Name || got.Version != vocabulary.Version {
				t.Fatalf("the report does not state the vocabulary version it applied: %+v", got)
			}
			if r.ID == "profile-extended" {
				if got.Result != vocabulary.ResultNotChecked || got.Reason != vocabulary.ReasonProfileUnsupported || len(got.Findings) != 0 || len(got.Advisories) != 0 {
					t.Fatalf("a base-only checker reports this row not applied, profile-unsupported; got %+v", got)
				}
				notRun++
				return
			}
			// The rows' Core expectations are a Core 0.5.0 reader's, so a pack
			// declaring another selector is held to 0.5.0 for this half.
			reader, operational := engine.Validate(data, validation.Options{Through: "semantic", ExpectedSpecVersion: vocabulary.SpecVersion, Limits: carrier.DefaultLimits()})
			if operational != nil {
				t.Fatalf("Core failed operationally: %v", operational)
			}
			if reader.Status != r.ExpectedCore.Result {
				t.Fatalf("Core says %s, the row expects %s: %+v", reader.Status, r.ExpectedCore.Result, reader.Diagnostics)
			}
			if r.ExpectedCore.Diagnostic != nil && !slices.Contains(pointers(reader.Diagnostics), *r.ExpectedCore.Diagnostic) {
				t.Fatalf("Core reports %+v, the row expects %+v", pointers(reader.Diagnostics), *r.ExpectedCore.Diagnostic)
			}
			if got.Result != r.ExpectedResult {
				t.Fatalf("result %s, the row expects %s: %+v", got.Result, r.ExpectedResult, got)
			}
			wantReason := ""
			if r.ExpectedReason != nil {
				wantReason = *r.ExpectedReason
			}
			if got.Reason != wantReason {
				t.Fatalf("reason %q, the row expects %q", got.Reason, wantReason)
			}
			if wantReason == vocabulary.ReasonCoreRefused && got.CoreLayer != "structural" {
				t.Fatalf("Core's refusal is reported with layer %q, the structural layer refused it", got.CoreLayer)
			}
			if !sameSet(pointers(got.Findings), r.ExpectedFindings) || len(got.Findings) != len(r.ExpectedFindings) {
				t.Fatalf("findings %+v, the row expects %+v", pointers(got.Findings), r.ExpectedFindings)
			}
			if !sameSet(pointers(got.Advisories), r.ExpectedAdvisories) || len(got.Advisories) != len(r.ExpectedAdvisories) {
				t.Fatalf("advisories %+v, the row expects %+v", pointers(got.Advisories), r.ExpectedAdvisories)
			}
			ran++
		})
	}
	t.Logf("%d rows run, %d not run (profile-extended: this runtime reads the base profile only)", ran, notRun)
	if ran != 31 || notRun != 1 || len(m.Rows) != 32 {
		t.Fatalf("%d of %d rows ran and %d were not run; the manifest at the pinned revision has 32, of which one needs the extended profile", ran, len(m.Rows), notRun)
	}
}

// The SDK's check of each row Load admits says what packs validate's says:
// Load refuses the packs Core refuses, so only the selector can stop it.
func TestSpecRowsThroughTheSDK(t *testing.T) {
	m := readManifest(t)
	engine := newEngine(t)
	admitted := 0
	for _, r := range m.Rows {
		data := readPack(t, r.Pack)
		pack, err := sdk.Load(context.Background(), data, sdk.LoadOptions{})
		if err != nil {
			continue
		}
		admitted++
		got, err := pack.CheckVocabulary()
		if err != nil {
			t.Fatalf("%s: %v", r.ID, err)
		}
		want := vocabulary.Check(data, packsValidateCore(engine, data), carrier.DefaultLimits())
		if got.Version != want.Version || got.Result != want.Result || got.Reason != want.Reason ||
			len(got.Findings) != len(want.Findings) || len(got.Advisories) != len(want.Advisories) {
			t.Fatalf("%s: the SDK reports %+v and packs validate %+v", r.ID, got, want)
		}
		for index, finding := range got.Findings {
			if finding.Code != want.Findings[index].Code || finding.InstancePath != want.Findings[index].InstancePath || finding.Message != want.Findings[index].Message {
				t.Fatalf("%s: finding %d differs: %+v and %+v", r.ID, index, finding, want.Findings[index])
			}
		}
	}
	// Every row but the four Core refuses, the extended-profile row and the
	// two selector rows: this runtime bundles no 0.6.0, and Core 0.3.0
	// reserves the annotations member the earlier row carries.
	if admitted != 25 {
		t.Fatalf("Load admitted %d rows", admitted)
	}
}

// removeSeverity is §2.6's removal: the member, then an annotations object it
// leaves empty, then an extensions object that leaves empty, on each outcome
// whose annotations hold the key, and nothing else.
func removeSeverity(root map[string]any) map[string]any {
	copied := deepCopy(root).(map[string]any)
	outcomes, _ := copied["outcomes"].([]any)
	for _, raw := range outcomes {
		outcome, _ := raw.(map[string]any)
		extensions, _ := outcome["extensions"].(map[string]any)
		annotations, _ := extensions[vocabulary.AnnotationsName].(map[string]any)
		if _, present := annotations[vocabulary.Key]; !present {
			continue
		}
		delete(annotations, vocabulary.Key)
		if len(annotations) == 0 {
			delete(extensions, vocabulary.AnnotationsName)
			if len(extensions) == 0 {
				delete(outcome, "extensions")
			}
		}
	}
	return copied
}

func deepCopy(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = deepCopy(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = deepCopy(item)
		}
		return out
	default:
		return value
	}
}

func decode(t *testing.T, data []byte) map[string]any {
	t.Helper()
	value, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	return value.(map[string]any)
}

func encode(t *testing.T, root map[string]any) []byte {
	t.Helper()
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(root); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// The pair, evaluated: the three packs give each facts document the
// disposition the row expects, the same bytes for all three, and the same
// bytes as each pack with the key removed as §2.6 removes it.
func TestInertnessPair(t *testing.T) {
	m := readManifest(t)
	if len(m.Pairs) != 1 || m.Pairs[0].ID != "inert-severity" || m.Pairs[0].Basis != "evaluation-form" || len(m.Pairs[0].Packs) != 3 || len(m.Pairs[0].Evaluations) != 5 {
		t.Fatalf("the manifest's pairs are %+v", m.Pairs)
	}
	pair := m.Pairs[0]
	var forms []map[string]any
	for _, name := range pair.Packs {
		form, err := evalform.Of(decode(t, readPack(t, name)))
		if err != nil {
			t.Fatal(err)
		}
		forms = append(forms, form)
	}
	for index := 1; index < len(forms); index++ {
		if !reflect.DeepEqual(forms[0], forms[index]) {
			t.Fatalf("%s and %s have different evaluation forms", pair.Packs[0], pair.Packs[index])
		}
	}
	ctx := context.Background()
	for _, evaluation := range pair.Evaluations {
		var first []byte
		for _, name := range pair.Packs {
			data := readPack(t, name)
			removed := encode(t, removeSeverity(decode(t, data)))
			original := evaluate(t, ctx, data, evaluation)
			stripped := evaluate(t, ctx, removed, evaluation)
			if !bytes.Equal(original.CanonicalDisposition(), stripped.CanonicalDisposition()) {
				t.Fatalf("%s %s: the disposition differs with the key removed", name, evaluation.ID)
			}
			originalJSON, err := original.JSON()
			if err != nil {
				t.Fatal(err)
			}
			strippedJSON, err := stripped.JSON()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(originalJSON, strippedJSON) {
				t.Fatalf("%s %s: the evaluation differs with the key removed:\n%s\n%s", name, evaluation.ID, originalJSON, strippedJSON)
			}
			var got, want any
			if err := json.Unmarshal(original.CanonicalDisposition(), &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(evaluation.ExpectedDisposition, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s %s: disposition %s, the row expects %s", name, evaluation.ID, original.CanonicalDisposition(), evaluation.ExpectedDisposition)
			}
			if first == nil {
				first = original.CanonicalDisposition()
			} else if !bytes.Equal(first, original.CanonicalDisposition()) {
				t.Fatalf("%s %s: the three packs give different dispositions", name, evaluation.ID)
			}
		}
	}
}

func evaluate(t *testing.T, ctx context.Context, data []byte, row evaluationRow) *sdk.Evaluation {
	t.Helper()
	pack, err := sdk.Load(ctx, data, sdk.LoadOptions{SupportedExtensions: row.SupportedExtensions})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := pack.Evaluate(ctx, row.Facts, row.EvidenceAvailability)
	if err != nil {
		t.Fatal(err)
	}
	return evaluation
}

// §2.6's removal on every row's pack Core 0.5.0 admits gives a pack Core still
// admits and an equal evaluation form, whatever the vocabulary said of it.
func TestRemovalKeepsTheEvaluationForm(t *testing.T) {
	m := readManifest(t)
	engine := newEngine(t)
	checked := 0
	for _, r := range m.Rows {
		data := readPack(t, r.Pack)
		core := packsValidateCore(engine, data)
		if core == nil || core.SpecVersion != vocabulary.SpecVersion || (core.Status != "valid" && r.ExpectedCore.Result != "unsupported") {
			continue
		}
		root := decode(t, data)
		removed := removeSeverity(root)
		again, operational := engine.Validate(encode(t, removed), validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
		if operational != nil || again.Status != core.Status {
			t.Fatalf("%s: with the key removed Core says %s, before %s: %+v", r.ID, again.Status, core.Status, again.Diagnostics)
		}
		before, err := evalform.Of(root)
		if err != nil {
			t.Fatal(err)
		}
		after, err := evalform.Of(removed)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("%s: the evaluation form changes with the key removed", r.ID)
		}
		if report := vocabulary.Admitted(removed); report.Result != vocabulary.ResultPass || len(report.Advisories) != 0 {
			t.Fatalf("%s: with the key removed the vocabulary says %+v", r.ID, report)
		}
		checked++
	}
	// The 25 rows Core 0.5.0 admits, the unsupported-extension row included.
	if checked != 25 {
		t.Fatalf("%d rows' packs checked", checked)
	}
}
