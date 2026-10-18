package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func candidateRead(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := candidate.ReadFile("candidate/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func candidateFixture(t *testing.T, domain string) *fixture {
	t.Helper()
	base := "examples/" + domain + "/"
	manifest := mustMap(t, candidateRead(t, base+"fixture.json"))
	f := &fixture{policy: mustMap(t, candidateRead(t, base+"policy.json")), adoption: mustMap(t, candidateRead(t, base+"adoption.json")), raw: map[string][]byte{}}
	for _, item := range array(manifest["materials"]) {
		entry := object(item)
		name := text(entry["path"])
		raw := candidateRead(t, base+name)
		f.raw[name] = raw
		f.names = append(f.names, name)
		f.entries = append(f.entries, wireEntry{Content: object(entry["content"]), DataBase64: encodeMaterial(raw)})
	}
	return f
}

func vectorBytes(source map[string]any) ([]byte, *issue, int) {
	if name, ok := source["path"].(string); ok {
		if strings.ContainsRune(name, 0) || path.IsAbs(name) || path.Clean(name) != name || strings.HasPrefix(name, "../") {
			return nil, candidateIssue("invalid-carrier"), result.ExitInvalid
		}
		raw, err := candidate.ReadFile("candidate/" + name)
		if err != nil {
			return nil, candidateIssue("missing-material"), result.ExitInvalid
		}
		return raw, nil, 0
	}
	return bytes.Repeat([]byte{byte(number(source["repeatByte"]))}, number(source["byteLength"])), nil, 0
}

func TestCandidatePortableVectors(t *testing.T) {
	value, failure := carrier.Decode(candidateRead(t, "vectors/manifest.json"), limits())
	if failure != nil {
		t.Fatal(failure)
	}
	manifest := object(value)
	cases := array(manifest["cases"])
	if len(cases) != 124 {
		t.Fatalf("unexpected vector count %d", len(cases))
	}
	for _, value := range cases {
		entry := object(value)
		t.Run(text(entry["id"]), func(t *testing.T) {
			raw, failure, code := vectorBytes(object(entry["document"]))
			if failure != nil {
				t.Fatalf("root carrier failed: %v %d", failure, code)
			}

			r, doc, exit := parseFormat("vectors", raw, CandidateVersion)
			if exit == 0 {
				r, exit = checkMaterials(r, doc, func() (materialSet, *issue, int) {
					registrations := array(entry["materials"])
					if len(registrations) > MaxMaterials {
						return nil, candidateIssue("input-over-bound"), result.ExitUnsupported
					}
					materials := materialSet{}
					for _, value := range registrations {
						registration := object(value)
						retained, failure, code := vectorBytes(object(registration["source"]))
						if failure != nil {
							return nil, failure, code
						}
						if err, code := registerCandidateMaterial(object(registration["content"]), retained, materials); err != nil {
							return nil, err, code
						}
					}
					return materials, nil, 0
				}, engine(t))
			} else {
				r, exit = finishCandidateCheck(r, exit)
			}

			actual := mustMap(t, mustJSON(t, r.CandidateResult))
			if !reflect.DeepEqual(actual, object(entry["expected"])) {
				t.Fatalf("result %v want %v; exit %d diagnostics %v", actual, entry["expected"], exit, r.Diagnostics)
			}
			if (exit == 0) != (actual["status"] == "valid") {
				t.Fatal("exit/result disagreement")
			}
		})
	}
}

func TestCandidateProvenance(t *testing.T) {
	raw := candidateRead(t, "provenance.json")
	if digest(raw) != "sha256:f21466b49e2eb95c33f6e97bef74203a2e2255737476be27e72e3c43fc01f971" {
		t.Fatal("candidate manifest pin changed")
	}
	manifest := mustMap(t, raw)
	if manifest["sourceRevision"] != CandidateRevision || manifest["formatVersion"] != CandidateVersion || manifest["license"] != "Apache-2.0" || manifest["maturity"] != "proposed-draft" {
		t.Fatal("incorrect candidate provenance")
	}
	expected := map[string]bool{"provenance.json": true}
	for _, value := range array(manifest["files"]) {
		entry := object(value)
		name := text(entry["path"])
		raw := candidateRead(t, name)
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != entry["sha256"] || len(raw) != int(entry["byteLength"].(float64)) {
			t.Fatalf("candidate vendor mismatch: %s", name)
		}
		expected[name] = true
	}
	if err := fs.WalkDir(candidate, "candidate", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !expected[strings.TrimPrefix(name, "candidate/")] {
			t.Errorf("unmanifested candidate file %s", name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCandidateRawIdentityAndFormatIsolation(t *testing.T) {
	f := candidateFixture(t, "refunds")
	raw := f.raw["policy.json"]
	r, exit := Canonicalize("canon", raw)
	if exit != 0 || r.BytesDigest != digest(raw) || r.CanonicalDigest == r.BytesDigest || r.FormatVersion != CandidateVersion || r.SpecRevision != CandidateRevision || r.CandidateResult != nil {
		t.Fatalf("candidate identity %+v exit %d", r, exit)
	}
	if r.Document == "" {
		t.Fatal("canonical bytes missing")
	}
	prototypePolicy := loadFixture(t, "refunds").raw["policy.json"]
	f.replace(t, "policy.json", prototypePolicy)
	reference := object(object(array(f.adoption["governingPolicies"])[0])["policy"])
	reference["content"] = f.content(t, "policy.json")
	reference["id"], reference["version"] = mustMap(t, prototypePolicy)["id"], mustMap(t, prototypePolicy)["version"]
	delete(reference, "canonicalIdentity")
	r, exit = Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
	if exit != result.ExitInvalid || r.CandidateResult == nil || r.CandidateResult.Code != "invalid-selector" {
		t.Fatalf("prototype retained Policy accepted: %+v exit %d", r.CandidateResult, exit)
	}
}

func TestCandidateInitSchemaAndExplicitDiscovery(t *testing.T) {
	legacyJSON := mustMap(t, mustJSON(t, Contract("contract")))
	for _, key := range []string{"formatVersion", "candidateResult", "candidateCapabilities", "registryVersion"} {
		if _, found := legacyJSON[key]; found {
			t.Fatal("legacy contract extended", key)
		}
	}
	registry := AllFormats("contract")
	if len(registry.Formats) != 2 || !bytes.Equal(mustJSON(t, registry.Formats[0]), mustJSON(t, Contract("contract"))) {
		t.Fatal("registry changed legacy entry")
	}
	for _, kind := range []string{"policy", "governing-adoption"} {
		r, exit := Init("init", kind, CandidateVersion, "urn:test:candidate", "7.3.42")
		if exit != 0 || r.FormatVersion != CandidateVersion || !r.StarterTemplate {
			t.Fatalf("init %+v %d", r, exit)
		}
		if v, exit := Validate("validate", []byte(r.Document)); exit != 0 {
			t.Fatalf("starter shape %+v %d", v, exit)
		}
		if kind == "policy" && mustMap(t, []byte(r.Document))["managementMode"] != "workspace-managed" {
			t.Fatal("old management spelling")
		}
		schema, exit := Schema("schema", kind, CandidateVersion)
		if exit != 0 || schema.Resource == nil || strings.Contains(schema.Resource.Text, "8a56a95") {
			t.Fatal("candidate schema provenance")
		}
		compiled, err := validation.CompileSchema([]byte(schema.Resource.Text), "urn:test:candidate:"+kind)
		if err != nil {
			t.Fatal(err)
		}
		if err := compiled.Validate(mustMap(t, []byte(r.Document))); err != nil {
			t.Fatal(err)
		}
	}
	var capabilities map[string]any
	if json.Unmarshal(registry.Formats[1].CandidateCapabilities, &capabilities) != nil || capabilities["maturity"] != "proposed-draft" {
		t.Fatal("candidate discovery")
	}
}

func encodeMaterial(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

func TestCandidateUnicodeTextAndPrivateTransportBounds(t *testing.T) {
	f := candidateFixture(t, "refunds")
	f.policy["title"] = strings.Repeat("é", 4096)
	if r, exit := Validate("validate", mustJSON(t, f.policy)); exit != 0 {
		t.Fatalf("Unicode character maximum refused %+v %d", r, exit)
	}
	f.policy["title"] = strings.Repeat("é", 4097)
	if r, exit := Validate("validate", mustJSON(t, f.policy)); exit != result.ExitUnsupported || r.refusalCode != "input-over-bound" {
		t.Fatalf("Unicode character maximum accepted %+v %d", r, exit)
	}
	r, exit := Check("check", f.raw["policy.json"], bytes.Repeat([]byte{' '}, MaxMaterialsWireBytes+1), engine(t))
	if exit != result.ExitUnsupported || r.CandidateResult == nil || r.CandidateResult.Code != "input-over-bound" || r.Capabilities.MaxMaterialsWireBytes != MaxMaterialsWireBytes {
		t.Fatalf("private wire limit changed %+v %d", r, exit)
	}
}

func TestCandidateEnvironmentFailureAndSafeRefusal(t *testing.T) {
	f := candidateFixture(t, "refunds")
	r, exit := Check("check", f.raw["adoption.json"], f.wire(t), nil)
	if exit != result.ExitIO || r.CandidateResult != nil {
		t.Fatalf("environment failure became candidate result %+v %d", r, exit)
	}
	f.policy["kind"] = strings.Repeat("private-provider-prose", 4000)
	r, exit = Check("check", mustJSON(t, f.policy), f.wire(t), engine(t))
	if exit != result.ExitInvalid || r.CandidateResult == nil || r.CandidateResult.Code != "invalid-document" || bytes.Contains(mustJSON(t, r), []byte("private-provider-prose")) {
		t.Fatalf("unknown kind refusal leaked input or used wrong code %d", exit)
	}
}

func TestCandidateAdoptionValidatesRetainedPolicyGrammar(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		code   string
	}{
		{"unknown-approval", func(doc map[string]any) { doc["approved"] = true }, "invalid-document"},
		{"prototype-management-alias", func(doc map[string]any) { doc["managementMode"] = "desk-managed" }, "invalid-document"},
		{"missing-sections", func(doc map[string]any) { delete(doc, "sections") }, "invalid-document"},
		{"wrong-kind", func(doc map[string]any) { doc["kind"] = "governing-adoption" }, "invalid-document"},
		{"section-bound", func(doc map[string]any) { doc["sections"] = make([]any, 65) }, "input-over-bound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := candidateFixture(t, "refunds")
			tc.change(f.policy)
			f.retainPolicy(t)
			r, exit := Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
			if exit == 0 || r.CandidateResult == nil || r.CandidateResult.Code != tc.code {
				t.Fatalf("retained Policy grammar bypassed %+v %d", r.CandidateResult, exit)
			}
		})
	}
}
