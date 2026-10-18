package policy

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jpscanon"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// Check operates only on supplied bytes. The private carrier accepts no paths,
// URLs or acquisition instructions. A successful check grants no authority.
func Check(command string, data, materialsJSON []byte, engine *validation.Engine) (Report, int) {
	r, doc, code := validateDocument(command, data)
	if code != 0 {
		return r, code
	}
	materials, err, code := decodeMaterials(materialsJSON)
	if err != nil {
		return refused(r, err.code, err.path, err.message, code)
	}
	if doc["kind"] == "policy" {
		err, code = checkPolicy(doc, materials, "")
	} else {
		err, code = checkAdoption(doc, materials, engine)
	}
	if err != nil {
		return refused(r, err.code, err.path, err.message, code)
	}
	r.Status = "references-valid"
	r.ValidationScope = r.Capabilities.CheckScope
	return r, result.ExitSuccess
}

func checkPolicy(doc map[string]any, materials materialSet, base string) (*issue, int) {
	reference := object(doc["content"])
	if reference["mediaType"] != "text/plain" {
		return problem("JPS-POLICY-NARRATIVE-MEDIA", base+"/content/mediaType", "Policy narrative must be retained as text/plain."), result.ExitInvalid
	}
	narrative, err, code := materials.resolve(reference, MaxMaterialBytes, base+"/content")
	if err != nil {
		return err, code
	}
	if !utf8.Valid(narrative) {
		return problem("JPS-POLICY-NARRATIVE-UTF8", base+"/content", "Retained policy narrative must contain valid UTF-8 bytes."), result.ExitInvalid
	}
	for i, item := range array(doc["sourceProvenance"]) {
		_, err, code := materials.resolve(object(object(item)["content"]), MaxOriginalBytes, indexPath(base+"/sourceProvenance", i)+"/content")
		if err != nil {
			return err, code
		}
	}
	for i, item := range array(doc["sections"]) {
		section := object(item)
		anchor := object(section["anchor"])
		path := indexPath(base+"/sections", i) + "/anchor"
		start, end := number(anchor["startByte"]), number(anchor["endByte"])
		if start < 0 || start >= end || end > len(narrative) {
			return problem("JPS-POLICY-ANCHOR-RANGE", path, "Section range lies outside the exact retained narrative."), result.ExitInvalid
		}
		if !utf8.Valid(narrative[:start]) || !utf8.Valid(narrative[start:end]) {
			return problem("JPS-POLICY-ANCHOR-UTF8", path, "Both section endpoints must be boundaries in the retained UTF-8 narrative."), result.ExitInvalid
		}
		if digest(narrative[start:end]) != anchor["bytesDigest"] {
			return problem("JPS-POLICY-ANCHOR-DIGEST", path+"/bytesDigest", "The exact retained section bytes do not match the anchor digest."), result.ExitInvalid
		}
	}
	return nil, result.ExitSuccess
}

