package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
		Short: "Check, checkpoint, repair and sign a chained audit trail",
		Long: "Operations on the audit trail a project keeps (ADR-0018), chained over its exact bytes (ADR-0047). " +
			"verify checks every trail, sequence and previous from the first chained record on, and with --public-key the signatures beside it; checkpoint prints the checkpoint of the last chained record, for handing to someone who will hold it; " +
			"repair starts a new segment after a last line a write did not complete; key makes, shows and rotates the key that signs the records. " +
			"None of them evaluates anything.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	group.AddCommand(a.auditVerifyCommand(), a.auditCheckpointCommand(), a.auditRepairCommand(), a.auditKeyCommand())
	return group
}

func (a *App) auditVerifyCommand() *cobra.Command {
	const commandName = "audit verify"
	format := "human"
	trailPath := ""
	configPath := ""
	expectPaths := []string{}
	requireThrough := int64(0)
	publicKeyPaths := []string{}
	revokedPath := ""
	signaturesPath := ""
	requireSigned := int64(0)
	command := &cobra.Command{
		Use:   "verify",
		Short: "Check a chained audit trail, alone or against a checkpoint",
		Long: "Read an audit trail over its exact bytes and check every trail, sequence and previous from the first chained record on, by the rules the writer chains by (ADR-0047): " +
			"a chained line's sequence is its line number, it carries the identity of the last chained line before it, and its previous is the SHA-256 of the line before it when that line is chained, and of the whole trail before it when it is not. " +
			"The trail is --trail <file>, or else the one the project's jpack.json declares. " +
			"The size is read under the writer's lock, shared, so it falls between two writes, and the bytes before it are read without the lock: writers only append, so they are not delayed and nothing read changes underneath. " +
			"The report gives the coverage: lines before the first chained line (committed as one block), chained lines, unchained lines a later chained line commits to, lines nothing commits to, and lines a repair names as damaged. " +
			"Without --expect this is the integrity of one supplied chain: the lines are consistent with one another, which does not show the trail is complete, or that its last line, or lines rewritten from some point on with their links recomputed, are the ones first written. " +
			"With --expect <file>, the checkpoints a holder kept independently of the operator (jpack audit checkpoint prints them; a file may hold many, one per line, and --expect may be given more than once), the line at each one's sequence must be a chained record of its trail with its record digest: a trail that is shorter, has another identity, or has another record there fails. " +
			"The records up to the highest checkpoint that matched, with no failed check at or before it, are witnessed; the chained records after it are unwitnessed, and --require-checkpoint-through <sequence> fails the verification while the records up to that sequence are not all witnessed. " +
			"With --public-key <file>, the public key the trail was first signed with (64 hexadecimal characters; jpack audit key public prints it), every line of the signature sidecar beside the trail, or of --signatures <file>, is checked in step with the trail: a record signature must be for the record at its sequence, by its exact bytes, and verify under the key in force there, and a key rotation must be signed by the key in force and hands the records after its line to the key it names. " +
			"--public-key given more than once names the keys in the order the trail used them, and a rotation to any other key fails; --revoked <file> names keys not to trust from a sequence on, one {\"from\":N,\"publicKey\":\"...\"} per line, which is how a verifier refuses what a copied key signs. " +
			"The records up to the highest one whose own signature holds, with no failed check of the chain at or before it, are signed; --require-signed-through <sequence> fails the verification while the records up to that sequence are not all signed. A record with no signature is unsigned, never a failure by itself, since a signature that could not be written leaves its decision recorded. " +
			"A signature shows only that whoever held the key signed: nothing against the operator, who holds it, and nothing after the key is copied. " +
			"Time stamps from an RFC 3161 authority are not available yet. " +
			"A trail a repair has segmented is reported segment by segment, and never as intact across a discontinuity. " +
			"Exit 0 when every check passed, segmented or not, and 1 when any failed; each failed check is a named finding.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if requireThrough < 0 {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-REQUIRE", "--require-checkpoint-through must be a sequence from 1.")
			}
			if requireSigned < 0 {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-REQUIRE", "--require-signed-through must be a sequence from 1.")
			}
			if len(publicKeyPaths) == 0 && (requireSigned > 0 || revokedPath != "" || signaturesPath != "") {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-SIGNATURES", "--require-signed-through, --revoked and --signatures apply only with --public-key: no signature is checked without a key to check it by.")
			}
			signatures, failure := a.readSignatureOptions(commandName, format, publicKeyPaths, revokedPath, requireSigned)
			if failure != nil {
				return failure
			}
			held := []result.AuditCheckpoint{}
			for _, expectPath := range expectPaths {
				checkpoints, failure := a.readCheckpoints(commandName, format, expectPath)
				if failure != nil {
					return failure
				}
				held = append(held, checkpoints...)
			}
			opened, failure := a.openTrailFiles(commandName, format, trailPath, configPath, signatures != nil, signaturesPath)
			if failure != nil {
				return failure
			}
			defer opened.close()
			report, locked, failure := a.readTrailWith(commandName, format, opened, audit.Options{Held: held, RequireThrough: requireThrough, Signatures: signatures})
			if failure != nil {
				return failure
			}
			chain := report.Chain
			shownPath := opened.path
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
	command.Flags().StringArrayVar(&expectPaths, "expect", expectPaths, "a file of checkpoints a holder kept, one per line, to hold the trail to (repeatable)")
	command.Flags().Int64Var(&requireThrough, "require-checkpoint-through", requireThrough, "fail unless the held checkpoints cover every record up to this sequence")
	command.Flags().StringArrayVar(&publicKeyPaths, "public-key", publicKeyPaths, "a file holding the public key the trail was first signed with; again for each key it rotated to, in order (repeatable)")
	command.Flags().StringVar(&revokedPath, "revoked", revokedPath, "a file of keys not to trust from a sequence on, one {\"from\":N,\"publicKey\":\"...\"} per line")
	command.Flags().StringVar(&signaturesPath, "signatures", signaturesPath, "the signature sidecar to read; without it, the one beside the trail")
	command.Flags().Int64Var(&requireSigned, "require-signed-through", requireSigned, "fail unless valid signatures cover every record up to this sequence")
	return command
}

