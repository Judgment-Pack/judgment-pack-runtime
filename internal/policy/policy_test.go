package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifactversion"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

type wireEntry struct {
	Content    map[string]any `json:"content"`
	DataBase64 any            `json:"dataBase64"`
}
type fixture struct {
	policy, adoption map[string]any
	entries          []wireEntry
	raw              map[string][]byte
	names            []string
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	data, err := prototype.ReadFile("prototype/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func mustMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func loadFixture(t *testing.T, domain string) *fixture {
	t.Helper()
	base := "examples/" + domain + "/"
	manifest := mustMap(t, mustRead(t, base+"fixture.json"))
	f := &fixture{policy: mustMap(t, mustRead(t, base+"policy.json")), adoption: mustMap(t, mustRead(t, base+"adoption.json")), raw: map[string][]byte{}}
	for _, item := range array(manifest["materials"]) {
		entry := object(item)
		name := text(entry["path"])
		raw := mustRead(t, base+name)
		f.raw[name] = raw
		f.names = append(f.names, name)
		f.entries = append(f.entries, wireEntry{Content: object(entry["content"]), DataBase64: base64.StdEncoding.EncodeToString(raw)})
	}
	return f
}
func (f *fixture) wire(t *testing.T) []byte {
	return mustJSON(t, map[string]any{"materials": f.entries})
}
func (f *fixture) replace(t *testing.T, name string, raw []byte) {
	t.Helper()
	for i, n := range f.names {
		if n == name {
			ref := map[string]any{"bytesDigest": digest(raw), "byteLength": len(raw), "mediaType": f.entries[i].Content["mediaType"]}
			f.entries[i] = wireEntry{Content: ref, DataBase64: base64.StdEncoding.EncodeToString(raw)}
			f.raw[name] = raw
			return
		}
	}
	t.Fatalf("unknown fixture material %s", name)
}
func (f *fixture) content(t *testing.T, name string) map[string]any {
	t.Helper()
	for i, n := range f.names {
		if n == name {
			return f.entries[i].Content
		}
	}
	t.Fatalf("unknown material %s", name)
	return nil
}
func (f *fixture) retainPolicy(t *testing.T) {
	f.replace(t, "policy.json", mustJSON(t, f.policy))
	governing := object(array(f.adoption["governingPolicies"])[0])
	reference := object(governing["policy"])
	reference["content"] = f.content(t, "policy.json")
	delete(reference, "canonicalIdentity")
}
func (f *fixture) retainConsumer(t *testing.T, raw []byte) {
	f.replace(t, "consumer-pack.json", raw)
	reference := object(f.adoption["consumer"])
	reference["content"] = f.content(t, "consumer-pack.json")
	delete(reference, "canonicalIdentity")
}

var testEngineOnce sync.Once
var testEngine *validation.Engine
var testEngineError error

func engine(t *testing.T) *validation.Engine {
	t.Helper()
	testEngineOnce.Do(func() { testEngine, testEngineError = validation.NewEngine() })
	if testEngineError != nil {
		t.Fatal(testEngineError)
	}
	return testEngine
}
func requireRefusal(t *testing.T, r Report, code, want int) {
	t.Helper()
	if code != want || r.Status != "refused" || len(r.Diagnostics) == 0 {
		t.Fatalf("code %d want %d report %+v", code, want, r)
	}
}

func TestPinnedSyntheticScenarios(t *testing.T) {
	for _, domain := range []string{"data-access", "procurement", "refunds"} {
		t.Run(domain, func(t *testing.T) {
			f := loadFixture(t, domain)
			for _, name := range []string{"policy.json", "adoption.json"} {
				raw := f.raw[name]
				r, code := Canonicalize("canonicalize", raw)
				if code != 0 || r.Status != "encoded" || r.BytesDigest == r.CanonicalDigest {
					t.Fatalf("canonical report %+v code%d", r, code)
				}
				if r.BytesDigest != digest(raw) {
					t.Fatal("lost raw identity")
				}
				if r.Experimental != true || r.Maturity != "non-normative" || r.Capabilities.ExecutionAuthority || r.Capabilities.FullDocumentConformance {
					t.Fatal("claim posture")
				}
				r, code = Validate("validate", raw)
				if code != 0 || r.Status != "document-valid" || r.Document != "" {
					t.Fatalf("validate %+v code%d", r, code)
				}
				r, code = Check("check", raw, f.wire(t), engine(t))
				if code != 0 || r.Status != "references-valid" {
					t.Fatalf("check %+v code%d", r, code)
				}
				output := mustJSON(t, r)
				if bytes.Contains(output, f.raw["source.txt"]) || bytes.Contains(output, []byte(base64.StdEncoding.EncodeToString(f.raw["source.txt"]))) {
					t.Fatal("report leaks retained material")
				}
			}
		})
	}
}

func TestSourcePinAndVendoredHashes(t *testing.T) {
	raw := mustRead(t, "provenance.json")
	const expectedManifestDigest = "sha256:9f97eafac4ff8b2f355d65f44ee125fde04290ad199b872e9672807d3b8ef2d1"
	if digest(raw) != expectedManifestDigest {
		t.Fatal("vendor manifest changed; review the exact pinned source and update its independent test pin")
	}
	manifest := mustMap(t, raw)
	if manifest["sourceRevision"] != SpecRevision || manifest["profileVersion"] != ProfileVersion || manifest["license"] != "Apache-2.0" {
		t.Fatal("incorrect provenance")
	}
	expected := map[string]bool{"provenance.json": true}
	for _, item := range array(manifest["files"]) {
		entry := object(item)
		name := text(entry["path"])
		data := mustRead(t, name)
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != entry["sha256"] || len(data) != int(entry["byteLength"].(float64)) {
			t.Fatalf("vendor mismatch: %s", name)
		}
		expected[name] = true
	}
	if err := fs.WalkDir(prototype, "prototype", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !expected[strings.TrimPrefix(path, "prototype/")] {
			t.Errorf("unmanifested file: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaIsOfflineAndSelfContained(t *testing.T) {
	for _, kind := range []string{"policy", "governing-adoption"} {
		r, code := Schema("schema", kind, ProfileVersion)
		if code != 0 || r.Resource == nil {
			t.Fatalf("%+v %d", r, code)
		}
		compiled, err := validation.CompileSchema([]byte(r.Resource.Text), "urn:test:policy-schema:"+kind)
		if err != nil {
			t.Fatal(err)
		}
		f := loadFixture(t, "refunds")
		doc := f.policy
		if kind == "governing-adoption" {
			doc = f.adoption
		}
		if err := compiled.Validate(doc); err != nil {
			t.Fatal(err)
		}
		object(doc["prototype"])["proposalVersion"] = "0.0.1"
		if compiled.Validate(doc) == nil {
			t.Fatal("old selector accepted")
		}
	}
}

func TestInitIsUnsatisfiedStarterWithExactVersionGrammar(t *testing.T) {
	version := "0.0." + strings.Repeat("9", 252)
	if !artifactversion.Valid(version) {
		t.Fatal("boundary version invalid")
	}
	for _, kind := range []string{"policy", "governing-adoption"} {
		r, code := Init("init", kind, ProfileVersion, "urn:test:starter", version)
		if code != 0 || r.Status != "starter-template" || !r.StarterTemplate || len(r.RequiredMaterials) == 0 {
			t.Fatalf("%+v %d", r, code)
		}
		v, c := Validate("validate", []byte(r.Document))
		if c != 0 {
			t.Fatalf("starter shape: %+v %d", v, c)
		}
		checked, c := Check("check", []byte(r.Document), []byte(`{"materials":[]}`), engine(t))
		requireRefusal(t, checked, c, result.ExitInvalid)
	}
	for _, version := range []string{"01.0.0", "1.0.0-rc.1", "1.0.0\n", "1.0.0 ", "1.0", strings.Repeat("9", 257) + ".0.0"} {
		r, c := Init("init", "policy", ProfileVersion, "urn:test:starter", version)
		requireRefusal(t, r, c, result.ExitInvalid)
	}
	for _, id := range []string{"not a URI", "https://example.invalid/a b", "urn:test:%GG", "urn:test:é"} {
		r, c := Init("init", "policy", ProfileVersion, id, "1.0.0")
		requireRefusal(t, r, c, result.ExitInvalid)
	}
}

func TestStrictJSONAndCanonDomain(t *testing.T) {
	f := loadFixture(t, "refunds")
	good := string(mustJSON(t, f.policy))
	for _, raw := range []string{"\xef\xbb\xbf" + good, good + " {}", strings.Replace(good, `"kind":"policy"`, `"kind":"policy","kind":"policy"`, 1), strings.Replace(good, `"kind":"policy"`, `"kind":"\ud800"`, 1), strings.Replace(good, `"kind":"policy"`, `"kind":NaN`, 1)} {
		r, c := Canonicalize("canon", []byte(raw))
		requireRefusal(t, r, c, result.ExitInvalid)
	}
	for _, value := range []string{"1.0", "1e0", "9007199254740992", "-9007199254740992"} {
		raw := strings.Replace(good, `"byteLength":`, `"byteLength":`, 1)
		raw = strings.Replace(raw, `"byteLength":`, `"testNumber":`+value+`,"byteLength":`, 1)
		r, c := Canonicalize("canon", []byte(raw))
		requireRefusal(t, r, c, result.ExitInvalid)
	}
	r, c := Canonicalize("canon", []byte(strings.Repeat(" ", MaxDocumentBytes+1)))
	requireRefusal(t, r, c, result.ExitUnsupported)
	for _, version := range []string{"0.0.1", "0.0.3", "0.0.2 "} {
		object(f.policy["prototype"])["proposalVersion"] = version
		r, c := Validate("validate", mustJSON(t, f.policy))
		requireRefusal(t, r, c, result.ExitUnsupported)
	}
}

func TestClosedSchemaAndLocalReferences(t *testing.T) {
	mutations := []struct {
		name   string
		modify func(*fixture)
	}{
		{"live-authority", func(f *fixture) { f.policy["approved"] = true }},
		{"requirements", func(f *fixture) { f.policy["requirements"] = []any{} }},
		{"unknown-selector", func(f *fixture) { object(f.policy["prototype"])["profile"] = "live-policy" }},
		{"duplicate-section", func(f *fixture) {
			sections := array(f.policy["sections"])
			object(sections[1])["id"] = object(sections[0])["id"]
		}},
		{"unknown-source", func(f *fixture) {
			section := object(array(f.policy["sections"])[0])
			object(array(section["sourceRefs"])[0])["sourceId"] = "missing"
		}},
		{"anchor-order", func(f *fixture) {
			anchor := object(object(array(f.policy["sections"])[0])["anchor"])
			anchor["endByte"] = anchor["startByte"]
		}},
		{"provider-object-open", func(f *fixture) {
			source := object(array(f.policy["sourceProvenance"])[0])
			object(source["source"])["authority"] = true
		}},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			f := loadFixture(t, "refunds")
			tc.modify(f)
			r, c := Validate("validate", mustJSON(t, f.policy))
			if c == 0 || r.Status != "refused" {
				t.Fatalf("accepted %+v", r)
			}
		})
	}
	f := loadFixture(t, "refunds")
	secret := strings.Repeat("sensitive-provider-prose-", 4000)
	f.policy[secret] = secret
	r, c := Validate("validate", mustJSON(t, f.policy))
	if c == 0 {
		t.Fatal("unknown member accepted")
	}
	output := mustJSON(t, r)
	if len(output) > 8192 || bytes.Contains(output, []byte("sensitive-provider-prose")) {
		t.Fatal("diagnostic leaked rejected prose")
	}
}

func TestMaterialsClosedShapeAndBase64(t *testing.T) {
	f := loadFixture(t, "refunds")
	raw := mustJSON(t, f.policy)
	for _, wire := range []string{`{"materials":[],"path":"/etc/passwd"}`, `{"materials":[{"content":{},"dataBase64":null}]}`, `{"materials":null}`, `{"materials":[],"materials":[]}`, `{"materials":[{"path":"file","content":{},"dataBase64":""}]}`} {
		r, c := Check("check", raw, []byte(wire), engine(t))
		requireRefusal(t, r, c, result.ExitInvalid)
	}
	for _, encoded := range []any{nil, 42, "YQ", "YQ==\n", "YQ==\r\n", "YR==", "YQ--", "===="} {
		f := loadFixture(t, "refunds")
		f.entries[0].DataBase64 = encoded
		r, c := Check("check", raw, f.wire(t), engine(t))
		requireRefusal(t, r, c, result.ExitInvalid)
	}
	r, c := Check("check", raw, []byte(strings.Repeat(" ", MaxMaterialsWireBytes+1)), engine(t))
	requireRefusal(t, r, c, result.ExitUnsupported)
	f.entries = append(f.entries, make([]wireEntry, MaxMaterials-len(f.entries)+1)...)
	r, c = Check("check", raw, f.wire(t), engine(t))
	requireRefusal(t, r, c, result.ExitUnsupported)
}

func TestMissingAlteredAndTypedRetainedBytes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*fixture)
	}{
		{"missing", func(f *fixture) { f.entries = f.entries[1:] }},
		{"altered", func(f *fixture) {
			encoded := f.entries[0].DataBase64.(string)
			raw, _ := base64.StdEncoding.DecodeString(encoded)
			raw[0] ^= 1
			f.entries[0].DataBase64 = base64.StdEncoding.EncodeToString(raw)
		}},
		{"wrong-length", func(f *fixture) { f.entries[0].Content["byteLength"] = 1 }},
		{"unregistered-media", func(f *fixture) { f.entries[0].Content["mediaType"] = "text/markdown" }},
		{"changed-whitespace", func(f *fixture) { f.replace(t, "consumer-pack.json", append(f.raw["consumer-pack.json"], ' ')) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := loadFixture(t, "refunds")
			tc.modify(f)
			r, c := Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
			requireRefusal(t, r, c, result.ExitInvalid)
		})
	}
	f := loadFixture(t, "refunds")
	entry := f.entries[0]
	content := map[string]any{}
	for k, v := range entry.Content {
		content[k] = v
	}
	content["mediaType"] = "text/markdown"
	entry.Content = content
	f.entries = append(f.entries, entry)
	material, err, c := decodeMaterials(f.wire(t))
	if err != nil || c != 0 {
		t.Fatalf("typed registration %v %d", err, c)
	}
	content["byteLength"] = json.Number(fmt.Sprint(content["byteLength"]))
	raw, err, c := material.resolve(content, MaxMaterialBytes, "/test")
	if err != nil || c != 0 || !bytes.Equal(raw, f.raw["narrative.txt"]) {
		t.Fatal("same bytes/multiple media types not resolved")
	}
}