func checkArtifact(reference map[string]any, kindName string, materials materialSet, path string) (map[string]any, []byte, *issue, int) {
	if reference["kind"] != kindName {
		return nil, nil, problem("JPS-POLICY-ARTIFACT-KIND", path+"/kind", "Retained artifact kind does not match its consuming role."), result.ExitUnsupported
	}
	content := object(reference["content"])
	if content["mediaType"] != "application/json" {
		return nil, nil, problem("JPS-POLICY-ARTIFACT-MEDIA", path+"/content/mediaType", "Retained artifact documents must use application/json."), result.ExitInvalid
	}
	raw, err, code := materials.resolve(content, MaxMaterialBytes, path+"/content")
	if err != nil {
		return nil, nil, err, code
	}
	if len(raw) > MaxDocumentBytes {
		return nil, nil, problem("JPS-POLICY-DOCUMENT-BOUND", path+"/content", "Retained parsed artifact exceeds the prototype document byte support bound."), result.ExitUnsupported
	}
	var doc map[string]any
	if kindName == "pack" {
		value, failure := carrier.Decode(raw, limits())
		if failure != nil {
			code := result.ExitInvalid
			if failure.Resource {
				code = result.ExitUnsupported
			}
			return nil, nil, problem("JPS-POLICY-CONSUMER-JSON", path+"/content", "Retained consumer JSON is malformed or exceeds this prototype's support bounds."), code
		}
		doc = object(value)
		if doc == nil {
			return nil, nil, problem("JPS-POLICY-CONSUMER-JSON", path+"/content", "The retained consumer must be a JSON object."), result.ExitInvalid
		}
		if err := coreNumberBounds(value, nil); err != nil {
			return nil, nil, prefixed(err, path), result.ExitUnsupported
		}
	} else {
		r, parsed, c := validateDocument("retained-policy", raw)
		if c != 0 {
			d := r.Diagnostics[0]
			return nil, nil, problem(d.Code, path+"/content", "Retained Policy does not satisfy the supported closed prototype document and local-reference checks."), c
		}
		doc = parsed
	}
	if reference["id"] != doc["id"] || reference["version"] != doc["version"] {
		return nil, nil, problem("JPS-POLICY-ARTIFACT-IDENTITY", path, "Retained artifact identity/version does not match the exact reference."), result.ExitInvalid
	}
	if identity, found := reference["canonicalIdentity"]; found {
		var out strings.Builder
		if _, ok := jpscanon.Write(&out, doc, nil); !ok {
			return nil, nil, problem("JPS-POLICY-CANONICAL-DOMAIN", path+"/canonicalIdentity", "The retained artifact cannot supply the requested jps-canon identity because its numbers are outside that profile."), result.ExitUnsupported
		}
		if object(identity)["profile"] != "jps-canon" {
			return nil, nil, problem("JPS-POLICY-CANONICAL-PROFILE", path+"/canonicalIdentity/profile", "The only supported canonical identity profile is jps-canon."), result.ExitUnsupported
		}
		if object(identity)["canonicalDigest"] != digest([]byte(out.String())) {
			return nil, nil, problem("JPS-POLICY-CANONICAL-DIGEST", path+"/canonicalIdentity", "The retained artifact does not match the separately requested canonical identity."), result.ExitInvalid
		}
	}
	return doc, raw, nil, result.ExitSuccess
}

