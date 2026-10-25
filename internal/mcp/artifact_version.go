package mcp

import (
	"encoding/json"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifactversion"
)

func artifactVersionDefinition() map[string]any {
	version := func(description string) map[string]any {
		return map[string]any{"type": "string", "pattern": artifactversion.Pattern, "maxLength": artifactversion.MaxVersionBytes, "description": description}
	}
	return map[string]any{"name": "get_artifact_version_options", "description": "Calculate exact patch/minor/major options from a saved x.y.z artifact version, and optionally compare a candidate numerically. No content checks, compatibility decision, identity reservation, format migration or save permission. No project/file access. Choose the bump only after reviewing compatibility.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"base"}, "properties": map[string]any{"base": version("Saved artifact version, not a format version or edit counter."), "candidate": version("Optional proposed version for numeric comparison.")}}}
}
func (s *Server) toolArtifactVersion(args json.RawMessage) any {
	const name = "get_artifact_version_options"
	if m := exactMembers(name, args, "base", "candidate"); m != "" {
		return toolError(m)
	}
	var input struct {
		Base      *string         `json:"base"`
		Candidate json.RawMessage `json:"candidate"`
	}
	if json.Unmarshal(args, &input) != nil || input.Base == nil {
		return toolError("Provide base as a three-component version string.")
	}
	var candidate *string
	if input.Candidate != nil {
		var value *string
		if json.Unmarshal(input.Candidate, &value) != nil || value == nil {
			return toolError("Provide candidate as a three-component version string.")
		}
		candidate = value
	}
	report, code := artifactversion.Options("mcp "+name, *input.Base, candidate)
	reply := toolResult(report)
	if code != 0 {
		reply["isError"] = true
	}
	return reply
}
