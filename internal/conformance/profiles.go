package conformance

import (
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// A manifest that names readerProfiles (Core 0.5.0 onward) runs a case whose
// fixture declares a profile other than base under a reader that supports
// it. This reader implements base only: such a case is reported as skipped,
// not run, and what is checked is that the reader refused the fixture's
// profile with the one diagnostic of §4.1 rather than reading it as base.

// declaredProfile reads the fixture's root profile declaration without
// validating it: "" when the fixture declares none or cannot be scanned.
func declaredProfile(fixture []byte) string {
	root, failure := carrier.ScanRootDeclarations(fixture, carrier.DefaultLimits())
	if failure != nil {
		return ""
	}
	profile, _ := root["profile"].(string)
	return profile
}

// readerProfileCase reports whether a case is run by a reader of another
// profile: its fixture declares, as a string, a profile the manifest's
// readerProfiles define and that is not base, the one this reader implements.
// A fixture declaring a profile no reader runs (a value outside the profile
// enum, or one the manifest does not list) is an ordinary case: its expected
// finding, a refusal of the declaration, is checked like any other.
func readerProfileCase(profile string, readerProfiles []string) bool {
	return profile != "" && profile != "base" && slices.Contains(readerProfiles, profile)
}

// skippedProfileCase reports a case written for a reader profile this
// reader does not implement. It passes as skipped only when the reader
// refused the fixture's profile; reading it any other way is a mismatch.
func skippedProfileCase(id, expected, profile string, actual result.Validation) result.Case {
	caseResult := result.Case{
		ID:                id,
		ExpectedStatus:    expected,
		ActualStatus:      actual.Status,
		Status:            "skipped",
		SkipReason:        "the fixture declares the " + profile + " profile; this reader implements base only, and refused it as profile-unsupported",
		ActualDiagnostics: actual.Diagnostics,
	}
	if caseResult.ActualDiagnostics == nil {
		caseResult.ActualDiagnostics = []result.Diagnostic{}
	}
	refused := actual.Status == "unsupported" && len(actual.Diagnostics) == 1 && actual.Diagnostics[0].Code == "profile-unsupported"
	if !refused {
		caseResult.Status = "mismatch"
		caseResult.SkipReason = ""
	}
	return caseResult
}