func TestUTF8AnchorsAndNarrative(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*fixture)
	}{
		{"digest", func(f *fixture) {
			anchor := object(object(array(f.policy["sections"])[0])["anchor"])
			anchor["bytesDigest"] = "sha256:" + strings.Repeat("0", 64)
		}},
		{"boundary", func(f *fixture) {
			raw := []byte("éx")
			f.replace(t, "narrative.txt", raw)
			f.policy["content"] = f.content(t, "narrative.txt")
			f.policy["sections"] = []any{map[string]any{"id": "split", "title": "split", "anchor": map[string]any{"startByte": 1, "endByte": 2, "bytesDigest": digest(raw[1:2])}}}
		}},
		{"invalid-utf8", func(f *fixture) {
			raw := []byte{0xff}
			f.replace(t, "narrative.txt", raw)
			f.policy["content"] = f.content(t, "narrative.txt")
			f.policy["sections"] = []any{map[string]any{"id": "bad", "title": "bad", "anchor": map[string]any{"startByte": 0, "endByte": 1, "bytesDigest": digest(raw)}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := loadFixture(t, "refunds")
			tc.modify(f)
			r, c := Check("check", mustJSON(t, f.policy), f.wire(t), engine(t))
			requireRefusal(t, r, c, result.ExitInvalid)
		})
	}
}

func TestAdoptionCoverageAndTypedTargets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*fixture)
	}{
		{"graph", func(f *fixture) { object(f.adoption["consumer"])["kind"] = "graph" }},
		{"unknown-canonical-profile", func(f *fixture) { object(object(f.adoption["consumer"])["canonicalIdentity"])["profile"] = "jcs" }},
		{"canonical-digest", func(f *fixture) {
			object(object(f.adoption["consumer"])["canonicalIdentity"])["canonicalDigest"] = "sha256:" + strings.Repeat("0", 64)
		}},
		{"identity", func(f *fixture) { object(f.adoption["consumer"])["version"] = "1.0.1" }},
		{"policy-repeat", func(f *fixture) {
			rows := array(f.adoption["governingPolicies"])
			f.adoption["governingPolicies"] = append(rows, rows[0])
		}},
		{"missing-coverage", func(f *fixture) {
			row := object(array(f.adoption["governingPolicies"])[0])
			row["coverage"] = array(row["coverage"])[1:]
		}},
		{"duplicate-coverage", func(f *fixture) {
			row := object(array(f.adoption["governingPolicies"])[0])
			coverage := array(row["coverage"])
			row["coverage"] = append(coverage, coverage[0])
		}},
		{"out-of-scope", func(f *fixture) {
			row := object(array(f.adoption["governingPolicies"])[0])
			object(array(row["coverage"])[0])["sectionId"] = "unknown"
		}},
		{"unknown-rule", func(f *fixture) {
			row := object(array(f.adoption["governingPolicies"])[0])
			coverage := object(array(row["coverage"])[0])
			coverage["consumerElements"] = []any{map[string]any{"kind": "rule", "id": "missing"}}
		}},
		{"kind-confusion", func(f *fixture) {
			row := object(array(f.adoption["governingPolicies"])[0])
			coverage := object(array(row["coverage"])[0])
			coverage["consumerElements"] = []any{map[string]any{"kind": "exception", "id": "eligible"}}
		}},
		{"singleton-id", func(f *fixture) {
			row := object(array(f.adoption["governingPolicies"])[0])
			coverage := object(array(row["coverage"])[0])
			coverage["consumerElements"] = []any{map[string]any{"kind": "fallback", "id": "fabricated"}}
		}},
		{"unsupported-kind", func(f *fixture) {
			row := object(array(f.adoption["governingPolicies"])[0])
			coverage := object(array(row["coverage"])[0])
			coverage["consumerElements"] = []any{map[string]any{"kind": "outcome", "id": "approve"}}
		}},
		{"duplicate-target", func(f *fixture) {
			row := object(array(f.adoption["governingPolicies"])[0])
			coverage := object(array(row["coverage"])[0])
			refs := array(coverage["consumerElements"])
			coverage["consumerElements"] = append(refs, refs[0])
		}},
		{"missing-review", func(f *fixture) {
			for i, n := range f.names {
				if n == "interpretation.txt" {
					f.entries = append(f.entries[:i], f.entries[i+1:]...)
					break
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := loadFixture(t, "refunds")
			tc.modify(f)
			r, c := Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
			if c == 0 || r.Status != "refused" {
				t.Fatalf("accepted %+v", r)
			}
		})
	}
}

func TestCoreRawNumericExtensionsAndOptionalCanonicalIdentity(t *testing.T) {
	for _, literal := range []string{"1.5", "1e0", "9007199254740992", "-0.0", "1e-308", "1e308"} {
		t.Run(literal, func(t *testing.T) {
			f := loadFixture(t, "refunds")
			pack := f.raw["consumer-pack.json"]
			pack = bytes.Replace(pack, []byte(`"specVersion":`), []byte(`"extensions":{"com.example.numeric":`+literal+`},"specVersion":`), 1)
			f.retainConsumer(t, pack)
			r, c := Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
			if c != 0 {
				t.Fatalf("Core raw domain rejected %+v %d", r, c)
			}
			object(f.adoption["consumer"])["canonicalIdentity"] = map[string]any{"profile": "jps-canon", "canonicalDigest": "sha256:" + strings.Repeat("0", 64)}
			r, c = Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
			requireRefusal(t, r, c, result.ExitUnsupported)
		})
	}
	for _, literal := range []string{"1e309", "1e-309", strings.Repeat("9", 129)} {
		f := loadFixture(t, "refunds")
		pack := bytes.Replace(f.raw["consumer-pack.json"], []byte(`"specVersion":`), []byte(`"extensions":{"com.example.numeric":`+literal+`},"specVersion":`), 1)
		f.retainConsumer(t, pack)
		r, c := Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
		requireRefusal(t, r, c, result.ExitUnsupported)
	}
}

func TestCoreScopedReferenceChecks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(map[string]any)
	}{
		{"rule-id-duplicate", func(p map[string]any) { rows := array(p["rules"]); p["rules"] = append(rows, rows[0]) }},
		{"exception-id-duplicate", func(p map[string]any) { rows := array(p["exceptions"]); p["exceptions"] = append(rows, rows[0]) }},
		{"outcome-id-duplicate", func(p map[string]any) { rows := array(p["outcomes"]); p["outcomes"] = append(rows, rows[0]) }},
		{"fallback-unknown", func(p map[string]any) { p["fallbackOutcome"] = "unknown" }},
		{"exception-target-unknown", func(p map[string]any) {
			row := object(array(p["exceptions"])[0])
			row["effect"] = "suppress-rule"
			delete(row, "outcome")
			row["targetRule"] = "unknown"
		}},
		{"exception-outcome-unknown", func(p map[string]any) {
			row := object(array(p["exceptions"])[0])
			row["effect"] = "force-outcome"
			delete(row, "targetRule")
			row["outcome"] = "unknown"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := loadFixture(t, "refunds")
			p := mustMap(t, f.raw["consumer-pack.json"])
			tc.modify(p)
			f.retainConsumer(t, mustJSON(t, p))
			r, c := Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
			requireRefusal(t, r, c, result.ExitInvalid)
		})
	}
	// Unrelated full Core semantic checks are intentionally outside this operation.
	f := loadFixture(t, "refunds")
	p := mustMap(t, f.raw["consumer-pack.json"])
	object(array(p["rules"])[0])["outcome"] = "not-a-declared-outcome"
	f.retainConsumer(t, mustJSON(t, p))
	r, c := Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
	if c != 0 {
		t.Fatalf("added unadvertised full semantic gate: %+v %d", r, c)
	}
}

