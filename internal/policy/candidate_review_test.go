package policy

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestCandidateURIFormatAgreementAndInitSchemaParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/candidate-uri-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		URI   string `json:"uri"`
		Valid bool   `json:"valid"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	schema, exit := Schema("schema", "policy", CandidateVersion)
	if exit != 0 {
		t.Fatal(schema, exit)
	}
	compiled, err := compileCandidateSchemas(map[string][]byte{"urn:test:candidate-editing-schema": []byte(schema.Resource.Text)}, "urn:test:candidate-editing-schema")
	if err != nil {
		t.Fatal(err)
	}
	f := candidateFixture(t, "refunds")
	for _, tc := range cases {
		t.Run(tc.URI, func(t *testing.T) {
			f.policy["id"] = tc.URI
			r, exit := Check("check", mustJSON(t, f.policy), f.wire(t), engine(t))
			if (exit == 0) != tc.Valid || !tc.Valid && r.CandidateResult.Code != "invalid-document" {
				t.Fatalf("URI result %+v exit %d expected valid %v", r.CandidateResult, exit, tc.Valid)
			}
			if (compiled.Validate(f.policy) == nil) != tc.Valid {
				t.Fatal("editing schema URI assertion differs")
			}
			if init, exit := Init("init", "policy", CandidateVersion, tc.URI, "7.3.42"); (exit == 0) != tc.Valid {
				t.Fatalf("init URI assertion differs %+v exit %d", init, exit)
			}
		})
	}
	// Candidate compilation must not replace the historical URI assertion used
	// by Core/prototype consumers, even within the same process.
	if !validURI("urn:test:[x]") {
		t.Fatal("prototype URI behavior changed")
	}
	legacy := loadFixture(t, "refunds")
	legacy.policy["id"] = "urn:test:[x]"
	if report, exit := Validate("legacy", mustJSON(t, legacy.policy)); exit != 0 {
		t.Fatalf("prototype schema URI behavior changed %+v %d", report, exit)
	}
	consumer := mustMap(t, legacy.raw["consumer-pack.json"])
	consumer["id"] = "urn:test:[x]"
	if report, failure := engine(t).Validate(mustJSON(t, consumer), validation.Options{Through: "structural", Limits: limits()}); failure != nil || report.Status != "valid" {
		t.Fatalf("Core URI behavior changed %+v %v", report, failure)
	}
}

func TestCandidateURIAssertionsCoverArtifactReferences(t *testing.T) {
	for _, member := range []string{"id", "consumer", "policy"} {
		f := candidateFixture(t, "refunds")
		switch member {
		case "id":
			f.adoption["id"] = "urn:test:[x]"
		case "consumer":
			object(f.adoption["consumer"])["id"] = "urn:test:[x]"
		case "policy":
			object(object(array(f.adoption["governingPolicies"])[0])["policy"])["id"] = "urn:test:[x]"
		}
		r, exit := Validate("validate", mustJSON(t, f.adoption))
		if exit != result.ExitInvalid || r.refusalCode != "invalid-document" {
			t.Fatalf("URI member %s bypassed assertion %+v exit %d", member, r, exit)
		}
	}
}

func TestCandidateConsumerURIUsesSameAssertionAndPreservesRawBytes(t *testing.T) {
	raw, err := os.ReadFile("testdata/candidate-uri-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		URI   string `json:"uri"`
		Valid bool   `json:"valid"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.URI, func(t *testing.T) {
			f := candidateFixture(t, "refunds")
			reference := object(f.adoption["consumer"])
			pack := bytes.Replace(f.raw["consumer-pack.json"], mustJSON(t, reference["id"]), mustJSON(t, tc.URI), 1)
			// An optional Core number outside jps-canon proves that checking the
			// candidate URI neither reserializes the Pack nor narrows its numbers.
			pack = bytes.Replace(pack, []byte(`"specVersion":`), []byte(`"extensions":{"com.example.uri":1.0},"specVersion":`), 1)
			reference["id"] = tc.URI
			f.retainConsumer(t, pack)
			r, exit := Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
			if (exit == 0) != tc.Valid || !tc.Valid && r.CandidateResult.Code != "invalid-document" {
				t.Fatalf("consumer URI result %+v exit %d expected valid %v", r.CandidateResult, exit, tc.Valid)
			}
			if !bytes.Equal(f.raw["consumer-pack.json"], pack) || text(object(reference["content"])["bytesDigest"]) != digest(pack) {
				t.Fatal("consumer raw identity changed")
			}
		})
	}
	// Overrides belong to one candidate check, not the caller's shared engine.
	f := candidateFixture(t, "refunds")
	reference := object(f.adoption["consumer"])
	pack := bytes.Replace(f.raw["consumer-pack.json"], mustJSON(t, reference["id"]), mustJSON(t, "https://%65xample.invalid/a"), 1)
	if report, failure := engine(t).Validate(pack, validation.Options{Through: "structural", Limits: limits()}); failure != nil || report.Status != "invalid" {
		t.Fatalf("default Core URI behavior changed %+v %v", report, failure)
	}
}

