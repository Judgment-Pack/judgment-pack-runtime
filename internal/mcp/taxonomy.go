package mcp

import "fmt"

// Tool owners declare capabilities; neither names nor configuration downgrade them.
type toolTaxonomy struct {
	Operation    string `json:"operation"`
	Effect       string `json:"effect"`
	CheckKind    string `json:"checkKind,omitempty"`
	Experimental bool   `json:"experimental,omitempty"`
}

var toolDeclarations = map[string]toolTaxonomy{
	"describe_runtime":                      {Operation: "discover", Effect: "read"},
	"list_examples":                         {Operation: "discover", Effect: "read"},
	"list_packs":                            {Operation: "discover", Effect: "read"},
	"experimental_list_graphs":              {Operation: "discover", Effect: "read", Experimental: true},
	"get_dialect_contract":                  {Operation: "discover", Effect: "pure"},
	"experimental_get_policy_contract":      {Operation: "discover", Effect: "pure", Experimental: true},
	"experimental_get_test_matrix_contract": {Operation: "discover", Effect: "pure", Experimental: true},
	"get_schema":                            {Operation: "read", Effect: "read"},
	"get_example":                           {Operation: "read", Effect: "read"},
	"get_pack":                              {Operation: "read", Effect: "read"},
	"get_dialect_schema":                    {Operation: "read", Effect: "read"},
	"get_dialect_reference":                 {Operation: "read", Effect: "read"},
	"experimental_get_policy_schema":        {Operation: "read", Effect: "read", Experimental: true},
	"experimental_get_graph":                {Operation: "read", Effect: "read", Experimental: true},
	"experimental_list_audit":               {Operation: "read", Effect: "read", Experimental: true},
	"experimental_list_audit_history":       {Operation: "read", Effect: "read", Experimental: true},
	"validate":                              {Operation: "check", Effect: "pure", CheckKind: "validate"},
	"test_conformance":                      {Operation: "check", Effect: "read", CheckKind: "test"},
	"dialect_validate":                      {Operation: "check", Effect: "pure", CheckKind: "validate"},
	"dialect_check":                         {Operation: "check", Effect: "pure", CheckKind: "check"},
	"experimental_policy_validate":          {Operation: "check", Effect: "pure", CheckKind: "validate", Experimental: true},
	"experimental_policy_check":             {Operation: "check", Effect: "pure", CheckKind: "check", Experimental: true},
	"experimental_validate_test_matrix":     {Operation: "check", Effect: "pure", CheckKind: "validate", Experimental: true},
	"experimental_validate_expectations":    {Operation: "check", Effect: "pure", CheckKind: "validate", Experimental: true},
	"experimental_check_fact_types":         {Operation: "check", Effect: "pure", CheckKind: "advise", Experimental: true},
	"experimental_analyze_pack":             {Operation: "check", Effect: "pure", CheckKind: "analyze", Experimental: true},
	"experimental_test_cases":               {Operation: "check", Effect: "pure", CheckKind: "test", Experimental: true},
	"experimental_test_packs":               {Operation: "check", Effect: "read", CheckKind: "test", Experimental: true},
	"experimental_test_graphs":              {Operation: "check", Effect: "read", CheckKind: "test", Experimental: true},
	"experimental_verify_audit":             {Operation: "check", Effect: "read", CheckKind: "verify", Experimental: true},
	"get_artifact_version_options":          {Operation: "transform", Effect: "pure"},
	"dialect_init":                          {Operation: "transform", Effect: "pure"},
	"dialect_canonicalize":                  {Operation: "transform", Effect: "pure"},
	"dialect_digest":                        {Operation: "transform", Effect: "pure"},
	"experimental_policy_init":              {Operation: "transform", Effect: "pure", Experimental: true},
	"experimental_policy_canonicalize":      {Operation: "transform", Effect: "pure", Experimental: true},
	"experimental_policy_digest":            {Operation: "transform", Effect: "pure", Experimental: true},
	"experimental_evaluate":                 {Operation: "evaluate", Effect: "act", Experimental: true},
}

func (t toolTaxonomy) valid() bool {
	switch t.Operation {
	case "discover":
		if t.Effect != "pure" && t.Effect != "read" {
			return false
		}
	case "read":
		if t.Effect != "read" {
			return false
		}
	case "check":
		if t.Effect != "pure" && t.Effect != "read" {
			return false
		}
		switch t.CheckKind {
		case "validate", "check", "verify", "test", "analyze", "advise":
			return true
		}
		return false
	case "transform":
		if t.Effect != "pure" {
			return false
		}
	case "evaluate":
		if t.Effect != "pure" && t.Effect != "read" && t.Effect != "act" {
			return false
		}
	default:
		return false
	}
	return t.CheckKind == ""
}

func (t toolTaxonomy) metadata() map[string]any {
	m := map[string]any{"operation": t.Operation, "effect": t.Effect}
	if t.CheckKind != "" {
		m["checkKind"] = t.CheckKind
	}
	if t.Experimental {
		m["experimental"] = true
	}
	return m
}
func (t toolTaxonomy) annotations() map[string]any {
	act := t.Effect == "act"
	return map[string]any{"readOnlyHint": !act, "destructiveHint": act, "idempotentHint": !act, "openWorldHint": act}
}

func classifiedTools(definitions []map[string]any, declarations map[string]toolTaxonomy) ([]map[string]any, error) {
	for _, definition := range definitions {
		name, _ := definition["name"].(string)
		t, ok := declarations[name]
		if !ok || !t.valid() {
			return nil, fmt.Errorf("tool taxonomy is missing or invalid")
		}
		definition["_meta"] = t.metadata()
		definition["annotations"] = t.annotations()
	}
	return definitions, nil
}