func TestOriginalAndMaterialRoleBounds(t *testing.T) {
	f := loadFixture(t, "refunds")
	raw := bytes.Repeat([]byte{'x'}, MaxOriginalBytes)
	f.replace(t, "source.txt", raw)
	source := object(array(f.policy["sourceProvenance"])[0])
	source["content"] = f.content(t, "source.txt")
	r, c := Check("check", mustJSON(t, f.policy), f.wire(t), engine(t))
	if c != 0 {
		t.Fatalf("4MiB original rejected %+v %d", r, c)
	}
	source["content"] = map[string]any{"bytesDigest": digest(append(raw, 'x')), "byteLength": MaxOriginalBytes + 1, "mediaType": "application/pdf"}
	r, c = Check("check", mustJSON(t, f.policy), f.wire(t), engine(t))
	requireRefusal(t, r, c, result.ExitUnsupported)
	materials := materialSet{"test": &retained{raw: bytes.Repeat([]byte{'x'}, MaxMaterialBytes+1), mediaTypes: map[string]bool{"text/plain": true}}}
	ref := map[string]any{"bytesDigest": "test", "byteLength": json.Number(strconvInt(MaxMaterialBytes + 1)), "mediaType": "text/plain"}
	_, err, c := materials.resolve(ref, MaxMaterialBytes, "/reviewBasis")
	if err == nil || c != result.ExitUnsupported {
		t.Fatal("role bound omitted")
	}
}
func strconvInt(n int) string { return fmt.Sprint(n) }

