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
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Check operates only on supplied bytes. The private carrier accepts no paths,
// URLs or acquisition instructions. A successful check grants no authority.
func Check(command string, data, materialsJSON []byte, engine *validation.Engine) (Report, int) {
	return CheckFormat(command, data, materialsJSON, engine, "")
}

func CheckFormat(command string, data, materialsJSON []byte, engine *validation.Engine, format string) (Report, int) {
	r, doc, code := parseFormat(command, data, format)
	if code != 0 {
		return finishCandidateCheck(r, code)
	}
	candidateMode := r.FormatVersion == CandidateVersion
	if !candidateMode {
		r, doc, code = validateDocumentFormat(command, data, ProfileVersion)
		if code != 0 {
			return r, code
		}
	}
	return checkMaterials(r, doc, func() (materialSet, *issue, int) { return decodeMaterialsFormat(materialsJSON, candidateMode) }, engine)
}

func checkMaterials(r Report, doc map[string]any, load func() (materialSet, *issue, int), engine *validation.Engine) (Report, int) {
	candidateMode := r.FormatVersion == CandidateVersion
	materials, err, code := load()
	if err == nil && candidateMode && r.Kind != "policy" {
		err, code = candidateSchema(r.Kind, doc)
	}
	if err == nil {
		if doc["kind"] == "policy" {
			err, code = checkPolicy(doc, materials, "", candidateMode)
		} else {
			err, code = checkAdoption(doc, materials, engine, candidateMode)
		}
	}
	if err != nil {
		if candidateMode && code != result.ExitIO {
			r, code = candidateRefused(r, candidateCode(err, code))
		} else {
			r, code = refused(r, err.code, err.path, err.message, code)
		}
		return finishCandidateCheck(r, code)
	}
	r.Status, r.ValidationScope, r.Document = "references-valid", r.Capabilities.CheckScope, ""
	return finishCandidateCheck(r, result.ExitSuccess)
}

