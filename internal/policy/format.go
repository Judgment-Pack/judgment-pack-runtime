package policy

import (
	"strings"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifactversion"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jpscanon"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const CandidateVersion = "1.0.0"
const CandidateRevision = "0852c5fa7726e51b36e574a5b110e7f5e012e03d"
const CandidateURL = "https://github.com/Judgment-Pack/judgment-pack-spec/blob/" + CandidateRevision + "/rfcs/policy-companion-contract/README.md"
const DraftRelease = "policy-v1.0.0-draft.1"
const DraftRevision = "0799b37b74e35c4a86bfbf70f7bee80bdb7bbe94"
const DraftURL = "https://github.com/Judgment-Pack/judgment-pack-spec/blob/" + DraftRevision + "/spec/policy.md"

type formatDescriptor struct {
	version, profile, maturity, revision, url, directory, commonURI string
}

var prototypeFormat = formatDescriptor{ProfileVersion, Profile, "non-normative", SpecRevision, SpecURL, "prototype", commonID}
var candidateFormat = formatDescriptor{CandidateVersion, "jps-policy-companion", "draft", DraftRevision, DraftURL, "official/schema/policy/1.0.0", draftCommonID}

// CandidateResult retains the existing report member name for the complete
// portable draft full-check result. It is absent from
// legacy reports and from encoding/local validation, which have narrower scopes.
type CandidateResult struct {
	Status        string `json:"status"`
	Code          string `json:"code,omitempty"`
	FormatVersion string `json:"formatVersion,omitempty"`
	Maturity      string `json:"maturity,omitempty"`
}

// Registry is an explicitly requested discovery surface, separate from the
// unchanged no-argument prototype contract consumed by strict older clients.
type Registry struct {
	OutputVersion   string      `json:"outputVersion"`
	Tool            result.Tool `json:"tool"`
	Command         string      `json:"command"`
	RegistryVersion string      `json:"registryVersion"`
	Status          string      `json:"status"`
	Formats         []Report    `json:"formats"`
}

func AllFormats(command string) Registry {
	legacy := Contract(command)
	candidate := candidateReport(command)
	candidate.Status = "available"
	return Registry{result.OutputVersion, result.CurrentTool(), command, "1", "available", []Report{legacy, candidate}}
}

func ContractFormat(command, version string) (Report, int) {
	if version == ProfileVersion {
		return Contract(command), 0
	}
	r := candidateReport(command)
	if !artifactversion.Valid(version) {
		return candidateRefused(r, "invalid-selector")
	}
	if version != CandidateVersion {
		return candidateRefused(r, "unsupported-format")
	}
	r.Status = "available"
	return r, 0
}

func candidateReport(command string) Report {
	r := report(command)
	f := candidateFormat
	r.Specification, r.SpecRevision = f.url, f.revision
	r.Maturity, r.Profile, r.ProfileVersion = f.maturity, f.profile, f.version
	r.FormatVersion = f.version
	r.Capabilities.ProfileVersions = []string{f.version}
	r.Capabilities.Maturity = f.maturity
	r.Capabilities.ValidationScope = "closed-draft-document-and-local-references"
	r.Capabilities.CheckScope = "draft-supplied-exact-bytes-anchors-and-scoped-core-references"
	r.CandidateCapabilities, _ = official.ReadFile("official/conformance/policy/1.0.0/capabilities.json")
	return r
}

func candidateRefused(r Report, code string) (Report, int) {
	r.refusalCode = code
	exit := result.ExitInvalid
	if code == "input-over-bound" || code == "unsupported-format" || code == "unsupported-canonical-identity" {
		exit = result.ExitUnsupported
	}
	return refused(r, "JPS-POLICY-CANDIDATE-"+strings.ToUpper(code), "", "Policy draft check refused: "+code+".", exit)
}

// Only a strict object carrying the new selector opts into automatic candidate
// dispatch. Explicit operation format selection also covers malformed/missing
// selector inputs; legacy malformed-input reports remain byte-compatible.
func inputFormat(data []byte) string {
	if len(data) <= MaxDocumentBytes {
		value, failure := carrier.Decode(data, limits())
		if failure == nil {
			if _, named := object(value)["formatVersion"]; named {
				return CandidateVersion
			}
		}
	}
	return ProfileVersion
}

func parseFormat(command string, data []byte, format string) (Report, map[string]any, int) {
	if format == "" {
		format = inputFormat(data)
	}
	if format == ProfileVersion {
		return parsePrototype(command, data)
	}
	r := candidateReport(command)
	refuse := func(code string) (Report, map[string]any, int) { rr, c := candidateRefused(r, code); return rr, nil, c }
	if !artifactversion.Valid(format) {
		return refuse("invalid-selector")
	}
	if format != CandidateVersion {
		return refuse("unsupported-format")
	}
	value, code := candidateJSON(data)
	if code != "" {
		return refuse(code)
	}
	doc := object(value)
	if code := candidateSelector(doc); code != "" {
		return refuse(code)
	}
	var out strings.Builder
	if _, ok := jpscanon.Write(&out, value, nil); !ok {
		return refuse("invalid-document")
	}
	kind := text(doc["kind"])
	if kind == "policy" || kind == "governing-adoption" {
		r.Kind = kind
	}
	r.ArtifactID, r.ArtifactVersion = text(doc["id"]), text(doc["version"])
	if utf8.RuneCountInString(r.ArtifactID) > 2048 {
		r.ArtifactID = ""
	}
	if len(r.ArtifactVersion) > artifactversion.MaxVersionBytes {
		r.ArtifactVersion = ""
	}
	r.Document, r.BytesDigest, r.CanonicalDigest = out.String(), digest(data), digest([]byte(out.String()))
	return r, doc, 0
}

func candidateSelector(doc map[string]any) string {
	version, ok := doc["formatVersion"].(string)
	if !ok || !artifactversion.Valid(version) {
		return "invalid-selector"
	}
	if version != CandidateVersion {
		return "unsupported-format"
	}
	return ""
}

// Measure physical bounds before parsing even malformed JSON. Maximal number
// character runs outside strings deliberately match the pinned reader's phase.
func measureCandidate(data []byte, maxBytes, maxDepth int) bool {
	if len(data) > maxBytes {
		return false
	}
	depth, inString, escaped := 0, false, false
	for i := 0; i < len(data); i++ {
		b := data[i]
		if inString {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '[', '{':
			depth++
			if depth > maxDepth {
				return false
			}
		case ']', '}':
			depth--
		default:
			if !numberCharacter(b) {
				continue
			}
			end := i + 1
			for end < len(data) && numberCharacter(data[end]) {
				end++
			}
			literal := data[i:end]
			if len(literal) > 128 {
				return false
			}
			if e := strings.IndexAny(string(literal), "eE"); e >= 0 {
				j := e + 1
				if j < len(literal) && (literal[j] == '+' || literal[j] == '-') {
					j++
				}
				start := j
				for j < len(literal) && literal[j] >= '0' && literal[j] <= '9' {
					j++
				}
				exponent := strings.TrimLeft(string(literal[start:j]), "0")
				if len(exponent) > 3 || len(exponent) == 3 && exponent > "308" {
					return false
				}
			}
			i = end - 1
		}
	}
	return true
}

func numberCharacter(b byte) bool {
	return b >= '0' && b <= '9' || b == '+' || b == '-' || b == '.' || b == 'e' || b == 'E'
}

func candidateJSON(data []byte) (any, string) {
	if !measureCandidate(data, MaxDocumentBytes, MaxDepth) {
		return nil, "input-over-bound"
	}
	value, failure := carrier.Decode(data, limits())
	if failure != nil {
		if failure.Resource {
			return nil, "input-over-bound"
		}
		return nil, "invalid-json"
	}
	return value, ""
}

func CanonicalizeFormat(command string, data []byte, format string) (Report, int) {
	r, _, code := parseFormat(command, data, format)
	return r, code
}
func DigestFormat(command string, data []byte, format string) (Report, int) {
	r, code := CanonicalizeFormat(command, data, format)
	r.Document = ""
	return r, code
}

func candidateIssue(code string) *issue {
	return &issue{code: "JPS-POLICY-CANDIDATE-" + strings.ToUpper(code), message: "Policy draft check refused: " + code + ".", candidateCode: code}
}

func candidateCode(err *issue, exit int) string {
	if err.candidateCode != "" {
		return err.candidateCode
	}
	if err.code == "JPS-POLICY-CANONICAL-DOMAIN" {
		return "unsupported-canonical-identity"
	}
	if exit == result.ExitUnsupported {
		return "input-over-bound"
	}
	switch err.code {
	case "JPS-POLICY-MATERIAL-MISSING":
		return "missing-material"
	case "JPS-POLICY-NARRATIVE-MEDIA", "JPS-POLICY-ARTIFACT-MEDIA":
		return "invalid-document"
	case "JPS-POLICY-NARRATIVE-UTF8", "JPS-POLICY-ANCHOR-RANGE", "JPS-POLICY-ANCHOR-UTF8", "JPS-POLICY-ANCHOR-DIGEST", "JPS-POLICY-SOURCE-REFERENCE":
		return "invalid-anchor"
	case "JPS-POLICY-ELEMENT-REFERENCE", "JPS-POLICY-COVERAGE-SCOPE", "JPS-POLICY-COVERAGE-MISSING", "JPS-POLICY-DUPLICATE-POLICY", "JPS-POLICY-DUPLICATE-COVERAGE", "JPS-POLICY-DUPLICATE-ELEMENT", "JPS-POLICY-SCOPE-REFERENCE":
		return "invalid-coverage"
	case "JPS-POLICY-CONSUMER-STRUCTURE", "JPS-POLICY-FALLBACK-REFERENCE", "JPS-POLICY-EXCEPTION-REFERENCE":
		return "invalid-consumer"
	}
	return "reference-mismatch"
}

func finishCandidateCheck(r Report, code int) (Report, int) {
	if r.FormatVersion == "" {
		return r, code
	}
	if code == 0 {
		r.CandidateResult = &CandidateResult{Status: "valid", FormatVersion: CandidateVersion, Maturity: "draft"}
	} else if r.refusalCode != "" {
		r.CandidateResult = &CandidateResult{Status: "refused", Code: r.refusalCode}
	}
	return r, code
}

func artifactLimit(candidateMode bool) int {
	if candidateMode {
		return MaxDocumentBytes
	}
	return MaxMaterialBytes
}
func candidateExit(code string) int {
	if code == "input-over-bound" || code == "unsupported-format" || code == "unsupported-canonical-identity" {
		return result.ExitUnsupported
	}
	return result.ExitInvalid
}