// Core extension numbers retain their original literals. These are prototype
// input support bounds, and do not add the Policy canon domain to Core.
func coreNumberBounds(value any, path []string) *issue {
	switch v := value.(type) {
	case json.Number:
		literal := v.String()
		if len(literal) > 128 {
			return problem("JPS-POLICY-CONSUMER-NUMBER-BOUND", "/content", "Consumer number literal exceeds the prototype support bound.")
		}
		if i := strings.IndexAny(literal, "eE"); i >= 0 {
			exponent, err := strconv.ParseInt(literal[i+1:], 10, 64)
			if err != nil || exponent < -308 || exponent > 308 {
				return problem("JPS-POLICY-CONSUMER-NUMBER-BOUND", "/content", "Consumer exponent exceeds the prototype support bound.")
			}
		}
	case []any:
		for i, item := range v {
			if err := coreNumberBounds(item, append(path, strconv.Itoa(i))); err != nil {
				return err
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := coreNumberBounds(v[key], append(path, key)); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkAdoption(doc map[string]any, materials materialSet, engine *validation.Engine) (*issue, int) {
	consumer, raw, err, code := checkArtifact(object(doc["consumer"]), "pack", materials, "/consumer")
	if err != nil {
		return err, code
	}
	if engine == nil {
		return problem("JPS-POLICY-ENGINE", "/consumer", "A bundled Core structural validation engine is required."), result.ExitIO
	}
	checked, failure := engine.Validate(raw, validation.Options{Through: "structural", Limits: limits()})
	if failure != nil {
		return problem("JPS-POLICY-CONSUMER-BOUND", "/consumer", "Core structural validation could not complete within supported input bounds."), failure.ExitCode
	}
	if checked.Status != "valid" {
		code := result.ExitInvalid
		if checked.Status == "unsupported" {
			code = result.ExitUnsupported
		}
		return problem("JPS-POLICY-CONSUMER-STRUCTURE", "/consumer", "The exact retained consumer does not satisfy a bundled Core structural schema."), code
	}
	rules, err := unique(array(consumer["rules"]), "/consumer/rules", "consumer rule")
	if err != nil {
		return err, result.ExitInvalid
	}
	exceptions, err := unique(array(consumer["exceptions"]), "/consumer/exceptions", "consumer exception")
	if err != nil {
		return err, result.ExitInvalid
	}
	outcomes, err := unique(array(consumer["outcomes"]), "/consumer/outcomes", "consumer outcome")
	if err != nil {
		return err, result.ExitInvalid
	}
	if fallback, present := consumer["fallbackOutcome"]; present {
		if _, ok := outcomes[text(fallback)]; !ok {
			return problem("JPS-POLICY-FALLBACK-REFERENCE", "/consumer/fallbackOutcome", "Consumer fallback must name a unique declared outcome."), result.ExitInvalid
		}
	}
	for i, item := range array(consumer["exceptions"]) {
		exception := object(item)
		path := indexPath("/consumer/exceptions", i)
		if id, present := exception["targetRule"]; present {
			if _, ok := rules[text(id)]; !ok {
				return problem("JPS-POLICY-EXCEPTION-REFERENCE", path+"/targetRule", "Consumer exception names an unknown target rule."), result.ExitInvalid
			}
		}
		if id, present := exception["outcome"]; present {
			if _, ok := outcomes[text(id)]; !ok {
				return problem("JPS-POLICY-EXCEPTION-REFERENCE", path+"/outcome", "Consumer exception names an unknown outcome."), result.ExitInvalid
			}
		}
	}
	for i, item := range array(doc["governingPolicies"]) {
		governing := object(item)
		base := indexPath("/governingPolicies", i)
		policy, _, err, code := checkArtifact(object(governing["policy"]), "policy", materials, base+"/policy")
		if err != nil {
			return err, code
		}
		if err, code := checkPolicy(policy, materials, base+"/policy"); err != nil {
			return err, code
		}
		sections, err := unique(array(policy["sections"]), base+"/policy/sections", "section")
		if err != nil {
			return err, result.ExitInvalid
		}
		scope := object(governing["scope"])
		selected := map[string]bool{}
		if scope["kind"] == "whole-policy" {
			for id := range sections {
				selected[id] = true
			}
		} else {
			for j, item := range array(scope["sectionIds"]) {
				id := text(item)
				if _, found := sections[id]; !found {
					return problem("JPS-POLICY-SCOPE-REFERENCE", indexPath(base+"/scope/sectionIds", j), "Declared scope names an unknown retained policy section."), result.ExitInvalid
				}
				selected[id] = true
			}
			if _, err, code := materials.resolve(object(scope["reviewBasis"]), MaxMaterialBytes, base+"/scope/reviewBasis"); err != nil {
				return err, code
			}
		}
		covered := map[string]bool{}
		for j, item := range array(governing["coverage"]) {
			row := object(item)
			path := indexPath(base+"/coverage", j)
			id := text(row["sectionId"])
			if !selected[id] {
				return problem("JPS-POLICY-COVERAGE-SCOPE", path+"/sectionId", "Coverage names a section outside the exact selected policy scope."), result.ExitInvalid
			}
			covered[id] = true
			if row["status"] != "implemented" {
				continue
			}
			for k, item := range array(row["consumerElements"]) {
				element := object(item)
				elementPath := indexPath(path+"/consumerElements", k)
				found := false
				switch element["kind"] {
				case "rule":
					_, found = rules[text(element["id"])]
				case "exception":
					_, found = exceptions[text(element["id"])]
				case "applicability":
					_, found = consumer["applicability"]
				case "fallback":
					_, found = consumer["fallbackOutcome"]
				}
				if !found {
					return problem("JPS-POLICY-ELEMENT-REFERENCE", elementPath, "Coverage names an absent or unknown typed Core consumer element."), result.ExitInvalid
				}
			}
			if _, err, code := materials.resolve(object(row["reviewBasis"]), MaxMaterialBytes, path+"/reviewBasis"); err != nil {
				return err, code
			}
		}
		if len(covered) != len(selected) {
			return problem("JPS-POLICY-COVERAGE-MISSING", base+"/coverage", "Coverage must include exactly one record for every section in the exact policy scope."), result.ExitInvalid
		}
	}
	for i, item := range array(doc["informationalSources"]) {
		if _, err, code := materials.resolve(object(item), MaxMaterialBytes, indexPath("/informationalSources", i)); err != nil {
			return err, code
		}
	}
	return nil, result.ExitSuccess
}
