// Package policy supplies bounded offline tools for the explicitly non-normative
// RFC 0023 prototype. It is not a policy authority or an evaluator.
package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifactversion"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jpscanon"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	Profile               = "jps-policy-companion-prototype"
	ProfileVersion        = "0.0.2"
	PrototypeVersion      = ProfileVersion
	SpecRevision          = "8a56a95d555c17640d2db4984363b779536b3c2a"
	SpecURL               = "https://github.com/Judgment-Pack/judgment-pack-spec/blob/" + SpecRevision + "/rfcs/0023-policy-companion.md"
	MaxDocumentBytes      = 262144
	MaxMaterialBytes      = 1048576
	MaxOriginalBytes      = 4194304
	MaxTotalMaterialBytes = 16777216
	MaxMaterials          = 16
	// This private transport bound includes base64 and metadata. It is distinct
	// from the unique retained-byte bound and from MCP's request envelope limit.
	MaxMaterialsWireBytes = 24 * 1024 * 1024
	MaxMaterialsJSONBytes = MaxMaterialsWireBytes
	MaxDepth              = 32
)

type Report struct {
	OutputVersion        string                `json:"outputVersion"`
	Tool                 result.Tool           `json:"tool"`
	Command              string                `json:"command"`
	ContractVersion      string                `json:"contractVersion"`
	Specification        string                `json:"specification"`
	SpecRevision         string                `json:"specRevision"`
	Experimental         bool                  `json:"experimental"`
	Maturity             string                `json:"maturity"`
	Status               string                `json:"status"`
	Profile              string                `json:"profile"`
	ProfileVersion       string                `json:"profileVersion"`
	Kind                 string                `json:"kind,omitempty"`
	ArtifactID           string                `json:"artifactId,omitempty"`
	ArtifactVersion      string                `json:"artifactVersion,omitempty"`
	ValidationScope      string                `json:"validationScope,omitempty"`
	Document             string                `json:"document,omitempty"`
	BytesDigest          string                `json:"bytesDigest,omitempty"`
	CanonicalDigest      string                `json:"canonicalDigest,omitempty"`
	StarterTemplate      bool                  `json:"starterTemplate,omitempty"`
	RequiredMaterials    []MaterialRequirement `json:"requiredMaterials,omitempty"`
	Resource             *Resource             `json:"resource,omitempty"`
	Capabilities         Capabilities          `json:"capabilities"`
	Diagnostics          []result.Diagnostic   `json:"diagnostics"`
	DiagnosticsTruncated bool                  `json:"diagnosticsTruncated,omitempty"`
}

type Resource struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Text      string `json:"text"`
}

type MaterialRequirement struct {
	Role        string `json:"role"`
	Instruction string `json:"instruction"`
}

type Capabilities struct {
	Kinds                    []string `json:"kinds"`
	ProfileVersions          []string `json:"profileVersions"`
	CanonicalProfiles        []string `json:"canonicalProfiles"`
	ConsumerKinds            []string `json:"consumerKinds"`
	ConsumerElementKinds     []string `json:"consumerElementKinds"`
	Operations               []string `json:"operations"`
	ValidationScope          string   `json:"validationScope"`
	CheckScope               string   `json:"checkScope"`
	MaxDocumentBytes         int      `json:"maxDocumentBytes"`
	MaxMaterialBytes         int      `json:"maxMaterialBytes"`
	MaxOriginalBytes         int      `json:"maxOriginalBytes"`
	MaxTotalMaterialBytes    int      `json:"maxTotalMaterialBytes"`
	MaxMaterials             int      `json:"maxMaterials"`
	MaxMaterialsWireBytes    int      `json:"maxMaterialsWireBytes"`
	MaxDepth                 int      `json:"maxDepth"`
	MaxCoreNumberBytes       int      `json:"maxCoreNumberBytes"`
	MaxCoreExponentMagnitude int      `json:"maxCoreExponentMagnitude"`
	MaxArtifactVersionBytes  int      `json:"maxArtifactVersionBytes"`
	Experimental             bool     `json:"experimental"`
	Maturity                 string   `json:"maturity"`
	FullDocumentConformance  bool     `json:"fullDocumentConformance"`
	ApprovalVerification     bool     `json:"approvalVerification"`
	ExecutionAuthority       bool     `json:"executionAuthority"`
	LiveAuthority            bool     `json:"liveAuthority"`
	NetworkAcquisition       bool     `json:"networkAcquisition"`
}

