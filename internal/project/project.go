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
	"slices"
	"sort"
	"strconv"
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
	// MaxLockBytes bounds one reviewed-set lock (ADR-0019). It is one digest per
	// declared document, so the configuration's own limit is more than enough
	// and stating it separately keeps the two numbers from being read as one
	// rule about "the project's files".
	MaxLockBytes = int64(1 << 20)
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
	// ConfigDigest names the exact bytes this configuration was decoded from,
	// taken where those bytes and this decoded shape are known to be one thing.
	// The reviewed-set lock (ADR-0019) is what needs it: the configuration is
	// the law's index, so a lock that did not pin it would leave the one file
	// that says which packs count unpinned.
	ConfigDigest string
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
		Root:         root.Dir(),
		Config:       config,
		ConfigDigest: audit.Digest(data),
		IDs:          ids,
		GraphIDs:     graphIDs,
		root:         root,
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
	message := fmt.Sprintf("The project configuration %s declares configVersion %q, which this runtime does not support. It accepts: %s.", display.Sanitize(configPath), display.Sanitize(declared), strings.Join(SupportedConfigVersions(), ", "))
	// A declared version above what this runtime reads means the runtime is
	// behind, not that the configuration is wrong. Say which side to change:
	// left ambiguous, a reader — human or agent — reaches for the edit that
	// makes the refusal go away, and editing the declaration down silently
	// discards whatever the newer version declares.
	if newerThanSupported(declared) {
		message += " This configuration comes from a newer toolchain: upgrade the runtime. Do not edit the declaration to an older version — that discards what this configuration declares."
	}
	return &Failure{
		Code:     "JPS-PROJECT-CONFIG-VERSION",
		Message:  message,
		ExitCode: result.ExitUnsupported,
	}
}

