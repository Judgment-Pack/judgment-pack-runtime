package emit

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// An options schema is a schema in the kind subset, $defs at its root
// included: its $ref is followed, and a $defs below the root is refused.
func TestAnOptionsSchemaMayUseDefs(t *testing.T) {
	root := testRepo(t)
	schema := map[string]any{
		"$defs":                map[string]any{"name": map[string]any{"type": "string", "minLength": json.Number("1")}},
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{"schemaName": map[string]any{"$ref": "#/$defs/name"}},
	}
	run := func(options any) *Report {
		m := mustManifest(t, manifestDoc([]string{"request", "gen/req"}, map[string]any{"options": schema, "environments": "all"}))
		return Emit(context.Background(), m, nil, Inputs{Registry: testRegistry(t), Options: options}, Options{Root: root, Timeout: 30 * time.Second})
	}
	report := run(map[string]any{"schemaName": "billing"})
	if text, _, _ := onDisk(t, root, "gen/req/request.json"); report.Outcome != OutcomeWritten || !strings.Contains(text, `"options":{"schemaName":"billing"}`) {
		t.Fatalf("valid options: %s\n%s", reportJSON(t, report), text)
	}
	if report := run(map[string]any{"schemaName": ""}); !slices.Equal(codes(report.Diagnostics), []string{"EMIT-OPTIONS@/schemaName"}) {
		t.Fatalf("options failing the $defs schema: %s", reportJSON(t, report))
	}
	nested := map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"$defs": map[string]any{}}}}
	data, err := json.Marshal(manifestDoc([]string{"exit", "0"}, map[string]any{"options": nested}))
	if err != nil {
		t.Fatal(err)
	}
	if m, diagnostics := ReadManifest(data); m != nil || len(diagnostics) != 1 || diagnostics[0].Code != "EMIT-MANIFEST" || diagnostics[0].Location.Pointer != "/options/properties/a/$defs" {
		t.Logf("%+v", diagnostics)
		t.Fatalf("a $defs below the root: %v", diagnostics)
	}
}
