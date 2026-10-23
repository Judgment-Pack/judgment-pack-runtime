package validation

import (
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// NegotiateProfile performs only bounded carrier parsing and Core profile
// negotiation. It neither validates the pack schema nor admits evaluation.
func NegotiateProfile(data []byte, limits carrier.Limits, supported []string) (map[string]any, *result.Diagnostic, *carrier.Failure) {
	root, failure := carrier.ScanRootDeclarations(data, limits)
	if failure != nil {
		return nil, nil, failure
	}
	if root["specVersion"] != "0.3.0" {
		return root, nil, nil
	}
	profile := "base"
	if raw, present := root["profile"]; present {
		var ok bool
		profile, ok = raw.(string)
		if !ok {
			d := result.ErrorDiagnostic("JPS-STRUCTURE-TYPE", "structural", "/profile", "Profile must be a string.")
			return root, &d, nil
		}
		if !slices.Contains([]string{"base", "extended"}, profile) {
			d := result.ErrorDiagnostic("JPS-STRUCTURE-ENUM", "structural", "/profile", "Profile must be base or extended.")
			return root, &d, nil
		}
	}
	if supported == nil {
		supported = []string{"base"}
	}
	// This reader implements base only. A caller cannot enable extended ceilings.
	if profile != "base" || !slices.Contains(supported, profile) {
		return root, &result.Diagnostic{Code: "profile-unsupported", Profile: profile}, nil
	}
	return root, nil, nil
}
