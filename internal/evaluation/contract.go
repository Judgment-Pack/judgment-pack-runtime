package evaluation

import (
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func (o Options) contract() result.EvaluatorContract {
	c := result.EvaluatorContract{SpecVersion: o.SpecVersion}
	if c.Core030Rules() {
		c.Profile = "base"
	}
	return c
}

// SelectContract scans exact pack bytes and negotiates profiles, without schema
// validation or evaluation admission. It never reads facts or evidence. Missing
// and invalid versions are refused; no evaluator version is inferred from routes.
func SelectContract(pack []byte) (result.EvaluatorContract, *Failure) {
	if failure := byteLimit("pack", pack, result.ClassPackNotConformant, false); failure != nil {
		return result.EvaluatorContract{}, failure
	}
	root, diagnostic, scanFailure := validation.NegotiateProfile(pack, carrier.DefaultLimits(), nil)
	if scanFailure != nil {
		return result.EvaluatorContract{}, decodeFailure("pack", result.ClassPackNotConformant, scanFailure)
	}
	if diagnostic != nil {
		if diagnostic.Code == "profile-unsupported" {
			return result.EvaluatorContract{}, &Failure{Code: diagnostic.Code, Profile: diagnostic.Profile, ExitCode: result.ExitUnsupported, Phase: result.PhasePreflight}
		}
		return result.EvaluatorContract{}, &Failure{Class: result.ClassPackNotConformant, Phase: result.PhasePreflight, Code: diagnostic.Code, Message: diagnostic.Message, ExitCode: result.ExitInvalid}
	}
	version, ok := root["specVersion"].(string)
	if !ok || version == "" {
		return result.EvaluatorContract{}, &Failure{Code: "JPS-EVALUATION-PACK-SPEC-VERSION", Class: result.ClassPackNotConformant, Phase: result.PhasePreflight, Message: "The pack must declare an exact supported evaluator specVersion.", ExitCode: result.ExitInvalid}
	}
	contract := result.EvaluatorContract{SpecVersion: version}
	if contract.Core030Rules() {
		contract.Profile = "base"
	}
	if err := contract.Validate(); err != nil {
		return result.EvaluatorContract{}, declaredSpecVersionWith(version, result.LegacyEvaluatorContract())
	}
	return contract, nil
}

func declaredSpecVersionWith(version string, contract result.EvaluatorContract) *Failure {
	if contract.Version() == result.EvaluatorSpecVersion {
		return declaredSpecVersion(version)
	}
	if err := contract.Validate(); err != nil {
		return &Failure{Code: "JPS-EVALUATION-CONTRACT", Message: err.Error(), ExitCode: result.ExitUnsupported}
	}
	if version == contract.Version() {
		return nil
	}
	return &Failure{Class: result.ClassPackNotConformant, Phase: result.PhasePreflight, Code: "JPS-EVALUATION-PACK-SPEC-VERSION", Message: fmt.Sprintf("The pack declares specVersion %q; this operation applies Core %s.", version, contract.Version()), ExitCode: result.ExitInvalid}
}

// StagedDraftUnsupported refuses a pack declaring the staged draft selector on
// a surface that does not read it. The selector is admitted EXPERIMENTALLY by
// the surfaces that evaluate through this engine's admission, which reads the
// pack's evaluation form; a surface that reads a pack's conditions or layout
// some other way refuses it here, explicitly, rather than reading members it
// was not written for. surface names the refusing surface in the message.
func StagedDraftUnsupported(surface string) *Failure {
	return &Failure{
		Code:     result.StagedDraftUnsupportedCode,
		Phase:    result.PhasePreflight,
		Message:  result.StagedDraftUnsupportedMessage(surface),
		ExitCode: result.ExitUnsupported,
	}
}
