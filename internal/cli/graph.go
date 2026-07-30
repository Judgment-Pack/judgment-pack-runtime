package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/graph"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// graphCommand is the experimental composition namespace (ADR-0015). It lives
// under experimental rather than under packs because everything in it may
// change or vanish, and because its evaluate verb runs the evaluator; the
// project convention it builds on is stable by comparison.
func (a *App) graphCommand() *cobra.Command {
	graphGroup := &cobra.Command{
		Use:   "graph",
		Short: "EXPERIMENTAL SURFACE: compose declared packs into one evaluated graph (spec RFC 0002 prototype)",
		Long: "EXPERIMENTAL composition prototype (ADR-0015), prototyping the composition the Judgment Pack " +
			"Specification's RFC 0002 (Draft) proposes. A graph document composes packs the project's jpack.json " +
			"declares into one directed acyclic structure: a node references one pack by its decision id, an edge " +
			"feeds one node's outcome to another node's inputs -- as a fact written at a declared pointer, as " +
			"tri-state evidence availability, or both -- and one declared result node's disposition is the " +
			"composite's headline. No JPS version defines any of this: the graph format, the evaluation order, and " +
			"the composite payload are conventions of this runtime, they may change or be removed without " +
			"compatibility promise, and every payload is labeled non-normative. Each node is evaluated by the same " +
			"experimental evaluator experimental evaluate runs, whose conformance claim is stated, in full and " +
			"only, in CONFORMANCE.md; this text states no part of it. No command here authorizes anything.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	graphGroup.AddCommand(
		a.graphValidateCommand(),
		a.graphEvaluateCommand(),
		a.graphExplainCommand(),
		a.graphTestCommand(),
		a.graphSchemaCommand(),
	)
	return graphGroup
}

