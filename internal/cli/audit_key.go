package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// auditKeyCommand is the jpack audit key group (ADR-0047 §2b): making a
// signing key, showing its public half, and handing a trail's signing over to
// a next key. Nothing here prints or logs anything of a key's private half.
func (a *App) auditKeyCommand() *cobra.Command {
	group := &cobra.Command{
		Use:   "key",
		Short: "Make, show and rotate the key that signs a chained audit trail",
		Long: "The key that signs a project's chained audit trail (ADR-0047): an Ed25519 seed held outside the project, named by the audit member's signingKey or by the JPACK_SIGNING_KEY environment variable. " +
			"generate writes a new one, public prints a key's public half for whoever will verify, and rotate hands the trail's signing over to a next key with a line the current key signs. " +
			"None of them prints anything of a key's private half.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	group.AddCommand(a.auditKeyGenerateCommand(), a.auditKeyPublicCommand(), a.auditKeyRotateCommand())
	return group
}

func (a *App) auditKeyGenerateCommand() *cobra.Command {
	const commandName = "audit key generate"
	format := "human"
	command := &cobra.Command{
		Use:   "generate <seed-file>",
		Short: "Write a new signing key and print its public key",
		Long: "Write a new Ed25519 seed to <seed-file>, 64 lowercase hexadecimal characters and a newline, the form the gateway's keygen writes, created readable and writable by its owner alone; an existing file is never overwritten. " +
			"Human output is the public key alone, one line, so it can be saved for whoever will verify (jpack audit verify --public-key reads it); the seed itself is never printed. " +
			"The directory part of <seed-file> is resolved to its real path first, and the note on standard error names the seed by that path, which is the one to configure: a signing key is named by its real path, with no symbolic link anywhere in it. " +
			"Keep the seed outside every project, readable by the user the runtime runs as and nobody else: a key inside the project, or one others can read, is refused and signs nothing. " +
			"On Windows a key's privacy cannot be checked, so no key signs there; generate and public still work.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			target, err := realParent(args[0])
			if err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "The seed file's directory could not be resolved to its real path; it must exist.")
			}
			seed := make([]byte, 32)
			// crypto/rand.Read cannot fail on a supported platform.
			_, _ = rand.Read(seed)
			if err := writeNewSeed(target, []byte(hex.EncodeToString(seed)+"\n")); err != nil {
				if errors.Is(err, fs.ErrExist) {
					return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-KEY-EXISTS", fmt.Sprintf("Something is already at %s, and a key is never written over anything.", display.Sanitize(target)))
				}
				return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-KEY-WRITE", fmt.Sprintf("The seed could not be written to %s.", display.Sanitize(target)))
			}
			// The public key is derived from the bytes that were persisted, so
			// a short write cannot print a key for a seed that is not there.
			signer, err := audit.ReadKey(target)
			if err != nil {
				return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-KEY-READ", fmt.Sprintf("The seed written to %s could not be read back: %s.", display.Sanitize(target), audit.KeyRefusal(err)))
			}
			if err := a.renderKey(format, commandName, "generated", signer); err != nil {
				return err
			}
			if format == "human" {
				fmt.Fprintf(a.errOut, "note: seed written to %s; keep it outside every project, readable by you alone; keyId %s\n", display.Sanitize(target), signer.KeyID())
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human (the public key alone) or json")
	return command
}

func (a *App) auditKeyPublicCommand() *cobra.Command {
	const commandName = "audit key public"
	format := "human"
	command := &cobra.Command{
		Use:   "public <seed-file>",
		Short: "Print a signing key's public key",
		Long: "Print the public key of the Ed25519 seed in <seed-file>, held to the rules a signing key is held to, but for being outside a project: one regular file with one name, not a symbolic link, and on unix owned by the user the runtime runs as and readable by nobody else. The directory part of <seed-file> is resolved to its real path first. " +
			"Human output is the public key alone, one line, for whoever will verify; JSON adds its keyId, the first 32 hexadecimal characters of the SHA-256 of its 32 bytes, which each signature names.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			target, err := realParent(args[0])
			if err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "The seed file's directory could not be resolved to its real path; it must exist.")
			}
			signer, err := audit.ReadKey(target)
			if err != nil {
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-KEY-REFUSED", fmt.Sprintf("The key at %s is refused: %s.", display.Sanitize(target), audit.KeyRefusal(err)))
			}
			return a.renderKey(format, commandName, "read", signer)
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human (the public key alone) or json")
	return command
}

