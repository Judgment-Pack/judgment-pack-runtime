package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/edkey"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/packsig"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// betweenPackChecks runs between the two checks packs sign and packs
// verify-signature make of one pack's bytes: for sign, after the pack
// validated and before it is signed; for verify-signature, after the
// signature was accepted and before the pack is validated. It does nothing; a
// test replaces the pack file there, to hold each command to the bytes it
// read once.
var betweenPackChecks = func() {}

// packsSignCommand writes a detached signature over one pack file's bytes
// (ADR-0083), with a signing key held to the audit trail's key rules.
func (a *App) packsSignCommand() *cobra.Command {
	const commandName = "packs sign"
	format := "human"
	keyPath := ""
	publicKeyPath := ""
	outputPath := ""
	command := &cobra.Command{
		Use:   "sign <pack-file>",
		Short: "Write a detached signature over one pack file's exact bytes (EXPERIMENTAL)",
		Long: "Sign the exact bytes of <pack-file> with the Ed25519 seed in --key <seed-file> and write the signature document beside it, as <pack-file>.sig.json, or to --output <file> (ADR-0083). " +
			"The signature is over the bytes the pack's sha256 digest is over, the digest a reviewed-set lock pins: no canonical form of a pack is introduced, so a pack re-indented or re-encoded is other bytes and needs a signature of its own. " +
			"What is signed is a fixed context string and the document's version with that digest, never the bare bytes, so the signature is one of a pack and of nothing else: an audit trail's record signature made with the same key is not one, and the reverse. " +
			"The document is one JSON object, {\"algorithm\":\"ed25519\",\"packDigest\":\"sha256:...\",\"publicKey\":\"...\",\"signature\":\"...\",\"signatureVersion\":\"1.0.0\"}, in lowercase hexadecimal. " +
			"The seed is held to the rules a trail's signing key is held to (jpack audit key generate writes one): named by its real path, with no symbolic link anywhere in it; one regular file with one name; on unix owned by the user the runtime runs as and readable by nobody else, in directories owned by root or by that user that nobody else can write unless their sticky bit is set. On Windows a key's privacy cannot be checked, so no key signs there. No project is opened, so the rule that keeps a trail's key outside its project does not apply. " +
			"--public-key <file>, the public key the seed is expected to have (64 hexadecimal characters; jpack audit key public prints it), refuses a seed whose public key is another before anything is signed, so whatever was put at the seed's path signs nothing. The environment variables that name a trail's key are not read: a pack's signing key need not be the trail's. " +
			"Only a pack this runtime validates is signed, as jpack spec validate reports it, whatever specVersion it declares among those bundled; any other file is refused and nothing is written. " +
			"A signature document is never written over anything: to sign again, remove the old one first; --output names a file, and - (standard output) is refused. " +
			"A valid signature shows that whoever held the key signed these bytes. It says nothing about whether the pack is correct, tested, reviewed or current, and nothing about when it was signed; jpack packs verify-signature checks one against a key the verifier names. " +
			"This command and the signature document's format are EXPERIMENTAL: each may change or be removed without a compatibility promise, and the document's own signatureVersion says which format a reader holds.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			packPath := args[0]
			if failure := a.packSignatureInputs(commandName, format, packPath, outputPath, publicKeyPath); failure != nil {
				return failure
			}
			if keyPath == "" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-PACK-SIGNATURE", "--key <seed-file> names the key to sign with.")
			}
			if outputPath == "-" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "--output names the file the signature document is written to; it is not written to standard output, so nothing was signed.")
			}
			var expected *string
			if publicKeyPath != "" {
				public, failure := a.readExpectedPublicKey(commandName, format, publicKeyPath)
				if failure != nil {
					return failure
				}
				named := hex.EncodeToString(public)
				expected = &named
			}
			absoluteKey, err := filepath.Abs(keyPath)
			if err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "The signing key's path could not be made absolute.")
			}
			signer, err := audit.LoadSigner(absoluteKey, nil, expected)
			if err != nil {
				return a.operational(commandName, format, result.ExitInvalid, "JPS-AUDIT-KEY-REFUSED", fmt.Sprintf("The signing key at %s is refused, so nothing was signed: %s.", display.Sanitize(absoluteKey), audit.KeyRefusal(err)))
			}
			data, failure := a.readPackOnce(commandName, format, packPath)
			if failure != nil {
				return failure
			}
			checked, operational := a.engine.Validate(data, validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
			if operational != nil {
				return a.operational(commandName, format, operational.ExitCode, operational.Code, operational.Message+" Nothing was signed.")
			}
			if checked.Status != "valid" {
				return a.operational(commandName, format, validationExit(checked.Status), "JPS-PACK-SIGNATURE-PACK-NOT-VALID", fmt.Sprintf("The pack is %s, as jpack spec validate reports it, so nothing was signed: only a pack this runtime validates is signed.", checked.Status))
			}
			betweenPackChecks()
			document := signer.SignPack(data)
			target := outputPath
			if target == "" {
				target = packPath + packsig.Suffix
			}
			if err := writeNewFile(target, document); err != nil {
				if errors.Is(err, fs.ErrExist) {
					return a.operational(commandName, format, result.ExitIO, "JPS-PACK-SIGNATURE-EXISTS", fmt.Sprintf("Something is already at %s, and a signature is never written over anything; remove it first to sign again.", display.Sanitize(target)))
				}
				return a.operational(commandName, format, result.ExitIO, "JPS-PACK-SIGNATURE-WRITE", fmt.Sprintf("The signature could not be written to %s.", display.Sanitize(target)))
			}
			output := result.PackSignature{
				OutputVersion:    result.OutputVersion,
				Tool:             result.CurrentTool(),
				Command:          commandName,
				Status:           "signed",
				Signature:        display.Sanitize(target),
				SignatureVersion: packsig.Version,
				PackDigest:       packsig.DigestText(sha256.Sum256(data)),
				PublicKey:        signer.PublicKey(),
				KeyID:            signer.KeyID(),
			}
			if format == "json" {
				if err := a.writeJSON(output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			fmt.Fprintf(a.out, "signed: %s %s\n", display.Sanitize(packPath), output.PackDigest)
			fmt.Fprintf(a.out, "signature: %s\n", output.Signature)
			fmt.Fprintf(a.out, "key: %s (public key %s)\n", output.KeyID, output.PublicKey)
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&keyPath, "key", keyPath, "the Ed25519 seed file to sign with")
	command.Flags().StringVar(&publicKeyPath, "public-key", publicKeyPath, "a file holding the public key the seed is expected to have; a seed with another is refused")
	command.Flags().StringVar(&outputPath, "output", outputPath, "where to write the signature document; defaults to <pack-file>.sig.json, and is never written over")
	return command
}

// packsVerifySignatureCommand checks a pack's detached signature against a
// key the verifier names, before the pack is read as a document (ADR-0083).
func (a *App) packsVerifySignatureCommand() *cobra.Command {
	const commandName = "packs verify-signature"
	format := "human"
	publicKeyPath := ""
	signaturePath := ""
	command := &cobra.Command{
		Use:   "verify-signature <pack-file>",
		Short: "Check a pack file's detached signature against the key you expect (EXPERIMENTAL)",
		Long: "Check the signature document <pack-file>.sig.json, or --signature <file>, against the exact bytes of <pack-file> and the public key in --public-key <file> (ADR-0083). " +
			"--public-key is required: a signature is checked only against a key the verifier names (64 hexadecimal characters; jpack audit key public prints it), and the key a signature document carries is never trusted on its own, so a document naming another key is refused whether or not its signature holds under that key. " +
			"The signature is checked before the pack is read as a document, and the pack's bytes are read once: the bytes checked are the bytes then validated. " +
			"The document is read strictly, at most 4096 bytes, one JSON object of exactly algorithm, packDigest, publicKey, signature and signatureVersion, each once and each of its exact form, with no escape, and each refusal has its own code: JPS-PACK-SIGNATURE-TOO-LARGE, -MALFORMED (truncated, not one object, or an escape), -DUPLICATE-MEMBER, -UNKNOWN-MEMBER, -MISSING-MEMBER, -VERSION, -ALGORITHM, -PUBLIC-KEY, -PACK-DIGEST and -VALUE. " +
			"It is then refused when it names another key (JPS-PACK-SIGNATURE-UNEXPECTED-KEY), when its packDigest is not the sha256 digest of the pack's bytes, which changed or are another pack's (JPS-PACK-SIGNATURE-DIGEST-MISMATCH), and when its signature does not hold under the expected key over those bytes (JPS-PACK-SIGNATURE-INVALID). " +
			"No document at the path is JPS-PACK-SIGNATURE-MISSING, and one that cannot be read as one regular file named by its own path, a symbolic link included, is JPS-PACK-SIGNATURE-READ. " +
			"Once the signature is accepted the pack is validated as jpack spec validate validates it, and the report gives both: exit 0 when the pack is valid, 1 when it is invalid and 2 when it is unsupported. Any refusal of the signature exits 1 (4 when the document cannot be read), and the pack is then not read as a document. " +
			"Exit 1 is shared by a refused signature, no signature document and an accepted signature over an invalid pack; --format json tells them apart: the first two are an error whose code is the refusal's JPS-PACK-SIGNATURE code (JPS-PACK-SIGNATURE-MISSING for no document), and the third is a report whose status is pack-invalid. " +
			"An accepted signature shows that whoever held the expected key signed these bytes. It says nothing about whether the pack is correct, tested, reviewed or current, nothing about when it was signed, and nothing after the key is copied: which key to expect, and when to stop expecting it, are the verifier's. " +
			"This command and the signature document's format are EXPERIMENTAL: each may change or be removed without a compatibility promise, and the document's own signatureVersion says which format a reader holds.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			packPath := args[0]
			if failure := a.packSignatureInputs(commandName, format, packPath, signaturePath, publicKeyPath); failure != nil {
				return failure
			}
			if publicKeyPath == "" {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-PACK-SIGNATURE", "--public-key <file> names the public key the signature must be made with, and is required: a signature is checked only against a key the verifier names, never against the key its document carries alone, so nothing was checked.")
			}
			expected, failure := a.readExpectedPublicKey(commandName, format, publicKeyPath)
			if failure != nil {
				return failure
			}
			target := signaturePath
			if target == "" {
				target = packPath + packsig.Suffix
			}
			document, err := a.readInput(target, packsig.MaxBytes, "the signature document")
			switch {
			case errors.Is(err, fssecure.ErrTooLarge):
				return a.operational(commandName, format, result.ExitInvalid, packsig.CodeTooLarge, fmt.Sprintf("The signature %s is larger than the %d bytes a signature document may be.", display.Sanitize(target), packsig.MaxBytes))
			case errors.Is(err, fs.ErrNotExist):
				return a.operational(commandName, format, result.ExitInvalid, "JPS-PACK-SIGNATURE-MISSING", fmt.Sprintf("No signature document is at %s: the pack is unsigned, or its signature is elsewhere (--signature).", display.Sanitize(target)))
			case err != nil:
				return a.operational(commandName, format, result.ExitIO, "JPS-PACK-SIGNATURE-READ", fmt.Sprintf("The signature %s could not be read as one regular file named by its own path; a symbolic link is not followed.", display.Sanitize(target)))
			}
			data, failure := a.readPackOnce(commandName, format, packPath)
			if failure != nil {
				return failure
			}
			accepted, refusal := packsig.Verify(data, document, expected)
			if refusal != nil {
				return a.operational(commandName, format, result.ExitInvalid, refusal.Code, fmt.Sprintf("The signature %s is refused, and the pack was not read as a document. %s", display.Sanitize(target), refusal.Message))
			}
			betweenPackChecks()
			checked, operational := a.engine.Validate(data, validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
			if operational != nil {
				return a.operational(commandName, format, operational.ExitCode, operational.Code, "The signature was accepted, and the pack could not be validated: "+operational.Message)
			}
			output := result.PackSignatureCheck{
				OutputVersion:    result.OutputVersion,
				Tool:             result.CurrentTool(),
				Command:          commandName,
				Status:           "valid",
				Signature:        display.Sanitize(target),
				SignatureVersion: packsig.Version,
				PackDigest:       packsig.DigestText(accepted.PackDigest),
				PublicKey:        hex.EncodeToString(accepted.PublicKey),
				KeyID:            edkey.ID(accepted.PublicKey),
				PackStatus:       checked.Status,
				SpecVersion:      checked.SpecVersion,
			}
			if checked.Status != "valid" {
				output.Status = "pack-" + checked.Status
			}
			if format == "json" {
				if err := a.writeJSON(output); err != nil {
					return &handledExit{code: result.ExitIO}
				}
			} else {
				fmt.Fprintf(a.out, "signature: accepted: %s names key %s (public key %s), the expected key, over %s\n", output.Signature, output.KeyID, output.PublicKey, output.PackDigest)
				line := "pack: " + checked.Status
				if checked.SpecVersion != "" {
					line += " (specVersion " + display.Sanitize(checked.SpecVersion) + ")"
				}
				if checked.Status != "valid" {
					line += "; jpack spec validate reports why"
				}
				fmt.Fprintln(a.out, line)
			}
			return &handledExit{code: validationExit(checked.Status)}
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&publicKeyPath, "public-key", publicKeyPath, "a file holding the public key the signature must be made with (required)")
	command.Flags().StringVar(&signaturePath, "signature", signaturePath, "the signature document; defaults to <pack-file>.sig.json")
	return command
}

// packSignatureInputs refuses, before anything is read, a pack that is not a
// local file named by its path and any other input named by a URL or a
// remote path. A pack's signature is named after the pack's file, so the pack
// is never standard input.
func (a *App) packSignatureInputs(command, format, packPath string, others ...string) error {
	if packPath == "-" {
		return a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "The pack must be a file named by its path, not standard input: its signature document is named after it.")
	}
	for _, input := range append([]string{packPath}, others...) {
		if input != "" && (strings.Contains(input, "://") || fssecure.IsRemotePath(input)) {
			return a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use a local file.")
		}
	}
	return nil
}

// readExpectedPublicKey reads the public key a pack signature, or a seed, is
// expected to have, from a file as audit verify reads its --public-key: 64
// hexadecimal characters and surrounding white space, held to the rule every
// public key a verifier is given is held to.
func (a *App) readExpectedPublicKey(command, format, keyPath string) ([]byte, error) {
	data, err := a.readInput(keyPath, 4096, "a --public-key file")
	if err != nil {
		return nil, a.operational(command, format, result.ExitIO, "JPS-PACK-SIGNATURE-EXPECTED-KEY-READ", fmt.Sprintf("The public key %s could not be read as one bounded regular file.", display.Sanitize(keyPath)))
	}
	public, err := audit.ParsePublicKey(data)
	if err != nil {
		return nil, a.operational(command, format, result.ExitInvocation, packsig.CodeExpectedKey, fmt.Sprintf("The public key %s is not one: %s.", display.Sanitize(keyPath), err.Error()))
	}
	return public, nil
}

// readPackOnce reads the pack file whole, once; every check a command makes
// of the pack is made of these bytes.
func (a *App) readPackOnce(command, format, packPath string) ([]byte, error) {
	data, err := a.readPack(packPath, carrier.HardMaxBytes)
	if errors.Is(err, fssecure.ErrTooLarge) {
		return nil, a.operational(command, format, result.ExitIO, "JPS-RESOURCE-INPUT-BYTE-LIMIT", fmt.Sprintf("The pack exceeds the hard %d-byte limit.", carrier.HardMaxBytes))
	}
	if err != nil {
		return nil, a.operational(command, format, result.ExitIO, "JPS-INPUT-READ", "The pack could not be read as one bounded regular file.")
	}
	return data, nil
}
