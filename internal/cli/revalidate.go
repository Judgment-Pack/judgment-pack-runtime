package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/baseline"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// revalidateCommand takes a revalidation baseline of a project's matrices, or
// compares this runtime's results with one (ADR-0091), so that a pinned
// runtime version is moved only after the candidate is seen to give the
// project's own rows the results the pinned one gave.
func (a *App) revalidateCommand() *cobra.Command {
	const commandName = "experimental revalidate"
	format := "human"
	configPath := ""
	writeTarget := ""
	baselines := []string{}
	command := &cobra.Command{
		Use:   "revalidate",
		Short: "EXPERIMENTAL SURFACE: take a baseline of a project's matrix results, or compare this runtime's results with one before moving a pin",
		Long: "Take a baseline of what this runtime gives for a project's own matrix rows, or compare what this runtime gives " +
			"with a baseline another runtime version took, before a pinned runtime version is moved (ADR-0091). This command " +
			"runs the EXPERIMENTAL evaluator (ADR-0007, ADR-0011): the surface may change or be removed without compatibility " +
			"promise, and the conformance claim for that evaluator is stated, in full and only, in CONFORMANCE.md; this text " +
			"states no part of it. With --write FILE or --write -, every declared pack that has a matrix is run, each pack on " +
			"its own and by the code packs test runs, and a baseline document (baselineVersion \"1\") is written: per pack its " +
			"id, its packId and packVersion, the specVersion its declaration selected, and the sha256 digests of the exact pack " +
			"and matrix bytes its rows ran over; per row its id, its status, and the actual canonical §8.3 disposition, §8.4 " +
			"error class and phase, and handoff target, as packs test reports them. The baseline records what this runtime " +
			"gave, not what the matrix expects: a row that does not match its own expectation is in it as given, and the report " +
			"counts it. A declared pack whose pack or matrix does not load refuses the write, and so does a project with no " +
			"matrix at all. With --baseline FILE, the same matrices are run under this runtime and compared with the baseline " +
			"by pack id and row id: a row is the same exactly when the RFC 8785 canonical form of its {id, status, " +
			"disposition, errorClass, errorPhase, handoffTarget} object is the same bytes on both sides. A row that differs, " +
			"a row or a pack on one side only, a pack whose matrix did not run, and a pack whose pack bytes, matrix bytes, " +
			"packId, packVersion or specVersion differ from the baseline's are each a finding. --baseline also reads a " +
			"packs test --format json report (outputVersion \"2\"), which released runtimes already write, so a pin older " +
			"than this command can be checked: its rows are compared in the same way, but a report records no digest, so its " +
			"pack and matrix bytes are reported not-recorded and never as the same. --baseline may be given more than once, " +
			"for reports taken one pack at a time with packs test --id; a pack held by two baselines is refused. A baseline " +
			"is a closed input: invalid JSON, a duplicate member name, a member neither form carries, a wrong type, or more " +
			"than 64 MiB of baselines together is refused before any row runs. No record is appended, nothing is written " +
			"beside a pack, and no reviewed set is consulted. Exit 0 when there is no finding and 1 when there is any; 2 for a " +
			"baseline of a version this runtime does not read; 3 for a malformed baseline or an invalid invocation; 4 when a " +
			"baseline cannot be read or the baseline to write would exceed 64 MiB. --write exits 0 once the baseline is " +
			"written, 1 when a pack does not load, nothing has a matrix or the destination is a path this configuration " +
			"declares, 4 when the destination exists or cannot be written, and 5 when the baseline cannot be encoded. " +
			"--format renders the report about the run, on stdout, or on stderr when --write - takes stdout for the " +
			"baseline; --write - and --format json are refused together. The procedure is in " +
			"docs/revalidating-a-runtime-pin.md.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if (writeTarget == "") == (len(baselines) == 0) {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-REVALIDATE-MODE", "Give exactly one of --write, to take a baseline, and --baseline, to compare with one.")
			}
			if writeTarget == "-" && format == "json" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-STDOUT", "--write - cannot be combined with --format json: the baseline and the report about the run are two documents, and one stream cannot carry both.")
			}
			if writeTarget != "" && writeTarget != "-" && fssecure.IsRemotePath(writeTarget) {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-OUTPUT", "Remote filesystem output paths are not supported.")
			}
			// Every baseline is read and admitted before the project is run:
			// a malformed one is refused without a row having been spent on it.
			sources, err := a.readBaselines(commandName, format, baselines)
			if err != nil {
				return err
			}
			loaded, failure := a.loadProject(configPath, commandName, format)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			if writeTarget != "" && writeTarget != "-" {
				if owner, declared := loaded.DeclaresOutputPath(writeTarget); declared {
					return a.operational(commandName, format, result.ExitInvalid, "JPS-BASELINE-PATH",
						fmt.Sprintf("The baseline would be written at %s, which this configuration declares as %s. Name another destination; nothing was written.",
							display.Sanitize(writeTarget), display.Sanitize(owner)))
				}
			}
			runs, noMatrix, projectFailure := loaded.RunMatrices(evaluation.NewEngine(a.engine), commandName)
			if projectFailure != nil {
				return a.projectFailure(commandName, format, projectFailure)
			}
			if writeTarget != "" {
				return a.writeBaseline(commandName, format, writeTarget, loaded.ConfigPath, runs, noMatrix)
			}
			comparison, err := baseline.Compare(sources, runs, noMatrix)
			if err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-BASELINE-INVALID", "The baselines could not be compared: "+err.Error()+".")
			}
			output := result.Revalidation{
				OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: commandName, Status: comparison.Status, Experimental: true,
				ConformanceClaimReference: result.EvaluationClaimReference, Label: result.RevalidationLabel, Kind: result.ProjectKind, ConfigPath: loaded.ConfigPath,
				Baselines: comparison.Baselines, Summary: comparison.Summary, Packs: comparison.Packs, Findings: comparison.Findings,
			}
			if err := a.renderRevalidation(format, baselines, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			code := result.ExitSuccess
			if output.Status != "same" {
				code = result.ExitInvalid
			}
			return &handledExit{code: code}
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format for the report about the run: human or json")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	command.Flags().StringVar(&writeTarget, "write", writeTarget, "take a baseline: write it to a new file, or - for stdout")
	command.Flags().StringArrayVar(&baselines, "baseline", baselines, "compare with this baseline or packs test --format json report; repeat for reports taken one pack at a time")
	return command
}