func (a *App) graphValidateCommand() *cobra.Command {
	format := "human"
	configPath := ""
	graphID := ""
	command := &cobra.Command{
		Use:   "validate [graph-or-]",
		Short: "Check graph documents and their references against the project",
		Long: "Check one graph document and its references. The document is held to the strict carrier decode " +
			"every surface uses, the formatVersion gate, and the embedded graph schema printed by experimental " +
			"graph schema, and is refused with the exact reason when it fails one of those. The graph's own rules " +
			"are then checked: every edge endpoint and the result name declared nodes, no edge feeds a node from " +
			"itself, no two edges feed the same fact pointer or the same evidence requirement of one node, no fact " +
			"pointer sits inside another edge's fact pointer for the same node, no fact pointer is deeper than the " +
			"documented injection depth, every node's pack is a decision id the project's jpack.json declares, and " +
			"the edges form a directed acyclic graph. One kind of pack read then happens, and only one: each pack " +
			"an evidence feed targets is read to check that it declares the requirement id the edge names; no " +
			"other pack is opened, and no pack is validated -- spec validate and packs validate are what report " +
			"that. Every finding is reported, not the first one. It evaluates nothing. With no argument it walks " +
			"the project instead: every graph the configuration declares (configVersion 2, ADR-0017), or the one " +
			"--id selects, each held to the same checks plus its declared rows path staying inside the project; a " +
			"configuration declaring no graphs is reported skipped, which is an answer and not a failure. Exit 0 " +
			"when every check passed, 1 when any failed.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			const commandName = "experimental graph validate"
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if len(args) == 1 && graphID != "" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-GRAPH-ID", "--id selects a configured graph in the project walk; with an explicit graph path it is not applicable.")
			}
			if len(args) == 0 {
				loaded, projectFailure := project.Load(project.Locate(configPath))
				if projectFailure != nil {
					return a.projectFailure(commandName, format, projectFailure)
				}
				defer loaded.Close()
				output, walkFailure := graph.ValidateProject(loaded, graphID, commandName)
				if walkFailure != nil {
					return a.evaluationFailure(commandName, format, walkFailure)
				}
				if err := a.renderGraphValidationSuite(format, output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				code := result.ExitSuccess
				if output.Status == "invalid" {
					code = result.ExitInvalid
				}
				return &handledExit{code: code}
			}
			document, graphPath, loaded, failure := a.loadGraph(commandName, format, args[0], configPath)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			output := result.GraphValidation{
				OutputVersion: result.OutputVersion,
				Tool:          result.CurrentTool(),
				Command:       commandName,
				Status:        "valid",
				Kind:          result.ProjectKind,
				FormatVersion: graph.FormatVersion,
				ConfigPath:    loaded.ConfigPath,
				GraphPath:     graphPath,
				GraphID:       document.ID,
				GraphVersion:  document.Version,
				Diagnostics:   document.Semantic(loaded),
			}
			if len(output.Diagnostics) > 0 {
				output.Status = "invalid"
			}
			if err := a.renderGraphValidation(format, output); err != nil {
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
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	command.Flags().StringVar(&graphID, "id", graphID, "check one declared graph by its configured id instead of all of them (project walk only)")
	return command
}

func (a *App) graphEvaluateCommand() *cobra.Command {
	format := "human"
	inputsPath := ""
	supported := []string{}
	configPath := ""
	command := &cobra.Command{
		Use:   "evaluate <graph-or->",
		Short: "EXPERIMENTAL SURFACE: evaluate every node of one graph in dependency order",
		Long: "Evaluate every node of one graph, in the deterministic topological order, each node as one ordinary " +
			"experimental evaluation of its resolved pack -- the same resolution model, input preflight, portable " +
			"disposition, and error classes as experimental evaluate, by the same engine. Inputs are supplied per " +
			"node in one JSON document keyed by node id, each entry an object with optional facts and evidence " +
			"members; a node with no entry evaluates against an empty facts document. An edge feeds forward what " +
			"its upstream node produced: an outcome id is written at the declared fact pointer -- never over a " +
			"caller's value, a collision is refused -- and an evidence feed contributes present for an outcome and " +
			"its declared onUnresolved tri-state, unknown by default, for anything else, so an upstream that " +
			"resolved nothing reaches a downstream pack only through that pack's own declared unknown handling. An " +
			"escalation any node requests is aggregated beside the composite's headline, which echoes the declared " +
			"result node's own disposition. A node the engine refuses refuses the whole run, with that node named " +
			"and its error class intact; no partial composite is reported, because errors are not dispositions. " +
			"The composite envelope is this runtime's own: no JPS version defines a graph or a composite result, " +
			"and composition is the specification's RFC 0002, a draft proposal. The evaluator's conformance claim " +
			"is stated, in full and only, in CONFORMANCE.md; this text states no part of it, and no result is an " +
			"authorization, an executed action, or any statement about whether acting on any disposition is " +
			"correct (§3.5). Producing a composite exits 0.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			const commandName = "experimental graph evaluate"
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if args[0] == "-" && inputsPath == "-" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-STDIN", "The graph and the inputs document cannot both be standard input.")
			}
			if inputsPath != "" && inputsPath != "-" && (strings.Contains(inputsPath, "://") || fssecure.IsRemotePath(inputsPath)) {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use local files or standard input.")
			}
			document, graphPath, loaded, failure := a.loadGraph(commandName, format, args[0], configPath)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			var inputs []byte
			if inputsPath != "" {
				data, err := a.readPack(inputsPath, graph.MaxInputsBytes)
				if errors.Is(err, fssecure.ErrTooLarge) {
					return a.operational(commandName, format, result.ExitIO, "JPS-RESOURCE-GRAPH-INPUTS-BYTE-LIMIT", "The inputs document exceeds this runtime's documented byte limit.")
				}
				if err != nil {
					return a.operational(commandName, format, result.ExitIO, "JPS-INPUT-READ", "The inputs document could not be read as one bounded regular file or standard input stream.")
				}
				inputs = data
			}
			output, evaluateFailure := graph.Evaluate(loaded, evaluation.NewEngine(a.engine), document, graphPath, inputs, inputsPath != "", graph.Options{
				Command:             commandName,
				SupportedExtensions: supported,
			})
			if evaluateFailure != nil {
				return a.evaluationFailure(commandName, format, evaluateFailure)
			}
			if err := a.renderGraphEvaluation(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&inputsPath, "inputs", inputsPath, "JSON inputs document keyed by node id, each entry {\"facts\": <document>, \"evidence\": {\"<requirement-id>\": \"present\"|\"absent\"|\"unknown\"}}: a file path, or - for standard input")
	command.Flags().StringArrayVar(&supported, "supported-extension", supported, "extension name this consumer supports, applied to every node (repeatable)")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

func (a *App) graphExplainCommand() *cobra.Command {
	format := "human"
	configPath := ""
	command := &cobra.Command{
		Use:   "explain <graph-or->",
		Short: "Print one graph's evaluation plan without evaluating anything",
		Long: "Print one graph's evaluation plan: the deterministic node order, each node's resolved pack with the " +
			"document's own identity echoed beside the project's decision id, and every feed each node would " +
			"receive before it runs. Pack documents are read only for that identity echo -- a document that cannot " +
			"be read is reported on its step rather than failing the plan -- and nothing is evaluated: no condition " +
			"is interpreted, no disposition is produced, and the plan authorizes nothing. A graph whose own " +
			"graph-level checks fail -- its nodes, edges, result, and acyclicity -- has no plan; run experimental " +
			"graph validate for the complete report, which also checks the references only reading packs can check.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			const commandName = "experimental graph explain"
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			document, graphPath, loaded, failure := a.loadGraph(commandName, format, args[0], configPath)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			output, planFailure := graph.Plan(loaded, document, graphPath, commandName)
			if planFailure != nil {
				return a.evaluationFailure(commandName, format, planFailure)
			}
			if err := a.renderGraphPlan(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

func (a *App) graphTestCommand() *cobra.Command {
	format := "human"
	rowsPath := ""
	supported := []string{}
	configPath := ""
	graphID := ""
	command := &cobra.Command{
		Use:   "test [graph-or-]",
		Short: "EXPERIMENTAL SURFACE: run graph matrix rows and compare every disposition byte for byte",
		Long: "Run every row of one graph matrix through this runtime's experimental composition and report " +
			"each one. This command runs the EXPERIMENTAL evaluator on every node of every row (ADR-0007, " +
			"ADR-0011, ADR-0015): both surfaces may change or be removed without compatibility promise, and " +
			"the conformance claim for the evaluator is stated, in full and only, in CONFORMANCE.md -- this " +
			"text states no part of it, and neither does any payload this command writes. A row carries one " +
			"inputs document for the whole graph and exactly one expectation: the composite headline " +
			"disposition, or the evaluation error class (and optionally phase) of a refused run. A row may " +
			"also pin named nodes' dispositions with expectedNodes; a node a row does not name is unchecked, " +
			"which is the row author's stated choice. Every disposition comparison is the RFC 8785 canonical " +
			"byte comparison used everywhere else -- the composite headline and each named node -- and a row " +
			"whose expectation is not a legal disposition is a mismatch of the row, never compared loosely. A " +
			"refusal carrying no error class -- a graph-layer or invocation refusal, such as a feed colliding " +
			"with a row's own inputs -- is deliberately unassertable and mismatches every expectation: rows " +
			"assert the stable vocabularies only, and this runtime's diagnostic codes are provisional detail. " +
			"What this reports is what one project's rows did: no row is an authorization, and no JPS version " +
			"defines a graph, a composition, or a composite result. With no argument it walks the project " +
			"instead: every graph the configuration declares (configVersion 2, ADR-0017), or the one --id " +
			"selects, each entry that graph's declared rows run exactly as the path-named form runs them, " +
			"coverage included; an entry declaring no rows is skipped and says so, and a walk in which no row " +
			"ran at all reports skipped and exits 1, because a green gate over nothing tested is not a pass. " +
			"Exit 0 when every row matched its expectation, 1 when any did not (or none ran).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			const commandName = "experimental graph test"
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			// One constructor for both forms: the walk and the path-named run
			// are one surface, and CONFORMANCE.md's enumeration is held to
			// constructor sites per file.
			evaluator := evaluation.NewEngine(a.engine)
			if len(args) == 0 {
				if rowsPath != "" {
					return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-ROWS", "--rows names a rows document for one path-named graph; the project walk reads each configured graph's declared rows.")
				}
				loaded, projectFailure := project.Load(project.Locate(configPath))
				if projectFailure != nil {
					return a.projectFailure(commandName, format, projectFailure)
				}
				defer loaded.Close()
				output, walkFailure := graph.TestProject(loaded, evaluator, graphID, graph.Options{
					Command:             commandName,
					SupportedExtensions: supported,
				})
				if walkFailure != nil {
					return a.evaluationFailure(commandName, format, walkFailure)
				}
				if err := a.renderGraphSuite(format, output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				code := result.ExitSuccess
				if output.Status != "passed" {
					code = result.ExitInvalid
				}
				return &handledExit{code: code}
			}
			if graphID != "" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-GRAPH-ID", "--id selects a configured graph in the project walk; with an explicit graph path it is not applicable.")
			}
			if rowsPath == "" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-ROWS", "--rows is required: one graph matrix document (a file path, or - for standard input).")
			}
			if args[0] == "-" && rowsPath == "-" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-STDIN", "The graph and the rows document cannot both be standard input.")
			}
			if rowsPath != "-" && (strings.Contains(rowsPath, "://") || fssecure.IsRemotePath(rowsPath)) {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use local files or standard input.")
			}
			document, graphPath, loaded, failure := a.loadGraph(commandName, format, args[0], configPath)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			data, err := a.readPack(rowsPath, graph.MaxRowsBytes)
			if errors.Is(err, fssecure.ErrTooLarge) {
				return a.operational(commandName, format, result.ExitIO, "JPS-RESOURCE-GRAPH-ROWS-BYTE-LIMIT", "The rows document exceeds this runtime's documented byte limit.")
			}
			if err != nil {
				return a.operational(commandName, format, result.ExitIO, "JPS-INPUT-READ", "The rows document could not be read as one bounded regular file or standard input stream.")
			}
			rows, loadFailure := graph.LoadRows(data, rowsPath)
			if loadFailure != nil {
				return a.evaluationFailure(commandName, format, loadFailure)
			}
			output, testFailure := graph.Test(loaded, evaluator, document, graphPath, rowsPath, rows, graph.Options{
				Command:             commandName,
				SupportedExtensions: supported,
			})
			if testFailure != nil {
				return a.evaluationFailure(commandName, format, testFailure)
			}
			if err := a.renderGraphTest(format, output); err != nil {
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
	command.Flags().StringVar(&rowsPath, "rows", rowsPath, "the graph matrix document: {\"cases\":[{\"id\",\"inputs\",\"expectedDisposition\"|\"expectedErrorClass\"[,\"expectedNodes\"]}]}; a file path, or - for standard input (required with a path-named graph)")
	command.Flags().StringArrayVar(&supported, "supported-extension", supported, "extension name this consumer supports, applied to every node (repeatable)")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	command.Flags().StringVar(&graphID, "id", graphID, "run one declared graph's rows by its configured id instead of all of them (project walk only)")
	return command
}

// graphSchemaCommand mirrors packs schema, including its two write guards. The
// schema it prints is this runtime's own and names no specification version:
// the graph document is an experimental runtime convention with a version of
// its own.
func (a *App) graphSchemaCommand() *cobra.Command {
	format := "human"
	writeTarget := ""
	command := &cobra.Command{
		Use:   "schema",
		Short: "Inspect or write the graph document schema",
		Long: "Print the JSON Schema this runtime holds a graph document to, or write its exact bytes. The schema " +
			"is closed: every member it does not name is rejected, so a misspelled key in a graph is an error " +
			"rather than a silently ignored intention. It describes an experimental convention of this runtime and " +
			"no part of the Judgment Pack Specification, which is why it names no specification version and " +
			"carries a formatVersion of its own.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const commandName = "experimental graph schema"
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if writeTarget == "-" && format == "json" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-STDOUT", "--write - cannot be combined with --format json.")
			}
			if writeTarget != "" && writeTarget != "-" && fssecure.IsRemotePath(writeTarget) {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-OUTPUT", "Remote filesystem output paths are not supported.")
			}
			schemaBytes := graph.Schema()
			if writeTarget == "-" {
				if _, err := a.out.Write(schemaBytes); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			output := graph.SchemaDescription(commandName)
			if writeTarget != "" {
				if err := writeNewFile(writeTarget, schemaBytes); err != nil {
					return a.operational(commandName, format, result.ExitIO, "JPS-SCHEMA-WRITE", "Schema destination must be a new writable file.")
				}
				output.WrittenTo = writeTarget
			}
			if err := a.renderGraphSchema(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&writeTarget, "write", writeTarget, "write the schema's exact bytes to a new file or -")
	return command
}

// loadGraph reads and loads one graph document and the project it references,
// for one command: the argument names the graph (a path, or -), and the
// project resolves exactly as every packs command resolves it. The returned
// error is already a handled exit; a caller returns it unchanged. On success
// the caller owns closing the project.
func (a *App) loadGraph(commandName, format, argument, configPath string) (graph.Document, string, *project.Project, error) {
	if argument != "-" && (strings.Contains(argument, "://") || fssecure.IsRemotePath(argument)) {
		return graph.Document{}, "", nil, a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use local files or standard input.")
	}
	data, err := a.readPack(argument, graph.MaxGraphBytes)
	if errors.Is(err, fssecure.ErrTooLarge) {
		return graph.Document{}, "", nil, a.operational(commandName, format, result.ExitIO, "JPS-RESOURCE-GRAPH-BYTE-LIMIT", "The graph document exceeds this runtime's documented byte limit.")
	}
	if err != nil {
		return graph.Document{}, "", nil, a.operational(commandName, format, result.ExitIO, "JPS-INPUT-READ", "The graph document could not be read as one bounded regular file or standard input stream.")
	}
	document, loadFailure := graph.Load(data, argument)
	if loadFailure != nil {
		return graph.Document{}, "", nil, a.evaluationFailure(commandName, format, loadFailure)
	}
	loaded, projectFailure := project.Load(project.Locate(configPath))
	if projectFailure != nil {
		return graph.Document{}, "", nil, a.projectFailure(commandName, format, projectFailure)
	}
	return document, argument, loaded, nil
}