// readSignatureOptions reads the public keys and revocations a verification
// checks the signature sidecar by, or nil when no public key was given.
func (a *App) readSignatureOptions(command, format string, publicKeyPaths []string, revokedPath string, requireSigned int64) (*audit.SignatureOptions, error) {
	if len(publicKeyPaths) == 0 {
		return nil, nil
	}
	options := &audit.SignatureOptions{RequireThrough: requireSigned}
	for _, keyPath := range publicKeyPaths {
		if strings.Contains(keyPath, "://") || fssecure.IsRemotePath(keyPath) {
			return nil, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use a local file.")
		}
		data, err := a.readPack(keyPath, 4096)
		if err != nil {
			return nil, a.operational(command, format, result.ExitIO, "JPS-AUDIT-PUBLIC-KEY-READ", fmt.Sprintf("The public key %s could not be read as one bounded regular file.", display.Sanitize(keyPath)))
		}
		public, err := audit.ParsePublicKey(data)
		if err != nil {
			return nil, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-PUBLIC-KEY-INVALID", fmt.Sprintf("The public key %s is not one: %s.", display.Sanitize(keyPath), err.Error()))
		}
		options.Keys = append(options.Keys, public)
	}
	if revokedPath != "" {
		if strings.Contains(revokedPath, "://") || fssecure.IsRemotePath(revokedPath) {
			return nil, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use a local file.")
		}
		data, err := a.readPack(revokedPath, audit.MaxRevocationBytes)
		if err != nil {
			return nil, a.operational(command, format, result.ExitIO, "JPS-AUDIT-REVOKED-READ", "The revocations could not be read as one bounded regular file or standard input stream.")
		}
		revoked, err := audit.ParseRevocations(data)
		if err != nil {
			return nil, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-REVOKED-INVALID", "The revocations could not be read: "+err.Error()+".")
		}
		options.Revoked = revoked
	}
	return options, nil
}