func TestAggregateDistinctBytesAndRegistrationBounds(t *testing.T) {
	entries := []wireEntry{}
	for i := 0; i < 4; i++ {
		raw := bytes.Repeat([]byte{byte('a' + i)}, MaxOriginalBytes)
		entries = append(entries, wireEntry{Content: map[string]any{"bytesDigest": digest(raw), "byteLength": len(raw), "mediaType": "application/octet-stream"}, DataBase64: base64.StdEncoding.EncodeToString(raw)})
	}
	wire := func() []byte { return mustJSON(t, map[string]any{"materials": entries}) }
	material, err, c := decodeMaterials(wire())
	if err != nil || c != 0 || len(material) != 4 {
		t.Fatalf("exact aggregate bound: %v %d", err, c)
	}
	// A second type registration of the same 4MiB blob counts its bytes once.
	one := entries[0]
	one.Content = map[string]any{"bytesDigest": one.Content["bytesDigest"], "byteLength": MaxOriginalBytes, "mediaType": "application/pdf"}
	same := mustJSON(t, map[string]any{"materials": []wireEntry{entries[0], one}})
	material, err, c = decodeMaterials(same)
	if err != nil || c != 0 || len(material) != 1 || len(material[text(one.Content["bytesDigest"])].mediaTypes) != 2 {
		t.Fatalf("duplicate bytes counted: %v %d", err, c)
	}
	entries = append(entries, wireEntry{Content: map[string]any{"bytesDigest": digest([]byte("z")), "byteLength": 1, "mediaType": "text/plain"}, DataBase64: "eg=="})
	_, err, c = decodeMaterials(wire())
	if err == nil || c != result.ExitUnsupported {
		t.Fatal("aggregate+1 accepted")
	}
}