// newerThanSupported reports whether a declared configVersion is a plain
// integer above the newest one this runtime reads. Only that case earns the
// upgrade steer; an unparseable declaration says nothing about which side is
// behind.
func newerThanSupported(declared string) bool {
	newest, err := strconv.Atoi(ConfigVersion)
	if err != nil {
		return false
	}
	value, err := strconv.Atoi(declared)
	return err == nil && value > newest
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

// ContainsDir reports whether one declared directory is inside the project,
// resolving an existing final component rather than leaving it unresolved as
// Contains does. That difference is the point: a declared directory becomes an
// intermediate component of every path written beneath it, and an intermediate
// symlink out of the project is precisely what the handle refuses at write time
// — so the validate-time check has to ask the same question the write will.
func (p *Project) ContainsDir(relative string) error {
	return p.root.ContainsDir(relative)
}

// LockName is the reviewed-set lock's filename for this project: the
// configuration's own name with ".lock" before its extension — jpack.json's
// lock is jpack.lock.json, and jpack.staging.json's is
// jpack.staging.lock.json. The second result is false when the configuration's
// filename does not end in ".json", which is the one case the derivation cannot
// answer; see LockNameFor.
//
// It is a convention and never a declaration. A configuration member pointing
// at its own lock would be circular: the lock pins the configuration, so a
// configuration that named it could rename it, and a reader following the
// rename would verify against whatever the edit chose. Derivation cannot be
// edited without editing the configuration's own filename, which is the
// invocation's business rather than the configuration's.
func (p *Project) LockName() (string, bool) {
	_, configName := filepath.Split(p.ConfigPath)
	return LockNameFor(configName)
}

// LockNameFor derives one configuration filename's lock filename, and reports
// whether the derivation is available at all. It is exported for the surfaces
// that must name the file in a message before a project is loaded.
//
// A filename that does not end in ".json" has no lock name here, and that
// refusal is what makes the derivation one-to-one. Trimming a ".json" that is
// not there would map both "jpack.json" and a configuration literally named
// "jpack" onto one "jpack.lock.json", and two projects in one directory sharing
// one reviewed set is two projects each denying the other the convention. The
// configuration's own loader imposes no extension, so this is the place the
// requirement is stated.
func LockNameFor(configName string) (string, bool) {
	if !strings.HasSuffix(configName, ".json") || configName == ".json" {
		return "", false
	}
	return strings.TrimSuffix(configName, ".json") + ".lock.json", true
}

// LockPath is the lock's pathname, for messages only. Nothing opens it: the
// reads and the write go through this project's own handle, exactly as every
// declared path does. A configuration with no derivable lock name reports its
// own path, so a message about the lock still names the project it is about.
func (p *Project) LockPath() string {
	configDir, _ := filepath.Split(p.ConfigPath)
	name, ok := p.LockName()
	if !ok {
		return p.ConfigPath
	}
	return configDir + name
}

// HasLock reports whether this project carries a reviewed-set lock at all. Its
// presence is the whole of the opt-in (ADR-0019): a project without one reaches
// no new behavior on any surface.
//
// Presence, not readability, is the question — the same distinction Present
// draws for the configuration and for the same reason. Something that is there
// and will not read is a defect to report, not a project that declined the
// convention.
func (p *Project) HasLock() bool {
	name, ok := p.LockName()
	return ok && p.root.Contains(name) == nil
}

// LockNameFailure is the refusal for a configuration whose filename yields no
// lock name. It names the requirement rather than the derivation, because the
// fix is the invocation's: point --config at a file whose name ends in .json.
func (p *Project) LockNameFailure() *Failure {
	return &Failure{
		Code:     "JPS-LOCK-CONFIG-NAME",
		Message:  fmt.Sprintf("The reviewed-set lock is named after the configuration, and %s does not end in .json, so no lock name can be derived from it. Name the configuration with a .json extension.", display.Sanitize(p.ConfigPath)),
		ExitCode: result.ExitInvocation,
	}
}

// DeclaresPath reports whether the configuration declares any document at one
// relative path — a pack, its matrix, a graph, or its rows — and names which.
//
// It is what keeps a generated file from being written over declared law, so it
// has to answer about *files* and not about spellings. Three questions in
// order: the cleaned relative forms, which settles the ordinary case; file
// identity through the project's own handle when both names exist, which
// settles a hardlink and any alias the filesystem resolves to one inode; and a
// case-folded comparison of the cleaned forms, which is the conservative answer
// for a name that is not there yet on a filesystem that may not distinguish
// case. Refusing a collision that is not one costs a rename; missing one
// destroys a reviewed document.
func (p *Project) DeclaresPath(relative string) (string, bool) {
	target, err := fssecure.Relative(relative)
	if err != nil {
		return "", false
	}
	return p.declaredDocument(func(declared string) bool {
		cleaned, err := fssecure.Relative(declared)
		if err != nil {
			return false
		}
		if cleaned == target || strings.EqualFold(cleaned, target) {
			return true
		}
		return p.sameFile(cleaned, target)
	})
}

// declaredDocument names the first declared document a caller's own comparison
// admits: a pack, its matrix, a graph, or its rows, in configuration order.
//
// The enumeration lives in one place because "declared law" has to mean the
// same four things to every question asked about it. Two passes ask different
// questions — one compares cleaned relative spellings, the other resolved
// pathnames — and a fifth declared document added to only one of them would be
// law one pass protects and the other writes over.
func (p *Project) declaredDocument(same func(declared string) bool) (string, bool) {
	for _, id := range p.IDs {
		entry := p.Config.Packs[id]
		if same(entry.Path) {
			return fmt.Sprintf("the pack %q", id), true
		}
		if entry.Matrix != "" && same(entry.Matrix) {
			return fmt.Sprintf("the matrix of %q", id), true
		}
	}
	for _, id := range p.GraphIDs {
		entry := p.Config.Graphs[id]
		if same(entry.Path) {
			return fmt.Sprintf("the graph %q", id), true
		}
		if entry.Rows != "" && same(entry.Rows) {
			return fmt.Sprintf("the rows of %q", id), true
		}
	}
	return "", false
}

// DeclaresOutputPath is DeclaresPath asked about a destination a caller named
// on the command line rather than about a path relative to the configuration.
//
// It exists because a generated document has to be kept off declared law even
// when the caller is standing somewhere else in the tree: `--write` takes a
// path relative to the working directory, `DeclaresPath` takes one relative to
// the configuration's own directory, and comparing the two spellings directly
// would let `../project/packs/x.matrix.json` past a check that `packs lock`
// passes for free. A destination that resolves outside the configuration's
// directory declares nothing by construction, because every declared path is
// contained by that directory.
//
// It answers about the *destination*, not about permission: the caller still
// creates the file through the exclusive open every generated write uses, so a
// path this reports nothing about is not thereby a path anything may overwrite.
//
// The question is asked twice, of the lexical pathnames and of the symlinked
// ones, and either answer refuses. A lexical comparison alone is a comparison
// of spellings: a symlink aliasing the configuration's own directory is a
// second spelling of every declared document under it, and the file the alias
// names does not exist yet — so sameFile has no inodes to compare and the
// destination reads as undeclared. That is the case a reviewer would never
// notice, because what lands there is a declared matrix that was missing until
// the generator created it.
//
// The DECLARATION is a spelling too, and that is the second pass. A
// configuration may name its matrix through an alias of its own —
// "alias/x.matrix.json" where alias is a symlink to packs — and a caller
// writing at "packs/x.matrix.json" then names the same file under the name the
// configuration did not use. Resolving only the destination cannot see that:
// both ends have to be put through the same deepest-existing-ancestor
// resolution before they are compared, or the alias moves the blind spot from
// one side to the other rather than closing it.
func (p *Project) DeclaresOutputPath(target string) (string, bool) {
	for _, relative := range p.outputRelatives(target) {
		if owner, declared := p.DeclaresPath(relative); declared {
			return owner, true
		}
	}
	return p.declaresResolvedFile(target)
}

// declaresResolvedFile asks whether any declared document and this destination
// are one pathname once BOTH are resolved through their symlinks.
//
// It compares resolved absolute pathnames rather than relative spellings,
// because a declared path resolved through an alias need not stay under the
// configuration's directory at all, and a comparison that dropped it there
// would drop exactly the arrangement this pass exists for. The case-folded
// comparison is the same conservative answer DeclaresPath gives for a name that
// is not there yet on a filesystem that may not distinguish case: refusing a
// collision that is not one costs a rename, missing one destroys a reviewed
// document.
func (p *Project) declaresResolvedFile(target string) (string, bool) {
	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return "", false
	}
	resolvedTarget, ok := resolveExistingPrefix(absoluteTarget)
	if !ok {
		return "", false
	}
	absoluteRoot, err := filepath.Abs(p.Root)
	if err != nil {
		return "", false
	}
	return p.declaredDocument(func(declared string) bool {
		cleaned, err := fssecure.Relative(declared)
		if err != nil {
			return false
		}
		resolved, ok := resolveExistingPrefix(filepath.Join(absoluteRoot, cleaned))
		if !ok {
			return false
		}
		return resolved == resolvedTarget || strings.EqualFold(resolved, resolvedTarget)
	})
}