func TestCandidatePrivateWireRegistrationPhaseOrdering(t *testing.T) {
	f := candidateFixture(t, "refunds")
	validEntry := f.entries[0]
	sixteen := make([]wireEntry, 16)
	for i := range sixteen {
		sixteen[i] = validEntry
	}
	badDigest := wireEntry{Content: map[string]any{"bytesDigest": "sha256:" + strings.Repeat("0", 64), "byteLength": 1, "mediaType": "text/plain"}, DataBase64: encodeMaterial([]byte("x"))}
	for _, tc := range []struct {
		name    string
		entries []wireEntry
		code    string
		exit    int
	}{
		{"count-before-entry-shape", append(sixteen, wireEntry{Content: nil, DataBase64: nil}), "input-over-bound", result.ExitUnsupported},
		{"earlier-digest-before-later-content-shape", []wireEntry{badDigest, {Content: nil, DataBase64: encodeMaterial([]byte("x"))}}, "reference-mismatch", result.ExitInvalid},
		{"earlier-digest-before-later-base64-type", []wireEntry{badDigest, {Content: validEntry.Content, DataBase64: nil}}, "reference-mismatch", result.ExitInvalid},
		{"reached-invalid-content", []wireEntry{validEntry, {Content: nil, DataBase64: encodeMaterial([]byte("x"))}}, "invalid-carrier", result.ExitInvalid},
		{"reached-invalid-base64-type", []wireEntry{validEntry, {Content: validEntry.Content, DataBase64: nil}}, "invalid-carrier", result.ExitInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, exit := Check("check", f.raw["policy.json"], mustJSON(t, map[string]any{"materials": tc.entries}), engine(t))
			if exit != tc.exit || r.CandidateResult == nil || r.CandidateResult.Code != tc.code {
				t.Fatalf("native wire phase result %+v exit %d", r.CandidateResult, exit)
			}
		})
	}
}

func TestCandidateURIAssertionDoesNotAddCoreLengthBounds(t *testing.T) {
	f := candidateFixture(t, "refunds")
	id := "urn:test:" + strings.Repeat("a", 3000)
	pack := bytes.Replace(f.raw["consumer-pack.json"], mustJSON(t, object(f.adoption["consumer"])["id"]), mustJSON(t, id), 1)
	options := validation.Options{Through: "structural", Limits: limits(), FormatOverrides: []*jsonschema.Format{candidateURIFormat()}}
	if report, failure := engine(t).Validate(pack, options); failure != nil || report.Status != "valid" {
		t.Fatalf("URI assertion imposed a companion bound on Core %+v %v", report, failure)
	}
	// The companion schema remains the owner of its separate URI character cap.
	f.policy["id"] = id
	if report, exit := Validate("validate", mustJSON(t, f.policy)); exit != result.ExitUnsupported || report.refusalCode != "input-over-bound" {
		t.Fatalf("companion URI bound missing %+v %d", report, exit)
	}
}
