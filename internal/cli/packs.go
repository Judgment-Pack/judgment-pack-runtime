package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/diagram"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// configFlagUsage is one sentence, used identically wherever the configuration
// can be selected, so the resolution order is never described two ways.
const configFlagUsage = "path to the project's jpack.json; defaults to $JPACK_CONFIG, then ./jpack.json"

// packsCommand is the project-convention namespace (ADR-0012).
//
// It is deliberately a top-level namespace and not a member of spec: nothing
// under it is specified. spec commands answer questions the Judgment Pack
// Specification defines; these answer questions about how one project happens to
// keep its packs, which is this runtime's convention and no other
// implementation's obligation.
func (a *App) packsCommand() *cobra.Command {
	packs := &cobra.Command{
		Use:   "packs",
		Short: "Work with the packs a project declares in its jpack.json (a runtime convention)",
		Long: "Operate on the packs a project declares in its jpack.json (ADR-0012). This convention is " +
			"this runtime's and is NOT part of the Judgment Pack Specification: it names a project's packs and " +
			"where their files are, so one short decision id reaches the same pack from a shell, from a CI step, " +
			"and from an agent's tool call. It selects nothing at decision time -- choosing which decision to ask " +
			"is the application's, and the pack itself judges whether it applies -- it templates nothing, and it " +
			"declares no pack's version as a truth of its own. The pack document's own id and version members are " +
			"what a pack is; the configuration's expectedVersion pin, the optional filename convention, and the " +
			"packId and packVersion members of an evaluation payload are references checked against that document " +
			"rather than second places a version is stated. Every file is read through one rooted reader, and a " +
			"declared path that leaves the configuration's own directory is refused when the configuration is " +
			"validated and again when the file is read. The configuration is selected by --config, then " +
			"$JPACK_CONFIG, then ./jpack.json. Hints a configuration carries are non-normative guidance for an " +
			"agent gathering inputs: this runtime holds no credential, opens no network connection, and never " +
			"reads a source one names. One command here, packs test, runs the EXPERIMENTAL evaluator -- that " +
			"surface may change or be removed without compatibility promise, and the conformance claim for it is " +
			"stated, in full and only, in CONFORMANCE.md, which this text does not restate.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	// --config is registered on the three subcommands that read a configuration
	// rather than on this one as a persistent flag. packs schema prints an
	// embedded artifact and touches no configuration; a flag inherited into its
	// help would promise an effect it does not have.
	packs.AddCommand(
		a.packsListCommand(),
		a.packsValidateCommand(),
		a.packsTestCommand(),
		a.packsSchemaCommand(),
		a.packsDiagramCommand(),
	)
	return packs
}

func (a *App) packsDiagramCommand() *cobra.Command {
	id := ""
	configPath := ""
	command := &cobra.Command{
		Use:   "diagram",
		Short: "Render one declared pack as a Mermaid flowchart (a reading aid, not the document)",
		Long: "Render one pack the project declares as a deterministic Mermaid flowchart: applicability, " +
			"evidence requirements, rules, exceptions, outcomes, fallback, and escalation, each node quoting " +
			"the document member it reads, in document order, the same bytes on every run. The output is a " +
			"reading aid derived from the pack and never a second statement of it: it adds no member, decides " +
			"nothing, and diagrams a document exactly as written whether or not that document validates -- " +
			"spec validate and packs validate hold the verdicts. Pipe the output to any Mermaid renderer " +
			"(GitHub and VS Code render it in Markdown fences). With one declared pack --id is optional; with " +
			"several it names the one to render.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			loaded, failure := a.loadProject(configPath, "packs diagram", "human")
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			selected := id
			if selected == "" {
				if len(loaded.IDs) != 1 {
					return a.operational("packs diagram", "human", result.ExitInvocation, "JPS-INVOCATION-ID",
						"the project declares "+pluralPacks(len(loaded.IDs))+"; name one with --id")
				}
				selected = loaded.IDs[0]
			}
			meta, data, documentFailure := loaded.Document(selected, "packs diagram")
			if documentFailure != nil {
				return a.projectFailure("packs diagram", "human", documentFailure)
			}
			var document map[string]any
			if err := json.Unmarshal(data, &document); err != nil {
				detail := meta.Detail
				if detail == "" {
					detail = err.Error()
				}
				return a.operational("packs diagram", "human", result.ExitInvalid, "JPS-PROJECT-PACK-READ", detail)
			}
			if _, err := io.WriteString(a.out, diagram.Mermaid(document)); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&id, "id", id, "decision id of the pack to render; optional when the project declares exactly one")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

func pluralPacks(n int) string {
	if n == 1 {
		return "1 pack"
	}
	return fmt.Sprintf("%d packs", n)
}

func (a *App) packsListCommand() *cobra.Command {
	format := "human"
	configPath := ""
	command := &cobra.Command{
		Use:   "list",
		Short: "List the project's packs: decision id, the document's own id and version, path, description",
		Long: "List every pack the project's jpack.json declares. Each row carries the project's decision id " +
			"beside the pack document's own id and version, which are two different names and are reported as two " +
			"members for that reason, plus the path, the description, whether a matrix is declared, and whether an " +
			"expectedVersion pin agrees with the document. It reads each pack document to report its identity and " +
			"does not validate it: a project's inventory has to be readable while one pack is mid-edit, and a pack " +
			"that could not be read is listed with the reason instead of being dropped. Run packs validate for the " +
			"verdict on each document.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("packs list", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			loaded, failure := a.loadProject(configPath, "packs list", format)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			if err := a.renderPackInventory(format, loaded.Inventory("packs list")); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

func (a *App) packsValidateCommand() *cobra.Command {
	format := "human"
	id := ""
	configPath := ""
	command := &cobra.Command{
		Use:   "validate",
		Short: "Check the project configuration and every pack it declares",
		Long: "Check one project's jpack.json and the files it points at. The configuration itself is held to " +
			"the embedded schema printed by packs schema and to a configVersion this runtime accepts; a " +
			"configuration that fails either is refused before a single pack is read. Each declared pack is then " +
			"checked in six named steps, and every step is reported with its status so a reader can tell a check " +
			"that passed from one that was never asked for: the declared path resolves inside the configuration's " +
			"own directory, lexically and when resolved against the handle held open on that directory; the pack document " +
			"validates through the semantic layer, exactly as spec validate reports it; an expectedVersion pin, " +
			"when the entry carries one, equals the version the pack document declares; the filename, when it " +
			"follows the optional <decision-id>-<semver>.pack.json convention, names the configuration's key and " +
			"the document's version -- a filename outside the convention is not a defect and is skipped, and one " +
			"inside it is held to it; every hint key names something the pack document has -- an evidence hint a " +
			"declared evidence-requirement id, a fact hint a pointer some condition reads -- because a hint this " +
			"runtime never resolves is instructions an agent follows; and a declared matrix loads as rows. It " +
			"evaluates nothing and runs no row: that is packs test. Exit 0 when every check passed, 1 when any " +
			"failed.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("packs validate", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			loaded, failure := a.loadProject(configPath, "packs validate", format)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			output, projectFailure := loaded.Validate(a.engine, id, "packs validate")
			if projectFailure != nil {
				return a.projectFailure("packs validate", format, projectFailure)
			}
			if err := a.renderPackValidation(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			code := result.ExitSuccess
			if output.Status != "valid" {
				code = result.ExitInvalid
			}
			return &handledExit{code: code}
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&id, "id", id, "check one declared pack by its decision id instead of all of them")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

func (a *App) packsTestCommand() *cobra.Command {
	format := "human"
	id := ""
	configPath := ""
	command := &cobra.Command{
		Use:   "test",
		Short: "EXPERIMENTAL SURFACE: run each pack's instance matrix through the evaluator",
		Long: "Run every declared pack's instance matrix through this runtime's evaluator and report every row. " +
			"This command runs the EXPERIMENTAL evaluator (ADR-0007, ADR-0011): that surface may change or be " +
			"removed without compatibility promise, and the conformance claim for it is stated, in full and only, " +
			"in CONFORMANCE.md -- read that file for the claim, its exact version scope, its evidence, and " +
			"everything it does not assert. This text states no part of it, and neither does any payload this " +
			"command writes; every payload carries a conformanceClaimReference member pointing at that file. A row " +
			"is judged exactly as a row of the bundled evaluation corpus is, by the same code: the RFC 8785 " +
			"canonical §8.3 disposition compared byte for byte against the row's, or the §8.4 error class and " +
			"phase the row expects. Matrix rows use the same case-carrier shape that corpus uses -- id, facts, " +
			"evidenceAvailability, supportedExtensions, and exactly one of expectedDisposition and " +
			"expectedErrorClass. What this reports is what one project's own rows did: it is not the " +
			"specification's corpus, it is evidence about the pack a project wrote rather than about this " +
			"implementation, and no row is an authorization or a statement that acting on a disposition is correct " +
			"(§3.5). A pack that declares no matrix is reported as skipped and never as passed, and a run in which " +
			"no row ran at all is reported skipped rather than passed: a green gate over zero rows would say a " +
			"project was tested when nothing was. Exit 0 when every row matched its expectation, 1 when any did " +
			"not and 1 when no row ran.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("packs test", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			loaded, failure := a.loadProject(configPath, "packs test", format)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			output, projectFailure := loaded.Test(evaluation.NewEngine(a.engine), id, "packs test")
			if projectFailure != nil {
				return a.projectFailure("packs test", format, projectFailure)
			}
			if err := a.renderPackTest(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			code := result.ExitSuccess
			if output.Status != "passed" {
				code = result.ExitInvalid
			}
			return &handledExit{code: code}
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&id, "id", id, "run one declared pack's matrix by its decision id instead of all of them")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

// packsSchemaCommand mirrors spec schema, including its two write guards. The
// schema it prints is this runtime's own and names no specification version:
// jpack.json is a runtime convention with a version of its own.
func (a *App) packsSchemaCommand() *cobra.Command {
	format := "human"
	writeTarget := ""
	command := &cobra.Command{
		Use:   "schema",
		Short: "Inspect or write the jpack.json configuration schema",
		Long: "Print the JSON Schema this runtime holds a jpack.json to, or write its exact bytes. The schema is " +
			"closed: every member it does not name is rejected, so a misspelled key in a configuration is an error " +
			"rather than a silently ignored intention. It describes a convention of this runtime and not any part " +
			"of the Judgment Pack Specification, which is why it names no specification version and carries a " +
			"configVersion of its own.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("packs schema", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if writeTarget == "-" && format == "json" {
				return a.operational("packs schema", format, result.ExitInvocation, "JPS-INVOCATION-STDOUT", "--write - cannot be combined with --format json.")
			}
			if writeTarget != "" && writeTarget != "-" && fssecure.IsRemotePath(writeTarget) {
				return a.operational("packs schema", format, result.ExitInvocation, "JPS-INVOCATION-OUTPUT", "Remote filesystem output paths are not supported.")
			}
			schemaBytes := project.Schema()
			if writeTarget == "-" {
				if _, err := a.out.Write(schemaBytes); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			output := project.SchemaDescription("packs schema")
			if writeTarget != "" {
				if err := writeNewFile(writeTarget, schemaBytes); err != nil {
					return a.operational("packs schema", format, result.ExitIO, "JPS-SCHEMA-WRITE", "Schema destination must be a new writable file.")
				}
				output.WrittenTo = writeTarget
			}
			if err := a.renderConfigSchema(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&writeTarget, "write", writeTarget, "write the schema's exact bytes to a new file or -")
	return command
}

// loadProject resolves and loads the project configuration for one command,
// reporting a load failure in that command's own format. The returned error is
// already a handled exit; a caller returns it unchanged.
func (a *App) loadProject(configPath, command, format string) (*project.Project, error) {
	loaded, failure := project.Load(project.Locate(configPath))
	if failure != nil {
		return nil, a.projectFailure(command, format, failure)
	}
	return loaded, nil
}

// projectFailure reports one project-convention failure through the same
// operational path every other refusal takes. There is no §8.4 class here and
// there must not be: a configuration this runtime could not read is not an
// evaluation error, because no evaluation was ever attempted.
func (a *App) projectFailure(command, format string, failure *project.Failure) error {
	return a.operational(command, format, failure.ExitCode, failure.Code, failure.Message)
}