func checkPolicy(doc map[string]any, materials materialSet, base string, candidateMode bool) (*issue, int) {
	if candidateMode {
		if err, code := candidateSchema("policy", doc); err != nil {
			return err, code
		}
	}
	reference := object(doc["content"])
	if !candidateMode && reference["mediaType"] != "text/plain" {
		return problem("JPS-POLICY-NARRATIVE-MEDIA", base+"/content/mediaType", "Policy narrative must be retained as text/plain."), result.ExitInvalid
	}
	narrative, err, code := materials.resolve(reference, MaxMaterialBytes, base+"/content")
	if err != nil {
		return err, code
	}
	if reference["mediaType"] != "text/plain" {
		return candidateIssue("invalid-document"), result.ExitInvalid
	}
	if !utf8.Valid(narrative) {
		return problem("JPS-POLICY-NARRATIVE-UTF8", base+"/content", "Retained policy narrative must contain valid UTF-8 bytes."), result.ExitInvalid
	}
	var sources map[string]map[string]any
	if candidateMode {
		var err *issue
		sources, err = unique(array(doc["sourceProvenance"]), base+"/sourceProvenance", "source")
		if err != nil {
			return candidateIssue("invalid-document"), result.ExitInvalid
		}
	}
	for i, item := range array(doc["sourceProvenance"]) {
		_, err, code := materials.resolve(object(object(item)["content"]), MaxOriginalBytes, indexPath(base+"/sourceProvenance", i)+"/content")
		if err != nil {
			return err, code
		}
	}
	if candidateMode {
		if _, err := unique(array(doc["sections"]), base+"/sections", "section"); err != nil {
			return candidateIssue("invalid-anchor"), result.ExitInvalid
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
		if candidateMode {
			for _, source := range array(section["sourceRefs"]) {
				if _, found := sources[text(object(source)["sourceId"])]; !found {
					return candidateIssue("invalid-anchor"), result.ExitInvalid
				}
			}
		}
	}
	return nil, result.ExitSuccess
}

func checkArtifact(reference map[string]any, kindName string, materials materialSet, path string, candidateMode bool) (map[string]any, []byte, *issue, int) {
	if reference["kind"] != kindName {
		return nil, nil, problem("JPS-POLICY-ARTIFACT-KIND", path+"/kind", "Retained artifact kind does not match its consuming role."), result.ExitUnsupported
	}
	content := object(reference["content"])
	if content["mediaType"] != "application/json" {
		return nil, nil, problem("JPS-POLICY-ARTIFACT-MEDIA", path+"/content/mediaType", "Retained artifact documents must use application/json."), result.ExitInvalid
	}
	raw, err, code := materials.resolve(content, artifactLimit(candidateMode), path+"/content")
	if err != nil {
		return nil, nil, err, code
	}
	if len(raw) > MaxDocumentBytes {
		return nil, nil, problem("JPS-POLICY-DOCUMENT-BOUND", path+"/content", "Retained parsed artifact exceeds the prototype document byte support bound."), result.ExitUnsupported
	}
	var doc map[string]any
	if kindName == "pack" {
		var value any
		var failure *carrier.Failure
		if candidateMode {
			var c string
			value, c = candidateJSON(raw)
			if c != "" {
				return nil, nil, candidateIssue(c), candidateExit(c)
			}
		} else {
			value, failure = carrier.Decode(raw, limits())
		}
		if failure != nil {
			code := result.ExitInvalid
			if failure.Resource {
				code = result.ExitUnsupported
			}
			return nil, nil, problem("JPS-POLICY-CONSUMER-JSON", path+"/content", "Retained consumer JSON is malformed or exceeds this prototype's support bounds."), code
		}
		doc = object(value)
		if doc == nil {
			if candidateMode {
				return nil, nil, candidateIssue("invalid-consumer"), result.ExitInvalid
			}
			return nil, nil, problem("JPS-POLICY-CONSUMER-JSON", path+"/content", "The retained consumer must be a JSON object."), result.ExitInvalid
		}
		if !candidateMode {
			if err := coreNumberBounds(value, nil); err != nil {
				return nil, nil, prefixed(err, path), result.ExitUnsupported
			}
		}
	} else {
		var r Report
		var parsed map[string]any
		var c int
		if candidateMode {
			r, parsed, c = parseFormat("retained-policy", raw, CandidateVersion)
		} else {
			r, parsed, c = validateDocumentFormat("retained-policy", raw, ProfileVersion)
		}
		if c != 0 {
			if candidateMode {
				return nil, nil, candidateIssue(r.refusalCode), c
			}
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

func checkAdoption(doc map[string]any, materials materialSet, engine *validation.Engine, candidateMode bool) (*issue, int) {
	consumer, raw, err, code := checkArtifact(object(doc["consumer"]), "pack", materials, "/consumer", candidateMode)
	if err != nil {
		return err, code
	}
	if candidateMode && consumer["specVersion"] != "0.2.0-draft" {
		return candidateIssue("invalid-consumer"), result.ExitInvalid
	}
	if engine == nil {
		return problem("JPS-POLICY-ENGINE", "/consumer", "A bundled Core structural validation engine is required."), result.ExitIO
	}
	options := validation.Options{Through: "structural", Limits: limits()}
	if candidateMode {
		options.FormatOverrides = []*jsonschema.Format{candidateURIFormat()}
	}
	checked, failure := engine.Validate(raw, options)
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
		if candidateMode {
			return candidateIssue("invalid-consumer"), result.ExitInvalid
		}
		return err, result.ExitInvalid
	}
	exceptions, err := unique(array(consumer["exceptions"]), "/consumer/exceptions", "consumer exception")
	if err != nil {
		if candidateMode {
			return candidateIssue("invalid-consumer"), result.ExitInvalid
		}
		return err, result.ExitInvalid
	}
	outcomes, err := unique(array(consumer["outcomes"]), "/consumer/outcomes", "consumer outcome")
	if err != nil {
		if candidateMode {
			return candidateIssue("invalid-consumer"), result.ExitInvalid
		}
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
	policies := map[string]bool{}
	for i, item := range array(doc["governingPolicies"]) {
		governing := object(item)
		base := indexPath("/governingPolicies", i)
		policy, _, err, code := checkArtifact(object(governing["policy"]), "policy", materials, base+"/policy", candidateMode)
		if err != nil {
			return err, code
		}
		if err, code := checkPolicy(policy, materials, base+"/policy", candidateMode); err != nil {
			return err, code
		}
		if candidateMode {
			id := text(object(governing["policy"])["id"])
			if policies[id] {
				return candidateIssue("invalid-coverage"), result.ExitInvalid
			}
			policies[id] = true
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
			if !selected[id] || candidateMode && covered[id] {
				return problem("JPS-POLICY-COVERAGE-SCOPE", path+"/sectionId", "Coverage names a section outside the exact selected policy scope."), result.ExitInvalid
			}
			covered[id] = true
			if row["status"] != "implemented" {
				continue
			}
			elements := map[string]bool{}
			for k, item := range array(row["consumerElements"]) {
				element := object(item)
				elementPath := indexPath(path+"/consumerElements", k)
				key := text(element["kind"]) + ":" + text(element["id"])
				if candidateMode && elements[key] {
					return candidateIssue("invalid-coverage"), result.ExitInvalid
				}
				elements[key] = true
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
