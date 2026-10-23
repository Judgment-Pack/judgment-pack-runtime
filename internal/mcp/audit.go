package mcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	auditListTool         = "experimental_list_audit"
	auditVerifyTool       = "experimental_verify_audit"
	maxAuditExpectBytes   = 1 << 20
	maxAuditSuppliedBytes = 8 << 20
	maxAuditResponseBytes = 4 << 20
)

func auditListDefinition() map[string]any {
	return map[string]any{
		"name":        auditListTool,
		"description": "Export the configured audit evaluations, signatures and stamps read-only, in fixed file order. Exact base64 byte chunks and line results preserve all retained members and bytes, including malformed or incomplete data. A cursor continues one bounded snapshot; retry the latest answered cursor idempotently. Intermediate pages are provisional; only complete:true finishes an export. Appends are excluded, changes invalidate export. At most 64 MiB per file, 8 live exports for 5 minutes, 256 KiB raw bytes and 128 line results per page; decoded members/raw-line metadata cover lines up to 64 KiB. No caller file path, repair, keys, network or evaluation. Experimental runtime convention; no new conformance claim.",
		"inputSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"cursor":     map[string]any{"type": "string", "minLength": 1, "maxLength": 85},
			"page_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": audit.MaxWirePage, "default": audit.DefaultWirePage},
		}},
	}
}

func auditVerifyDefinition() map[string]any {
	return map[string]any{
		"name":        auditVerifyTool,
		"description": "Verify the configured audit trail with the same implementation and full report as CLI audit verify. expect is bounded supplied checkpoint JSONL (CLI --expect); trail_base64 optionally supplies exact trail bytes (content equivalent of CLI --trail), never a host path. Both modes require the server's configured audit project. require_checkpoint_through has CLI semantics. No signature keys, timestamp trust roots, witness inputs, repair or writes; unchecked protections remain not-checked. No network. At most 64 MiB configured trail, 8 MiB supplied trail and 1 MiB expectations. Experimental runtime convention; no new conformance claim.",
		"inputSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"expect":                     map[string]any{"type": "string", "maxLength": maxAuditExpectBytes},
			"trail_base64":               map[string]any{"type": "string", "maxLength": base64.StdEncoding.EncodedLen(maxAuditSuppliedBytes)},
			"require_checkpoint_through": map[string]any{"type": "integer", "minimum": 0, "maximum": int64(9007199254740990)},
		}},
	}
}

// auditArguments keeps standalone handlers strict too. Refusals never repeat
// unknown names, cursor text, retained bytes, configuration or key paths.
func auditArguments(raw json.RawMessage, allowed ...string) (map[string]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}
	if len(trimmed) == 0 || trimmed[0] != '{' || membersOnce(trimmed, allowed) != "" {
		return nil, false
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(trimmed, &args) != nil {
		return nil, false
	}
	for name := range args {
		found := false
		for _, known := range allowed {
			found = found || name == known
		}
		if !found {
			return nil, false
		}
	}
	return args, true
}

func auditInteger(raw json.RawMessage, fallback, max int64) (int64, bool) {
	if len(raw) == 0 {
		return fallback, true
	}
	n, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
	return n, err == nil && n >= 0 && n <= max
}

func loadAuditProject() (*project.Project, string) {
	configPath := project.Locate("")
	if !project.Present(configPath) {
		return nil, "No configured project is present; audit tools require a project declaring an audit directory."
	}
	loaded, failure := project.Load(configPath)
	if failure != nil {
		return nil, "The configured audit project could not be loaded."
	}
	if _, declared := loaded.TrailName(); !declared {
		loaded.Close()
		return nil, "The configured project declares no audit directory."
	}
	if err := loaded.ContainsDir(loaded.Config.Audit.Dir); err != nil {
		loaded.Close()
		return nil, "The configured audit directory is not a contained project directory."
	}
	return loaded, ""
}

func auditWireSource(loaded *project.Project) audit.WireSource {
	return audit.WireSource{
		Authority: loaded.Root + "\x00" + loaded.ConfigDigest,
		Open:      [3]func() (*os.File, error){loaded.OpenTrail, loaded.OpenSidecar, loaded.OpenStamps},
	}
}

func boundedAuditResult(report any) any {
	wrapped := toolResult(report)
	encoded, err := json.Marshal(wrapped)
	if err != nil || len(encoded) > maxAuditResponseBytes {
		return toolError("The audit response exceeds the 4 MiB serialized result limit; no complete result was produced.")
	}
	return wrapped
}

