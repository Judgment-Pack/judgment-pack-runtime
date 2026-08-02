// Package project implements the jpack.json project convention (ADR-0012).
//
// The convention is NON-NORMATIVE and belongs to this runtime alone. It is not
// part of the Judgment Pack Specification, no other implementation is obliged to
// understand it, and nothing it declares changes what a pack means: a jpack.json
// says which packs a project owns and where their files are, so a CLI invocation,
// a CI step, and an agent's tool call can all name one decision by the same short
// id instead of by a path.
//
// Four determinations shape everything here, and each is recorded in ADR-0012:
//
//   - Identity lives in the pack document. The document's own id and version
//     members are the statement of what a pack is. The configuration's
//     expectedVersion pin, the optional filename convention, and the packId and
//     packVersion echo in an evaluation payload are all *validated references* to
//     that one statement — checked against it, never preferred over it, and never
//     a second place a version is declared.
//   - No templating and no targets. A templated pack was never the pack anyone
//     reviewed, and per-environment target blocks buy variation this convention
//     deliberately does not sell: one file per environment, by convention.
//   - Selection stays with the application. This configuration lists packs; it
//     does not choose one for a request. Choosing is the application's, and the
//     pack itself judges whether it applies.
//   - Every file access is bound to a handle held open on the configuration's own
//     directory through internal/fssecure, and a declared path that escapes that
//     directory is refused when the configuration is validated and again when the
//     file is read. Containment is two checks — the lexical one, and resolution
//     against the handle — and the second is a handle rather than a pathname so
//     that containment holds through the open instead of only up to it. No surface
//     here or elsewhere stops at the lexical half, and none hands back a pathname
//     for something else to open: a caller that wants a pack by decision id gets
//     the bytes, read through this project's own handle. The one thing a
//     configuration can ask to have written — the audit directory of ADR-0018 —
//     is written through that same handle, under the same rule.
//
// The hints a configuration may carry are guidance for an agent gathering inputs.
// This runtime never acts on one: it holds no credential, opens no network
// connection, and reads no source named by a hint (ADR-0004, ADR-0006).
package project

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

const (
	// DefaultConfigName is the configuration a surface looks for when the caller
	// names none and the environment does not either.
	DefaultConfigName = "jpack.json"
	// ConfigEnv names the environment variable that selects a configuration.
	ConfigEnv = "JPACK_CONFIG"
	// ConfigVersion is the newest configVersion this runtime accepts — the shape
	// the embedded schema describes in full. It is a single integer as a string,
	// on the outputVersion precedent and deliberately not semantic versioning:
	// this file describes a shape a program reads, and a shape either is one this
	// program knows or is not. "2" added the experimental graphs member
	// (ADR-0017) and "3" the audit member (ADR-0018); the earlier shapes, without
	// them, are still read — see SupportedConfigVersions.
	ConfigVersion = "3"
	// SchemaID is the embedded schema's own $id.
	SchemaID = "urn:judgmentpack:runtime:jpack-config:3"
	// MaxConfigBytes bounds one configuration document. It is an index of a
	// project's packs, not a pack.
	MaxConfigBytes = int64(1 << 20)
	// MaxMatrixBytes bounds one instance matrix, which carries facts documents and
	// expected dispositions and is read whole before a row runs.
	MaxMatrixBytes = int64(16 << 20)
	// MaxMatrixCases bounds the rows of one matrix.
	MaxMatrixCases = 10_000
)

//go:embed jpack.schema.json
var schemaBytes []byte

// SupportedConfigVersions names every configVersion this runtime accepts, so a
// refusal can say what would have been accepted instead of only what was not.
// A "1" configuration is exactly a "2" without graphs, and a "2" exactly a "3"
// without audit, so all three are read by one schema and each version gate
// lives in that schema's own bytes.
func SupportedConfigVersions() []string { return []string{"1", "2", ConfigVersion} }

