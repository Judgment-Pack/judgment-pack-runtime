package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/conformance"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/describe"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/lock"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/mcp"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

type App struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	engine *validation.Engine
	runner *conformance.Runner
	pretty bool
}

type handledExit struct {
	code int
}

func (e *handledExit) Error() string { return "" }

func Run(args []string, in io.Reader, out, errOut io.Writer) int {
	configureSignals()
	engine, err := validation.NewEngine()
	if err != nil {
		message := "Bundled JPS artifacts failed their integrity check."
		if requestedFormat(args) == "json" {
			if writeJSON(out, result.NewOperationalError(requestedCommand(args), "JPS-ARTIFACT-INTEGRITY", message), false) != nil {
				return result.ExitIO
			}
		} else {
			fmt.Fprintln(errOut, "error:", message)
		}
		return result.ExitInternal
	}
	app := &App{in: in, out: out, errOut: errOut, engine: engine, runner: conformance.NewRunner(engine)}
	root := app.rootCommand()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var handled *handledExit
		if errors.As(err, &handled) {
			return handled.code
		}
		message := display.Sanitize(err.Error())
		if requestedFormat(args) == "json" {
			if writeJSON(out, result.NewOperationalError(requestedCommand(args), "JPS-INVOCATION-ARGUMENTS", message), false) != nil {
				return result.ExitIO
			}
		} else {
			fmt.Fprintln(errOut, "error:", message)
		}
		return result.ExitInvocation
	}
	return result.ExitSuccess
}

func (a *App) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "jpack",
		Short:         "Judgment Pack Specification tooling",
		Long:          "Judgment Pack Specification tooling. The spec commands validate JPS documents; they do not evaluate decisions or authorize actions. The experimental namespace is the one exception: it evaluates, under the JPS Core 0.2.0-draft evaluator contract, on a surface that may change without notice. This runtime's conformance claim is stated, in full and only, in CONFORMANCE.md; no help text, payload, or other file states it, and this one does not either. No command authorizes anything.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       result.CLIVersion,
		Args:          cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	root.SetIn(a.in)
	root.SetOut(a.out)
	root.SetErr(a.errOut)
	root.SetVersionTemplate("jpack {{.Version}}\n")
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().BoolVar(&a.pretty, "pretty", false, "indent JSON output")
	root.AddCommand(a.versionCommand(), a.specCommand(), a.packsCommand(), a.mcpCommand(), a.experimentalCommand())
	return root
}

