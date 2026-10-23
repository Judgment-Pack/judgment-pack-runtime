package mcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	auditListTool         = "experimental_list_audit"
	auditVerifyTool       = "experimental_verify_audit"
	auditHistoryTool      = "experimental_list_audit_history"
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
	return loadAuditProjectFor(false)
}

func loadAuditProjectFor(history bool) (*project.Project, string) {
	configPath := project.Locate("")
	if !project.Present(configPath) {
		if history {
			return nil, "No configured project is present; history requires a present, loadable project."
		}
		return nil, "No configured project is present; audit tools require a project declaring an audit directory."
	}
	loaded, failure := project.Load(configPath)
	if failure != nil {
		return nil, "The configured audit project could not be loaded."
	}
	if _, declared := loaded.TrailName(); !declared {
		if history {
			return loaded, ""
		}
		loaded.Close()
		return nil, "The configured project declares no audit directory."
	}
	if err := loaded.ContainsDir(loaded.Config.Audit.Dir); err != nil {
		loaded.Close()
		return nil, "The configured audit directory is not a contained project directory."
	}
	return loaded, ""
}

func auditHistoryDefinition() map[string]any {
	return map[string]any{"name": auditHistoryTool, "description": "Read configured definition history with Desk's exact 4 MiB backward window, newest-first 50-record selection, typed Go JSON decoding and unread/next semantics. Start with kind (pack or graph), id and optional before byte offset; continue with only cursor and optional page_bytes. Transport is bounded base64 chunks of one server-authored JSON result, never retained JSONL for client selection. Exact raw record strings/digests include CR and whitespace; surrogate replacement affects typed selection only. Immutable answer, configured authority checked on every continuation; shares eight five-minute slots with raw exports. 64 MiB source, 8 MiB plus 64 KiB projection, 256 KiB raw chunk and 4 MiB serialized-result ceilings. No caller path, evaluation, writes, keys or network. Experimental Runtime convention; no new Core claim.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"kind":       map[string]any{"type": "string", "enum": []string{"pack", "graph"}},
		"id":         map[string]any{"type": "string", "minLength": 1, "maxLength": 2048},
		"before":     map[string]any{"type": "integer", "minimum": 0, "maximum": int64(9007199254740990)},
		"cursor":     map[string]any{"type": "string", "minLength": 1, "maxLength": 85},
		"page_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": audit.MaxWirePage, "default": audit.DefaultWirePage},
	}}}
}

func (s *Server) toolAuditHistory(raw json.RawMessage) any {
	args, ok := auditArguments(raw, "kind", "id", "before", "cursor", "page_bytes")
	if !ok {
		return toolError(audit.ErrHistoryArguments.Error())
	}
	if _, badUnicode := carrier.UnpairedSurrogateEscape(raw); badUnicode || !utf8.Valid(raw) {
		return toolError(audit.ErrHistoryArguments.Error())
	}
	cursor, present, message := textArgument("cursor", args["cursor"])
	if message != "" || present && (len(cursor) == 0 || len(cursor) > 85) {
		return toolError(audit.ErrWireCursor.Error())
	}
	pageBytes, ok := auditInteger(args["page_bytes"], audit.DefaultWirePage, audit.MaxWirePage)
	if !ok || pageBytes == 0 {
		return toolError(audit.ErrWirePage.Error())
	}
	var request *audit.HistoryRequest
	if present {
		if args["kind"] != nil || args["id"] != nil || args["before"] != nil {
			return toolError(audit.ErrHistoryArguments.Error())
		}
	} else {
		kind, _, badKind := textArgument("kind", args["kind"])
		id, _, badID := textArgument("id", args["id"])
		before, ok := auditInteger(args["before"], -1, 9007199254740990)
		if badKind != "" || badID != "" || (kind != "pack" && kind != "graph") || len(id) == 0 || len(id) > 2048 || !ok {
			return toolError(audit.ErrHistoryArguments.Error())
		}
		request = &audit.HistoryRequest{Kind: kind, ID: id, Before: before}
	}
	loaded, refusal := loadAuditProjectFor(true)
	if refusal != "" {
		return toolError(refusal)
	}
	defer loaded.Close()
	_, declared := loaded.TrailName()
	page, err := s.auditPager.HistoryPage(auditWireSource(loaded), declared, cursor, request, int(pageBytes))
	if err != nil {
		return toolError(err.Error())
	}
	response := boundedAuditResult(page)
	if response.(map[string]any)["isError"] == true {
		s.auditPager.Discard(page.NextCursor)
		s.auditPager.Discard(cursor)
	}
	return response
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
	response := boundedAuditResult(page)
	if response.(map[string]any)["isError"] == true {
		// The rejected response cannot deliver its continuation. Invalidate
		// the whole export, including final-page retry state, so the caller
		// can immediately restart with a smaller raw-page size.
		s.auditPager.Discard(page.NextCursor)
		s.auditPager.Discard(cursor)
	}
	return response
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
