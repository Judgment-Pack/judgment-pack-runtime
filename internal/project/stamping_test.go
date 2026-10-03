package project

import (
	"strings"
	"testing"
)

// The audit member's timestampAuthority is the "6" shape's, like chain and
// signingKey: refused under an earlier version naming "6", refused with chain
// false, and an http or https address only.
func TestConfigVersionSixMayNameATimestampAuthority(t *testing.T) {
	for _, version := range []string{"3", "4", "5"} {
		_, failure := Load(writeProject(t, `{"configVersion":"`+version+`","audit":{"dir":"audit","timestampAuthority":"https://tsa.example/"},"packs":{}}`, nil))
		if failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" || !strings.Contains(failure.Message, "'6'") {
			t.Fatalf("timestampAuthority under %s names the version to change: %+v", version, failure)
		}
	}
	for _, config := range []string{
		`{"configVersion":"6","audit":{"dir":"audit","chain":false,"timestampAuthority":"https://tsa.example/"},"packs":{}}`,
		`{"configVersion":"6","audit":{"dir":"audit","timestampAuthority":"ftp://tsa.example/"},"packs":{}}`,
		`{"configVersion":"6","audit":{"dir":"audit","timestampAuthority":"https://tsa example/"},"packs":{}}`,
		`{"configVersion":"6","audit":{"dir":"audit","timestampAuthority":7},"packs":{}}`,
	} {
		if _, failure := Load(writeProject(t, config, nil)); failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" {
			t.Fatalf("%s is refused: %+v", config, failure)
		}
	}
	for _, address := range []string{"https://tsa.example/", "http://tsa.example:8080/stamp"} {
		loaded := mustLoad(t, writeProject(t, `{"configVersion":"6","audit":{"dir":"audit","timestampAuthority":"`+address+`"},"packs":{}}`, nil))
		if loaded.Config.Audit.TimestampAuthority != address {
			t.Fatalf("%s: %+v", address, loaded.Config.Audit)
		}
	}
}
