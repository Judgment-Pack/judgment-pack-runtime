package policy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

type retained struct {
	raw        []byte
	mediaTypes map[string]bool
}
type materialSet map[string]*retained

func decodeMaterials(data []byte) (materialSet, *issue, int) {
	if len(data) > MaxMaterialsWireBytes {
		return nil, problem("JPS-POLICY-MATERIALS-BOUND", "/materials", "The private Runtime materials JSON exceeds its transport byte support bound."), result.ExitUnsupported
	}
	value, failure := carrier.Decode(data, carrier.Limits{MaxDepth: 8, MaxNodes: 1024, MaxStringBytes: base64.StdEncoding.EncodedLen(MaxOriginalBytes)})
	if failure != nil {
		code := result.ExitInvalid
		if failure.Resource {
			code = result.ExitUnsupported
		}
		return nil, problem(failure.Diagnostic.Code, "/materials", "Retained-material JSON is malformed or exceeds its support bounds."), code
	}
	root := object(value)
	entries := array(root["materials"])
	if len(entries) > MaxMaterials {
		return nil, problem("JPS-POLICY-MATERIAL-COUNT", "/materials", "The private Runtime carrier supports at most 16 material registrations."), result.ExitUnsupported
	}
	// The schema library converts number tokens to arbitrary-precision values.
	// Refuse unsupported literals throughout this closed carrier first, including
	// numbers in unknown or wrong-typed members that schema validation will reject.
	if failure, code := preflightMaterialNumbers(value, nil); failure != nil {
		return nil, failure, code
	}
	compiled, err := loadSchemas()
	if err != nil {
		return nil, problem("JPS-POLICY-ARTIFACT", "", "Pinned prototype schemas could not be compiled."), result.ExitIO
	}
	if err := compiled["materials"].Validate(value); err != nil {
		if schemaBound(err) {
			return nil, problem("JPS-POLICY-MATERIALS-BOUND", "/materials", "Retained material exceeds the private Runtime carrier support bounds."), result.ExitUnsupported
		}
		return nil, problem("JPS-POLICY-MATERIALS-SCHEMA", "/materials", "Provide only materials entries containing a retained content reference and a standard padded dataBase64 string."), result.ExitInvalid
	}
	// Complete shape, type and encoded-length checks precede any base64 decoding.
	distinct := map[string]bool{}
	total := 0
	for i, item := range entries {
		entry := object(item)
		content := object(entry["content"])
		encoded := text(entry["dataBase64"])
		path := indexPath("/materials", i) + "/dataBase64"
		declared := number(content["byteLength"])
		n, valid := base64Length(encoded)
		if !valid {
			return nil, problem("JPS-POLICY-BASE64", path, "Retained material must use strict standard padded base64 without whitespace."), result.ExitInvalid
		}
		if n > MaxOriginalBytes {
			return nil, problem("JPS-POLICY-ORIGINAL-BOUND", path, "Retained material exceeds the original-source byte support bound."), result.ExitUnsupported
		}
		if n != declared {
			return nil, problem("JPS-POLICY-BYTE-LENGTH", path, "Decoded retained bytes do not match their declared byte length."), result.ExitInvalid
		}
		key := text(content["bytesDigest"])
		if !distinct[key] {
			distinct[key] = true
			total += n
			if total > MaxTotalMaterialBytes {
				return nil, problem("JPS-POLICY-AGGREGATE-BOUND", "/materials", "Distinct retained material exceeds the aggregate byte support bound."), result.ExitUnsupported
			}
		}
	}
	material := materialSet{}
	for i, item := range entries {
		entry := object(item)
		content := object(entry["content"])
		raw, err := base64.StdEncoding.Strict().DecodeString(text(entry["dataBase64"]))
		if err != nil {
			return nil, problem("JPS-POLICY-BASE64", indexPath("/materials", i)+"/dataBase64", "Retained material is not strict standard padded base64."), result.ExitInvalid
		}
		key := text(content["bytesDigest"])
		if digest(raw) != key {
			return nil, problem("JPS-POLICY-BYTES-DIGEST", indexPath("/materials", i)+"/content/bytesDigest", "Retained bytes do not match their exact SHA-256 reference."), result.ExitInvalid
		}
		existing := material[key]
		if existing == nil {
			existing = &retained{raw: raw, mediaTypes: map[string]bool{}}
			material[key] = existing
		} else if !bytes.Equal(existing.raw, raw) {
			return nil, problem("JPS-POLICY-CONFLICTING-MATERIAL", indexPath("/materials", i), "Repeated retained-byte identity contains conflicting bytes."), result.ExitInvalid
		}
		existing.mediaTypes[text(content["mediaType"])] = true
	}
	return material, nil, result.ExitSuccess
}