// outputRelatives renders one named destination as the paths relative to the
// configuration's directory that could be asked about: the lexical one, and the
// one both ends resolve to through their symlinks. A destination that resolves
// outside the directory contributes nothing, because every declared path is
// contained by it.
//
// Refusing a collision that is not one costs a rename; missing one writes
// machine-supplied inputs over reviewed law, so both spellings are asked and
// neither is trusted to stand for the other.
func (p *Project) outputRelatives(target string) []string {
	var relatives []string
	add := func(root, destination string) {
		relative, err := filepath.Rel(root, destination)
		if err != nil {
			return
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return
		}
		if !slices.Contains(relatives, relative) {
			relatives = append(relatives, relative)
		}
	}
	absoluteTarget, targetErr := filepath.Abs(target)
	absoluteRoot, rootErr := filepath.Abs(p.Root)
	if targetErr != nil || rootErr != nil {
		return nil
	}
	add(absoluteRoot, absoluteTarget)
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return relatives
	}
	resolvedTarget, ok := resolveExistingPrefix(absoluteTarget)
	if !ok {
		return relatives
	}
	add(resolvedRoot, resolvedTarget)
	return relatives
}

// resolveExistingPrefix resolves the symlinks of a pathname that need not
// exist: the deepest ancestor that does exist is resolved, and the tail that
// does not is rejoined to it lexically.
//
// A destination a generator is about to create is exactly that pathname — the
// file is not there, so filepath.EvalSymlinks refuses the whole of it, and
// refusing to answer is what let an aliased directory past the check. The
// ancestors are where a directory symlink can be, and they are the part that
// exists.
func resolveExistingPrefix(absolute string) (string, bool) {
	current, tail := absolute, ""
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if tail == "" {
				return resolved, true
			}
			return filepath.Join(resolved, tail), true
		}
		if !errors.Is(err, os.ErrNotExist) {
			// Anything else — a permission denial, a symlink loop — is a
			// question this cannot answer, and answering it wrongly would be
			// answering "not declared". The lexical comparison stands alone.
			return "", false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		tail = filepath.Join(filepath.Base(current), tail)
		current = parent
	}
}

