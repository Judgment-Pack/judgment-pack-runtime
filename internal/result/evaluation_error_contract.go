package result

// NewEvaluationErrorWith reports a refusal under an explicitly selected Core
// contract. The legacy constructor remains pinned to its original version.
func NewEvaluationErrorWith(command, status, class, phase, code, message string, contract EvaluatorContract) OperationalError {
	output := NewEvaluationError(command, status, class, phase, code, message)
	output.EvaluationError.EvaluatorSpecVersion = contract.Version()
	return output
}

// NewProfileUnsupported reports exactly the normative unsupported-profile
// diagnostic, without an input-derived message or an unrelated error class.
func NewProfileUnsupported(command, profile string) OperationalError {
	output := NewOperationalResult(command, "unsupported", "profile-unsupported", "")
	output.Diagnostics = []Diagnostic{{Code: "profile-unsupported", Profile: profile}}
	return output
}
