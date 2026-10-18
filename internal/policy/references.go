package policy

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type issue struct{ code, path, message string }

func problem(code, path, message string) *issue {
	return &issue{code: code, path: path, message: message}
}
func object(value any) map[string]any     { v, _ := value.(map[string]any); return v }
func array(value any) []any               { v, _ := value.([]any); return v }
func text(value any) string               { v, _ := value.(string); return v }
func number(value any) int                { v, _ := value.(json.Number); n, _ := strconv.Atoi(v.String()); return n }
func indexPath(base string, i int) string { return base + "/" + strconv.Itoa(i) }

func unique(items []any, path, what string) (map[string]map[string]any, *issue) {
	out := map[string]map[string]any{}
	for i, item := range items {
		row := object(item)
		id := text(row["id"])
		if _, found := out[id]; found {
			return nil, problem("JPS-POLICY-DUPLICATE-ID", indexPath(path, i)+"/id", "Duplicate "+what+" identity.")
		}
		out[id] = row
	}
	return out, nil
}

// localReferences only checks relationships visible in this document. Checking
// retained bytes, UTF-8 anchors and targets in other artifacts requires Check.
func localReferences(doc map[string]any) *issue {
	if doc["kind"] == "policy" {
		sources, err := unique(array(doc["sourceProvenance"]), "/sourceProvenance", "source")
		if err != nil {
			return err
		}
		sections, err := unique(array(doc["sections"]), "/sections", "section")
		if err != nil {
			return err
		}
		_ = sections
		length := number(object(doc["content"])["byteLength"])
		for i, item := range array(doc["sections"]) {
			section := object(item)
			base := indexPath("/sections", i)
			anchor := object(section["anchor"])
			start, end := number(anchor["startByte"]), number(anchor["endByte"])
			if start < 0 || start >= end || end > length {
				return problem("JPS-POLICY-ANCHOR-RANGE", base+"/anchor", "The nonempty section range must lie within the declared narrative byte length.")
			}
			for j, item := range array(section["sourceRefs"]) {
				if _, found := sources[text(object(item)["sourceId"])]; !found {
					return problem("JPS-POLICY-SOURCE-REFERENCE", indexPath(base+"/sourceRefs", j)+"/sourceId", "Section provenance names an unknown local source.")
				}
			}
		}
		return nil
	}
	seen := map[string]bool{}
	for i, item := range array(doc["governingPolicies"]) {
		governing := object(item)
		base := indexPath("/governingPolicies", i)
		id := text(object(governing["policy"])["id"])
		if seen[id] {
			return problem("JPS-POLICY-DUPLICATE-POLICY", base+"/policy/id", "One adoption cannot declare multiple versions or entries for the same policy identity.")
		}
		seen[id] = true
		scope := object(governing["scope"])
		selected := map[string]bool{}
		for _, id := range array(scope["sectionIds"]) {
			selected[text(id)] = true
		}
		covered := map[string]bool{}
		for j, item := range array(governing["coverage"]) {
			row := object(item)
			path := indexPath(base+"/coverage", j)
			id := text(row["sectionId"])
			if covered[id] {
				return problem("JPS-POLICY-DUPLICATE-COVERAGE", path+"/sectionId", "A selected section must have exactly one coverage record.")
			}
			covered[id] = true
			if scope["kind"] == "sections" && !selected[id] {
				return problem("JPS-POLICY-COVERAGE-SCOPE", path+"/sectionId", "Coverage names a section outside the declared section scope.")
			}
			elements := map[string]bool{}
			for k, item := range array(row["consumerElements"]) {
				element := object(item)
				key := fmt.Sprintf("%s:%s", text(element["kind"]), text(element["id"]))
				if elements[key] {
					return problem("JPS-POLICY-DUPLICATE-ELEMENT", indexPath(path+"/consumerElements", k), "A coverage record repeats a typed consumer element.")
				}
				elements[key] = true
			}
		}
		if scope["kind"] == "sections" && len(covered) != len(selected) {
			return problem("JPS-POLICY-COVERAGE-MISSING", base+"/coverage", "Coverage must contain exactly one record for every selected section.")
		}
	}
	return nil
}
