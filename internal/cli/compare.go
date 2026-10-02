package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/compare"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func (a *App) compareCommand() *cobra.Command {
	const commandName = "experimental compare"
	format := "human"
	inputsPath := ""
	supported := []string{}
	command := &cobra.Command{
		Use:   "compare <old-pack> <new-pack>",
		Short: "EXPERIMENTAL SURFACE: evaluate one set of inputs under two versions of a pack and list the inputs they decide differently",
		Long: "Evaluate every input of --inputs under two pack documents and report the inputs whose results differ (ADR-0045). " +
			"The inputs are a matrix, admitted under its own rules but whose expectations play no part in the comparison, or a candidates document written by packs suggest; each is held to its own closed shape. " +
			"Each input is evaluated as packs test evaluates a row that reaches evaluation -- its facts, its evidence availability, and its own supported extensions, joined by any --supported-extension names -- under the old pack and under the new one. " +
			"An input differs when the two JPS §8.3 canonical dispositions are not byte for byte equal, when the handoff targets differ, or when one evaluation is refused and the other is not, or both are refused differently; each difference lists both results and names what changed. " +
			"Inputs that are the same are counted, not listed. " +
			"Inputs unresolved under both versions are counted too, whether or not they differ, because an input that reaches no outcome under either version cannot show a change in which outcome it gets; when that is every input, the human output says the comparison could not see a change in any outcome, and that packs suggest --base writes candidates carrying a reviewed row's other facts and evidence. " +
			"Two packs whose ids differ are two decisions rather than two versions of one: they are compared all the same, and when both ids were read and differ, the human output's first line and the payload's \"differentDecisions\" member say so. " +
			"An id is read from an evaluation that succeeds, so when no input is evaluated, or every evaluation under one pack is refused, neither appears, and their absence does not establish that the ids match. " +
			"A difference says the two versions decide an input differently and nothing about which is right: it is not an expectation, and nothing here is a decision. " +
			"The command opens no project, so it appends no audit record and consults no reviewed set, and its payload carries \"rehearsal\": true (ADR-0028). " +
			"It exits 0 whenever the comparison ran, however many inputs differ; a run whose differences would pass 16 MiB of compact JSON is refused rather than truncated, and the rendered report is bounded by that and proportional to it. " +
			"The draft-RFC opt-ins of experimental evaluate are not offered here. " +
			"This runtime's conformance claim is stated, in full and only, in CONFORMANCE.md; this text states no claim.",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if len(args) != 2 {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-COMPARE", "Two packs are required: the old version and the new one, each a file path.")
			}
			if inputsPath == "" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-COMPARE", "--inputs is required: a matrix or a packs suggest candidates document, a file path.")
			}
			for _, input := range []string{args[0], args[1], inputsPath} {
				if input == "-" {
					return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-STDIN", "compare reads three documents, so none of them is read from standard input; pass file paths.")
				}
				if strings.Contains(input, "://") || fssecure.IsRemotePath(input) {
					return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use local files.")
				}
			}
			sides := [2]compare.Side{}
			for index, path := range args {
				pack, oversized, err := a.readEvaluationInput(path)
				if err != nil {
					name := [2]string{"old pack", "new pack"}[index]
					return a.operational(commandName, format, result.ExitIO, "JPS-INPUT-READ", fmt.Sprintf("The %s could not be read as one bounded regular file.", name))
				}
				sides[index] = compare.Side{Path: path, Pack: pack, Oversized: oversized}
			}
			data, err := a.readPack(inputsPath, project.MaxMatrixBytes)
			if errors.Is(err, fssecure.ErrTooLarge) {
				return a.operational(commandName, format, result.ExitInvalid, "JPS-COMPARE-INPUTS", "The inputs document exceeds the matrix byte limit.")
			}
			if err != nil {
				return a.operational(commandName, format, result.ExitIO, "JPS-INPUT-READ", "The inputs document could not be read as one bounded regular file.")
			}
			kind, inputs, err := compare.ReadInputs(data)
			if err != nil {
				return a.operational(commandName, format, result.ExitInvalid, "JPS-COMPARE-INPUTS", err.Error())
			}
			comparison, err := compare.Run(evaluation.NewEngine(a.engine), sides[0], sides[1], kind, inputs, supported, commandName)
			if errors.Is(err, compare.ErrReportTooLarge) {
				return a.operational(commandName, format, result.ExitIO, "JPS-RESOURCE-COMPARE-REPORT-LIMIT", fmt.Sprintf("The comparison's differences would exceed %d bytes of JSON; compare fewer inputs at a time.", compare.MaxReportBytes))
			}
			if err != nil {
				return a.operational(commandName, format, result.ExitIO, "JPS-COMPARE-REPORT", "The comparison's report could not be composed.")
			}
			if err := a.renderComparison(format, comparison); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&inputsPath, "inputs", inputsPath, "the inputs: a matrix (its expectations play no part) or a packs suggest candidates document, a file path")
	command.Flags().StringArrayVar(&supported, "supported-extension", supported, "extension name this consumer supports, joined to each input's own (repeatable)")
	return command
}