func (a *App) auditCheckpointCommand() *cobra.Command {
	const commandName = "audit checkpoint"
	format := "human"
	trailPath := ""
	configPath := ""
	since := int64(0)
	limit := 1000
	command := &cobra.Command{
		Use:   "checkpoint",
		Short: "Print the checkpoint of an audit trail's last chained record",
		Long: "Print the checkpoint of the last chained record of an audit trail (ADR-0047): its trail identity, its sequence, and the SHA-256 of its exact line bytes, which is also what a gateway action receipt's decision.recordDigest names for that record. " +
			"Human output is the checkpoint document alone, one line in its RFC 8785 canonical form, so it can be saved and handed to whoever will hold it; jpack audit verify --expect reads it back. " +
			"A checkpoint protects only what it covers and only as well as its holder keeps it: given to someone the operator does not control, it later shows whether the trail up to that record is the one that existed when it was made. " +
			"The trail is read and verified first, and a trail that fails a check is refused rather than given a checkpoint. " +
			"Lines after the last chained record are not covered, and a note on standard error says how many. " +
			"With --since <sequence> it prints instead the checkpoint of every chained record after that sequence, one line each, in sequence order and at most --limit of them, for a deliverer that hands each new checkpoint to a holder: it asks again after the last sequence it received, and nothing a decision does waits for it. " +
			"The runtime keeps no record of what was handed over, since a record the operator keeps is one the operator can rewrite: the holder's copy is what counts. " +
			"A checkpoint is a function of its record's bytes, so the same record always gives the same line and handing it over again is idempotent by that line's digest; a holder given two checkpoints for one trail and sequence that differ holds proof that the trail was rewritten.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			listing := command.Flags().Changed("since")
			if !listing && command.Flags().Changed("limit") {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-SINCE", "--limit applies only with --since.")
			}
			if since < 0 || limit < 1 || limit > maxCheckpointsListed {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-SINCE", fmt.Sprintf("--since must be a sequence from 0, and --limit a count from 1 to %d.", maxCheckpointsListed))
			}
			file, shownPath, failure := a.openTrail(commandName, format, trailPath, configPath)
			if failure != nil {
				return failure
			}
			defer file.Close()
			report, _, failure := a.readTrail(commandName, format, file, audit.Options{List: listing, ListAfter: since, ListLimit: limit})
			if failure != nil {
				return failure
			}
			chain := report.Chain
			if chain.Status == "invalid" {
				first := chain.Findings[0]
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-CHECKPOINT-REFUSED",
					fmt.Sprintf("The trail fails %d check(s), the first %s at line %d, so no checkpoint is given for it; jpack audit verify lists them.", chain.FindingsTotal, first.Name, first.Line))
			}
			if chain.Head == nil {
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-CHECKPOINT-NONE", "The trail has no chained record to checkpoint.")
			}
			if listing {
				return a.renderCheckpointList(format, result.AuditCheckpointList{
					OutputVersion: result.OutputVersion,
					Tool:          result.CurrentTool(),
					Command:       commandName,
					Status:        "listed",
					TrailPath:     shownPath,
					After:         since,
					Checkpoints:   append([]result.AuditCheckpoint{}, report.Listed...),
					More:          report.More,
				})
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
	command.Flags().StringVar(&format, "format", format, "output format: human (the checkpoint documents) or json")
	command.Flags().StringVar(&trailPath, "trail", trailPath, "the trail file to read; without it, the project's own")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	command.Flags().Int64Var(&since, "since", since, "print the checkpoint of every chained record after this sequence")
	command.Flags().IntVar(&limit, "limit", limit, "with --since, print at most this many")
	return command
}

// maxCheckpointsListed bounds one page of audit checkpoint --since.
const maxCheckpointsListed = 100000

// renderCheckpointList prints the checkpoints audit checkpoint --since
// listed: the canonical documents, one line each, or the JSON payload.
func (a *App) renderCheckpointList(format string, output result.AuditCheckpointList) error {
	if format == "json" {
		if err := a.writeJSON(output); err != nil {
			return &handledExit{code: result.ExitIO}
		}
		return nil
	}
	for _, checkpoint := range output.Checkpoints {
		if _, err := a.out.Write(audit.EncodeCheckpoint(checkpoint)); err != nil {
			return &handledExit{code: result.ExitIO}
		}
	}
	if output.More {
		last := output.After
		if count := len(output.Checkpoints); count > 0 {
			last = output.Checkpoints[count-1].Sequence
		}
		fmt.Fprintf(a.errOut, "note: more chained records follow; ask again with --since %d\n", last)
	}
	return nil
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
	opened, failure := a.openTrailFiles(command, format, trailPath, configPath, false, "")
	if failure != nil {
		return nil, "", failure
	}
	return opened.trail, opened.path, nil
}

// openedTrail is a trail opened for reading and, when signatures are checked,
// its signature sidecar, which is nil when there is none.
type openedTrail struct {
	trail   *os.File
	path    string
	sidecar *os.File
}

func (o openedTrail) close() {
	o.trail.Close()
	if o.sidecar != nil {
		o.sidecar.Close()
	}
}

// openTrailFiles is openTrail, and with sidecar the signature sidecar too: the
// file --signatures names, which must be there, or else the one beside the
// trail, opened the way the trail was, which may not be.
func (a *App) openTrailFiles(command, format, trailPath, configPath string, sidecar bool, signaturesPath string) (openedTrail, error) {
	if trailPath != "" && configPath != "" {
		return openedTrail{}, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-TRAIL", "Pass --trail or --config, not both: one trail is read.")
	}
	if signaturesPath != "" && (signaturesPath == "-" || strings.Contains(signaturesPath, "://") || fssecure.IsRemotePath(signaturesPath)) {
		return openedTrail{}, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "The signature sidecar is read as one local file; pass its path.")
	}
	var opened openedTrail
	if trailPath != "" {
		if trailPath == "-" {
			return openedTrail{}, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-STDIN", "The trail is read as a file, not from standard input; pass its path.")
		}
		if strings.Contains(trailPath, "://") || fssecure.IsRemotePath(trailPath) {
			return openedTrail{}, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use a local file.")
		}
		file, err := fssecure.OpenRegular(trailPath)
		if err != nil {
			return openedTrail{}, a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", fmt.Sprintf("The trail %s could not be opened as one regular file.", display.Sanitize(trailPath)))
		}
		opened = openedTrail{trail: file, path: trailPath}
		if sidecar && signaturesPath == "" {
			beside := filepath.Join(filepath.Dir(trailPath), audit.SidecarName)
			opened.sidecar, err = fssecure.OpenRegular(beside)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				opened.close()
				return openedTrail{}, a.operational(command, format, result.ExitIO, "JPS-AUDIT-SIGNATURES-READ", fmt.Sprintf("The signature sidecar %s could not be opened as one regular file.", display.Sanitize(beside)))
			}
		}
	} else {
		loaded, failure := a.loadProject(configPath, command, format)
		if failure != nil {
			return openedTrail{}, failure
		}
		defer loaded.Close()
		if _, declared := loaded.TrailName(); !declared {
			return openedTrail{}, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-NOT-DECLARED", notDeclaredMessage)
		}
		file, err := loaded.OpenTrail()
		if errors.Is(err, fs.ErrNotExist) {
			return openedTrail{}, a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", fmt.Sprintf("The project's trail %s does not exist yet: no record has been written.", display.Sanitize(loaded.TrailPath())))
		}
		if err != nil {
			return openedTrail{}, a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", fmt.Sprintf("The project's trail %s could not be opened as one regular file inside the project.", display.Sanitize(loaded.TrailPath())))
		}
		opened = openedTrail{trail: file, path: loaded.TrailPath()}
		if sidecar && signaturesPath == "" {
			opened.sidecar, err = loaded.OpenSidecar()
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				opened.close()
				return openedTrail{}, a.operational(command, format, result.ExitIO, "JPS-AUDIT-SIGNATURES-READ", "The project's signature sidecar could not be opened as one regular file inside the project.")
			}
		}
	}
	if sidecar && signaturesPath != "" {
		file, err := fssecure.OpenRegular(signaturesPath)
		if err != nil {
			opened.close()
			return openedTrail{}, a.operational(command, format, result.ExitIO, "JPS-AUDIT-SIGNATURES-READ", fmt.Sprintf("The signature sidecar %s could not be opened as one regular file.", display.Sanitize(signaturesPath)))
		}
		opened.sidecar = file
	}
	return opened, nil
}