// readBaselines reads and admits every --baseline file, bounded together by
// baseline.MaxBytes, in the order given.
func (a *App) readBaselines(commandName, format string, paths []string) ([]baseline.Source, error) {
	sources := make([]baseline.Source, 0, len(paths))
	remaining := baseline.MaxBytes
	for _, path := range paths {
		if strings.Contains(path, "://") || fssecure.IsRemotePath(path) {
			return nil, a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use a local file.")
		}
		data, err := a.readInput(path, remaining, "a --baseline file")
		if errors.Is(err, fssecure.ErrTooLarge) {
			return nil, a.operational(commandName, format, result.ExitInvocation, "JPS-BASELINE-INVALID", fmt.Sprintf("The baselines exceed %d bytes together.", baseline.MaxBytes))
		}
		if err != nil {
			return nil, a.operational(commandName, format, result.ExitIO, "JPS-BASELINE-READ", fmt.Sprintf("The baseline %s could not be read as one bounded regular file or standard input stream.", display.Sanitize(path)))
		}
		remaining -= int64(len(data))
		source, err := baseline.Decode(data)
		var version *baseline.VersionError
		if errors.As(err, &version) {
			return nil, a.operational(commandName, format, result.ExitUnsupported, "JPS-BASELINE-VERSION", fmt.Sprintf("The baseline %s is of a version this runtime does not read: %s.", display.Sanitize(path), err.Error()))
		}
		if err != nil {
			return nil, a.operational(commandName, format, result.ExitInvocation, "JPS-BASELINE-INVALID", fmt.Sprintf("The baseline %s is not one this runtime reads: %s.", display.Sanitize(path), err.Error()))
		}
		sources = append(sources, source)
	}
	return sources, nil
}