func TestParsedDocumentAndDepthBoundaries(t *testing.T) {
	f := loadFixture(t, "refunds")
	raw := mustJSON(t, f.policy)
	exact := append(append([]byte(nil), raw...), bytes.Repeat([]byte{' '}, MaxDocumentBytes-len(raw))...)
	r, c := Canonicalize("canon", exact)
	if c != 0 || r.BytesDigest != digest(exact) {
		t.Fatalf("exact parsed byte bound %+v %d", r, c)
	}
	r, c = Canonicalize("canon", append(exact, ' '))
	requireRefusal(t, r, c, result.ExitUnsupported)
	prefix := strings.TrimSuffix(string(raw), "}") + `,"nested":`
	for _, depth := range []int{31, 32} {
		input := prefix + strings.Repeat("[", depth) + "null" + strings.Repeat("]", depth) + "}"
		r, c := Canonicalize("canon", []byte(input))
		if depth == 31 {
			if c != 0 {
				t.Fatalf("32 containers refused %+v %d", r, c)
			}
		} else {
			requireRefusal(t, r, c, result.ExitUnsupported)
		}
	}
	f.retainConsumer(t, append(append([]byte(nil), f.raw["consumer-pack.json"]...), bytes.Repeat([]byte{' '}, MaxDocumentBytes+1-len(f.raw["consumer-pack.json"]))...))
	r, c = Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
	requireRefusal(t, r, c, result.ExitUnsupported)
}