func report(command string) Report {
	return Report{OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: command, ContractVersion: "1", Specification: SpecURL, SpecRevision: SpecRevision, Experimental: true, Maturity: "non-normative", Status: "encoded", Profile: Profile, ProfileVersion: ProfileVersion, Diagnostics: []result.Diagnostic{}, Capabilities: Capabilities{
		Kinds: []string{"policy", "governing-adoption"}, ProfileVersions: []string{ProfileVersion}, CanonicalProfiles: []string{"jps-canon"}, ConsumerKinds: []string{"pack"}, ConsumerElementKinds: []string{"rule", "exception", "applicability", "fallback"}, Operations: []string{"contract", "schema", "init", "canonicalize", "digest", "validate", "check"}, ValidationScope: "closed-prototype-document-and-local-references", CheckScope: "supplied-exact-bytes-anchors-and-scoped-core-references", MaxDocumentBytes: MaxDocumentBytes, MaxMaterialBytes: MaxMaterialBytes, MaxOriginalBytes: MaxOriginalBytes, MaxTotalMaterialBytes: MaxTotalMaterialBytes, MaxMaterials: MaxMaterials, MaxMaterialsWireBytes: MaxMaterialsWireBytes, MaxDepth: MaxDepth, MaxCoreNumberBytes: 128, MaxCoreExponentMagnitude: 308, MaxArtifactVersionBytes: artifactversion.MaxVersionBytes, Experimental: true, Maturity: "non-normative"}}
}

func Contract(command string) Report { r := report(command); r.Status = "available"; return r }