// writeBaseline writes one baseline of every run, or refuses the whole write:
// a baseline that leaves out a pack whose matrix would not load would compare
// "same" over a pack nobody ran.
func (a *App) writeBaseline(commandName, format, writeTarget, configPath string, runs []project.PackRun, noMatrix []string) error {
	if len(runs) == 0 {
		return a.operational(commandName, format, result.ExitInvalid, "JPS-BASELINE-EMPTY", "No declared pack has a matrix, so there is no result to take a baseline of; nothing was written.")
	}
	tool := result.CurrentTool()
	output := result.BaselineTaken{
		OutputVersion: result.OutputVersion, Tool: tool, Command: commandName, Status: "written", Experimental: true,
		ConformanceClaimReference: result.EvaluationClaimReference, Label: result.RevalidationLabel, Kind: result.ProjectKind, ConfigPath: configPath,
		BaselineVersion: baseline.Version, Packs: make([]result.BaselineTakenPack, 0, len(runs)), NoMatrix: noMatrix,
	}
	packs := make([]baseline.Pack, 0, len(runs))
	for _, run := range runs {
		if !run.Ran {
			return a.operational(commandName, format, result.ExitInvalid, "JPS-BASELINE-PACK-NOT-RUN",
				fmt.Sprintf("The matrix of the pack %s did not run, so a baseline would hold no result for it: %s Nothing was written.", display.Sanitize(run.ID), display.Sanitize(run.Detail)))
		}
		taken := result.BaselineTakenPack{ID: run.ID, PackID: run.PackID, PackVersion: run.PackVersion, SpecVersion: run.SpecVersion, Rows: len(run.Rows)}
		for _, row := range run.Rows {
			if row.Status != "passed" {
				taken.Unexpected++
			}
		}
		output.Packs = append(output.Packs, taken)
		packs = append(packs, baseline.PackFrom(run))
	}
	contents, err := baseline.Encode(tool, packs)
	if errors.Is(err, baseline.ErrTooLarge) {
		return a.operational(commandName, format, result.ExitIO, "JPS-RESOURCE-BASELINE-BYTE-LIMIT", fmt.Sprintf("The baseline would exceed the %d bytes a baseline may hold, and a baseline cut short would compare as a different one; nothing was written.", baseline.MaxBytes))
	}
	if err != nil {
		return a.operational(commandName, format, result.ExitInternal, "JPS-BASELINE-ENCODE", "The baseline could not be encoded: "+err.Error()+".")
	}
	stream := a.out
	if writeTarget == "-" {
		if _, err := a.out.Write(contents); err != nil {
			return &handledExit{code: result.ExitIO}
		}
		stream = a.errOut
	} else {
		if err := writeNewFile(writeTarget, contents); err != nil {
			return a.operational(commandName, format, result.ExitIO, "JPS-BASELINE-WRITE", "The baseline destination must be a new writable file.")
		}
		output.WrittenTo = writeTarget
	}
	if err := a.renderBaselineTaken(stream, format, output); err != nil {
		return &handledExit{code: result.ExitIO}
	}
	return nil
}