// readTrail takes a snapshot of an open trail between writes and verifies it.
// The second result says whether the snapshot was taken under the lock.
func (a *App) readTrail(command, format string, file *os.File, options audit.Options) (audit.Report, bool, error) {
	return a.readTrailWith(command, format, openedTrail{trail: file}, options)
}

// readTrailWith is readTrail for a trail and its signature sidecar, whose
// sizes are read under the one shared lock, so together they fall between two
// writes: a writer appends to the sidecar under the trail's lock.
func (a *App) readTrailWith(command, format string, opened openedTrail, options audit.Options) (audit.Report, bool, error) {
	sizes, locked, err := fssecure.SizesBetweenWrites(opened.trail, opened.sidecar)
	if err != nil {
		return audit.Report{}, false, a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", "The trail's size could not be read between writes.")
	}
	if options.Signatures != nil && opened.sidecar != nil {
		options.Signatures.Sidecar, options.Signatures.SidecarSize = opened.sidecar, sizes[1]
	}
	report, err := audit.Verify(opened.trail, sizes[0], options)
	if errors.Is(err, audit.ErrSidecarRead) {
		return audit.Report{}, false, a.operational(command, format, result.ExitIO, "JPS-AUDIT-SIGNATURES-READ", "The signature sidecar could not be read to its end.")
	}
	if err != nil {
		return audit.Report{}, false, a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", "The trail could not be read to its end.")
	}
	return report, locked, nil
}