func (s *Server) toolListAudit(raw json.RawMessage) any {
	args, ok := auditArguments(raw, "cursor", "page_bytes")
	if !ok {
		return toolError("Supply only cursor and page_bytes once, with their exact spellings, in an object.")
	}
	cursor, present, message := textArgument("cursor", args["cursor"])
	if message != "" || present && (len(cursor) == 0 || len(cursor) > 85) {
		return toolError("The audit cursor must be a nonempty string of at most 85 bytes; omit it to start an export.")
	}
	pageBytes, ok := auditInteger(args["page_bytes"], audit.DefaultWirePage, audit.MaxWirePage)
	if !ok || pageBytes == 0 {
		return toolError(audit.ErrWirePage.Error())
	}
	loaded, refusal := loadAuditProject()
	if refusal != "" {
		return toolError(refusal)
	}
	defer loaded.Close()
	page, err := s.auditPager.Page(auditWireSource(loaded), cursor, int(pageBytes))
	if err != nil {
		return toolError(err.Error())
	}
	return boundedAuditResult(page)
}

func (s *Server) toolVerifyAudit(raw json.RawMessage) any {
	args, ok := auditArguments(raw, "expect", "trail_base64", "require_checkpoint_through")
	if !ok {
		return toolError("Supply only expect, trail_base64 and require_checkpoint_through once, with their exact spellings, in an object.")
	}
	expect, expectPresent, message := textArgument("expect", args["expect"])
	if message != "" || len(expect) > maxAuditExpectBytes {
		return toolError("The audit expect must be checkpoint JSONL text of at most 1 MiB.")
	}
	held := []result.AuditCheckpoint{}
	if expectPresent {
		var err error
		held, err = audit.ParseCheckpoints([]byte(expect))
		if err != nil {
			return toolError("The supplied audit checkpoints are not supported checkpoint JSONL.")
		}
	}
	trail, trailPresent, message := textArgument("trail_base64", args["trail_base64"])
	if message != "" || len(trail) > base64.StdEncoding.EncodedLen(maxAuditSuppliedBytes) {
		return toolError("The audit trail_base64 must encode at most 8 MiB of exact trail bytes.")
	}
	var supplied []byte
	if trailPresent {
		var err error
		supplied, err = base64.StdEncoding.Strict().DecodeString(trail)
		// Strict base64 still accepts CR/LF. Require the one canonical
		// encoding to avoid ambiguous limits and hidden alternate forms.
		if err != nil || len(supplied) > maxAuditSuppliedBytes || base64.StdEncoding.EncodeToString(supplied) != trail {
			return toolError("The audit trail_base64 must be canonical padded base64 of at most 8 MiB.")
		}
	}
	through, ok := auditInteger(args["require_checkpoint_through"], 0, 9007199254740990)
	if !ok {
		return toolError("The audit require_checkpoint_through must be an integer from 0 to 9007199254740990.")
	}
	loaded, refusal := loadAuditProject()
	if refusal != "" {
		return toolError(refusal)
	}
	defer loaded.Close()
	options := audit.Options{Held: held, RequireThrough: through}
	var report audit.Report
	var err error
	locked := false
	shownPath, _ := loaded.TrailName()
	if trailPresent {
		report, err = audit.Verify(bytes.NewReader(supplied), int64(len(supplied)), options)
		shownPath = "(supplied trail)"
	} else {
		files, sizes, betweenWrites, openErr := fssecure.OpenSnapshot(loaded.OpenTrail)
		if openErr != nil {
			if errors.Is(openErr, os.ErrNotExist) {
				return toolError("The configured audit trail does not exist yet; no record has been written.")
			}
			return toolError(audit.ErrWireRead.Error())
		}
		defer files[0].Close()
		if sizes[0] > audit.MaxWireFileBytes {
			return toolError(audit.ErrWireLimit.Error())
		}
		locked = betweenWrites
		report, err = audit.Verify(files[0], sizes[0], options)
	}
	if err != nil {
		return toolError("The audit trail could not be read completely for verification.")
	}
	return boundedAuditResult(result.AuditVerification{
		OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: "mcp " + auditVerifyTool,
		TrailPath: shownPath, SnapshotBetweenWrites: locked, AuditChain: report.Chain,
	})
}
