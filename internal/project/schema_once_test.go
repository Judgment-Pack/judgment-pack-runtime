package project

import "testing"

// The configuration schema is compiled once per process (#357), and every
// Load still holds a configuration to it: a refused one is refused after the
// compilation is shared exactly as before it.
func TestTheConfigurationSchemaIsCompiledOnce(t *testing.T) {
	good := writeProject(t, `{"configVersion":"1","packs":{}}`, nil)
	bad := writeProject(t, `{"configVersion":"1","packs":{"Expense Approval":{"path":"a.json"}}}`, nil)
	loaded, failure := Load(good)
	if failure != nil {
		t.Fatalf("a good configuration: %s", failure.Message)
	}
	loaded.Close()
	before := configSchemaCompilations.Load()
	for range 3 {
		loaded, failure := Load(good)
		if failure != nil {
			t.Fatalf("a good configuration: %s", failure.Message)
		}
		loaded.Close()
		if _, failure := Load(bad); failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" {
			t.Fatalf("the schema still refuses: %+v", failure)
		}
	}
	if compiled := configSchemaCompilations.Load() - before; compiled != 0 {
		t.Fatalf("the schema was compiled %d more times", compiled)
	}
}
