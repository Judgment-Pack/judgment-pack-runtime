package validation

import (
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

func TestCore030RouteDiagnosticsDoNotChangeLegacyMappings(t *testing.T) {
	for _, item := range []struct {
		name       string
		failure    *jsonschema.ValidationError
		coreCode   string
		legacyCode string
		path       string
	}{
		{"exclusive-target", &jsonschema.ValidationError{InstanceLocation: []string{"escalation"}, ErrorKind: &kind.OneOf{}}, "JPS-STRUCTURE-ONEOF", "JPS-STRUCTURE-CONDITION-SHAPE", "/escalation"},
		{"route-ceiling", &jsonschema.ValidationError{InstanceLocation: []string{"escalation", "routes"}, ErrorKind: &kind.MaxItems{}}, "JPS-STRUCTURE-MAXITEMS", "JPS-STRUCTURE-SCHEMA", "/escalation/routes"},
		{"trigger-subset", &jsonschema.ValidationError{InstanceLocation: []string{"escalation", "routes", "0", "triggers"}, ErrorKind: &kind.Not{}}, "JPS-STRUCTURE-NOT", "JPS-STRUCTURE-SCHEMA", "/escalation/routes/0/triggers"},
	} {
		t.Run(item.name, func(t *testing.T) {
			for _, core030 := range []bool{false, true} {
				collector := newDiagnosticCollector()
				collectStructural(item.failure, collector, core030)
				want := item.legacyCode
				if core030 {
					want = item.coreCode
				}
				actual := collector.sorted()
				if len(actual) != 1 || !hasDiagnostic(actual, want, item.path) {
					t.Fatalf("Core 0.3=%v: %+v", core030, actual)
				}
			}
		})
	}
}
