package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// auditCommand is the jpack audit group (ADR-0047 §1): reading a chained trail,
// handing over its checkpoint, and starting a new segment after a damaged end.
func (a *App) auditCommand() *cobra.Command {
	group := &cobra.Command{
		Use:   "audit",
		Short: "Check, checkpoint and repair a chained audit trail",
		Long: "Operations on the audit trail a project keeps (ADR-0018), chained over its exact bytes (ADR-0047). " +
			"verify checks every trail, sequence and previous from the first chained record on; checkpoint prints the checkpoint of the last chained record, for handing to someone who will hold it; " +
			"repair starts a new segment after a last line a write did not complete. " +
			"None of them evaluates anything.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	group.AddCommand(a.auditVerifyCommand(), a.auditCheckpointCommand(), a.auditRepairCommand())
	return group
}

func (a *App) auditVerifyCommand() *cobra.Command {
	const commandName = "audit verify"
	format := "human"
	trailPath := ""
	configPath := ""
	expectPath := ""
	command := &cobra.Command{
		Use:   "verify",
		Short: "Check a chained audit trail, alone or against a checkpoint",
		Long: "Read an audit trail over its exact bytes and check every trail, sequence and previous from the first chained record on, by the rules the writer chains by (ADR-0047): " +
			"a chained line's sequence is its line number, it carries the identity of the last chained line before it, and its previous is the SHA-256 of the line before it when that line is chained, and of the whole trail before it when it is not. " +
			"The trail is --trail <file>, or else the one the project's jpack.json declares. " +
			"The size is read under the writer's lock, shared, so it falls between two writes, and the bytes before it are read without the lock: writers only append, so they are not delayed and nothing read changes underneath. " +
			"The report gives the coverage: lines before the first chained line (committed as one block), chained lines, unchained lines a later chained line commits to, lines nothing commits to, and lines a repair names as damaged; signatures are not available yet (#209). " +
			"Without --expect this is the integrity of one supplied chain: the lines are consistent with one another, which does not show the trail is complete, or that its last line, or lines rewritten from some point on with their links recomputed, are the ones first written. " +
			"With --expect <checkpoint>, a checkpoint held independently of the operator (jpack audit checkpoint prints one), the line at its sequence must be a chained record of its trail with its record digest: a trail that is shorter, has another identity, or has another record there fails. " +
			"A trail a repair has segmented is reported segment by segment, and never as intact across a discontinuity. " +
			"Exit 0 when every check passed, segmented or not, and 1 when any failed; each failed check is a named finding.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			var expect *result.AuditCheckpoint
			if expectPath != "" {
				checkpoint, failure := a.readCheckpoint(commandName, format, expectPath)
				if failure != nil {
					return failure
				}
				expect = &checkpoint
			}
			file, shownPath, failure := a.openTrail(commandName, format, trailPath, configPath)
			if failure != nil {
				return failure
			}
			defer file.Close()
			chain, locked, failure := a.readTrail(commandName, format, file, expect)
			if failure != nil {
				return failure
			}
			output := result.AuditVerification{
				OutputVersion:         result.OutputVersion,
				Tool:                  result.CurrentTool(),
				Command:               commandName,
				TrailPath:             shownPath,
				SnapshotBetweenWrites: locked,
				AuditChain:            chain,
			}
			if err := a.renderAuditVerification(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			if chain.Status == "invalid" {
				return &handledExit{code: result.ExitInvalid}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&trailPath, "trail", trailPath, "the trail file to read; without it, the project's own")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	command.Flags().StringVar(&expectPath, "expect", expectPath, "a checkpoint document, held independently, to hold the trail to")
	return command
}

func (a *App) auditCheckpointCommand() *cobra.Command {
	const commandName = "audit checkpoint"
	format := "human"
	trailPath := ""
	configPath := ""
	command := &cobra.Command{
		Use:   "checkpoint",
		Short: "Print the checkpoint of an audit trail's last chained record",
		Long: "Print the checkpoint of the last chained record of an audit trail (ADR-0047): its trail identity, its sequence, and the SHA-256 of its exact line bytes, which is also what a gateway action receipt's decision.recordDigest names for that record. " +
			"Human output is the checkpoint document alone, one line in its RFC 8785 canonical form, so it can be saved and handed to whoever will hold it; jpack audit verify --expect reads it back. " +
			"A checkpoint protects only what it covers and only as well as its holder keeps it: given to someone the operator does not control, it later shows whether the trail up to that record is the one that existed when it was made. " +
			"The trail is read and verified first, and a trail that fails a check is refused rather than given a checkpoint. " +
			"Lines after the last chained record are not covered, and a note on standard error says how many.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			file, shownPath, failure := a.openTrail(commandName, format, trailPath, configPath)
			if failure != nil {
				return failure
			}
			defer file.Close()
			chain, _, failure := a.readTrail(commandName, format, file, nil)
			if failure != nil {
				return failure
			}
			if chain.Status == "invalid" {
				first := chain.Findings[0]
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-CHECKPOINT-REFUSED",
					fmt.Sprintf("The trail fails %d check(s), the first %s at line %d, so no checkpoint is given for it; jpack audit verify lists them.", chain.FindingsTotal, first.Name, first.Line))
			}
			if chain.Head == nil {
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-CHECKPOINT-NONE", "The trail has no chained record to checkpoint.")
			}
			output := result.AuditCheckpointReport{
				OutputVersion:  result.OutputVersion,
				Tool:           result.CurrentTool(),
				Command:        commandName,
				Status:         "checkpointed",
				TrailPath:      shownPath,
				Checkpoint:     *chain.Head,
				UncoveredLines: chain.Coverage.Uncovered,
			}
			if format == "json" {
				if err := a.writeJSON(output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			if _, err := a.out.Write(audit.EncodeCheckpoint(output.Checkpoint)); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			if output.UncoveredLines > 0 {
				fmt.Fprintf(a.errOut, "note: the %d line(s) after sequence %d are not chained, and this checkpoint does not cover them\n", output.UncoveredLines, output.Checkpoint.Sequence)
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human (the checkpoint document) or json")
	command.Flags().StringVar(&trailPath, "trail", trailPath, "the trail file to read; without it, the project's own")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

func (a *App) auditRepairCommand() *cobra.Command {
	const commandName = "audit repair"
	format := "human"
	configPath := ""
	command := &cobra.Command{
		Use:   "repair",
		Short: "Start a new segment after an audit trail's incomplete last line",
		Long: "Repair the project's chained audit trail after a write that did not complete, which leaves a last line with no newline that the writer will not chain after (ADR-0047). " +
			"Nothing is removed or rewritten: under the writer's lock, the damaged bytes are ended with a newline and kept in place as a line of their own, and a discontinuity record is appended after them, naming that line, its length and the SHA-256 of its bytes, and linking over it to what a record in its place would have followed. " +
			"The writer then chains after the discontinuity, and jpack audit verify reports the trail as segments, never as intact across it. " +
			"It is refused when the last line is complete, so it never runs on a trail with nothing damaged at its end; a broken link elsewhere is not repaired, since jpack audit verify reports it and nothing appended after it would make it less broken. " +
			"It is refused too for a project whose audit member says chain false, where no lock can be taken, when the damaged bytes are longer than any line a chained trail holds, and when the incomplete last line is itself a discontinuity record whose write did not complete: a repair does not repair a repair, since a discontinuity that could be named damaged would leave the line it excused checked by nothing. Move such a trail aside and keep it. " +
			"It works only on the trail the project's jpack.json declares.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			loaded, failure := a.loadProject(configPath, commandName, format)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			writer := loaded.AuditWriter()
			if writer == nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-AUDIT-NOT-DECLARED", notDeclaredMessage)
			}
			repaired, err := writer.Repair()
			switch {
			case errors.Is(err, audit.ErrNothingToRepair):
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-REPAIR-NOTHING", "The trail's last line is complete, so there is nothing at its end to repair; jpack audit verify reports any other damage.")
			case errors.Is(err, audit.ErrRepairUnchained):
				return a.operational(commandName, format, result.ExitInvocation, "JPS-AUDIT-REPAIR-UNCHAINED", "This project's audit member says chain false, and a repair starts a chained segment.")
			case errors.Is(err, audit.ErrRepairNeedsLock):
				return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-REPAIR-NO-LOCK", "No lock can be taken on the trail here, so it cannot be repaired without risking a writer appending in between.")
			case errors.Is(err, audit.ErrOversizedLine):
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-REPAIR-TOO-LONG", "The damaged bytes are longer than any line a chained trail holds; move the trail aside and keep it, and the next record starts a new one.")
			case errors.Is(err, audit.ErrRepairDiscontinuity):
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-REPAIR-DISCONTINUITY", "The incomplete last line is a discontinuity record whose own write did not complete, and a repair does not repair a repair; move the trail aside and keep it, and the next record starts a new one.")
			case errors.Is(err, audit.ErrNoTrail):
				return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", "The project's audit trail could not be opened; no record may have been written yet.")
			case err != nil:
				return a.operational(commandName, format, result.ExitIO, audit.FailureCode, audit.FailureMessageFor(err))
			}
			output := result.AuditRepair{
				OutputVersion: result.OutputVersion,
				Tool:          result.CurrentTool(),
				Command:       commandName,
				Status:        "repaired",
				TrailPath:     loaded.TrailPath(),
				Discontinuity: repaired,
			}
			if format == "json" {
				if err := a.writeJSON(output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			fmt.Fprintf(a.out, "repaired: line %d (%d bytes, %s) is kept in place as damaged, and the discontinuity record at line %d starts a new segment\n",
				repaired.DamagedLine, repaired.Bytes, repaired.Digest, repaired.Line)
			fmt.Fprintln(a.out, "jpack audit verify now reports the trail as segments; the history is not intact across the discontinuity.")
			fmt.Fprintln(a.out, display.Sanitize(output.TrailPath))
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

const notDeclaredMessage = "This project's jpack.json declares no audit directory, so it keeps no trail; pass --trail <file> to read a trail file."

// openTrail opens the trail a command reads: the file --trail names, opened as
// an operator-named regular file, or else the one the project declares, opened
// through the project's own handle. The second result is the path to show.
func (a *App) openTrail(command, format, trailPath, configPath string) (*os.File, string, error) {
	if trailPath != "" && configPath != "" {
		return nil, "", a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-TRAIL", "Pass --trail or --config, not both: one trail is read.")
	}
	if trailPath != "" {
		if trailPath == "-" {
			return nil, "", a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-STDIN", "The trail is read as a file, not from standard input; pass its path.")
		}
		if strings.Contains(trailPath, "://") || fssecure.IsRemotePath(trailPath) {
			return nil, "", a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use a local file.")
		}
		file, err := fssecure.OpenRegular(trailPath)
		if err != nil {
			return nil, "", a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", fmt.Sprintf("The trail %s could not be opened as one regular file.", display.Sanitize(trailPath)))
		}
		return file, trailPath, nil
	}
	loaded, failure := a.loadProject(configPath, command, format)
	if failure != nil {
		return nil, "", failure
	}
	defer loaded.Close()
	if _, declared := loaded.TrailName(); !declared {
		return nil, "", a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-NOT-DECLARED", notDeclaredMessage)
	}
	file, err := loaded.OpenTrail()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", fmt.Sprintf("The project's trail %s does not exist yet: no record has been written.", display.Sanitize(loaded.TrailPath())))
	}
	if err != nil {
		return nil, "", a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", fmt.Sprintf("The project's trail %s could not be opened as one regular file inside the project.", display.Sanitize(loaded.TrailPath())))
	}
	return file, loaded.TrailPath(), nil
}

// readTrail takes a snapshot of an open trail between writes and verifies it.
// The second result says whether the snapshot was taken under the lock.
func (a *App) readTrail(command, format string, file *os.File, expect *result.AuditCheckpoint) (result.AuditChain, bool, error) {
	size, locked, err := fssecure.SizeBetweenWrites(file)
	if err != nil {
		return result.AuditChain{}, false, a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", "The trail's size could not be read between writes.")
	}
	chain, err := audit.Verify(file, size, expect)
	if err != nil {
		return result.AuditChain{}, false, a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", "The trail could not be read to its end.")
	}
	return chain, locked, nil
}

// readCheckpoint reads and parses the checkpoint document --expect names.
func (a *App) readCheckpoint(command, format, expectPath string) (result.AuditCheckpoint, error) {
	if strings.Contains(expectPath, "://") || fssecure.IsRemotePath(expectPath) {
		return result.AuditCheckpoint{}, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use a local file.")
	}
	data, err := a.readPack(expectPath, audit.MaxCheckpointBytes)
	if errors.Is(err, fssecure.ErrTooLarge) {
		return result.AuditCheckpoint{}, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-CHECKPOINT-INVALID", fmt.Sprintf("The checkpoint exceeds %d bytes.", audit.MaxCheckpointBytes))
	}
	if err != nil {
		return result.AuditCheckpoint{}, a.operational(command, format, result.ExitIO, "JPS-AUDIT-CHECKPOINT-READ", "The checkpoint could not be read as one bounded regular file or standard input stream.")
	}
	checkpoint, err := audit.ParseCheckpoint(data)
	if errors.Is(err, audit.ErrCheckpointVersion) {
		return result.AuditCheckpoint{}, a.operational(command, format, result.ExitUnsupported, "JPS-AUDIT-CHECKPOINT-VERSION", "The checkpoint's checkpointVersion is not one this runtime reads. It reads: "+result.CheckpointVersion+".")
	}
	if err != nil {
		return result.AuditCheckpoint{}, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-CHECKPOINT-INVALID", err.Error())
	}
	return checkpoint, nil
}

// renderAuditVerification reports one verification: a status line, the
// coverage, the head, the segments and discontinuities, every finding, and what
// the result establishes and does not.
func (a *App) renderAuditVerification(format string, output result.AuditVerification) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	switch output.Status {
	case "valid":
		if output.Scope == audit.ScopeCheckpoint {
			fmt.Fprintf(a.out, "consistent, and through the checkpoint at sequence %d: %d line(s)\n", output.Expect.Checkpoint.Sequence, output.Lines)
		} else {
			fmt.Fprintf(a.out, "consistent: the integrity of one supplied chain, %d line(s)\n", output.Lines)
		}
	case "segmented":
		fmt.Fprintf(a.out, "SEGMENTED: %d discontinuity record(s); the history is not intact across them, and each segment is consistent\n", output.DiscontinuitiesTotal)
	default:
		fmt.Fprintf(a.out, "INVALID: %d failed check(s)\n", output.FindingsTotal)
	}
	coverage := output.Coverage
	checkpointed := "none supplied"
	switch coverage.Checkpointed.Status {
	case "through":
		checkpointed = fmt.Sprintf("through sequence %d", coverage.Checkpointed.Through)
	case "failed":
		checkpointed = "failed"
	}
	fmt.Fprintf(a.out, "lines: %d before the first chained line (one block), %d chained, %d unchained and committed by a later chained line, %d uncovered, %d damaged; signed: not available (#209); checkpointed: %s\n",
		coverage.LegacyPrefix, coverage.Chained, coverage.Unchained, coverage.Uncovered, coverage.Damaged, checkpointed)
	if output.Head != nil {
		fmt.Fprintf(a.out, "trail %s · last chained record: sequence %d, %s\n", output.Head.Trail, output.Head.Sequence, output.Head.RecordDigest)
	}
	if output.DiscontinuitiesTotal > 0 {
		for _, segment := range output.Segments {
			fmt.Fprintf(a.out, "segment: lines %d to %d\n", segment.FirstLine, segment.LastLine)
		}
		if omitted := output.SegmentsTotal - int64(len(output.Segments)); omitted > 0 {
			fmt.Fprintf(a.out, "segment: and %d more, not listed\n", omitted)
		}
		for _, broken := range output.Discontinuities {
			fmt.Fprintf(a.out, "discontinuity at line %d: line %d is damaged (%s), %d bytes, %s\n", broken.Line, broken.DamagedLine, broken.Reason, broken.Bytes, broken.Digest)
		}
		if omitted := output.DiscontinuitiesTotal - int64(len(output.Discontinuities)); omitted > 0 {
			fmt.Fprintf(a.out, "discontinuity: and %d more, not listed\n", omitted)
		}
	}
	for _, finding := range output.Findings {
		fmt.Fprintf(a.out, "- %s (line %d): %s\n", finding.Name, finding.Line, display.Sanitize(finding.Detail))
	}
	if hidden := output.FindingsTotal - len(output.Findings); hidden > 0 {
		fmt.Fprintf(a.out, "- and %d more\n", hidden)
	}
	for _, statement := range output.Establishes {
		fmt.Fprintln(a.out, "ESTABLISHED: "+statement)
	}
	for _, statement := range output.DoesNotEstablish {
		fmt.Fprintln(a.out, "NOT ESTABLISHED: "+statement)
	}
	snapshot := "read between writes"
	if !output.SnapshotBetweenWrites {
		snapshot = "no lock: the snapshot may end inside a write in progress"
	}
	fmt.Fprintf(a.out, "%s · %s\n", display.Sanitize(output.TrailPath), snapshot)
	return nil
}