func (a *App) auditKeyRotateCommand() *cobra.Command {
	const commandName = "audit key rotate"
	format := "human"
	configPath := ""
	nextPath := ""
	command := &cobra.Command{
		Use:   "rotate",
		Short: "Hand a trail's signing over to a next key",
		Long: "Hand the project's chained audit trail over from the signing key it names to the key in --next <seed-file> (ADR-0047). " +
			"Under the writer's lock, a key-rotation line is appended to the signature sidecar, made with the current key and naming the next key's public key and the trail's last line: the records up to that line are signed with the current key, and the records after it with the next. " +
			"The current key, no longer in force, signs nothing more, and the next one signs nothing until the project names it, by its audit member's signingKey or JPACK_SIGNING_KEY: records written in between are unsigned, never failed. " +
			"The next key is held to the rules the current one is: outside the project and its owner's alone. " +
			"It is refused when the project names no usable key, when that key is not the key in force in the sidecar, when the next key is the same key, before the trail has a chained record, while its last line is incomplete, and where no lock can be taken. " +
			"A rotation does not revoke anything: whoever still holds the old key can sign as it until a verifier is told otherwise, which is the verifier's own trust configuration (jpack audit verify --revoked, and --public-key given once per key in the order the trail used them).",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if nextPath == "" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-KEY", "--next <seed-file> names the key to rotate to.")
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
				return a.operational(commandName, format, result.ExitInvocation, "JPS-AUDIT-REPAIR-UNCHAINED", "This project's audit member says chain false, and a signature binds a chained record.")
			}
			if keyPath, _ := loaded.SigningKeyPath(); keyPath == "" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-AUDIT-KEY-NONE", "The project names no signing key to rotate from: its audit member has no signingKey and JPACK_SIGNING_KEY is not set.")
			}
			if _, err := loaded.SigningKey(); err != nil {
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-KEY-REFUSED", fmt.Sprintf("The project's signing key is refused: %s.", audit.KeyRefusal(err)))
			}
			nextAbsolute, err := filepath.Abs(nextPath)
			if err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "The next key's path could not be made absolute.")
			}
			next, err := loaded.LoadKey(nextAbsolute)
			if err != nil {
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-KEY-REFUSED", fmt.Sprintf("The next key at %s is refused: %s.", display.Sanitize(nextAbsolute), audit.KeyRefusal(err)))
			}
			rotated, err := writer.Rotate(next)
			switch {
			case errors.Is(err, audit.ErrKeyNotInForce):
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-KEY-NOT-IN-FORCE", "The project's signing key is not the key in force in the signature sidecar, so it cannot hand signing over; only the key in force can.")
			case errors.Is(err, audit.ErrRotationSameKey):
				return a.operational(commandName, format, result.ExitInvocation, "JPS-AUDIT-KEY-SAME", "The next key is the key already in force.")
			case errors.Is(err, audit.ErrRotationNoTrail), errors.Is(err, audit.ErrNoTrail):
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-KEY-NO-TRAIL", "The trail has no chained record yet, so there is no trail to rotate the key of; the first record is signed with whichever key the project names.")
			case errors.Is(err, audit.ErrRepairNeedsLock):
				return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-REPAIR-NO-LOCK", "No lock can be taken on the trail here, so the rotation cannot be placed between two writes.")
			case err != nil:
				return a.operational(commandName, format, result.ExitIO, audit.FailureCode, audit.FailureMessageFor(err))
			}
			output := result.AuditRotation{
				OutputVersion: result.OutputVersion,
				Tool:          result.CurrentTool(),
				Command:       commandName,
				Status:        "rotated",
				TrailPath:     loaded.TrailPath(),
				At:            rotated.At,
				Trail:         rotated.Trail,
				From:          rotated.From,
				Next:          rotated.Next,
				NextPublicKey: rotated.NextPublicKey,
			}
			if format == "json" {
				if err := a.writeJSON(output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			fmt.Fprintf(a.out, "rotated: records after line %d are signed with key %s, and key %s signs nothing more\n", output.At, output.Next, output.From)
			fmt.Fprintf(a.out, "next public key: %s\n", output.NextPublicKey)
			fmt.Fprintln(a.out, "name the next key now, by the audit member's signingKey or JPACK_SIGNING_KEY: until then records are written unsigned.")
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	command.Flags().StringVar(&nextPath, "next", nextPath, "the seed file of the key to hand signing over to")
	return command
}

// realParent makes a seed file's path absolute with its directory part
// resolved to the real path, so the path printed and read back is one a
// signing key can be named by: no symbolic link anywhere in it. The final
// component is kept as given, so a symbolic link there is still refused.
func realParent(argument string) (string, error) {
	absolute, err := filepath.Abs(argument)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}

// renderKey prints a key's public half: the public key alone for human
// output, the payload for JSON.
func (a *App) renderKey(format, commandName, status string, signer *audit.Signer) error {
	if format == "json" {
		if err := a.writeJSON(result.AuditKey{
			OutputVersion: result.OutputVersion,
			Tool:          result.CurrentTool(),
			Command:       commandName,
			Status:        status,
			PublicKey:     signer.PublicKey(),
			KeyID:         signer.KeyID(),
		}); err != nil {
			return &handledExit{code: result.ExitIO}
		}
		return nil
	}
	if _, err := fmt.Fprintln(a.out, signer.PublicKey()); err != nil {
		return &handledExit{code: result.ExitIO}
	}
	return nil
}

// writeNewSeed creates a seed file readable and writable by its owner alone,
// never over anything already there, and removes what it created when the
// write does not complete.
func writeNewSeed(target string, data []byte) error {
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written := false
	defer func() {
		if !written {
			_ = file.Close()
			_ = os.Remove(target)
		}
	}()
	// The mode is set again after the create, so no umask leaves the seed
	// other than readable and writable by its owner.
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	written = true
	return nil
}