func TestNarrativeByteBoundaries(t *testing.T) {
	f := loadFixture(t, "refunds")
	raw := bytes.Repeat([]byte{'x'}, MaxMaterialBytes)
	f.replace(t, "narrative.txt", raw)
	f.policy["content"] = f.content(t, "narrative.txt")
	f.policy["sourceProvenance"] = []any{}
	f.policy["sections"] = []any{map[string]any{"id": "whole", "title": "whole", "anchor": map[string]any{"startByte": 0, "endByte": len(raw), "bytesDigest": digest(raw)}}}
	r, c := Check("check", mustJSON(t, f.policy), f.wire(t), engine(t))
	if c != 0 {
		t.Fatalf("exact 1MiB narrative rejected %+v %d", r, c)
	}
	f.replace(t, "narrative.txt", append(raw, 'x'))
	f.policy["content"] = f.content(t, "narrative.txt")
	r, c = Check("check", mustJSON(t, f.policy), f.wire(t), engine(t))
	requireRefusal(t, r, c, result.ExitUnsupported)
}

func TestScopedCoverageAndSingletonPresence(t *testing.T) {
	f := loadFixture(t, "procurement")
	governing := object(array(f.adoption["governingPolicies"])[0])
	scope := object(governing["scope"])
	scope["sectionIds"] = append(array(scope["sectionIds"]), "unknown")
	r, c := Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
	requireRefusal(t, r, c, result.ExitInvalid)
	for _, kindName := range []string{"applicability", "fallback"} {
		t.Run(kindName, func(t *testing.T) {
			f := loadFixture(t, "refunds")
			pack := mustMap(t, f.raw["consumer-pack.json"])
			member := "applicability"
			if kindName == "fallback" {
				member = "fallbackOutcome"
			}
			delete(pack, member)
			f.retainConsumer(t, mustJSON(t, pack))
			governing := object(array(f.adoption["governingPolicies"])[0])
			coverage := object(array(governing["coverage"])[0])
			coverage["consumerElements"] = []any{map[string]any{"kind": kindName}}
			r, c := Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
			requireRefusal(t, r, c, result.ExitInvalid)
		})
	}
}