// renderComparison reports one comparison. Two packs that are different
// decisions are said to be so first; then the label leads, the rehearsal line
// says nothing was recorded, the totals count the inputs unresolved under both
// versions, a line says so when that is every input, and each difference is
// one line naming the input, both results, and what changed.
func (a *App) renderComparison(format string, output result.PackComparison) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	if output.DifferentDecisions {
		fmt.Fprintln(a.out, "DIFFERENT DECISIONS: the two packs have different ids, so this compares two decisions, not two versions of one")
	}
	fmt.Fprintf(a.out, "EXPERIMENTAL SURFACE %s\n", output.Label)
	fmt.Fprintln(a.out, "REHEARSAL: not a decision; no audit record was appended and no reviewed set was consulted")
	for _, named := range []struct {
		name string
		pack result.ComparedPack
	}{{"old", output.Old}, {"new", output.New}} {
		identity := "identity unread"
		if named.pack.PackID != "" || named.pack.PackVersion != "" {
			identity = display.Sanitize(named.pack.PackID) + " " + display.Sanitize(named.pack.PackVersion)
		}
		digest := named.pack.Digest
		if digest == "" {
			digest = "digest unavailable (over the byte limit)"
		}
		fmt.Fprintf(a.out, "%s: %s · %s · %s\n", named.name, identity, display.Sanitize(named.pack.Path), digest)
	}
	kind := "a matrix (its expectations play no part)"
	if output.Inputs.Kind == "candidates" {
		kind = "a candidates document"
	}
	fmt.Fprintf(a.out, "inputs: %d from %s; %d differ, %d the same; %d of the %d unresolved under both versions\n",
		output.Inputs.Count, kind, output.Inputs.Different, output.Inputs.Same, output.Inputs.UnresolvedUnderBoth, output.Inputs.Count)
	if output.Inputs.Count > 0 && output.Inputs.UnresolvedUnderBoth == output.Inputs.Count {
		fmt.Fprintln(a.out, "NOTHING RESOLVED: every input was unresolved under both versions, so this comparison could not see a change in any outcome; "+
			"packs suggest --base <row-id> writes candidates that carry a reviewed row's other facts and evidence")
	}
	for _, difference := range output.Differences {
		fmt.Fprintf(a.out, "- %s: %s -> %s [%s]\n", display.Sanitize(difference.ID), comparedText(difference.Old), comparedText(difference.New), strings.Join(difference.Changed, ", "))
	}
	return nil
}

// comparedText is one side of a difference in a few words.
func comparedText(side result.ComparedSide) string {
	if side.EvaluationError != nil {
		refusal := "refused " + display.Sanitize(side.EvaluationError.Code)
		if side.EvaluationError.Class != "" {
			refusal += " (" + display.Sanitize(side.EvaluationError.Class) + ", " + display.Sanitize(side.EvaluationError.Phase) + ")"
		}
		return refusal
	}
	disposition := side.Disposition
	text := display.Sanitize(disposition.Kind)
	if disposition.OutcomeID != "" {
		text += " " + display.Sanitize(disposition.OutcomeID)
	}
	if len(disposition.Reasons) > 0 {
		text += " (" + display.Sanitize(strings.Join(disposition.Reasons, ", ")) + ")"
	}
	if disposition.Handoff.State == "requested" {
		target := "no declared destination"
		if side.HandoffTarget != nil {
			target = fmt.Sprintf("%s %q", display.Sanitize(side.HandoffTarget.Kind), display.Sanitize(side.HandoffTarget.Name))
		}
		text += ", handoff to " + target
	}
	return text
}
