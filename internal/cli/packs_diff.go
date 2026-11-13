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
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
	"github.com/spf13/cobra"
)

// packsDiffCommand writes the structural comparison of two pack documents, or
// of two with their common base (ADR-0086). It is the public Go API's
// sdk.Load and sdk.Diff or sdk.ThreeWayDiff and nothing else: the bytes it
// writes are the bytes those return. It loads the packs and never evaluates
// them.
func (a *App) packsDiffCommand() *cobra.Command {
	const commandName = "packs diff"
	format := "human"
	basePath := ""
	command := &cobra.Command{
		Use:   "diff <old-pack> <new-pack>",
		Short: "Compare two pack documents member by member, annotations included, or two with their common base (EXPERIMENTAL)",
		Long: "Load two pack documents, as the public Go API's sdk.Load admits them, and report what they say differently, " +
			"member by member: the pack's own members, and the evidence requirements, sources, outcomes, rules, exceptions " +
			"and value sets and their entries, each added, removed or changed, keyed by id (an entry by its value), with old " +
			"and new values for every member that changed. Annotations (Core 0.5.0) are compared by key at the pack, on each " +
			"outcome and rule and on each value-set entry, with no key treated apart from any other. The order of a " +
			"collection's members is reported apart from their content, and the order of escalation triggers, which Core " +
			"makes a set, is never a difference; docs/building-with-packs.md says which orders are reported. Nothing is " +
			"evaluated: a structural difference says the documents differ, not that they decide any input differently, " +
			"which experimental compare shows. The two packs must have one specVersion; packs of two are refused, because " +
			"one member can mean two things under two versions. Two packs whose ids differ are compared all the same, and " +
			"the first line and the payload's \"differentDecisions\" member say so. --base <pack> makes it three-way: the " +
			"two packs given are first and second, descendants of the base, and each member that differs anywhere is " +
			"reported with its status, first or second when only that one changed it, both when both changed it alike, and " +
			"conflict when they changed it differently; nothing is merged. --format human writes a short text in which " +
			"every string taken from a pack is written as JSON in printable ASCII; --format json writes the standard " +
			"envelope, whose diff (or threeWayDiff) member is a string holding the exact bytes of the comparison's " +
			"canonical JSON. The same pack bytes give the same bytes. It reads no jpack.json and takes no --config; each " +
			"pack is a local file, never standard input. A pack the loader refuses is reported with the loader's code, and " +
			"nothing is compared. It exits 0 whenever the comparison ran, however much differs. The comparison, its format " +
			"and this command are EXPERIMENTAL: each may change or be removed without a compatibility promise, and the " +
			"comparison's own diffVersion or threeWayDiffVersion says which format a reader holds.",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if len(args) != 2 {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-DIFF", "Two packs are required: the old version and the new one, or with --base the first and the second, each a file path.")
			}
			paths, names := args, []string{"old pack", "new pack"}
			if basePath != "" {
				paths, names = []string{basePath, args[0], args[1]}, []string{"base pack", "first pack", "second pack"}
			}
			for _, path := range paths {
				if path == "-" {
					return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-STDIN", "packs diff reads two or three packs, so none of them is read from standard input; pass file paths.")
				}
				if strings.Contains(path, "://") || fssecure.IsRemotePath(path) {
					return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use local files.")
				}
			}
			documents := make([][]byte, 0, len(paths))
			for index, path := range paths {
				data, err := a.readPack(path, carrier.HardMaxBytes)
				if errors.Is(err, fssecure.ErrTooLarge) {
					return a.operational(commandName, format, result.ExitIO, "JPS-RESOURCE-INPUT-BYTE-LIMIT", fmt.Sprintf("The %s exceeds the hard %d-byte limit; nothing was compared.", names[index], carrier.HardMaxBytes))
				}
				if err != nil {
					return a.operational(commandName, format, result.ExitIO, "JPS-INPUT-READ", fmt.Sprintf("The %s could not be read as one bounded regular file.", names[index]))
				}
				documents = append(documents, data)
			}
			compared, err := a.structuralComparison(commandName, format, documents, names)
			if err != nil {
				return err
			}
			if format == "json" {
				var envelope any = result.PackDiff{OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: commandName, Status: "compared", Diff: string(compared.json)}
				if len(documents) == 3 {
					envelope = result.PackThreeWayDiff{OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: commandName, Status: "compared", ThreeWayDiff: string(compared.json)}
				}
				if err := a.writeJSON(envelope); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			if _, err := a.out.Write(compared.text); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&basePath, "base", basePath, "the common base of the two packs, a file path: compare three-way, the two packs given being first and second")
	return command
}

// structuralOutput is one structural comparison's two encodings.
type structuralOutput struct {
	json []byte
	text []byte
}

// structuralComparison loads two pack documents, older and newer, or three,
// base, first and second, and compares them through sdk.Diff or
// sdk.ThreeWayDiff. It never evaluates. A refusal is reported, as the error
// returned, for command in format, naming the pack by names.
func (a *App) structuralComparison(command, format string, documents [][]byte, names []string) (structuralOutput, error) {
	packs := make([]*sdk.Pack, 0, len(documents))
	for index, data := range documents {
		loaded, err := sdk.Load(context.Background(), data, sdk.LoadOptions{})
		if err != nil {
			return structuralOutput{}, a.diffRefusal(command, format, names[index], err)
		}
		packs = append(packs, loaded)
	}
	var compared structuralOutput
	var err error
	if len(packs) == 2 {
		var two view.Diff
		two, err = sdk.Diff(packs[0], packs[1])
		compared = structuralOutput{json: two.JSON(), text: two.Text()}
	} else {
		var three view.ThreeWayDiff
		three, err = sdk.ThreeWayDiff(packs[0], packs[1], packs[2])
		compared = structuralOutput{json: three.JSON(), text: three.Text()}
	}
	if errors.Is(err, view.ErrDiffSpecVersion) {
		return structuralOutput{}, a.operational(command, format, result.ExitUnsupported, "JPS-DIFF-MIXED-SPEC-VERSION", err.Error()+"; nothing was compared.")
	}
	if err != nil {
		return structuralOutput{}, a.operational(command, format, result.ExitInternal, "JPS-DIFF-INTERNAL", "The packs were admitted but could not be compared; this is a fault of this runtime: "+err.Error())
	}
	return compared, nil
}

// diffRefusal reports a pack sdk.Load refused, as describeRefusal does, with
// the pack named: a profile this runtime does not implement as every command
// reports it, and any other refusal with the loader's own code and message.
func (a *App) diffRefusal(command, format, name string, err error) error {
	var refusal *sdk.Refusal
	if !errors.As(err, &refusal) {
		return a.operational(command, format, result.ExitInternal, "JPS-DIFF-INTERNAL", "The "+name+" could not be loaded because of a fault of this runtime: "+err.Error())
	}
	if refusal.Code == "profile-unsupported" {
		return a.evaluationFailure(command, format, &evaluation.Failure{Code: refusal.Code, Profile: refusal.Profile, ExitCode: result.ExitUnsupported})
	}
	return a.operational(command, format, describeRefusalExit(refusal), refusal.Code, "The "+name+" was refused, so nothing was compared. "+refusal.Message)
}