func TestMaterialLengthCannotCoerceCanonicalNumbers(t *testing.T) {
	f := loadFixture(t, "refunds")
	for _, literal := range []string{"1e0", "1.0"} {
		reference := `{"bytesDigest":"` + digest(nil) + `","byteLength":` + literal + `,"mediaType":"application/octet-stream"}`
		materials := []byte(`{"materials":[{"content":` + reference + `,"dataBase64":""}]}`)
		r, c := Check("check", mustJSON(t, f.policy), materials, engine(t))
		requireRefusal(t, r, c, result.ExitInvalid)
		if r.Diagnostics[0].Code != "JPS-POLICY-BYTE-LENGTH" {
			t.Fatalf("length coercion was not rejected before decoding: %+v", r.Diagnostics)
		}
	}
}

func TestPreSchemaFailuresDoNotEchoUnknownKeys(t *testing.T) {
	f := loadFixture(t, "refunds")
	secret := "private-source-prose-" + strings.Repeat("secret", 1000)
	// Force a duplicate unknown member so the carrier, before schema checking,
	// would ordinarily report its attacker-controlled JSON pointer.
	key := string(mustJSON(t, secret))
	wire := []byte(`{"materials":[],` + key + `:1,` + key + `:2}`)
	r, c := Check("check", mustJSON(t, f.policy), wire, engine(t))
	requireRefusal(t, r, c, result.ExitInvalid)
	if bytes.Contains(mustJSON(t, r), []byte("private-source-prose")) {
		t.Fatal("carrier reported rejected private keys")
	}
	doc := string(mustJSON(t, f.policy))
	doc = strings.TrimSuffix(doc, "}") + `,"` + secret + `":1.5}`
	r, c = Canonicalize("canon", []byte(doc))
	requireRefusal(t, r, c, result.ExitInvalid)
	if bytes.Contains(mustJSON(t, r), []byte("private-source-prose")) {
		t.Fatal("canonical failure reported rejected private keys")
	}
}

