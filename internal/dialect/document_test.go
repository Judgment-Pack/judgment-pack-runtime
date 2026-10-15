package dialect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func TestCanonicalDialectEncoding(t *testing.T) {
	raw := []byte("{\"version\":2,\"𐀀\":-0,\"\ue000\":\"<>&\u2028\u2029\\u0001\\b\\f\\n\\r\\t\\\"\\\\\",\"case\":{\"facts\":[],\"evidence\":[]},\"items\":[null,false,0,\"0\",9007199254740991,-9007199254740991]}")
	expected := "{\"case\":{\"evidence\":[],\"facts\":[]},\"items\":[null,false,0,\"0\",9007199254740991,-9007199254740991],\"version\":2,\"\ue000\":\"<>&\u2028\u2029\\u0001\\b\\f\\n\\r\\t\\\"\\\\\",\"𐀀\":0}"
	r, code := Encode("test", raw)
	if code != 0 || r.Document != expected {
		t.Fatalf("exit=%d output=%q expected=%q diagnostics=%v", code, r.Document, expected, r.Diagnostics)
	}
	digest := sha256.Sum256([]byte(expected))
	if r.Digest != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatal(r.Digest)
	}
	if r.FullDocumentConformance || r.Status != "encoded" {
		t.Fatal("encoding claimed document validity", r)
	}
	again, code := Encode("test", []byte(r.Document))
	if code != 0 || again.Document != r.Document || again.Digest != r.Digest {
		t.Fatal("canonicalization is not idempotent")
	}
}
func TestCarrierAndCanonicalDomainRefusals(t *testing.T) {
	cases := []struct {
		name, raw string
		exit      int
		path      string
	}{
		{"duplicate", `{"version":2,"case":{"x":0,"x":1}}`, 1, "/case/x"},
		{"decoded duplicate", `{"version":2,"\u0076ersion":2}`, 1, "/version"},
		{"surrogate", `{"version":2,"x":"\ud800"}`, 1, ""},
		{"surrogate member", `{"version":2,"\udfff":0}`, 1, ""},
		{"utf8", "{\"version\":2,\"x\":\"\xff\"}", 1, ""},
		{"trailing", `{"version":2} {}`, 1, ""},
		{"fraction", `{"version":2,"x":1.0}`, 1, "/x"},
		{"exponent", `{"version":2,"x":1e2}`, 1, "/x"},
		{"negative exponent", `{"version":2,"x":1e-1}`, 1, "/x"},
		{"unsafe", `{"version":2,"x":9007199254740992}`, 1, "/x"},
		{"unsafe negative", `{"version":2,"x":-9007199254740992}`, 1, "/x"},
		{"huge", `{"version":2,"x":` + strings.Repeat("9", 129) + `}`, 1, "/x"},
		{"escaped pointer", `{"version":2,"a/~":[0,1.1]}`, 1, "/a~1~0/1"},
		{"array", `[]`, 1, ""}, {"null", `null`, 1, ""},
		{"absent version", `{}`, 1, "/version"}, {"string version", `{"version":"2"}`, 1, "/version"},
		{"unsupported", `{"version":4}`, 2, "/version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, code := Encode("test", []byte(tc.raw))
			if code != tc.exit || r.Status != "refused" || r.Document != "" || r.Digest != "" || len(r.Diagnostics) != 1 || r.Diagnostics[0].InstancePath != tc.path {
				t.Fatalf("exit=%d %#v", code, r)
			}
		})
	}
}
func TestDocumentBounds(t *testing.T) {
	prefix := `{"version":2,"x":"`
	suffix := `"}`
	raw := prefix + strings.Repeat("a", MaxDocumentBytes-len(prefix)-len(suffix)) + suffix
	if _, code := Encode("test", []byte(raw)); code != 0 {
		t.Fatal("exact byte bound refused")
	}
	if r, code := Encode("test", []byte(raw+" ")); code != result.ExitInvalid || r.Diagnostics[0].Code != "JPS-DIALECT-BYTE-LIMIT" {
		t.Fatal(r, code)
	}
	for _, depth := range []int{63, 64} {
		raw = `{"version":2,"x":` + strings.Repeat("[", depth) + `0` + strings.Repeat("]", depth) + `}`
		_, code := Encode("test", []byte(raw))
		if (code == 0) != (depth == 63) {
			t.Fatalf("depth %d exit %d", depth, code)
		}
	}
}
func TestMembersAndArrayOrderRemainPartOfIdentity(t *testing.T) {
	a, _ := Encode("test", []byte(`{"version":2,"case":{},"items":[false,null,0]}`))
	b, _ := Encode("test", []byte(`{"version":2,"case":{},"items":[false,null,0],"sources":[]}`))
	c, _ := Encode("test", []byte(`{"version":2,"case":{},"items":[null,false,0]}`))
	if a.Digest == b.Digest || a.Digest == c.Digest {
		t.Fatal("members or order silently normalized")
	}
	// This encoder does not pass an unknown member or malformed source as valid.
	r, code := Encode("test", []byte(`{"version":3,"unexpected":true,"sources":"not an array"}`))
	if code != 0 || r.FullDocumentConformance || r.Status != "encoded" {
		t.Fatal("must distinguish encoding from schema validation", r)
	}
}
func TestPinnedIndependentMappingDigests(t *testing.T) {
	var pin struct {
		Revision string
		Files    map[string]struct{ Upstream, SHA256 string }
	}
	raw, err := os.ReadFile("testdata/provenance.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &pin); err != nil || pin.Revision != SpecRevision {
		t.Fatal("wrong reference pin", err)
	}
	checked := 0
	for name, entry := range pin.Files {
		raw, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(raw)
		if hex.EncodeToString(hash[:]) != entry.SHA256 {
			t.Fatalf("reference bytes changed: %s", name)
		}
		if !strings.HasSuffix(name, "/mapping.json") {
			continue
		}
		preparation, err := os.ReadFile(filepath.Join("testdata", filepath.Dir(name), "preparation.json"))
		if err != nil {
			t.Fatal(err)
		}
		var expected struct {
			MappingDigest string `json:"mappingDigest"`
		}
		if err = json.Unmarshal(preparation, &expected); err != nil {
			t.Fatal(err)
		}
		r, code := Encode("test", raw)
		if code != 0 || r.Digest != expected.MappingDigest {
			t.Fatalf("%s: exit=%d digest=%s expected=%s diagnostics=%v", name, code, r.Digest, expected.MappingDigest, r.Diagnostics)
		}
		checked++
	}
	if checked != 10 {
		t.Fatalf("expected ten independent mappings, got %d", checked)
	}
}
func TestInitAndCapabilities(t *testing.T) {
	for _, version := range []int{2, 3} {
		r, code := Init("test", version)
		if code != 0 || r.DialectVersion != version || r.Digest == "" {
			t.Fatal(r, code)
		}
	}
	if _, code := Init("test", 4); code != result.ExitUnsupported {
		t.Fatal(code)
	}
	c := Contract("test")
	if len(c.Capabilities.Operations) != 4 || c.Capabilities.StructuralValidation || c.Capabilities.SemanticValidation || c.Capabilities.PreparationVerification || c.Capabilities.PackCompatibility {
		t.Fatal(c)
	}
}
