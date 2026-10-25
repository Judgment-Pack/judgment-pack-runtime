package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

func TestTaxonomyActualToolsPublication(t *testing.T) {
	raw, err := os.ReadFile("testdata/tool-taxonomy.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []map[string]any
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	expected := map[string]map[string]any{}
	for _, f := range fixtures {
		name := f["name"].(string)
		delete(f, "name")
		expected[name] = f
	}
	response := runServer(t, message(t, 1, "tools/list", nil))[0]
	if response["error"] != nil {
		t.Fatalf("classified tools/list failed: %v", response)
	}
	tools := response["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 38 || len(tools) != len(expected) || len(toolDeclarations) != len(expected) {
		t.Fatalf("taxonomy completeness: served=%d declared=%d expected=%d", len(tools), len(toolDeclarations), len(expected))
	}
	seen := map[string]bool{}
	for _, item := range tools {
		tool := item.(map[string]any)
		name := tool["name"].(string)
		if seen[name] || !reflect.DeepEqual(expected[name], tool["_meta"]) {
			t.Fatalf("owner metadata differs for %s: %v", name, tool["_meta"])
		}
		seen[name] = true
		act := expected[name]["effect"] == "act"
		hints := map[string]any{"readOnlyHint": !act, "destructiveHint": act, "idempotentHint": !act, "openWorldHint": act}
		if !reflect.DeepEqual(hints, tool["annotations"]) {
			t.Fatalf("derived annotations differ for %s: %v", name, tool["annotations"])
		}
	}
}
func TestTaxonomyMissingDeclarationRefusesActualPublication(t *testing.T) {
	original := toolDeclarations["validate"]
	delete(toolDeclarations, "validate")
	t.Cleanup(func() { toolDeclarations["validate"] = original })
	for _, method := range []string{"tools/list", "resources/list"} {
		response := runServer(t, message(t, 1, method, nil))[0]
		if response["error"] == nil || response["result"] != nil {
			t.Fatalf("missing declaration published %s: %v", method, response)
		}
	}
}
func TestTaxonomyNewToolHasNoDefaultAndInvalidDeclarationsRefuse(t *testing.T) {
	if _, err := classifiedTools([]map[string]any{{"name": "new_tool"}}, toolDeclarations); err == nil {
		t.Fatal("unknown tool acquired a default classification")
	}
	for _, bad := range []toolTaxonomy{{}, {Operation: "read", Effect: "pure"}, {Operation: "check", Effect: "pure"}, {Operation: "check", Effect: "read", CheckKind: "invented"}, {Operation: "transform", Effect: "read"}, {Operation: "read", Effect: "read", CheckKind: "test"}, {Operation: "unknown", Effect: "pure"}, {Operation: "evaluate", Effect: "unknown"}} {
		if _, err := classifiedTools([]map[string]any{{"name": "tool"}}, map[string]toolTaxonomy{"tool": bad}); err == nil {
			t.Fatalf("invalid declaration admitted: %+v", bad)
		}
	}
}
func TestMethodResourcesActualDiscoveryReadAndPromptEquivalence(t *testing.T) {
	response := runServer(t, message(t, 1, "initialize", nil)+message(t, 2, "resources/list", nil))
	caps := response[0]["result"].(map[string]any)["capabilities"].(map[string]any)
	if caps["resources"] == nil || caps["prompts"] == nil {
		t.Fatal("resources/prompts capability missing")
	}
	if response[1]["error"] != nil {
		t.Fatalf("method discovery failed: %v", response[1])
	}
	resources := response[1]["result"].(map[string]any)["resources"].([]any)
	if len(resources) != 6 {
		t.Fatalf("method discovery count=%d", len(resources))
	}
	expectedTools := map[string][]string{
		"author_pack":         {"get_schema", "list_examples", "get_example", "validate", "experimental_evaluate"},
		"fix_pack":            {"validate", "experimental_test_packs"},
		"test_pack":           {"experimental_evaluate", "experimental_test_packs"},
		"present_pack":        {"get_pack", "experimental_evaluate"},
		"explain_disposition": {"experimental_evaluate", "get_pack"},
		"author_graph":        {"get_pack", "experimental_test_graphs", "experimental_list_graphs", "experimental_get_graph"},
	}
	effects := map[string]string{"author_pack": "act", "fix_pack": "read", "test_pack": "act", "present_pack": "act", "explain_disposition": "act", "author_graph": "read"}
	for _, item := range resources {
		resource := item.(map[string]any)
		name := resource["name"].(string)
		uri := resource["uri"].(string)
		read := runServer(t, message(t, 1, "resources/read", map[string]any{"uri": uri})+message(t, 2, "prompts/get", map[string]any{"name": name}))[0:2]
		if read[0]["error"] != nil {
			t.Fatalf("method read failed: %v", read[0])
		}
		text := read[0]["result"].(map[string]any)["contents"].([]any)[0].(map[string]any)["text"].(string)
		if len(text) > maxMethodBytes {
			t.Fatal("method bound exceeded")
		}
		var definition map[string]any
		if err := json.Unmarshal([]byte(text), &definition); err != nil {
			t.Fatal(err)
		}
		prompt := read[1]["result"].(map[string]any)["messages"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"]
		if definition["instructions"] != prompt {
			t.Fatalf("method prompt bytes changed for %s", name)
		}
		if definition["kind"] != "method" || definition["version"] != "1" || definition["publisher"] != "judgment-pack-runtime" {
			t.Fatalf("method identity differs: %v", definition)
		}
		depBytes, _ := json.Marshal(definition["tools"])
		var deps []string
		_ = json.Unmarshal(depBytes, &deps)
		if !reflect.DeepEqual(deps, expectedTools[name]) {
			t.Fatalf("method dependencies differ for %s: %v", name, deps)
		}
		if definition["profile"].(map[string]any)["effect"] != effects[name] {
			t.Fatalf("method understates effect for %s", name)
		}
		digest := definition["digest"].(string)
		bundle := map[string]any{}
		for _, key := range []string{"kind", "name", "description", "instructions", "tools", "resources", "dependencies"} {
			if v, ok := definition[key]; ok {
				bundle[key] = v
			}
		}
		got, err := methodDigest(bundle)
		if err != nil || digest != got || uri != methodResourcePrefix+name+"/"+strings.TrimPrefix(digest, "sha256:") {
			t.Fatalf("method digest/URI differs for %s", name)
		}
		if name == "author_graph" {
			members := definition["resources"].([]any)
			if len(members) != 1 || members[0].(map[string]any)["name"] != "external-terminal-prerequisite" || !strings.Contains(members[0].(map[string]any)["text"].(string), "must refuse unresolved") {
				t.Fatal("terminal prerequisite missing")
			}
		}
		repeat := runServer(t, message(t, 1, "resources/read", map[string]any{"uri": uri}))[0]
		repeated := repeat["result"].(map[string]any)["contents"].([]any)[0].(map[string]any)["text"]
		if repeated != text {
			t.Fatalf("method resource not immutable for %s", name)
		}
	}
	prompts := runServer(t, message(t, 1, "prompts/list", nil))[0]["result"].(map[string]any)["prompts"].([]any)
	if len(prompts) != 7 {
		t.Fatal("existing prompts changed")
	}
}
func TestMethodResourcesRefuseMalformedAndUnknownURIs(t *testing.T) {
	for _, tc := range []struct{ method, params string }{
		{"resources/list", `null`}, {"resources/list", `[]`}, {"resources/list", `{"cursor":"next"}`},
		{"resources/read", `{}`}, {"resources/read", `{"uri":null}`}, {"resources/read", `{"URI":"x"}`}, {"resources/read", `{"uri":"x","uri":"y"}`},
		{"resources/read", `{"uri":"judgment-pack://methods/1/author_pack"}`}, {"resources/read", `{"uri":"https://example.invalid"}`},
		{"resources/read", `{"uri":"` + strings.Repeat("x", 257) + `"}`}, {"resources/list", `{"` + strings.Repeat("x", 2048) + `":true}`},
	} {
		input := `{"jsonrpc":"2.0","id":1,"method":"` + tc.method + `","params":` + tc.params + "}\n"
		response := runServer(t, input)[0]
		if response["error"] == nil || response["result"] != nil {
			t.Fatalf("invalid method request admitted: %s", tc.params)
		}
		raw, _ := json.Marshal(response)
		if len(raw) > 1024 {
			t.Fatal("method refusal unbounded")
		}
	}
}
func TestMethodDependenciesAndPrerequisitesAreBound(t *testing.T) {
	original, err := publishedMethods(toolDefinitions(), toolDeclarations)
	if err != nil {
		t.Fatal(err)
	}
	changed := map[string]toolTaxonomy{}
	for name, taxonomy := range toolDeclarations {
		changed[name] = taxonomy
	}
	taxonomy := changed["get_pack"]
	taxonomy.Experimental = true
	changed["get_pack"] = taxonomy
	updated, err := publishedMethods(toolDefinitions(), changed)
	if err != nil {
		t.Fatal(err)
	}
	if original[3]["digest"] == updated[3]["digest"] || original[3]["uri"] == updated[3]["uri"] {
		t.Fatal("dependency metadata change retained method identity")
	}
	var withheld []map[string]any
	for _, tool := range toolDefinitions() {
		if tool["name"] != "get_pack" {
			withheld = append(withheld, tool)
		}
	}
	if _, err := publishedMethods(withheld, toolDeclarations); err == nil {
		t.Fatal("withheld Runtime dependency acquired a profile")
	}
	if _, _, err := methodBundle("bad", "", "", []string{"undeclared"}, []any{}, toolDeclarations); err == nil {
		t.Fatal("unknown Runtime dependency acquired a profile")
	}
	graph := original[5]
	before := graph["digest"]
	bundle := map[string]any{}
	for _, key := range []string{"kind", "name", "description", "instructions", "tools", "resources", "dependencies"} {
		bundle[key] = graph[key]
	}
	bundle["resources"] = []any{}
	after, err := methodDigest(bundle)
	if err != nil || before == after {
		t.Fatal("external terminal prerequisite not digest-bound")
	}
}
func TestMethodSharedCanonicalFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/method-digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Description, Instructions, Canonical string
		Tools                                      []string
		Resources                                  []any
		Taxonomies                                 map[string]toolTaxonomy
		Profile                                    map[string]any
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		bundle, profile, err := methodBundle(f.Name, f.Description, f.Instructions, f.Tools, f.Resources, f.Taxonomies)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := jcs.Encode(bundle)
		if err != nil || string(canonical) != f.Canonical {
			t.Fatalf("hand-authored canonical bytes differ for %s: %s", f.Name, canonical)
		}
		actualProfile, _ := json.Marshal(profile)
		expectedProfile, _ := json.Marshal(f.Profile)
		if string(actualProfile) != string(expectedProfile) {
			t.Fatalf("hand-authored profile differs for %s", f.Name)
		}
		digest := sha256.Sum256([]byte(f.Canonical))
		got, err := methodDigest(bundle)
		if err != nil || got != "sha256:"+hex.EncodeToString(digest[:]) {
			t.Fatalf("hand-authored digest differs for %s", f.Name)
		}
	}
}
