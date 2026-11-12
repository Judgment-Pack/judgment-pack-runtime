package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
	"github.com/spf13/cobra"
)

// packsDescribeCommand writes the view of one pack (ADR-0074). It is the
// public Go API's sdk.Load and Pack.View and nothing else: the bytes it
// writes are the bytes those return, so a Go program and a shell see the same
// view. It loads the pack and never evaluates it.
func (a *App) packsDescribeCommand() *cobra.Command {
	format := "markdown"
	command := &cobra.Command{
		Use:   "describe <pack-or->",
		Short: "Describe one pack, every member as written, in Markdown or canonical JSON (EXPERIMENTAL)",
		Long: "Load one pack document, as the public Go API's sdk.Load admits it, and write its view: the whole " +
			"document, every member carried and nothing inferred, summarised or evaluated. An absent member is left " +
			"out and a present empty one is shown empty, rules keep the order the document gives them, and numbers " +
			"and decimal strings are shown exactly as written. --format markdown, the default, writes the Markdown " +
			"rendering a reviewer reads, in which every string taken from the pack is escaped so that no Markdown " +
			"syntax in it can add a heading, a table cell, a link, raw HTML or a control character. Escaping does " +
			"not stop a GitHub-flavoured renderer from linking an email address, or a forge from linking mentions, " +
			"issue references and commit ids: to post the Markdown on a forge, attach it as a file or put it inside " +
			"a fenced code block whose fence is longer than any run of backticks in it. --format json writes the standard " +
			"envelope, whose view member is a string holding the exact bytes of the view's canonical JSON; decode " +
			"that string, for example with jq -j .view, to get them. The same pack bytes give the same view bytes. " +
			"The view carries the pack's id, version, specVersion, its profile when it states one, and the sha256 " +
			"digest of the exact bytes read. It reads no jpack.json and takes no --config: the pack is one local " +
			"file or standard input. A pack the loader refuses is reported with the loader's code, and nothing is " +
			"described. The view, its format and this command are EXPERIMENTAL: each may change or be removed " +
			"without a compatibility promise, and the view's own viewVersion says which format a reader holds.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			const commandName = "packs describe"
			if format != "markdown" && format != "json" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", "--format must be markdown or json")
			}
			if args[0] != "-" && (strings.Contains(args[0], "://") || fssecure.IsRemotePath(args[0])) {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL inputs are not supported; use one local file or standard input.")
			}
			data, err := a.readPack(args[0], carrier.HardMaxBytes)
			if errors.Is(err, fssecure.ErrTooLarge) {
				return a.operational(commandName, format, result.ExitIO, "JPS-RESOURCE-INPUT-BYTE-LIMIT", fmt.Sprintf("The pack input exceeds the hard %d-byte limit; nothing was described.", carrier.HardMaxBytes))
			}
			if err != nil {
				return a.operational(commandName, format, result.ExitIO, "JPS-INPUT-READ", "Input could not be read as one bounded regular file or standard input stream.")
			}
			loaded, err := sdk.Load(context.Background(), data, sdk.LoadOptions{})
			if err != nil {
				return a.describeRefusal(commandName, format, err)
			}
			view, err := loaded.View()
			if err != nil {
				return a.operational(commandName, format, result.ExitInternal, "JPS-DESCRIBE-INTERNAL", "The pack was admitted but could not be described; this is a fault of this runtime: "+err.Error())
			}
			if format == "json" {
				if err := a.writeJSON(result.PackDescription{
					OutputVersion: result.OutputVersion,
					Tool:          result.CurrentTool(),
					Command:       commandName,
					Status:        "described",
					View:          string(view.JSON()),
				}); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			if _, err := a.out.Write(view.Markdown()); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: markdown or json")
	return command
}

// describeRefusal reports a pack sdk.Load refused. A profile this runtime
// does not implement is reported as every command reports it; any other
// refusal is an operational error with the loader's own code and message,
// and never an evaluation error, because nothing was evaluated.
func (a *App) describeRefusal(command, format string, err error) error {
	var refusal *sdk.Refusal
	if !errors.As(err, &refusal) {
		return a.operational(command, format, result.ExitInternal, "JPS-DESCRIBE-INTERNAL", "The pack could not be loaded because of a fault of this runtime: "+err.Error())
	}
	if refusal.Code == "profile-unsupported" {
		return a.evaluationFailure(command, format, &evaluation.Failure{Code: refusal.Code, Profile: refusal.Profile, ExitCode: result.ExitUnsupported})
	}
	return a.operational(command, format, describeRefusalExit(refusal), refusal.Code, "The pack was refused, so nothing was described. "+refusal.Message)
}

// describeRefusalExit is the exit code of one Load refusal: the code
// "jpack experimental evaluate" exits with when its preflight refuses the
// same pack at the same step. The refusal does not carry the code, so it is
// derived from what the refusal does carry, by the rules the preflight
// assigns it with; a test holds the two equal over every refused
// conformance case and each refusal Load can give. Only the pack half of the
// preflight runs in Load, so the refusals of the facts and evidence inputs
// (JPS-EVALUATION-INPUT-MISSING among them) and of an unsupported required
// extension, which Evaluate gives, never reach this.
func describeRefusalExit(refusal *sdk.Refusal) int {
	switch {
	case refusal.Code == "profile-unsupported":
		return result.ExitUnsupported
	case strings.HasPrefix(refusal.Code, "JPS-RESOURCE-"):
		return result.ExitIO
	case refusal.Code == "JPS-EVALUATION-INPUT-JSON":
		return result.ExitInvocation
	case refusal.Code == "JPS-EVALUATION-PACK-NOT-CONFORMANT" && capabilityOnly(refusal.Diagnostics):
		// A validation verdict of "unsupported": the pack is well formed and
		// names a capability this runtime lacks.
		return result.ExitUnsupported
	}
	return result.ExitInvalid
}

func capabilityOnly(diagnostics []sdk.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Layer != "capability" {
			return false
		}
	}
	return len(diagnostics) > 0
}