// sameFile reports whether two cleaned relative names are one file, asked
// through the project's own handle. It answers only when both are there: a name
// that is not there yet has no identity to compare, which is what the caller's
// case-folded comparison covers.
func (p *Project) sameFile(left, right string) bool {
	return p.root.SameFile(left, right)
}

// ContainsAuditDir reports whether one relative path is, or contains, the audit
// directory this configuration declares.
//
// A generated file written at the audit directory's own name — or at any
// component above it — leaves that directory uncreatable, and every later
// evaluation then refuses *after* evaluating because its record cannot be
// written. That is a defect the write can see coming and the evaluation cannot.
func (p *Project) ContainsAuditDir(relative string) bool {
	if p.Config.Audit == nil {
		return false
	}
	target, err := fssecure.Relative(relative)
	if err != nil {
		return false
	}
	dir, err := fssecure.Relative(p.Config.Audit.Dir)
	if err != nil {
		return false
	}
	if strings.EqualFold(dir, target) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(dir), strings.ToLower(target)+string(filepath.Separator))
}

// DeclaredGraphID reports which configured graph, if any, an invocation's graph
// argument names.
//
// The graph verbs take a path rather than a decision id, so this is the only way
// to tell "the reviewed graph this project declares" from "a graph document
// someone is drafting". It compares resolved pathnames, which is an
// *identification* and never an access: nothing is opened by the result, the
// document was already read through the ordinary reader, and the pathname is
// never handed anywhere. Its failure mode is the safe one — a declared graph
// reached by a pathname that does not compare equal is treated as a draft, so
// the run records itself unreviewed instead of claiming a review it did not
// verify.
func (p *Project) DeclaredGraphID(graphPath string) (string, bool) {
	if graphPath == "" || graphPath == "-" {
		return "", false
	}
	absolute, err := filepath.Abs(graphPath)
	if err != nil {
		return "", false
	}
	for _, id := range p.GraphIDs {
		entry := p.Config.Graphs[id]
		declared, err := fssecure.Resolve(p.Root, entry.Path)
		if err != nil {
			continue
		}
		if filepath.Clean(declared) == filepath.Clean(absolute) {
			return id, true
		}
	}
	return "", false
}

// ReadLock reads the lock through the project's own handle.
func (p *Project) ReadLock() ([]byte, error) {
	name, ok := p.LockName()
	if !ok {
		return nil, os.ErrNotExist
	}
	return p.root.Read(name, MaxLockBytes)
}

// WriteLock replaces the lock through the same handle. The bytes are the
// caller's whole file: a lock is generated in full from what was read, so there
// is nothing in an older one to preserve.
func (p *Project) WriteLock(contents []byte) error {
	name, ok := p.LockName()
	if !ok {
		return os.ErrInvalid
	}
	return p.root.Replace(name, contents)
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
