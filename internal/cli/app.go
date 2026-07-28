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
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/conformance"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/describe"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/mcp"
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
		Use:           "judgment-pack",
		Short:         "Judgment Pack Specification tooling",
		Long:          "Judgment Pack Specification tooling. The spec commands validate JPS documents; they do not evaluate decisions or authorize actions. The experimental namespace is the one exception: it evaluates experimentally, claims no conformance, and may change without notice.",
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
	root.SetVersionTemplate("judgment-pack {{.Version}}\n")
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().BoolVar(&a.pretty, "pretty", false, "indent JSON output")
	root.AddCommand(a.versionCommand(), a.specCommand(), a.mcpCommand(), a.experimentalCommand())
	return root
}

func (a *App) experimentalCommand() *cobra.Command {
	experimental := &cobra.Command{
		Use:   "experimental",
		Short: "Experimental operations that claim no conformance and may change or vanish",
		Long:  "Experimental operations (ADR-0007). Nothing under this command claims any JPS conformance; JPS 0.1.0-draft §3.4 forbids evaluator-conformance claims outright. Behavior may change or be removed without compatibility promise.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	experimental.AddCommand(a.evaluateCommand())
	return experimental
}

func (a *App) evaluateCommand() *cobra.Command {
	format := "human"
	factsPath := ""
	evidencePath := ""
	supported := []string{}
	quantifiers := false
	command := &cobra.Command{
		Use:   "evaluate <pack-or->",
		Short: "EXPERIMENTAL: apply the JPS §§7-8 experiment to one pack; no conformance claim",
		Long:  "Apply the experimental JPS Core §§7-8 resolution model, as pinned by the specification's RFC 0006 (Draft), to one conformant pack and one facts document. The result is a disposition, not a conformance claim, an authorization, or an executed action; producing any disposition exits 0. With --rfc0008-quantifiers the condition grammar of the specification's RFC 0008 (Draft) is admitted as a prototype; such a pack is not valid under any published JPS version and every evaluation payload produced this way says so in band.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if factsPath == "" {
				return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-FACTS", "--facts is required: one JSON facts document (a file path, or - for standard input).")
			}
			if args[0] == "-" && factsPath == "-" {
				return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-STDIN", "The pack and the facts document cannot both be standard input.")
			}
			if evidencePath == "-" {
				return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-STDIN", "The evidence document cannot be standard input; pass a file path.")
			}
			for _, input := range []string{args[0], factsPath, evidencePath} {
				if input != "" && input != "-" && (strings.Contains(input, "://") || fssecure.IsRemotePath(input)) {
					return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use local files or standard input.")
				}
			}
			pack, err := a.readPack(args[0], carrier.HardMaxBytes)
			if err != nil {
				return a.evaluateReadFailure(format, "pack", err)
			}
			facts, err := a.readPack(factsPath, carrier.HardMaxBytes)
			if err != nil {
				return a.evaluateReadFailure(format, "facts", err)
			}
			var evidence []byte
			if evidencePath != "" {
				evidence, err = fssecure.ReadRegular(evidencePath, carrier.HardMaxBytes)
				if err != nil {
					return a.evaluateReadFailure(format, "evidence", err)
				}
				if len(evidence) == 0 {
					return a.operational("experimental evaluate", format, result.ExitInvocation, "JPS-EVALUATION-INPUT-JSON", "The evidence file is empty; supply a JSON object, or omit --evidence to treat every requirement as unknown.")
				}
			}
			evaluator := evaluation.NewEngine(a.engine)
			output, failure := evaluator.EvaluateWith(pack, facts, evidence, evaluation.Options{
				Command:             "experimental evaluate",
				SupportedExtensions: supported,
				RFC0008Quantifiers:  quantifiers,
			})
			if failure != nil {
				return a.operational("experimental evaluate", format, failure.ExitCode, failure.Code, failure.Message)
			}
			if err := a.renderEvaluation(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&factsPath, "facts", factsPath, "JSON facts document: a file path, or - for standard input (required)")
	command.Flags().StringVar(&evidencePath, "evidence", evidencePath, "optional tri-state evidence availability: {\"<requirement-id>\": \"present\"|\"absent\"|\"unknown\"}")
	command.Flags().StringArrayVar(&supported, "supported-extension", supported, "extension name this consumer supports (repeatable)")
	command.Flags().BoolVar(&quantifiers, "rfc0008-quantifiers", quantifiers, "DRAFT-RFC PROTOTYPE: admit the spec's RFC 0008 (Draft) collection quantifiers -- exists, every, uniform -- in conditions. A pack using them is NOT valid under any published JPS version; spec validate rejects it, and every successful evaluation payload produced this way is labeled a draft-RFC prototype (a refusal is reported as an operational error and carries no such label)")
	return command
}

// evaluateReadFailure reports a failed evaluation-input read, keeping the
// byte-limit case as informative as spec validate's.
func (a *App) evaluateReadFailure(format, name string, err error) error {
	if errors.Is(err, fssecure.ErrTooLarge) {
		return a.operational("experimental evaluate", format, result.ExitIO, "JPS-RESOURCE-INPUT-BYTE-LIMIT", fmt.Sprintf("The %s input exceeds the hard %d-byte limit.", name, carrier.HardMaxBytes))
	}
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
		Short: "Serve the offline validator and one experimental tool to an MCP client over stdio",
		Long:  "Run a Model Context Protocol server on standard input and output. It exposes the offline validation, conformance, and description operations as MCP tools, plus one explicitly EXPERIMENTAL evaluation tool (experimental_evaluate; ADR-0007) that claims no conformance. It holds no credential, opens no network connection, and authorizes nothing.",
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
				fmt.Fprintf(a.out, "judgment-pack %s\n", result.CLIVersion)
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
		case "experimental":
			if index+1 < len(args) && args[index+1] == "evaluate" {
				return "experimental evaluate"
			}
			return "experimental"
		}
	}
	for _, argument := range args {
		if argument == "version" {
			return "version"
		}
	}
	return "judgment-pack"
}