func refused(r Report, code, path, message string, exit int) (Report, int) {
	r.Status = "refused"
	r.Document = ""
	r.Resource = nil
	r.Diagnostics = []result.Diagnostic{result.ErrorDiagnostic(code, "policy", path, message)}
	return r, exit
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func limits() carrier.Limits {
	return carrier.Limits{MaxDepth: MaxDepth, MaxNodes: MaxDocumentBytes, MaxStringBytes: MaxDocumentBytes}
}

func parse(command string, data []byte) (Report, map[string]any, int) {
	r := report(command)
	if len(data) > MaxDocumentBytes {
		r, c := refused(r, "JPS-POLICY-DOCUMENT-BOUND", "", "Input exceeds this prototype's parsed-document byte support bound.", result.ExitUnsupported)
		return r, nil, c
	}
	value, failure := carrier.Decode(data, limits())
	if failure != nil {
		r.Status = "refused"
		r.Diagnostics = []result.Diagnostic{result.ErrorDiagnostic(failure.Diagnostic.Code, "policy", "", "Strict prototype JSON is malformed or exceeds its support bounds.")}
		code := result.ExitInvalid
		if failure.Resource {
			code = result.ExitUnsupported
		}
		return r, nil, code
	}
	var out strings.Builder
	if _, ok := jpscanon.Write(&out, value, nil); !ok {
		r, c := refused(r, "JPS-POLICY-CANONICAL-NUMBER", "", "The existing jps-canon profile requires safe integer literals and refuses fractions and exponent notation.", result.ExitInvalid)
		return r, nil, c
	}
	object, ok := value.(map[string]any)
	if !ok {
		r, c := refused(r, "JPS-POLICY-DOCUMENT", "", "A prototype document must be a JSON object.", result.ExitInvalid)
		return r, nil, c
	}
	proto, ok := object["prototype"].(map[string]any)
	if !ok || len(proto) != 3 || proto["profile"] != Profile || proto["proposalVersion"] != ProfileVersion || proto["status"] != "non-normative" {
		r, c := refused(r, "JPS-POLICY-SELECTOR", "/prototype", "Provide the exact supported non-normative prototype selector 0.0.2.", result.ExitUnsupported)
		return r, nil, c
	}
	kind, _ := object["kind"].(string)
	if kind != "policy" && kind != "governing-adoption" {
		r, c := refused(r, "JPS-POLICY-KIND", "/kind", "Supported prototype kinds are policy and governing-adoption.", result.ExitUnsupported)
		return r, nil, c
	}
	r.Kind = kind
	r.ArtifactID, _ = object["id"].(string)
	r.ArtifactVersion, _ = object["version"].(string)
	if len(r.ArtifactID) > 2048 {
		r.ArtifactID = ""
	}
	if len(r.ArtifactVersion) > artifactversion.MaxVersionBytes {
		r.ArtifactVersion = ""
	}
	r.Document = out.String()
	r.BytesDigest = digest(data)
	r.CanonicalDigest = digest([]byte(r.Document))
	return r, object, result.ExitSuccess
}

// Canonicalize checks strict JSON, the existing canon domain and exact selectors.
// Encoded is deliberately distinct from document-valid and references-valid.
func Canonicalize(command string, data []byte) (Report, int) {
	r, _, code := parse(command, data)
	return r, code
}
func Digest(command string, data []byte) (Report, int) {
	r, code := Canonicalize(command, data)
	r.Document = ""
	return r, code
}

// Init is an unsatisfied starter: zero identities for external material are
// placeholders, and the caller must supply its own narrative/pack/policy bytes.
func Init(command, kind, profileVersion, id, version string) (Report, int) {
	r := report(command)
	if code, path, message := selector(kind, profileVersion); code != 0 {
		return refused(r, "JPS-POLICY-SELECTOR", path, message, code)
	}
	if !artifactversion.Valid(version) {
		return refused(r, "JPS-POLICY-ARTIFACT-VERSION", "/version", "Provide an exact three-component artifact version within the reported byte bound.", result.ExitInvalid)
	}
	if len(id) > 2048 || !validURI(id) {
		return refused(r, "JPS-POLICY-ID", "/id", "Provide an absolute RFC 3986 URI within the prototype identity bound.", result.ExitInvalid)
	}
	content := func(media string) map[string]any {
		return map[string]any{"bytesDigest": "sha256:" + strings.Repeat("0", 64), "byteLength": 1, "mediaType": media}
	}
	proto := map[string]any{"profile": Profile, "proposalVersion": ProfileVersion, "status": "non-normative"}
	doc := map[string]any{"prototype": proto, "kind": kind, "id": id, "version": version}
	if kind == "policy" {
		doc["title"] = "Replace with a policy title"
		doc["managementMode"] = "desk-managed"
		doc["content"] = content("text/plain")
		doc["sourceProvenance"] = []any{}
		doc["sections"] = []any{map[string]any{"id": "replace-section", "title": "Replace with a section title", "anchor": map[string]any{"startByte": 0, "endByte": 1, "bytesDigest": "sha256:" + strings.Repeat("0", 64)}}}
		r.RequiredMaterials = []MaterialRequirement{{Role: "narrative", Instruction: "Supply retained UTF-8 text/plain narrative; replace its exact byte reference and every section anchor. Retain and reference originals when applicable."}}
	} else {
		ref := func(k string) map[string]any {
			return map[string]any{"kind": k, "id": "urn:replace:" + k, "version": "0.0.0", "content": content("application/json")}
		}
		doc["consumer"] = ref("pack")
		doc["governingPolicies"] = []any{map[string]any{"policy": ref("policy"), "scope": map[string]any{"kind": "whole-policy"}, "coverage": []any{map[string]any{"sectionId": "replace-section", "status": "awaiting-interpretation", "explanation": "Replace after reviewing the exact policy and consumer; this starter records no approval."}}}}
		r.RequiredMaterials = []MaterialRequirement{{Role: "consumer-and-policies", Instruction: "Replace artifact identities and exact byte references with retained Core pack and Policy documents; supply their complete narrative/source/review material and exact section coverage."}}
	}
	raw, _ := json.Marshal(doc)
	encoded, code := Canonicalize(command, raw)
	if code != 0 {
		return encoded, code
	}
	encoded.Status = "starter-template"
	encoded.StarterTemplate = true
	encoded.RequiredMaterials = r.RequiredMaterials
	return encoded, result.ExitSuccess
}

func selector(kind, version string) (int, string, string) {
	if version != ProfileVersion {
		return result.ExitUnsupported, "/profileVersion", "The exact supported prototype version is 0.0.2."
	}
	if kind != "policy" && kind != "governing-adoption" {
		return result.ExitUnsupported, "/kind", "Supported prototype kinds are policy and governing-adoption."
	}
	return 0, "", ""
}
