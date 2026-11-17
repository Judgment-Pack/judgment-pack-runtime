package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/terraformfile"
	"github.com/spf13/cobra"
)

// terraformCommand is the terraform-file source's batch tool (spec RFC 0038
// §6, ADR-0097).
func (a *App) terraformCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "terraform",
		Short: "Read the JSON terraform show -json writes, with every value marked sensitive removed (spec RFC 0038)",
		Long:  "Terraform operations over local files: the JSON terraform show -json writes for a saved plan or for state. Nothing runs Terraform, reads a state backend or a binary plan, or contacts a network. It follows the accepted RFC 0038; the dialect companion's normative text, format table and vectors are not released yet.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(a.terraformFactsCommand())
	return command
}

// terraformFactsCommand writes the selected resources of a plan or a state as
// a facts batch for the outcome gate, one line per resource (RFC 0038 §6).
func (a *App) terraformFactsCommand() *cobra.Command {
	var planPath, statePath, mode, address string
	var types, actions []string
	selectMax := terraformfile.DefaultSelectMax
	maxBytes := terraformfile.DefaultMaxBytes
	command := &cobra.Command{
		Use:   "facts (--plan FILE | --state FILE) --type T[,T...] [--mode managed|data] [--address PATTERN] [--actions a,b] [--max N] [--max-bytes N]",
		Short: "Write a plan's or a state's selected resources as a facts batch for evaluate's outcome gate (spec RFC 0038)",
		Long: "Read the output of terraform show -json for a saved plan (--plan) or for state (--state) by RFC 0038 §2 to §5, then write one batch line per selected resource, sorted by address: " +
			`{"facts": {"resource": <resource>, "terraform": {"document", "formatVersion", "sha256"}}, "id": "<address>"}, RFC 8785 canonical, one per line. ` +
			"Every value marked sensitive is removed, with its member, before anything is selected, and a resource without its sensitivity mirror refuses the file. Numbers are decimal strings in one normal form. " +
			"--type selects resources of these types (at most 64, repeat it or separate with commas), --mode managed (the default) or data resources, --address resources whose whole address matches the pattern, where * matches any run of characters other than '.', '[' and ']', " +
			"and --actions, with --plan only, changes whose actions include one of create, update, delete, read and no-op. More selected resources than --max (1 to 10000, default 1000) refuse the file, as does a file longer than --max-bytes (1 to 268435456, default 67108864). " +
			"Nothing is written unless every line can be: a refused file writes its code, sha256, size and fixed message to standard error, never anything read from the file, and exits 2. " +
			"Exit 0 when the batch was written (an empty one included), 2 when the file was refused, 3 for an unusable command line, and 4 when the file could not be read or the batch written.",
		Example: "jpack terraform facts --plan plan.json --type google_sql_user --actions delete,update > changes.jsonl\n" +
			"jpack experimental evaluate --pack-id sql-user-change --batch changes.jsonl --format json --require-outcome allowed",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			const name = "terraform facts"
			invocation := func(message string) error {
				return a.operational(name, "text", result.ExitInvocation, "JPS-INVOCATION-INPUT", message)
			}
			flags := command.Flags()
			src := terraformfile.Source{Document: terraformfile.DocumentPlan, MaxBytes: maxBytes}
			path := planPath
			switch {
			case (planPath == "") == (statePath == ""):
				return invocation("Supply exactly one of --plan and --state, the output of terraform show -json.")
			case statePath != "":
				src.Document, path = terraformfile.DocumentState, statePath
			}
			if strings.Contains(path, "://") || fssecure.IsRemotePath(path) {
				return invocation("Use a local file or standard input.")
			}
			if message := terraformSelect(&src, flags.Changed("address"), address, mode, types, flags.Changed("actions"), actions, selectMax); message != "" {
				return invocation(message)
			}
			if maxBytes < 1 || maxBytes > terraformfile.MaxBytesLimit {
				return invocation(fmt.Sprintf("--max-bytes is an integer from 1 to %d.", terraformfile.MaxBytesLimit))
			}
			if src.Validate() != nil {
				return invocation("The selection flags do not form a terraform-file source.")
			}
			acquisition, refusal, err := a.readTerraformFile(path, src)
			switch {
			case errors.Is(err, terraformfile.ErrInvalidSource):
				return invocation("The selection flags do not form a terraform-file source.")
			case err != nil:
				return a.operational(name, "text", result.ExitIO, "JPS-INPUT-READ", "The file could not be read as one regular file or standard input stream.")
			case refusal != nil:
				if _, err := a.errOut.Write(append(refusal.JSON(), '\n')); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return &handledExit{code: factsExitNotJudged}
			}
			if err := writeTerraformFacts(a.out, acquisition); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.StringVar(&planPath, "plan", "", "The output of terraform show -json for a saved plan (or - for standard input)")
	flags.StringVar(&statePath, "state", "", "The output of terraform show -json for state (or - for standard input)")
	flags.StringSliceVar(&types, "type", nil, "The resource types selected; repeat or separate with commas, at most 64")
	flags.StringVar(&mode, "mode", "", "The resource mode selected: managed (the default) or data")
	flags.StringVar(&address, "address", "", "Only resources whose whole address matches this pattern; * matches any run of characters other than '.', '[' and ']'")
	flags.StringSliceVar(&actions, "actions", nil, "With --plan only: changes whose actions include one of these (create, update, delete, read, no-op)")
	flags.IntVar(&selectMax, "max", terraformfile.DefaultSelectMax, "The most resources selected; more refuse the file (1 to 10000)")
	flags.Int64Var(&maxBytes, "max-bytes", terraformfile.DefaultMaxBytes, "The longest file read, in bytes (1 to 268435456)")
	return command
}

// terraformSelect fills src.Select from the flags, and returns the message of
// the first rule they break, or "". The rules are RFC 0038 §4's; a flag the
// command line names takes the form its member does, and an empty --address
// or --actions, which would select nothing, is refused as a mistake.
func terraformSelect(src *terraformfile.Source, addressSet bool, address, mode string, types []string, actionsSet bool, actions []string, selectMax int) string {
	selection := &src.Select
	switch {
	case len(types) == 0:
		return "Supply --type, the resource types selected."
	case len(types) > terraformfile.MaxTypes:
		return fmt.Sprintf("--type lists more than %d resource types.", terraformfile.MaxTypes)
	}
	seen := make(map[string]bool, len(types))
	for _, typ := range types {
		if typ == "" {
			return "--type holds an empty resource type."
		}
		if seen[typ] {
			return "--type lists a resource type twice."
		}
		seen[typ] = true
	}
	selection.Types = types
	switch mode {
	case "", terraformfile.ModeManaged, terraformfile.ModeData:
		selection.Mode = mode
	default:
		return "--mode is managed or data."
	}
	if addressSet {
		if address == "" {
			return "--address is an empty pattern, which matches no resource."
		}
		if utf8.RuneCountInString(address) > terraformfile.MaxPatternChars {
			return fmt.Sprintf("--address is longer than %d characters.", terraformfile.MaxPatternChars)
		}
		selection.Address = &address
	}
	if actionsSet {
		if src.Document != terraformfile.DocumentPlan {
			return "--actions selects plan changes, and needs --plan."
		}
		if len(actions) == 0 {
			return "--actions names no action."
		}
		for _, action := range actions {
			switch action {
			case "create", "update", "delete", "read", "no-op":
			default:
				return "--actions holds a value that is not create, update, delete, read or no-op."
			}
		}
		selection.Actions = actions
	}
	if selectMax < 1 || selectMax > terraformfile.SelectMaxLimit {
		return fmt.Sprintf("--max is an integer from 1 to %d.", terraformfile.SelectMaxLimit)
	}
	selection.Max = selectMax
	return ""
}

// readTerraformFile reads the file through the reader, which holds it to the
// source's maxBytes as it reads and never keeps more. Standard input is read
// for "-".
func (a *App) readTerraformFile(path string, src terraformfile.Source) (*terraformfile.Acquisition, *terraformfile.Refusal, error) {
	what := "Terraform " + src.Document
	if path == "-" {
		return terraformfile.Read(a.in, src)
	}
	file, err := a.openInput(path, what)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	return terraformfile.Read(file, src)
}

// writeTerraformFacts writes one batch line per selected resource, in the
// acquisition's address order: {"facts":{"resource":…,"terraform":{…}},"id":…}.
// Every member is RFC 8785 canonical, and the members are in code-point
// order, so the line is canonical.
func writeTerraformFacts(out io.Writer, acquisition *terraformfile.Acquisition) error {
	var terraform []byte
	terraform = append(terraform, `,"terraform":{"document":`...)
	for i, value := range []string{acquisition.Document, acquisition.FormatVersion, acquisition.SHA256} {
		encoded, err := jcs.EncodeDocument(value)
		if err != nil {
			return err
		}
		switch i {
		case 1:
			terraform = append(terraform, `,"formatVersion":`...)
		case 2:
			terraform = append(terraform, `,"sha256":`...)
		}
		terraform = append(terraform, encoded...)
	}
	terraform = append(terraform, `}},"id":`...)
	buffered := bufio.NewWriter(out)
	for _, resource := range acquisition.Resources {
		id, err := jcs.EncodeDocument(resource.Address)
		if err != nil {
			return err
		}
		buffered.WriteString(`{"facts":{"resource":`)
		buffered.Write(resource.JSON)
		buffered.Write(terraform)
		buffered.Write(id)
		if _, err := buffered.WriteString("}\n"); err != nil {
			return err
		}
	}
	return buffered.Flush()
}