func (a *App) experimentalCommand() *cobra.Command {
	experimental := &cobra.Command{
		Use:   "experimental",
		Short: "Experimental-surface operations: the evaluator, which may change or vanish",
		Long:  "Experimental operations (ADR-0007, ADR-0010, ADR-0011). \"Experimental\" here is a stability statement and not a conformance one: this surface may change or be removed without compatibility promise. The evaluator implements the evaluator conformance class of JPS Core 0.2.0-draft -- the §8.2 input preflight, the §8.3 portable disposition, the §8.4 error classes, and the §10 limits. This runtime's conformance claim is stated, in full and only, in CONFORMANCE.md: read that file for the claim, its exact version scope, its evidence, and everything it does not assert (§3.4.1, §3.5). This text states no claim, and neither does any payload; every payload carries a conformanceClaimReference member pointing at that file, beside the contract's own version as evaluatorSpecVersion and the pack's own specVersion. Only a pack declaring specVersion 0.2.0-draft is evaluated: JPS §11 makes the declared value exact and requires an unedited 0.1.0-draft pack to be re-declared -- one edit, the specVersion string -- before an implementation claiming this draft evaluates it, so a pack declaring any other version is refused as pack-not-conformant in the preflight phase.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	experimental.AddCommand(a.evaluateCommand(), a.evaluateCorpusCommand(), a.graphCommand())
	return experimental
}

// evaluateCorpusCommand runs the bundled evaluation corpus of the specification
// version that publishes one. It exists so a harness can drive the rows the claim
// in CONFORMANCE.md cites: the run produces results, which §3.4.1 makes the
// required and non-exhaustive evidence for that claim rather than the claim
// itself, and every payload references that file rather than restating it.
func (a *App) evaluateCorpusCommand() *cobra.Command {
	format := "human"
	specVersion := ""
	command := &cobra.Command{
		Use:   "evaluate-corpus",
		Short: "EXPERIMENTAL SURFACE: run the bundled JPS evaluation corpus; results, the claim's evidence",
		Long:  "Run the evaluation corpus bundled for one exact JPS version through this runtime's evaluator and report every row: the RFC 8785 canonical disposition compared byte for byte against the row's, or the expected JPS §8.4 error class and phase. This reports corpus results. Those results are the required evidence for the evaluator-conformance claim in CONFORMANCE.md and are not that claim and not exhaustive evidence of it (§3.4.1): the corpus is a version-pinned seed corpus with a published gap list, so passing every row demonstrates nothing directly about an input no row contains. A mismatching row decides nothing by itself -- §3.4 makes a divergence as likely to be a defect in the row as in this implementation, and only a project-issued erratum can excuse a row.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("experimental evaluate-corpus", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			evaluator := evaluation.NewEngine(a.engine)
			output, failure := evaluator.RunCorpus(specVersion, "experimental evaluate-corpus")
			if failure != nil {
				return a.evaluationFailure("experimental evaluate-corpus", format, failure)
			}
			if err := a.renderEvaluationCorpus(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			code := result.ExitSuccess
			if output.Status == "mismatch" {
				code = result.ExitInvalid
			}
			return &handledExit{code: code}
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&specVersion, "spec-version", specVersion, "exact JPS version whose evaluation corpus to run; defaults to "+artifacts.EvaluatorDraftVersion)
	return command
}

func (a *App) evaluateCommand() *cobra.Command {
	format := "human"
	factsPath := ""
	evidencePath := ""
	supported := []string{}
	quantifiers := false
	rehearsal := false
	packID := ""
	configPath := ""
	command := &cobra.Command{
		Use:   "evaluate <pack-or->",
		Short: "EXPERIMENTAL SURFACE: apply the JPS §§7-8 resolution model to one pack",
		Long:  "Apply the JPS Core §§7-8 resolution model to one conformant pack and one facts document. Inputs are admitted in the order §8.2 fixes -- pack, facts, evidence, required extensions -- before any rule is interpreted, and a refused evaluation reports its §8.4 error class with no disposition at all, including a reached §10 evaluation-work limit (resource-exhaustion, evaluation phase). The result is the §8.3 portable disposition, written under --format json (without --pretty, which re-indents it) in its RFC 8785 canonical form. The §§8.2-8.4 contract is JPS Core 0.2.0-draft's, and only a pack declaring that exact specVersion is evaluated: §11 makes the value exact and requires an unedited 0.1.0-draft pack to be re-declared -- one edit, the specVersion string, and nothing else in the document -- before an implementation claiming this draft evaluates it, so any other version is refused as pack-not-conformant in the preflight phase. The payload names the pack's specVersion and the contract's evaluatorSpecVersion. This runtime's conformance claim is stated, in full and only, in CONFORMANCE.md; this text states no claim, and one run of this command is a result rather than the claim or evidence about anything beyond that run -- no result is an authorization, an executed action, or any statement about whether the pack, the facts, or acting on the disposition is correct (§3.5). Producing any disposition exits 0. With --rfc0008-quantifiers the condition grammar of the specification's RFC 0008 (Draft) is admitted as a prototype; such a pack is not valid under any published JPS version, is not an input the claimed class defines, and every evaluation payload produced this way says so in band. The pack may also be named by --pack-id, which resolves one decision id through a project's jpack.json (ADR-0012, a convention of this runtime and not of the specification); it is mutually exclusive with the pack argument, because a command with two sources for one input has an order of precedence nobody asked for. Every payload echoes the evaluated pack's own id and version as packId and packVersion, read off the document that was evaluated. The project's configuration is consulted on every run, however the pack was named, because whether an evaluation is recorded is that configuration's to say: under configVersion \"3\" an audit member declares a directory, and each completed evaluation then appends one record to it (ADR-0018). A refused evaluation records nothing, a record that cannot be written refuses the run rather than reporting a disposition nothing kept, and a project that declares no audit member has nothing written for it.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			packArgument := ""
			if len(args) == 1 {
				packArgument = args[0]
			}
			// One input, one source. Both supplied is a bad invocation rather than a
			// precedence rule, and neither supplied names both ways of supplying it.
			if packArgument != "" && packID != "" {
				return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-PACK-ID", "The pack argument and --pack-id are mutually exclusive: pass one pack, as a path (or -), or as a decision id resolved through the project's jpack.json.")
			}
			if packArgument == "" && packID == "" {
				return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-PACK-ID", "A pack is required: pass a file path (or - for standard input), or --pack-id to resolve one through the project's jpack.json.")
			}
			// Every argument refusal precedes the filesystem, on this surface as
			// on the MCP one. A call missing a required argument, or naming an
			// input this command does not take, never became an evaluation — and
			// a broken configuration must not answer in place of the mistake the
			// caller actually made.
			if factsPath == "" {
				return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-FACTS", "--facts is required: one JSON facts document (a file path, or - for standard input).")
			}
			if packArgument == "-" && factsPath == "-" {
				return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-STDIN", "The pack and the facts document cannot both be standard input.")
			}
			if evidencePath == "-" {
				return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-STDIN", "The evidence document cannot be standard input; pass a file path.")
			}
			for _, input := range []string{packArgument, factsPath, evidencePath} {
				if input != "" && input != "-" && (strings.Contains(input, "://") || fssecure.IsRemotePath(input)) {
					return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use local files or standard input.")
				}
			}
			// The project is consulted on every evaluation this command runs, not
			// only on the ones that name a pack by id: whether an evaluation is
			// recorded is the configuration's to say (ADR-0018), and a pack named
			// by path is still evaluated in the project the command was run in. A
			// configuration that is there and cannot be read therefore refuses
			// the run, on either form of the pack argument — the alternative is
			// evaluating unrecorded for a project that asked to be told.
			var loaded *project.Project
			configFile := project.Locate(configPath)
			if packID != "" || project.Present(configFile) {
				opened, failure := project.Load(configFile)
				if failure != nil {
					return a.projectFailure("experimental evaluate", format, failure)
				}
				defer opened.Close()
				loaded = opened
			}
			// A pack named by id is read here, through the project's directory
			// handle, and never becomes a pathname the generic reader opens again.
			var packFromID []byte
			packIDOversized := false
			if packID != "" {
				data, oversized, failure := a.resolvePackID(loaded, packID, format)
				if failure != nil {
					return failure
				}
				packFromID, packIDOversized = data, oversized
			}
			// The reviewed set is consulted before anything is evaluated
			// (ADR-0019). A pack named by decision id is declared law, so its
			// bytes and the configuration that resolves the id are held to what
			// the project last declared reviewed; a pack named by path is a
			// draft, never refused for being unlocked, and the record it leaves
			// says so.
			auditWriter := loaded.AuditWriter()
			// An oversized input is reported to the engine rather than refused
			// here: the byte limit is a §8.2 preflight condition whose §8.4 class
			// and place in the fixed order the engine assigns. Refusing it at the
			// read would report a failure with no class at all, and would let the
			// facts document's limit outrank the pack's conformance.
			oversized := []string{}
			pack, packOversized := packFromID, packIDOversized
			if packID == "" {
				var err error
				pack, packOversized, err = a.readEvaluationInput(packArgument)
				if err != nil {
					return a.evaluateReadFailure(format, "pack")
				}
			}
			if packOversized {
				oversized = append(oversized, "pack")
			}
			facts, factsOversized, err := a.readEvaluationInput(factsPath)
			if err != nil {
				return a.evaluateReadFailure(format, "facts")
			}
			if factsOversized {
				oversized = append(oversized, "facts")
			}
			var evidence []byte
			if evidencePath != "" {
				evidenceOversized := false
				evidence, evidenceOversized, err = a.readEvaluationInput(evidencePath)
				if err != nil {
					return a.evaluateReadFailure(format, "evidence")
				}
				if evidenceOversized {
					oversized = append(oversized, "evidence")
				}
			}
			// The reviewed set is consulted on the bytes this run is about to
			// evaluate, never on a second read of the path they came from
			// (ADR-0019): a claim about a review has to be about the document
			// that produced the disposition. A pack named by decision id is
			// declared law; a pack named by path is a draft, never refused for
			// being unlocked, and the record it leaves says so.
			var applied []lock.Applied
			if packID != "" && pack != nil {
				applied = []lock.Applied{lock.AppliedPack(packID, pack)}
			}
			// A run applying no declared document never reads the lock at all,
			// so an unreadable one does not stop a draft; a run that does
			// applies it once, and the record names the revision it was judged
			// under.
			// A rehearsal consults no reviewed set and appends no record —
			// exactly the standing a matrix row has (ADR-0021), extended to one
			// declared exploratory run by ADR-0028. It is the caller's explicit
			// declaration, never inferred, and the payload it produces says so
			// in band.
			var set *lock.Set
			reviewed := lock.DraftRun(loaded)
			if len(applied) > 0 && !rehearsal {
				opened, lockFailure := lock.Open(loaded)
				if lockFailure != nil {
					return a.lockFailure("experimental evaluate", format, lockFailure)
				}
				set = opened
				reviewed, lockFailure = set.Consult(loaded, applied, false)
				if lockFailure != nil {
					return a.lockFailure("experimental evaluate", format, lockFailure)
				}
			}
			auditWriter.UnderLaw(reviewed, set.Provenance())
			evaluator := evaluation.NewEngine(a.engine)
			output, failure := evaluator.EvaluateWith(pack, facts, evidence, evaluation.Options{
				Command:             "experimental evaluate",
				SupportedExtensions: supported,
				RFC0008Quantifiers:  quantifiers,
				// An empty --evidence file is a supplied document, not an omitted one:
				// §8.2's absence is the omitted document, and empty bytes are a
				// malformed input the preflight reaches in its own place in the order.
				EvidenceSupplied: evidencePath != "",
				OversizedInputs:  oversized,
			})
			if failure != nil {
				return a.evaluationFailure("experimental evaluate", format, failure)
			}
			// The record is written before the disposition is reported, and a
			// failed write refuses the run. A project that asked to be told what
			// its packs decided is not served by an answer it has no record of;
			// the evaluation itself is untouched either way, having already
			// happened. A declared rehearsal writes nothing even here, and its
			// payload carries the label instead of a record (ADR-0028).
			if rehearsal {
				output.Rehearsal = true
			} else if err := auditWriter.Evaluation(output, audit.Inputs{
				Facts:            facts,
				Evidence:         evidence,
				EvidenceSupplied: evidencePath != "",
			}, pack, nil); err != nil {
				return a.operational("experimental evaluate", format, result.ExitIO, audit.FailureCode, audit.FailureMessage)
			}
			if err := a.renderEvaluation(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&factsPath, "facts", factsPath, "JSON facts document: a file path, or - for standard input (required). The nested document fact pointers descend into: for /request/type write {\"request\":{\"type\":\"data-access\"}} -- a flat member literally named \"/request/type\" does not resolve that pointer")
	command.Flags().StringVar(&evidencePath, "evidence", evidencePath, "optional tri-state evidence availability: {\"<requirement-id>\": \"present\"|\"absent\"|\"unknown\"}")
	command.Flags().StringArrayVar(&supported, "supported-extension", supported, "extension name this consumer supports (repeatable)")
	command.Flags().BoolVar(&quantifiers, "rfc0008-quantifiers", quantifiers, "DRAFT-RFC PROTOTYPE: admit the spec's RFC 0008 (Draft) collection quantifiers -- exists, every, uniform -- in conditions. A pack using them is NOT valid under any published JPS version; spec validate rejects it, and every successful evaluation payload produced this way is labeled a draft-RFC prototype (a refusal is reported as an operational error and carries no such label)")
	command.Flags().BoolVar(&rehearsal, "rehearsal", rehearsal, "declare this run a rehearsal, not a decision: the evaluation runs identically, but no audit record is appended (ADR-0018) and no reviewed set is consulted (ADR-0019) -- the standing a matrix row already has (ADR-0021) -- and the payload carries \"rehearsal\": true so a stored copy can never pass as a decision")
	command.Flags().StringVar(&packID, "pack-id", packID, "decision id of a pack declared in the project's jpack.json; mutually exclusive with the pack argument")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

// resolvePackID reads the pack a decision id names, through the project
// configuration's own directory handle.
//
// It returns the bytes, not a path. A path would have to be opened again by
// somebody else, and that second open is a different operation on a filesystem
// that may have changed in between — exactly the gap the handle-bound reader
// exists to close. Handing back a pathname here would put containment and the
// read on two different files and make this the one pack access in the runtime
// that does not go through project.ReadPack; the MCP surface already returns
// bytes for the same reason.
//
// The oversized case is reported rather than refused, because the byte limit is a
// §8.2 preflight condition the engine classes: a pack over the limit is the same
// non-admission whether it was named by path or by id.
//
// The project is the caller's, already loaded and still open: this command
// consults the configuration whether or not a pack was named by id, and loading
// it twice would be a second derivation of the one thing the handle exists to
// make single.
func (a *App) resolvePackID(loaded *project.Project, packID, format string) ([]byte, bool, error) {
	entry, ok := loaded.Entry(packID)
	if !ok {
		return nil, false, a.projectFailure("experimental evaluate", format, loaded.UnknownPackFailure(packID))
	}
	data, err := loaded.ReadPack(entry)
	if errors.Is(err, fssecure.ErrOutsideRoot) {
		return nil, false, a.operational("experimental evaluate", format, result.ExitIO, "JPS-PROJECT-PACK-PATH",
			fmt.Sprintf("The path declared for %q resolves outside the configuration's own directory, which no configured path may.", display.Sanitize(packID)))
	}
	if errors.Is(err, fssecure.ErrTooLarge) {
		return nil, true, nil
	}
	if err != nil {
		// The path is inside the project and no file there could be read — a missing
		// directory, an unreadable one — which is the ordinary read failure every
		// other evaluation input reports, and not an escape.
		return nil, false, a.evaluateReadFailure(format, "pack")
	}
	return data, false, nil
}

// readEvaluationInput reads one evaluation input and reports an oversized one
// rather than refusing it.
//
// The byte limit belongs to the §8.2 preflight: reaching it means the input was
// never admitted, which §8.4 classes as pack-not-conformant for the pack and
// malformed-input for the facts or evidence document, in that fixed order. A
// bounded read that stopped at the limit therefore returns no bytes and the
// oversized marker, and the engine reaches the limit at that input's own place
// in the preflight. Every other read failure is an operational one — the file is
// not a readable bounded regular file — which §8.4 does not class at all.
func (a *App) readEvaluationInput(argument string) ([]byte, bool, error) {
	data, err := a.readPack(argument, carrier.HardMaxBytes)
	if errors.Is(err, fssecure.ErrTooLarge) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, false, nil
}

// evaluateReadFailure reports a failed evaluation-input read. An input above the
// byte limit does not reach here: readEvaluationInput hands that condition to the
// engine, which reports it as the §8.4 evaluation error it is.
func (a *App) evaluateReadFailure(format, name string) error {
	return a.operational("experimental evaluate", format, result.ExitIO, "JPS-INPUT-READ", fmt.Sprintf("The %s input could not be read as one bounded regular file or standard input stream.", name))
}

func (a *App) specCommand() *cobra.Command {
	spec := &cobra.Command{
		Use:   "spec",
		Short: "Inspect and test Judgment Pack Specification documents",
		Long:  "Offline, nonnormative tooling for JPS carrier, structural, and semantic document conformance.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	spec.AddCommand(a.validateCommand(), a.testConformanceCommand(), a.schemaCommand(), a.examplesCommand())
	return spec
}

func (a *App) mcpCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the offline validator and three experimental tools to an MCP client over stdio",
		Long:  "Run a Model Context Protocol server on standard input and output. It exposes the offline validation, conformance, and description operations as MCP tools, plus three tools on this runtime's experimental surface (experimental_evaluate, experimental_test_packs and experimental_test_graphs; ADR-0007, ADR-0011, ADR-0021, ADR-0026), whose surfaces may still change without notice. That evaluator's conformance claim is stated, in full and only, in CONFORMANCE.md; this text states no claim. It holds no credential, opens no network connection, and authorizes nothing.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			server := mcp.NewServer(a.engine, a.runner)
			if err := server.Serve(a.in, a.out, a.errOut); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
}

func (a *App) versionCommand() *cobra.Command {
	format := "human"
	command := &cobra.Command{
		Use:   "version",
		Short: "Show CLI and bundled JPS versions",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("version", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			set, err := artifacts.Load(artifacts.DraftVersion)
			if err != nil {
				return a.operational("version", format, result.ExitInternal, "JPS-ARTIFACT-INTEGRITY", "Bundled artifact metadata is unavailable.")
			}
			output := describe.Runtime(set, "version")
			if format == "json" {
				if err := a.writeJSON(output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
			} else {
				fmt.Fprintf(a.out, "jpack %s\n", result.CLIVersion)
				fmt.Fprintf(a.out, "JPS: %s (%s)\n", strings.Join(output.SupportedSpecs, ", "), output.ArtifactProvenance)
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	return command
}

func (a *App) validateCommand() *cobra.Command {
	format := "human"
	through := "semantic"
	maxBytes := carrier.HardMaxBytes
	quiet := false
	noColor := false
	command := &cobra.Command{
		Use:   "validate <pack-or->",
		Short: "Validate one JPS document without evaluating it",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateCommonOptions(format, quiet); err != nil {
				return a.operational("spec validate", format, result.ExitInvocation, "JPS-INVOCATION-OPTIONS", err.Error())
			}
			if through != "carrier" && through != "structural" && through != "semantic" {
				return a.operational("spec validate", format, result.ExitInvocation, "JPS-INVOCATION-THROUGH", "--through must be carrier, structural, or semantic.")
			}
			if maxBytes <= 0 || maxBytes > carrier.HardMaxBytes {
				return a.operational("spec validate", format, result.ExitInvocation, "JPS-INVOCATION-MAX-BYTES", "--max-bytes must be positive and cannot exceed the hard 10 MiB limit.")
			}
			if args[0] != "-" && (strings.Contains(args[0], "://") || fssecure.IsRemotePath(args[0])) {
				return a.operational("spec validate", format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL inputs are not supported; use one local file or standard input.")
			}
			data, err := a.readPack(args[0], maxBytes)
			if err != nil {
				if errors.Is(err, fssecure.ErrTooLarge) {
					return a.operational("spec validate", format, result.ExitIO, "JPS-RESOURCE-INPUT-BYTE-LIMIT", fmt.Sprintf("Input exceeds the %d-byte limit.", maxBytes))
				}
				return a.operational("spec validate", format, result.ExitIO, "JPS-INPUT-READ", "Input could not be read as one bounded regular file or standard input stream.")
			}
			output, operational := a.engine.Validate(data, validation.Options{Through: through, Limits: carrier.DefaultLimits()})
			if operational != nil {
				return a.operational("spec validate", format, operational.ExitCode, operational.Code, operational.Message)
			}
			if !quiet || output.Status != "valid" {
				if err := a.renderValidation(format, output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
			}
			return &handledExit{code: validationExit(output.Status)}
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&through, "through", through, "last validation layer: carrier, structural, or semantic")
	command.Flags().Int64Var(&maxBytes, "max-bytes", maxBytes, "maximum input bytes, up to the hard 10 MiB limit")
	command.Flags().BoolVar(&quiet, "quiet", quiet, "suppress successful human output")
	command.Flags().BoolVar(&noColor, "no-color", noColor, "disable terminal color")
	_ = noColor
	return command
}

func (a *App) testConformanceCommand() *cobra.Command {
	format := "human"
	specVersion := ""
	quiet := false
	noColor := false
	command := &cobra.Command{
		Use:   "test-conformance [suite]",
		Short: "Run a version-pinned JPS conformance corpus",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateCommonOptions(format, quiet); err != nil {
				return a.operational("spec test-conformance", format, result.ExitInvocation, "JPS-INVOCATION-OPTIONS", err.Error())
			}
			suite := ""
			if len(args) == 1 {
				suite = args[0]
			}
			output, operational := a.runner.Run(suite, specVersion)
			if operational != nil {
				return a.operational("spec test-conformance", format, operational.ExitCode, operational.Code, operational.Message)
			}
			if !quiet || output.Status != "valid" {
				if err := a.renderSuite(format, output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
			}
			code := result.ExitSuccess
			if output.Status == "mismatch" {
				code = result.ExitInvalid
			}
			return &handledExit{code: code}
		},
	}
	command.Flags().StringVar(&specVersion, "spec-version", specVersion, "exact JPS version; defaults to "+artifacts.DraftVersion)
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().BoolVar(&quiet, "quiet", quiet, "suppress successful human output")
	command.Flags().BoolVar(&noColor, "no-color", noColor, "disable terminal color")
	_ = noColor
	return command
}

func (a *App) schemaCommand() *cobra.Command {
	format := "human"
	writeTarget := ""
	command := &cobra.Command{
		Use:   "schema <spec-version>",
		Short: "Inspect or write an exact bundled JPS schema",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("spec schema", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if writeTarget == "-" && format == "json" {
				return a.operational("spec schema", format, result.ExitInvocation, "JPS-INVOCATION-STDOUT", "--write - cannot be combined with --format json.")
			}
			if writeTarget != "" && writeTarget != "-" && fssecure.IsRemotePath(writeTarget) {
				return a.operational("spec schema", format, result.ExitInvocation, "JPS-INVOCATION-OUTPUT", "Remote filesystem output paths are not supported.")
			}
			set, err := artifacts.Load(args[0])
			if err != nil {
				return a.operational("spec schema", format, result.ExitUnsupported, "JPS-CAPABILITY-SPEC-VERSION", "The exact JPS specification version is not bundled with this CLI.")
			}
			schemaBytes, err := set.Schema()
			if err != nil {
				return a.operational("spec schema", format, result.ExitInternal, "JPS-ARTIFACT-SCHEMA", "Bundled schema is unavailable.")
			}
			if writeTarget == "-" {
				if _, err := a.out.Write(schemaBytes); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			writtenTo := ""
			if writeTarget != "" {
				if err := writeNewFile(writeTarget, schemaBytes); err != nil {
					return a.operational("spec schema", format, result.ExitIO, "JPS-SCHEMA-WRITE", "Schema destination must be a new writable file.")
				}
				writtenTo = writeTarget
			}
			output, err := describe.Schema(set, args[0], "spec schema", schemaBytes)
			if err != nil {
				return a.operational("spec schema", format, result.ExitInternal, "JPS-ARTIFACT-SCHEMA", "Bundled schema metadata is invalid.")
			}
			output.WrittenTo = writtenTo
			if err := a.renderSchema(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&writeTarget, "write", writeTarget, "write original schema bytes to a new file or -")
	return command
}

func (a *App) examplesCommand() *cobra.Command {
	format := "human"
	writeTarget := ""
	command := &cobra.Command{
		Use:   "examples [name]",
		Short: "List or print bundled valid JPS example documents",
		Long:  "List the valid, version-pinned example documents this CLI embeds, or print one by name. These are digest-locked conformance fixtures from the specification, offered read-only as authoring starting points -- not authored templates. With no name, list them; with a name, show its metadata, or write its exact bytes with --write.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("spec examples", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			set, err := artifacts.Load(artifacts.DraftVersion)
			if err != nil {
				return a.operational("spec examples", format, result.ExitInternal, "JPS-ARTIFACT-INTEGRITY", "Bundled artifact metadata is unavailable.")
			}
			if len(args) == 0 {
				if writeTarget != "" {
					return a.operational("spec examples", format, result.ExitInvocation, "JPS-INVOCATION-OUTPUT", "--write requires an example name.")
				}
				output, err := describe.Examples(set, "spec examples")
				if err != nil {
					return a.operational("spec examples", format, result.ExitInternal, "JPS-ARTIFACT-MANIFEST", "Bundled example catalog is unavailable.")
				}
				if err := a.renderExamples(format, output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			if writeTarget == "-" && format == "json" {
				return a.operational("spec examples", format, result.ExitInvocation, "JPS-INVOCATION-STDOUT", "--write - cannot be combined with --format json.")
			}
			if writeTarget != "" && writeTarget != "-" && fssecure.IsRemotePath(writeTarget) {
				return a.operational("spec examples", format, result.ExitInvocation, "JPS-INVOCATION-OUTPUT", "Remote filesystem output paths are not supported.")
			}
			output, data, err := describe.Example(set, args[0], "spec examples")
			if err != nil {
				var unknown *artifacts.UnknownExampleError
				if errors.As(err, &unknown) {
					return a.operational("spec examples", format, result.ExitUnsupported, "JPS-CAPABILITY-EXAMPLE", "No bundled example has that name; run spec examples to list the available names.")
				}
				return a.operational("spec examples", format, result.ExitInternal, "JPS-ARTIFACT-CASE", "Bundled example is unavailable.")
			}
			if writeTarget == "-" {
				if _, err := a.out.Write(data); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			if writeTarget != "" {
				if err := writeNewFile(writeTarget, data); err != nil {
					return a.operational("spec examples", format, result.ExitIO, "JPS-EXAMPLE-WRITE", "Example destination must be a new writable file.")
				}
				output.WrittenTo = writeTarget
			}
			if err := a.renderExample(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&writeTarget, "write", writeTarget, "write the example's exact bytes to a new file or -")
	return command
}

func (a *App) readPack(argument string, limit int64) ([]byte, error) {
	if argument == "-" {
		return readBounded(a.in, limit)
	}
	return fssecure.ReadRegular(argument, limit)
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fssecure.ErrTooLarge
	}
	return data, nil
}

func writeNewFile(target string, data []byte) error {
	cleanTarget := filepath.Clean(target)
	if cleanTarget == "." {
		return errors.New("invalid target")
	}
	file, err := os.OpenFile(cleanTarget, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	written := false
	defer func() {
		if !written {
			_ = file.Close()
			_ = os.Remove(cleanTarget)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	written = true
	return nil
}

func validateFormat(format string) error {
	if format != "human" && format != "json" {
		return errors.New("--format must be human or json")
	}
	return nil
}

func validateCommonOptions(format string, quiet bool) error {
	if err := validateFormat(format); err != nil {
		return err
	}
	if quiet && format == "json" {
		return errors.New("--quiet cannot be combined with --format json")
	}
	return nil
}

func validationExit(status string) int {
	switch status {
	case "valid":
		return result.ExitSuccess
	case "invalid":
		return result.ExitInvalid
	case "unsupported":
		return result.ExitUnsupported
	default:
		return result.ExitInternal
	}
}

func requestedFormat(args []string) string {
	for index, argument := range args {
		if argument == "--format=json" {
			return "json"
		}
		if argument == "--format" && index+1 < len(args) && args[index+1] == "json" {
			return "json"
		}
	}
	return "human"
}

func requestedCommand(args []string) string {
	// One scan, first command group wins, so an operand that happens to spell a
	// group name (for example a file called "spec") cannot relabel the command.
	for index, argument := range args {
		switch argument {
		case "spec":
			if index+1 < len(args) {
				switch args[index+1] {
				case "validate", "test-conformance", "schema", "examples":
					return "spec " + args[index+1]
				}
			}
			return "spec"
		case "packs":
			if index+1 < len(args) {
				switch args[index+1] {
				case "list", "validate", "test", "lock", "verify", "lint", "schema":
					return "packs " + args[index+1]
				}
			}
			return "packs"
		case "experimental":
			if index+1 < len(args) {
				switch args[index+1] {
				case "evaluate", "evaluate-corpus":
					return "experimental " + args[index+1]
				case "graph":
					if index+2 < len(args) {
						switch args[index+2] {
						case "validate", "evaluate", "explain", "test", "schema":
							return "experimental graph " + args[index+2]
						}
					}
					return "experimental graph"
				}
			}
			return "experimental"
		}
	}
	for _, argument := range args {
		if argument == "version" {
			return "version"
		}
	}
	return "jpack"
}
