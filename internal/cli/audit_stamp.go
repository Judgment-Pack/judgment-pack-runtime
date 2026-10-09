package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp"
)

// maxStampTimeout bounds how long audit stamp waits for an authority.
const maxStampTimeout = 10 * time.Minute

// beforeStampAppend lets tests move files after the authority replies.
// It is nil outside tests.
var beforeStampAppend func()

var errStampTrailMoved = errors.New("the stamped trail moved")

func (a *App) auditStampCommand() *cobra.Command {
	const commandName = "audit stamp"
	format := "human"
	configPath := ""
	authority := ""
	timeout := 30 * time.Second
	command := &cobra.Command{
		Use:   "stamp",
		Short: "Have a time-stamping authority stamp the trail's current checkpoint",
		Long: "Ask an RFC 3161 time-stamping authority to stamp the checkpoint of the project's trail's last chained record (ADR-0047), and keep the token in stamps.jsonl beside the trail. " +
			"The authority is the audit member's timestampAuthority, or --tsa <address>, http or https. " +
			"Nothing on the decision path stamps: a decision is appended first, and stamped when something runs this, a scheduler, Desk or a person, at whatever interval or after whatever records that caller chooses; a decision never waits for an authority, and an authority that cannot be reached leaves the trail and every decision in it as they were. " +
			"The request carries the SHA-256 of the checkpoint's canonical form, a nonce, and a request for the authority's certificate, and nothing else of the trail. The reply is held to the request, its digest and its nonce, and its token to its own signature, before it is kept; whether the authority is to be trusted is for a verifier with roots to decide (jpack audit verify --tsa-roots). " +
			"It is idempotent by the checkpoint's digest: a checkpoint already stamped is not asked for again, and the stamps file is written under its own lock, never the trail's. " +
			"If the trail at its path changes identity while stamping, JPS-AUDIT-STAMP-TRAIL-MOVED refuses the append and discards the token. " +
			"The trail is read and verified first, and a trail that fails a check is refused rather than stamped. " +
			"A stamp shows that the checkpoint, and every line before it, existed by the time the authority states, as far as that authority is independent of the operator: not how long before, and a record's at stays the operator's word.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if timeout <= 0 || timeout > maxStampTimeout {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-STAMP", fmt.Sprintf("--timeout must be above zero and at most %s.", maxStampTimeout))
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
			if !loaded.Config.Audit.Chains() {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-AUDIT-STAMP-UNCHAINED", "This project's audit member says chain false, and a stamp is of a chained record's checkpoint.")
			}
			address := authority
			if address == "" {
				address = loaded.Config.Audit.TimestampAuthority
			}
			if address == "" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-AUDIT-STAMP-NO-AUTHORITY", "No time-stamping authority is named: the audit member has no timestampAuthority and --tsa is not given.")
			}
			if parsed, err := url.Parse(address); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-STAMP", "The time-stamping authority's address must be an http or https URL.")
			}
			opened, failure := a.openTrail(commandName, format, "", configPath)
			if failure != nil {
				return failure
			}
			defer opened.close()
			report, _, failure := a.readTrailWith(commandName, format, opened, audit.Options{})
			if failure != nil {
				return failure
			}
			chain := report.Chain
			if chain.Status == "invalid" {
				first := chain.Findings[0]
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-STAMP-REFUSED",
					fmt.Sprintf("The trail fails %d check(s), the first %s at line %d, so its checkpoint is not stamped; jpack audit verify lists them.", chain.FindingsTotal, first.Name, first.Line))
			}
			if chain.Head == nil {
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-CHECKPOINT-NONE", "The trail has no chained record to stamp.")
			}
			checkpoint := *chain.Head
			output := result.AuditStamp{
				OutputVersion: result.OutputVersion,
				Tool:          result.CurrentTool(),
				Command:       commandName,
				Status:        "already-stamped",
				TrailPath:     opened.path,
				Checkpoint:    checkpoint,
			}
			trailInfo, err := opened.trail.Stat()
			if err != nil {
				return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", "The snapshot trail's identity could not be read; no authority was asked.")
			}
			stamps, err := writer.OpenStamps()
			if err != nil {
				return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-STAMPS-WRITE", "The stamps file could not be held open; no authority was asked.")
			}
			defer stamps.Close()
			stamped, err := stamps.Stamped(checkpoint)
			if errors.Is(err, audit.ErrStampsTooLarge) {
				return a.stampsTooLarge(commandName, format, "The stamps file is larger than its 67108864-byte limit, so it could not be checked and no authority was asked.")
			}
			if err != nil {
				return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-STAMPS-READ", "The stamps file could not be read to see whether the checkpoint is stamped already.")
			}
			if !stamped {
				if err := stamps.CheckStampRoom(); errors.Is(err, audit.ErrStampsTooLarge) {
					return a.stampsTooLarge(commandName, format, "The stamps file has no room for another maximum-size stamp line within its 67108864-byte limit, so no authority was asked.")
				} else if err != nil {
					return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-STAMPS-READ", "The stamps file's size could not be read before asking the authority.")
				}
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()
				der, token, err := timestamp.Ask(ctx, http.DefaultClient, address, audit.CheckpointDigest(checkpoint))
				if err != nil {
					return a.stampFailure(commandName, format, err)
				}
				if beforeStampAppend != nil {
					beforeStampAppend()
				}
				appended, err := stamps.RecordStamp(checkpoint, der, func() error {
					current, err := loaded.OpenTrail()
					if err != nil {
						return errStampTrailMoved
					}
					defer current.Close()
					named, err := current.Stat()
					if err != nil || !os.SameFile(trailInfo, named) {
						return errStampTrailMoved
					}
					return nil
				})
				if errors.Is(err, errStampTrailMoved) {
					return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-STAMP-TRAIL-MOVED", "The trail at the path is not the one whose checkpoint was stamped; nothing was written, and the token was discarded.")
				}
				if errors.Is(err, audit.ErrStampsTooLarge) {
					return a.stampsTooLarge(commandName, format, "The stamps file filled before the token could be kept; the trail and the decisions in it are as they were.")
				}
				if err != nil {
					return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-STAMPS-WRITE", "The token could not be kept in the stamps file; the trail and the decisions in it are as they were, and asking again stamps the same checkpoint.")
				}
				if appended {
					output.Status = "stamped"
					output.StampedAt = token.GenTime.UTC().Format(time.RFC3339Nano)
					output.ExistedBy = token.ExistedBy().UTC().Format(time.RFC3339Nano)
					output.Policy = token.Policy.String()
				}
			}
			if format == "json" {
				if err := a.writeJSON(output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			if output.Status == "stamped" {
				fmt.Fprintf(a.out, "stamped: the checkpoint at sequence %d existed by %s, as the authority states (policy %s)\n", checkpoint.Sequence, output.ExistedBy, output.Policy)
			} else {
				fmt.Fprintf(a.out, "already stamped: the checkpoint at sequence %d; nothing was asked\n", checkpoint.Sequence)
			}
			fmt.Fprintln(a.out, "jpack audit verify --tsa-roots <file> checks the stamps against the authorities you trust.")
			fmt.Fprintln(a.out, display.Sanitize(output.TrailPath))
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	command.Flags().StringVar(&authority, "tsa", authority, "the time-stamping authority's http or https address; without it, the audit member's timestampAuthority")
	command.Flags().DurationVar(&timeout, "timeout", timeout, "how long to wait for the authority")
	return command
}

func (a *App) stampsTooLarge(commandName, format, message string) error {
	return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-STAMPS-TOO-LARGE", message+" Move stamps.jsonl aside and keep it, then run audit stamp again to start a new stamps file.")
}

// stampFailure reports an authority that could not stamp, never with its
// address, which may hold credentials. Nothing was written.
func (a *App) stampFailure(commandName, format string, err error) error {
	const untouched = " Nothing was written, and the trail and the decisions in it are as they were; asking again stamps the same checkpoint."
	code := "JPS-AUDIT-STAMP-INVALID"
	switch {
	case errors.Is(err, timestamp.ErrUnreachable):
		code = "JPS-AUDIT-STAMP-UNREACHABLE"
	case errors.Is(err, timestamp.ErrRejected):
		code = "JPS-AUDIT-STAMP-REJECTED"
	}
	return a.operational(commandName, format, result.ExitIO, code, display.Sanitize(err.Error())+"."+untouched)
}