func TestUnsupportedReferenceSelectors(t *testing.T) {
	for _, tc := range []struct {
		path   string
		modify func(*fixture)
	}{
		{"/consumer/kind", func(f *fixture) { object(f.adoption["consumer"])["kind"] = "graph" }},
		{"/consumer/canonicalIdentity/profile", func(f *fixture) { object(object(f.adoption["consumer"])["canonicalIdentity"])["profile"] = "jcs" }},
		{"/governingPolicies/0/policy/kind", func(f *fixture) {
			object(object(array(f.adoption["governingPolicies"])[0])["policy"])["kind"] = "policy-v2"
		}},
	} {
		f := loadFixture(t, "refunds")
		tc.modify(f)
		r, c := Validate("validate", mustJSON(t, f.adoption))
		requireRefusal(t, r, c, result.ExitUnsupported)
		if r.Diagnostics[0].InstancePath != tc.path {
			t.Fatalf("path: %+v", r.Diagnostics)
		}
	}
}

func TestMultiplePoliciesAndSameIdentityVersions(t *testing.T) {
	f := loadFixture(t, "refunds")
	first := object(array(f.adoption["governingPolicies"])[0])
	second := mustMap(t, mustJSON(t, first))
	secondPolicy := mustMap(t, mustJSON(t, f.policy))
	secondPolicy["id"] = text(secondPolicy["id"]) + "-second"
	raw := mustJSON(t, secondPolicy)
	reference := object(second["policy"])
	reference["id"] = secondPolicy["id"]
	reference["content"] = map[string]any{"bytesDigest": digest(raw), "byteLength": len(raw), "mediaType": "application/json"}
	delete(reference, "canonicalIdentity")
	f.entries = append(f.entries, wireEntry{Content: object(reference["content"]), DataBase64: base64.StdEncoding.EncodeToString(raw)})
	f.adoption["governingPolicies"] = []any{first, second}
	r, c := Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
	if c != 0 || r.Status != "references-valid" {
		t.Fatalf("multiple governing policies refused: %+v %d", r, c)
	}
	reference["id"] = object(first["policy"])["id"]
	reference["version"] = "2.0.0"
	r, c = Validate("validate", mustJSON(t, f.adoption))
	requireRefusal(t, r, c, result.ExitInvalid)
	if r.Diagnostics[0].Code != "JPS-POLICY-DUPLICATE-POLICY" {
		t.Fatalf("same identity/different versions bypassed local refusal %+v", r)
	}
}