func (a *App) renderBaselineTaken(writer io.Writer, format string, output result.BaselineTaken) error {
	if format == "json" {
		return writeJSON(writer, output, a.pretty)
	}
	rows, unexpected := 0, 0
	for _, pack := range output.Packs {
		rows += pack.Rows
		unexpected += pack.Unexpected
	}
	destination := "standard output"
	if output.WrittenTo != "" {
		destination = display.Sanitize(output.WrittenTo)
	}
	fmt.Fprintf(writer, "EXPERIMENTAL SURFACE %s\n", output.Label)
	fmt.Fprintf(writer, "BASELINE: %d pack(s), %d row(s), baselineVersion %s, taken by %s %s, written to %s\n",
		len(output.Packs), rows, output.BaselineVersion, display.Sanitize(output.Tool.Name), display.Sanitize(output.Tool.Version), destination)
	for _, pack := range output.Packs {
		fmt.Fprintf(writer, "pack %s: %s %s · specVersion %s · %d row(s)\n",
			display.Sanitize(pack.ID), display.Sanitize(pack.PackID), display.Sanitize(pack.PackVersion), display.Sanitize(pack.SpecVersion), pack.Rows)
	}
	if unexpected > 0 {
		fmt.Fprintf(writer, "AS GIVEN: %d row(s) did not match their own expectation; the baseline records what this runtime gave, and packs test is what judges a row\n", unexpected)
	}
	if len(output.NoMatrix) > 0 {
		shown := make([]string, 0, len(output.NoMatrix))
		for _, id := range output.NoMatrix {
			shown = append(shown, display.Sanitize(id))
		}
		fmt.Fprintf(writer, "NOT IN THE BASELINE: %s declare(s) no matrix, so no row of it ran\n", strings.Join(shown, ", "))
	}
	return nil
}

func (a *App) renderRevalidation(format string, paths []string, output result.Revalidation) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "EXPERIMENTAL SURFACE %s\n", output.Label)
	summary := output.Summary
	if output.Status == "same" {
		fmt.Fprintf(a.out, "SAME: %d pack(s), %d row(s) compared; no row, pack or byte comparison found a difference\n", summary.Packs, summary.RowsCompared)
	} else {
		fmt.Fprintf(a.out, "DIFFERENT: %d finding(s); %d of %d pack(s) and %d of %d compared row(s) the same\n",
			summary.Findings, summary.PacksSame, summary.Packs, summary.RowsSame, summary.RowsCompared)
	}
	for index, source := range output.Baselines {
		form := "baseline, baselineVersion " + display.Sanitize(source.Version)
		if source.Form == baseline.FormReport {
			form = "packs test report, outputVersion " + display.Sanitize(source.Version)
		}
		path := ""
		if index < len(paths) {
			path = display.Sanitize(paths[index])
		}
		fmt.Fprintf(a.out, "baseline %d: %s · %s · taken by %s %s\n", index+1, path, form, display.Sanitize(source.Tool.Name), display.Sanitize(source.Tool.Version))
	}
	if summary.BytesNotRecorded > 0 {
		fmt.Fprintf(a.out, "BYTES NOT COMPARED: %d pack(s) come from a packs test report, which records no digest; their rows were compared, the pack and matrix bytes those rows ran over were not\n", summary.BytesNotRecorded)
	}
	for _, pack := range output.Packs {
		fmt.Fprintf(a.out, "pack %s: %s · %d of %d row(s) the same · bytes %s\n", display.Sanitize(pack.ID), pack.Status, pack.Rows.Same, pack.Rows.Compared, pack.Bytes)
	}
	for _, finding := range output.Findings {
		line := "- " + display.Sanitize(finding.Pack)
		if finding.Row != "" {
			line += " row " + display.Sanitize(finding.Row)
		}
		line += ": " + finding.Kind
		if len(finding.Members) > 0 {
			line += " [" + strings.Join(finding.Members, ", ") + "]"
		}
		fmt.Fprintln(a.out, line)
		if finding.Detail != "" {
			fmt.Fprintf(a.out, "    %s\n", display.Sanitize(finding.Detail))
		}
		if finding.Baseline != "" {
			fmt.Fprintf(a.out, "    baseline:  %s\n", display.Sanitize(finding.Baseline))
		}
		if finding.Candidate != "" {
			fmt.Fprintf(a.out, "    candidate: %s\n", display.Sanitize(finding.Candidate))
		}
	}
	return nil
}
