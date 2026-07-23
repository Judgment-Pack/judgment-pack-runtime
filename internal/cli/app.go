package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

type App struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	engine *validation.Engine
	runner *conformance.Runner
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
			if writeJSON(out, result.NewOperationalError(requestedCommand(args), "JPS-ARTIFACT-INTEGRITY", message)) != nil {
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
			if writeJSON(out, result.NewOperationalError(requestedCommand(args), "JPR-INVOCATION-ARGUMENTS", message)) != nil {
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
		Long:          "Judgment Pack Specification tooling. The spec commands validate JPS documents; they do not evaluate decisions or authorize actions.",
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
	root.AddCommand(a.versionCommand(), a.specCommand())
	return root
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
	spec.AddCommand(a.validateCommand(), a.testConformanceCommand(), a.schemaCommand())
	return spec
}

func (a *App) versionCommand() *cobra.Command {
	format := "human"
	command := &cobra.Command{
		Use:   "version",
		Short: "Show CLI and bundled JPS versions",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("version", format, result.ExitInvocation, "JPR-INVOCATION-FORMAT", err.Error())
			}
			set, err := artifacts.Load(artifacts.DraftVersion)
			if err != nil {
				return a.operational("version", format, result.ExitInternal, "JPS-ARTIFACT-INTEGRITY", "Bundled artifact metadata is unavailable.")
			}
			output := result.Version{
				OutputVersion:      result.OutputVersion,
				Tool:               result.CurrentTool(),
				Command:            "version",
				Status:             "valid",
				SupportedSpecs:     artifacts.SupportedVersions(),
				ArtifactProvenance: set.Lock().Source.Kind,
			}
			if format == "json" {
				if err := writeJSON(a.out, output); err != nil {
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
				return a.operational("spec validate", format, result.ExitInvocation, "JPR-INVOCATION-OPTIONS", err.Error())
			}
			if through != "carrier" && through != "structural" && through != "semantic" {
				return a.operational("spec validate", format, result.ExitInvocation, "JPR-INVOCATION-THROUGH", "--through must be carrier, structural, or semantic.")
			}
			if maxBytes <= 0 || maxBytes > carrier.HardMaxBytes {
				return a.operational("spec validate", format, result.ExitInvocation, "JPR-INVOCATION-MAX-BYTES", "--max-bytes must be positive and cannot exceed the hard 10 MiB limit.")
			}
			if args[0] != "-" && (strings.Contains(args[0], "://") || fssecure.IsRemotePath(args[0])) {
				return a.operational("spec validate", format, result.ExitInvocation, "JPR-INVOCATION-INPUT", "URL inputs are not supported; use one local file or standard input.")
			}
			data, err := a.readPack(args[0], maxBytes)
			if err != nil {
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
				return a.operational("spec test-conformance", format, result.ExitInvocation, "JPR-INVOCATION-OPTIONS", err.Error())
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
				return a.operational("spec schema", format, result.ExitInvocation, "JPR-INVOCATION-FORMAT", err.Error())
			}
			if writeTarget == "-" && format == "json" {
				return a.operational("spec schema", format, result.ExitInvocation, "JPR-INVOCATION-STDOUT", "--write - cannot be combined with --format json.")
			}
			if writeTarget != "" && writeTarget != "-" && fssecure.IsRemotePath(writeTarget) {
				return a.operational("spec schema", format, result.ExitInvocation, "JPR-INVOCATION-OUTPUT", "Remote filesystem output paths are not supported.")
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
			var schemaDocument map[string]any
			if err := json.Unmarshal(schemaBytes, &schemaDocument); err != nil {
				return a.operational("spec schema", format, result.ExitInternal, "JPS-ARTIFACT-SCHEMA", "Bundled schema metadata is invalid.")
			}
			sum := sha256.Sum256(schemaBytes)
			output := result.Schema{
				OutputVersion: result.OutputVersion,
				Tool:          result.CurrentTool(),
				Command:       "spec schema",
				Status:        "valid",
				SpecVersion:   args[0],
				SchemaID:      stringFrom(schemaDocument["$id"]),
				Bytes:         len(schemaBytes),
				SHA256:        hex.EncodeToString(sum[:]),
				Provenance:    set.Lock().Source.Kind,
				WrittenTo:     writtenTo,
			}
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
		return nil, errors.New("input exceeds byte limit")
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

func stringFrom(value any) string {
	text, _ := value.(string)
	return text
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
	for index, argument := range args {
		if argument != "spec" || index+1 >= len(args) {
			continue
		}
		switch args[index+1] {
		case "validate", "test-conformance", "schema":
			return "spec " + args[index+1]
		default:
			return "spec"
		}
	}
	for _, argument := range args {
		if argument == "version" {
			return "version"
		}
	}
	return "judgment-pack"
}
