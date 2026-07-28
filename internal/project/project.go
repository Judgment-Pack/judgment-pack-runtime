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
//   - Every file access is rooted at the configuration's own directory through
//     internal/fssecure, and a declared path that escapes that directory is
//     refused when the configuration is validated and again when the file is read.
//     Containment is two checks — the lexical one and the canonicalized containing
//     directory — with one implementation, fssecure.ResolveWithin; no surface here
//     or elsewhere stops at the lexical half, including one that wants the resolved
//     path rather than the bytes.
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
	// ConfigVersion is the one configVersion value this runtime accepts. It is a
	// single integer as a string, on the outputVersion precedent and deliberately
	// not semantic versioning: this file describes a shape a program reads, and a
	// shape either is the one this program knows or is not.
	ConfigVersion = "1"
	// SchemaID is the embedded schema's own $id.
	SchemaID = "urn:judgmentpack:runtime:jpack-config:1"
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
func SupportedConfigVersions() []string { return []string{ConfigVersion} }

// Schema returns the exact embedded configuration schema bytes.
func Schema() []byte { return schemaBytes }

// SchemaDescription composes the metadata payload for the embedded schema, on
// the shape spec schema reports for a bundled JPS schema.
func SchemaDescription(command string) result.ConfigSchema {
	sum := sha256.Sum256(schemaBytes)
	return result.ConfigSchema{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "valid",
		Kind:          result.ProjectKind,
		ConfigVersion: ConfigVersion,
		SchemaID:      SchemaID,
		Bytes:         len(schemaBytes),
		SHA256:        hex.EncodeToString(sum[:]),
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

// Config is one jpack.json document. It is a closed shape: the embedded schema
// rejects every member not named here, so a misspelled key is an error rather
// than a silently ignored intention.
type Config struct {
	ConfigVersion string          `json:"configVersion"`
	Packs         map[string]Pack `json:"packs"`
}

// Project is one loaded configuration together with the directory every path in
// it resolves against — the configuration file's own directory, and the root of
// every read this package performs.
type Project struct {
	ConfigPath string
	Root       string
	Config     Config
	// IDs are the configured decision ids in sorted order, so every report this
	// package produces is ordered the same way on every run and in every process.
	IDs []string
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

// Load reads and validates one configuration.
//
// The order of the checks is deliberate. The version is read before the schema
// runs, so a configuration written for a later version of this convention is
// told exactly that instead of being buried in schema diagnostics about members
// this version does not know. The schema runs before anything is resolved, so a
// misspelled member is never read as an absent one. Path containment is checked
// last, once there are paths to check, and it is checked again at every read.
func Load(configPath string) (*Project, *Failure) {
	if fssecure.IsRemotePath(configPath) {
		return nil, &Failure{
			Code:     "JPS-PROJECT-CONFIG-PATH",
			Message:  "Remote filesystem paths are not supported for the project configuration; use a local file.",
			ExitCode: result.ExitIO,
		}
	}
	data, err := fssecure.ReadRegular(configPath, MaxConfigBytes)
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
	root, ok := document.(map[string]any)
	if !ok {
		return nil, &Failure{
			Code:     "JPS-PROJECT-CONFIG-SCHEMA",
			Message:  fmt.Sprintf("The project configuration %s must be a JSON object.", display.Sanitize(configPath)),
			ExitCode: result.ExitInvalid,
		}
	}
	if failure := declaredConfigVersion(configPath, root); failure != nil {
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
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return nil, &Failure{
			Code:     "JPS-PROJECT-CONFIG-PATH",
			Message:  fmt.Sprintf("The project configuration path %s could not be resolved.", display.Sanitize(configPath)),
			ExitCode: result.ExitIO,
		}
	}
	ids := make([]string, 0, len(config.Packs))
	for id := range config.Packs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return &Project{
		ConfigPath: configPath,
		Root:       filepath.Dir(absolute),
		Config:     config,
		IDs:        ids,
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

// ReadPack reads one configured pack document through the rooted reader.
func (p *Project) ReadPack(entry Pack) ([]byte, error) {
	return fssecure.ReadWithin(p.Root, entry.Path, carrier.HardMaxBytes)
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