// Schema returns the exact embedded configuration schema bytes.
func Schema() []byte { return schemaBytes }

// SchemaDescription composes the metadata payload for the embedded schema, on
// the shape spec schema reports for a bundled JPS schema.
func SchemaDescription(command string) result.ConfigSchema {
	sum := sha256.Sum256(schemaBytes)
	return result.ConfigSchema{
		OutputVersion:           result.OutputVersion,
		Tool:                    result.CurrentTool(),
		Command:                 command,
		Status:                  "valid",
		Kind:                    result.ProjectKind,
		ConfigVersion:           ConfigVersion,
		SupportedConfigVersions: SupportedConfigVersions(),
		SchemaID:                SchemaID,
		Bytes:                   len(schemaBytes),
		SHA256:                  hex.EncodeToString(sum[:]),
	}
}

// Failure reports why a project operation could not run. It mirrors the
// operational failures the rest of the runtime reports: a code, a message, and
// the exit class the CLI uses; the MCP surface reports the same message in band.
type Failure struct {
	Code     string
	Message  string
	ExitCode int
}

// Hint is one non-normative agent hint: where a value is held, and how to get it.
type Hint struct {
	Source string `json:"source,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

// Pack is one configured pack. Every path is relative to the configuration's own
// directory, and no member of this type carries a value the pack document also
// carries except ExpectedVersion, which is a reference checked against it.
type Pack struct {
	Path            string          `json:"path"`
	Matrix          string          `json:"matrix,omitempty"`
	Description     string          `json:"description,omitempty"`
	ExpectedVersion string          `json:"expectedVersion,omitempty"`
	Facts           map[string]Hint `json:"facts,omitempty"`
	Evidence        map[string]Hint `json:"evidence,omitempty"`
}

// Graph is one configured experimental graph (ADR-0017): the graph document,
// its optional matrix, and one line about it. Every path is relative to the
// configuration's own directory, and the entry carries no identity of its own
// — the graph document's id and version are the graph's, exactly as a pack
// entry's key is not the pack's id. Deliberately no expectedVersion pin: the
// graph format is experimental, and a pin is one more member to remove with it.
type Graph struct {
	Path        string `json:"path"`
	Rows        string `json:"rows,omitempty"`
	Description string `json:"description,omitempty"`
}

// Audit is the one thing a configuration can ask this runtime to write
// (ADR-0018): the directory an evaluation record is appended to, relative to
// the configuration's own directory and contained by it like every other
// declared path. The member is the whole of the opt-in — a configuration that
// does not carry it is a configuration nothing writes for.
type Audit struct {
	Dir string `json:"dir"`
}

// Config is one jpack.json document. It is a closed shape: the embedded schema
// rejects every member not named here, so a misspelled key is an error rather
// than a silently ignored intention. Graphs exists only under configVersion
// "2" and Audit only under "3" — the schema's own version gates hold that,
// each stated once in its bytes. Audit is a pointer because a single-object
// member has no other way to tell "declared, with defaults" from "absent"; a
// map member gets that distinction for free.
type Config struct {
	ConfigVersion string           `json:"configVersion"`
	Audit         *Audit           `json:"audit,omitempty"`
	Packs         map[string]Pack  `json:"packs"`
	Graphs        map[string]Graph `json:"graphs,omitempty"`
}

// Project is one loaded configuration together with the directory every path in
// it resolves against — the configuration file's own directory, and the root of
// every read this package performs.
//
// That directory is held open, not remembered as a pathname. The handle is opened
// before the configuration is read and the configuration is read through it, so
// the root is by construction the directory the configuration bytes came out of:
// there is no second derivation that could name a different directory, and no
// later rearrangement of the pathname can move the root a pack read is bounded
// by. Root is the same directory as a string, for messages only.
//
// A loaded Project owns an open descriptor. Close it.
type Project struct {
	ConfigPath string
	Root       string
	Config     Config
	// IDs are the configured decision ids in sorted order, so every report this
	// package produces is ordered the same way on every run and in every process.
	IDs []string
	// GraphIDs are the configured graph ids in the same sorted order, for the
	// same reason.
	GraphIDs []string

	root *fssecure.Root
}

// Close releases the project's directory handle.
func (p *Project) Close() error {
	if p == nil {
		return nil
	}
	return p.root.Close()
}

// Locate reports the configuration path a surface should use: the caller's
// explicit choice, else the environment's, else the default in the working
// directory. That order is the same on the CLI and on the MCP server, and the
// server has no flag, so a client configures it by launching the server in a
// project root or by setting the environment variable.
func Locate(explicit string) string {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return trimmed
	}
	if fromEnv := strings.TrimSpace(os.Getenv(ConfigEnv)); fromEnv != "" {
		return fromEnv
	}
	return DefaultConfigName
}

// Exists reports whether a readable regular configuration file is at path. It is
// how a surface tells "this project does not use the convention" — an answer —
// from "the configuration is broken", which is a failure.
func Exists(configPath string) bool {
	info, err := os.Lstat(configPath)
	return err == nil && info.Mode().IsRegular()
}

// Present reports whether anything at all is at path, whatever it is.
//
// It is the narrower question's opposite, and the two are not interchangeable.
// Exists asks whether a *readable regular file* is there, so a symlink, a
// directory, or a special file all answer "no" — which a surface then reads as
// "this project does not use the convention". On a surface that decides, that
// reading is a fail-open: the configuration is demonstrably there, it would
// demonstrably fail to load, and the run would report a decision anyway,
// unrecorded, for a project that asked to be recorded. So a deciding surface
// asks this question instead, and anything present-but-unloadable reaches Load
// and is refused with Load's own diagnostic. An error that is not "nothing is
// there" also answers yes, for the same fail-closed reason.
func Present(configPath string) bool {
	_, err := os.Lstat(configPath)
	return !errors.Is(err, os.ErrNotExist)
}

// Load reads and validates one configuration.
//
// The root is established first, by opening the configuration's own directory,
// and the configuration is then read through that handle. This order is what
// makes the root and the configuration one fact rather than two: deriving the
// root from the pathname *after* reading the bytes is a second, independent
// resolution, and an intermediate component retargeted between the two would
// leave a project whose pack reads are bounded by a directory the configuration
// never came from. Every later read — packs, matrices, the containment check
// packs validate reports — goes through this same handle.
//
// The order of the remaining checks is deliberate. The version is read before the
// schema runs, so a configuration written for a later version of this convention
// is told exactly that instead of being buried in schema diagnostics about
// members this version does not know. The schema runs before anything is
// resolved, so a misspelled member is never read as an absent one. Path
// containment is checked last, once there are paths to check, and it is checked
// again at every read.
//
// A returned Project holds the directory open; the caller closes it.
func Load(configPath string) (*Project, *Failure) {
	if fssecure.IsRemotePath(configPath) {
		return nil, &Failure{
			Code:     "JPS-PROJECT-CONFIG-PATH",
			Message:  "Remote filesystem paths are not supported for the project configuration; use a local file.",
			ExitCode: result.ExitIO,
		}
	}
	configDir, configName := filepath.Split(configPath)
	if configDir == "" {
		configDir = "."
	}
	root, err := fssecure.OpenRoot(configDir)
	if err != nil {
		return nil, &Failure{
			Code:     "JPS-PROJECT-CONFIG-READ",
			Message:  fmt.Sprintf("No project configuration could be read as one bounded regular file at %s. Pass --config, set %s, or add a %s at the project root.", display.Sanitize(configPath), ConfigEnv, DefaultConfigName),
			ExitCode: result.ExitIO,
		}
	}
	loaded, failure := loadThrough(root, configPath, configName)
	if failure != nil {
		// The Project was never built, so nothing will close the handle it would
		// have owned.
		root.Close()
		return nil, failure
	}
	return loaded, nil
}

// loadThrough reads and checks the configuration named by configName through an
// already-open root. Every refusal here abandons the load, which is why its one
// caller owns closing the handle.
func loadThrough(root *fssecure.Root, configPath, configName string) (*Project, *Failure) {
	data, err := root.Read(configName, MaxConfigBytes)
	if err != nil {
		if errors.Is(err, fssecure.ErrTooLarge) {
			return nil, &Failure{
				Code:     "JPS-RESOURCE-PROJECT-CONFIG-BYTE-LIMIT",
				Message:  fmt.Sprintf("The project configuration %s exceeds the %d-byte limit; it is an index of a project's packs, not a pack.", display.Sanitize(configPath), MaxConfigBytes),
				ExitCode: result.ExitIO,
			}
		}
		return nil, &Failure{
			Code:     "JPS-PROJECT-CONFIG-READ",
			Message:  fmt.Sprintf("No project configuration could be read as one bounded regular file at %s. Pass --config, set %s, or add a %s at the project root.", display.Sanitize(configPath), ConfigEnv, DefaultConfigName),
			ExitCode: result.ExitIO,
		}
	}
	document, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return nil, &Failure{
			Code:     "JPS-PROJECT-CONFIG-JSON",
			Message:  fmt.Sprintf("The project configuration %s is not acceptable JSON: %s", display.Sanitize(configPath), display.Sanitize(carrierFailure.Diagnostic.Message)),
			ExitCode: result.ExitInvocation,
		}
	}
	documentRoot, ok := document.(map[string]any)
	if !ok {
		return nil, &Failure{
			Code:     "JPS-PROJECT-CONFIG-SCHEMA",
			Message:  fmt.Sprintf("The project configuration %s must be a JSON object.", display.Sanitize(configPath)),
			ExitCode: result.ExitInvalid,
		}
	}
	if failure := declaredConfigVersion(configPath, documentRoot); failure != nil {
		return nil, failure
	}
	compiled, err := validation.CompileSchema(schemaBytes, SchemaID)
	if err != nil {
		return nil, &Failure{
			Code:     "JPS-ARTIFACT-PROJECT-SCHEMA",
			Message:  "The embedded project-configuration schema could not be compiled.",
			ExitCode: result.ExitInternal,
		}
	}
	if err := compiled.Validate(document); err != nil {
		return nil, &Failure{
			Code:     "JPS-PROJECT-CONFIG-SCHEMA",
			Message:  fmt.Sprintf("The project configuration %s does not satisfy the %s schema: %s", display.Sanitize(configPath), DefaultConfigName, schemaDetail(err)),
			ExitCode: result.ExitInvalid,
		}
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, &Failure{
			Code:     "JPS-PROJECT-CONFIG-SCHEMA",
			Message:  fmt.Sprintf("The project configuration %s satisfied its schema but could not be decoded.", display.Sanitize(configPath)),
			ExitCode: result.ExitInternal,
		}
	}
	ids := make([]string, 0, len(config.Packs))
	for id := range config.Packs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	graphIDs := make([]string, 0, len(config.Graphs))
	for id := range config.Graphs {
		graphIDs = append(graphIDs, id)
	}
	sort.Strings(graphIDs)
	return &Project{
		ConfigPath: configPath,
		// The handle's own directory, not a second derivation from configPath: the
		// string and the handle must not be able to disagree.
		Root:     root.Dir(),
		Config:   config,
		IDs:      ids,
		GraphIDs: graphIDs,
		root:     root,
	}, nil
}

// declaredConfigVersion holds one configuration to a configVersion this runtime
// accepts, before the schema is consulted.
//
// A single integer is the whole of the version: "1" is this convention's shape,
// and a later shape will be "2". There is no minor or patch component to
// negotiate, because there is nothing to negotiate — a program either knows the
// shape or does not. The refusal names what this runtime accepts, so a
// configuration from a newer toolchain produces an actionable sentence rather
// than a list of unknown members.
func declaredConfigVersion(configPath string, root map[string]any) *Failure {
	raw, present := root["configVersion"]
	if !present {
		return &Failure{
			Code:     "JPS-PROJECT-CONFIG-VERSION",
			Message:  fmt.Sprintf("The project configuration %s declares no configVersion. This runtime accepts: %s.", display.Sanitize(configPath), strings.Join(SupportedConfigVersions(), ", ")),
			ExitCode: result.ExitInvalid,
		}
	}
	declared, ok := raw.(string)
	if !ok {
		return &Failure{
			Code:     "JPS-PROJECT-CONFIG-VERSION",
			Message:  fmt.Sprintf("The configVersion of %s must be a string. This runtime accepts: %s.", display.Sanitize(configPath), strings.Join(SupportedConfigVersions(), ", ")),
			ExitCode: result.ExitInvalid,
		}
	}
	for _, supported := range SupportedConfigVersions() {
		if declared == supported {
			return nil
		}
	}
	return &Failure{
		Code:     "JPS-PROJECT-CONFIG-VERSION",
		Message:  fmt.Sprintf("The project configuration %s declares configVersion %q, which this runtime does not support. It accepts: %s.", display.Sanitize(configPath), display.Sanitize(declared), strings.Join(SupportedConfigVersions(), ", ")),
		ExitCode: result.ExitUnsupported,
	}
}

// schemaDetail compresses one schema error into a single sanitized line. The
// library's rendering is a multi-line tree; a configuration author needs the
// first thing that went wrong on one line, and the schema itself is printable
// with packs schema for the rest.
func schemaDetail(err error) string {
	detail := strings.Join(strings.Fields(err.Error()), " ")
	const limit = 400
	if len(detail) > limit {
		detail = detail[:limit] + "..."
	}
	return display.Sanitize(detail)
}

// Entry returns one configured pack by decision id.
func (p *Project) Entry(id string) (Pack, bool) {
	entry, ok := p.Config.Packs[id]
	return entry, ok
}

// UnknownPackFailure is the refusal every surface gives for a decision id the
// configuration does not name. It lists the configured ids, which is what makes
// the message enough to act on.
func (p *Project) UnknownPackFailure(id string) *Failure {
	known := "The configuration declares no packs."
	if len(p.IDs) > 0 {
		known = "Configured ids: " + strings.Join(p.IDs, ", ") + "."
	}
	return &Failure{
		Code:     "JPS-PROJECT-UNKNOWN-PACK",
		Message:  fmt.Sprintf("No pack in %s is named %q. %s", display.Sanitize(p.ConfigPath), display.Sanitize(id), known),
		ExitCode: result.ExitUnsupported,
	}
}

// selection is the decision ids one operation runs over: all of them, or the one
// the caller named.
func (p *Project) selection(id string) ([]string, *Failure) {
	if id == "" {
		return p.IDs, nil
	}
	if _, ok := p.Config.Packs[id]; !ok {
		return nil, p.UnknownPackFailure(id)
	}
	return []string{id}, nil
}

// ReadPack reads one configured pack document through the project's own
// directory handle, which is the only way anything here reaches a declared path.
func (p *Project) ReadPack(entry Pack) ([]byte, error) {
	return p.root.Read(entry.Path, carrier.HardMaxBytes)
}

// GraphEntry returns one configured graph by id.
func (p *Project) GraphEntry(id string) (Graph, bool) {
	entry, ok := p.Config.Graphs[id]
	return entry, ok
}

// UnknownGraphFailure is the refusal every surface gives for a graph id the
// configuration does not name, on UnknownPackFailure's shape: it lists the
// configured graph ids, which is what makes the message enough to act on.
func (p *Project) UnknownGraphFailure(id string) *Failure {
	known := "The configuration declares no graphs."
	if len(p.GraphIDs) > 0 {
		known = "Configured graph ids: " + strings.Join(p.GraphIDs, ", ") + "."
	}
	return &Failure{
		Code:     "JPS-PROJECT-UNKNOWN-GRAPH",
		Message:  fmt.Sprintf("No graph in %s is named %q. %s", display.Sanitize(p.ConfigPath), display.Sanitize(id), known),
		ExitCode: result.ExitUnsupported,
	}
}

// GraphSelection is the graph ids one operation runs over: all of them, or the
// one the caller named. It is exported, unlike the pack selection, because the
// graph walk lives in internal/graph — which already imports this package — and
// a second unexported copy of the same rule would be a place for the two to
// disagree.
func (p *Project) GraphSelection(id string) ([]string, *Failure) {
	if id == "" {
		return p.GraphIDs, nil
	}
	if _, ok := p.Config.Graphs[id]; !ok {
		return nil, p.UnknownGraphFailure(id)
	}
	return []string{id}, nil
}

// ReadGraph reads one configured graph document through the project's own
// directory handle. The byte limit is the caller's, unlike ReadPack's, because
// the graph limits live with the graph surface (MaxGraphBytes, MaxRowsBytes in
// internal/graph) and restating them here would be a second place for a number
// to drift; the deliberate part is that the only relative paths readable are
// ones the configuration declared, and no caller is ever handed a pathname.
func (p *Project) ReadGraph(entry Graph, limit int64) ([]byte, error) {
	return p.root.Read(entry.Path, limit)
}

// ReadGraphRows reads one configured graph's matrix through the same handle,
// under the same caller-owned limit rule as ReadGraph.
func (p *Project) ReadGraphRows(entry Graph, limit int64) ([]byte, error) {
	return p.root.Read(entry.Rows, limit)
}

// Contains reports whether one declared path is inside the project, without
// reading it — the containment question packs validate answers before it has
// read anything. It is decided against the same handle every read uses, so the
// check and the read cannot be about two different directories.
func (p *Project) Contains(relative string) error {
	return p.root.Contains(relative)
}

// AuditWriter returns the writer for the audit trail this configuration asks
// for, or nil when it asks for none — which is what a caller that never checks
// gets right by default, because the returned writer's nil value writes
// nothing. It is bound to the same handle every read is bound to, so the
// records go into the directory the configuration came out of and no pathname
// is handed to anything.
func (p *Project) AuditWriter() *audit.Writer {
	if p == nil || p.Config.Audit == nil {
		return nil
	}
	return audit.NewWriter(p.root, p.Config.Audit.Dir)
}

// ReadFailureMessage turns one failure to obtain a configured file into a
// sentence naming which of three different things went wrong: the path left the
// project, the file could not be read, or it was read and did not decode. They
// call for three different fixes, and a single "could not be read" would send a
// reader looking for a missing file that is sitting right there.
func ReadFailureMessage(relative string, err error) string {
	switch {
	case errors.Is(err, fssecure.ErrOutsideRoot):
		return fmt.Sprintf("The path %q resolves outside the configuration's own directory, which no configured path may.", display.Sanitize(relative))
	case errors.Is(err, fssecure.ErrTooLarge):
		return fmt.Sprintf("The file %q exceeds this runtime's documented byte limit.", display.Sanitize(relative))
	case errors.Is(err, os.ErrNotExist), errors.Is(err, os.ErrPermission):
		return fmt.Sprintf("The file %q could not be read as one bounded regular file inside the configuration's own directory.", display.Sanitize(relative))
	default:
		// Everything left is a decode failure carrying its own detail, or a read
		// failure the operating system described in its own words. Both are more
		// useful than a sentence that replaces them.
		return fmt.Sprintf("The file %q could not be used: %s", display.Sanitize(relative), display.Sanitize(err.Error()))
	}
}