func preflightMaterialNumbers(value any, location []string) (*issue, int) {
	switch value := value.(type) {
	case json.Number:
		path, code := "/materials", "JPS-POLICY-MATERIAL-NUMBER"
		if len(location) == 4 && location[0] == "materials" && location[2] == "content" && location[3] == "byteLength" {
			// A wrong-typed materials object can contain an arbitrary key where
			// the array index belongs. Only bounded, canonical indexes are safe
			// to include in an error path before the closed schema has passed.
			if len(location[1]) <= 2 {
				index, err := strconv.Atoi(location[1])
				if err == nil && index >= 0 && index < MaxMaterials && strconv.Itoa(index) == location[1] {
					path, code = carrier.Pointer(location), "JPS-POLICY-BYTE-LENGTH"
				}
			}
		}
		literal := value.String()
		if strings.ContainsAny(literal, ".eE") || strings.HasPrefix(literal, "-") && literal != "-0" {
			return problem(code, path, "Retained-material numbers must use nonnegative integer literals within the carrier support bound."), result.ExitInvalid
		}
		if literal == "-0" {
			literal = "0"
		}
		maximum := strconv.Itoa(MaxOriginalBytes)
		if len(literal) > len(maximum) || len(literal) == len(maximum) && literal > maximum {
			return problem(code+"-BOUND", path, "Retained-material integer exceeds the original-source byte support bound."), result.ExitUnsupported
		}
	case []any:
		for index, item := range value {
			if failure, code := preflightMaterialNumbers(item, append(location, strconv.Itoa(index))); failure != nil {
				return failure, code
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if failure, code := preflightMaterialNumbers(value[key], append(location, key)); failure != nil {
				return failure, code
			}
		}
	}
	return nil, result.ExitSuccess
}

// base64Length checks alphabet, padding and decoded size without allocating.
// The strict decoder separately checks unused trailing bits after this preflight.
func base64Length(s string) (int, bool) {
	if len(s)%4 != 0 {
		return 0, false
	}
	if len(s) == 0 {
		return 0, true
	}
	padding := 0
	for i := len(s) - 1; i >= 0 && s[i] == '='; i-- {
		padding++
	}
	if padding > 2 {
		return 0, false
	}
	for i := 0; i < len(s)-padding; i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/') {
			return 0, false
		}
	}
	return len(s)/4*3 - padding, true
}

func (materials materialSet) resolve(reference map[string]any, limit int, path string) ([]byte, *issue, int) {
	length := number(reference["byteLength"])
	if length > limit {
		return nil, problem("JPS-POLICY-MATERIAL-ROLE-BOUND", path, "Retained material exceeds this consuming role's byte support bound."), result.ExitUnsupported
	}
	entry := materials[text(reference["bytesDigest"])]
	if entry == nil {
		return nil, problem("JPS-POLICY-MATERIAL-MISSING", path, "Required exact retained material was not supplied."), result.ExitInvalid
	}
	if !entry.mediaTypes[text(reference["mediaType"])] {
		return nil, problem("JPS-POLICY-MEDIA-TYPE", path+"/mediaType", "These retained bytes were not registered under the referenced media type."), result.ExitInvalid
	}
	if len(entry.raw) != length || digest(entry.raw) != text(reference["bytesDigest"]) {
		return nil, problem("JPS-POLICY-BYTE-IDENTITY", path, "Retained bytes do not match their exact content reference."), result.ExitInvalid
	}
	return entry.raw, nil, result.ExitSuccess
}

func prefixed(err *issue, prefix string) *issue {
	if err == nil {
		return nil
	}
	return &issue{code: err.code, path: prefix + err.path, message: err.message}
}
