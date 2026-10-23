package policy

import (
	"bytes"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func TestPrototypeURICompatibilityIsExplicitAndScoped(t *testing.T) {
	// Preserve both directions of the historical disagreement. The compatibility
	// predicate is not the RFC 3986 assertion used by ordinary Core/official Policy.
	for _, tc := range []struct {
		id          string
		legacyValid bool
	}{
		{"urn:test:[x]", true}, {"https://example.invalid/[x]", true},
		{"https://%65xample.invalid/a", false}, {"foo://[v1.a]/", false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			f := loadFixture(t, "refunds")
			f.policy["id"] = tc.id
			report, exit := Validate("validate", mustJSON(t, f.policy))
			if (exit == 0) != tc.legacyValid {
				t.Fatalf("prototype document compatibility: %+v %d", report, exit)
			}
			init, exit := Init("init", "policy", ProfileVersion, tc.id, "1.0.0")
			if (exit == 0) != tc.legacyValid {
				t.Fatalf("prototype init compatibility: %+v %d", init, exit)
			}

			f = loadFixture(t, "refunds")
			reference := object(f.adoption["consumer"])
			pack := bytes.Replace(f.raw["consumer-pack.json"], mustJSON(t, reference["id"]), mustJSON(t, tc.id), 1)
			reference["id"] = tc.id
			f.retainConsumer(t, pack)
			report, exit = Check("check", mustJSON(t, f.adoption), f.wire(t), engine(t))
			if (exit == 0) != tc.legacyValid {
				t.Fatalf("prototype retained consumer compatibility: %+v %d", report, exit)
			}
			if !bytes.Equal(f.raw["consumer-pack.json"], pack) || text(object(reference["content"])["bytesDigest"]) != digest(pack) {
				t.Fatal("prototype identity changed")
			}

			// A compatibility check must leave the shared engine's default untouched.
			core, failure := engine(t).Validate(pack, validation.Options{Through: "structural", Limits: limits()})
			if failure != nil || (core.Status == "valid") == tc.legacyValid {
				t.Fatalf("prototype assertion leaked into ordinary Core: %+v %+v", core, failure)
			}
		})
	}
}