// readCheckpoints reads and parses one file of held checkpoints --expect
// names: one or more checkpoint documents, one per line.
func (a *App) readCheckpoints(command, format, expectPath string) ([]result.AuditCheckpoint, error) {
	if strings.Contains(expectPath, "://") || fssecure.IsRemotePath(expectPath) {
		return nil, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use a local file.")
	}
	data, err := a.readPack(expectPath, audit.MaxHeldBytes)
	if errors.Is(err, fssecure.ErrTooLarge) {
		return nil, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-CHECKPOINT-INVALID", fmt.Sprintf("The checkpoints exceed %d bytes.", audit.MaxHeldBytes))
	}
	if err != nil {
		return nil, a.operational(command, format, result.ExitIO, "JPS-AUDIT-CHECKPOINT-READ", "The checkpoints could not be read as one bounded regular file or standard input stream.")
	}
	checkpoints, err := audit.ParseCheckpoints(data)
	if errors.Is(err, audit.ErrCheckpointVersion) {
		// The parser's error names the line; it is kept, so a holder's file
		// of many checkpoints says which one is of another version.
		return nil, a.operational(command, format, result.ExitUnsupported, "JPS-AUDIT-CHECKPOINT-VERSION", "The checkpoints could not be read: "+err.Error()+". It reads: "+result.CheckpointVersion+".")
	}
	if err != nil {
		return nil, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-CHECKPOINT-INVALID", err.Error())
	}
	return checkpoints, nil
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
		if output.Held != nil && output.Held.Latest != nil {
			fmt.Fprintf(a.out, "consistent, and witnessed through the held checkpoint at sequence %d: %d line(s)\n", output.Held.Latest.Sequence, output.Lines)
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
	signed := "not checked (no --public-key)"
	switch coverage.Signed.Status {
	case "through":
		signed = fmt.Sprintf("through sequence %d", coverage.Signed.Through)
	case "none":
		signed = "none"
	}
	fmt.Fprintf(a.out, "lines: %d before the first chained line (one block), %d chained, %d unchained and committed by a later chained line, %d uncovered, %d damaged; signed: %s; checkpointed: %s\n",
		coverage.LegacyPrefix, coverage.Chained, coverage.Unchained, coverage.Uncovered, coverage.Damaged, signed, checkpointed)
	fmt.Fprintf(a.out, "records: %d witnessed by a held checkpoint, %d unwitnessed; stamped: not available (#208)\n", coverage.Witnessed, coverage.Unwitnessed)
	if output.Signatures != nil {
		fmt.Fprintf(a.out, "records: %d with a valid signature of their own, %d without\n", coverage.SignedRecords, coverage.UnsignedRecords)
		fmt.Fprintf(a.out, "signature sidecar: %d line(s), %d unreadable, %d rotation(s) followed; first key %s, key in force %s; %d public key(s) and %d revocation(s) supplied\n",
			output.Signatures.Lines, output.Signatures.Unreadable, output.Signatures.Rotations, output.Signatures.FirstKey, output.Signatures.KeyInForce, output.Signatures.KeysSupplied, output.Signatures.Revocations)
	}
	if output.Held != nil {
		fmt.Fprintf(a.out, "held checkpoints: %d supplied, %d matched, %d failed\n", output.Held.Supplied, output.Held.Matched, output.Held.Failed)
	}
	if output.Required != nil {
		fmt.Fprintf(a.out, "required: every record through sequence %d witnessed: %s\n", output.Required.Through, output.Required.Status)
	}
	if output.RequiredSigned != nil {
		fmt.Fprintf(a.out, "required: every record through sequence %d signed: %s\n", output.RequiredSigned.Through, output.RequiredSigned.Status)
	}
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
