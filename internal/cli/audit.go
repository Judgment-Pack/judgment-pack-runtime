package cli

import (
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// auditCommand is the jpack audit group (ADR-0047 §1): reading a chained trail,
// handing over its checkpoint, and starting a new segment after a damaged end.
func (a *App) auditCommand() *cobra.Command {
	group := &cobra.Command{
		Use:   "audit",
		Short: "Check, checkpoint, repair, sign and stamp a chained audit trail",
		Long: "Operations on the audit trail a project keeps (ADR-0018), chained over its exact bytes (ADR-0047). " +
			"verify checks every trail, sequence and previous from the first chained record on, with --public-key the signatures beside it, and with --witness-key a checkpoint witness's statements; checkpoint prints the checkpoint of the last chained record, for handing to someone who will hold it; " +
			"repair starts a new segment after a last line a write did not complete; key makes, shows and rotates the key that signs the records; stamp has a time-stamping authority stamp the current checkpoint. " +
			"None of them evaluates anything.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	group.AddCommand(a.auditVerifyCommand(), a.auditCheckpointCommand(), a.auditRepairCommand(), a.auditKeyCommand(), a.auditStampCommand())
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
	tsaRootPaths := []string{}
	tsaPolicies := []string{}
	tsaCRLPaths := []string{}
	stampsPath := ""
	requireStamped := int64(0)
	witnessPaths := []string{}
	witnessHead := ""
	witnessResume := ""
	witnessSave := ""
	witnessKeyPaths := []string{}
	requireCountersigned := int64(0)
	command := &cobra.Command{
		Use:   "verify",
		Short: "Check a chained audit trail, alone or against a checkpoint",
		Long: "Read an audit trail over its exact bytes and check every trail, sequence and previous from the first chained record on, by the rules the writer chains by (ADR-0047): " +
			"a chained line's sequence is its line number, it carries the identity of the last chained line before it, and its previous is the SHA-256 of the line before it when that line is chained, and of the whole trail before it when it is not. " +
			"The trail is --trail <file>, or else the one the project's jpack.json declares. " +
			"The size is read under the writer's lock, shared, so it falls between two writes, and the bytes before it are read without the lock: writers only append, so they are not delayed and nothing read changes underneath. " +
			"The report gives the coverage: lines before the first chained line (committed as one block), chained lines, unchained lines a later chained line commits to, lines nothing commits to, and lines a repair names as damaged. " +
			"Without --expect this is the integrity of one supplied chain: successful chain-link checks describe only the committed prefix ending at the last chained record; unchained lines after it are uncovered and outside that prefix, and a file with no chained record has no chained history. This does not show the trail is complete, or that its last chained line, or lines rewritten from some point on with their links recomputed, are the ones first written. " +
			"With --expect <file>, the checkpoints a holder kept independently of the operator (jpack audit checkpoint prints them; a file may hold many, one per line, and --expect may be given more than once), the line at each one's sequence must be a chained record of its trail with its record digest: a trail that is shorter, has another identity, or has another record there fails. " +
			"The records up to the highest checkpoint that matched, with no failed check at or before it, are witnessed; the chained records after it are unwitnessed, and --require-checkpoint-through <sequence> fails the verification while the records up to that sequence are not all witnessed. " +
			"With --public-key <file>, the public key the trail was first signed with (64 hexadecimal characters; jpack audit key public prints it), every line of the signature sidecar beside the trail, or of --signatures <file>, is checked in step with the trail: a record signature must be for the record at its sequence, by its exact bytes, and verify under the key in force there, and a key rotation must be signed by the key in force and hands the records after its line to the key it names. " +
			"--public-key given more than once names the keys in the order the trail used them, and a rotation to any other key fails; --revoked <file> names keys not to trust from a sequence on, one {\"from\":N,\"publicKey\":\"...\"} per line, which is how a verifier refuses what a copied key signs. " +
			"A public key under which anyone could sign is refused, as --public-key and in --revoked, and a rotation to one fails: a point of small order, an encoding that is not canonical, or no point of the curve. " +
			"The records up to the highest one whose own signature holds, with no failed check of the chain at or before it, are signed as one uninterrupted prefix; --require-signed-through <sequence> fails the verification while the records up to that sequence are not all signed. Separately, signedRecords counts individual records whose own signatures hold even after an earlier chain break. A record with no signature is unsigned, never a failure by itself, since a signature that could not be written leaves its decision recorded. " +
			"A signature shows only that whoever held the key signed: nothing against the operator, who holds it, and nothing after the key is copied. " +
			"With --tsa-roots <file>, the roots of the time-stamping authorities the verifier trusts (PEM; repeatable), every line of the stamps file beside the trail, or of --stamps <file>, is checked: the token must stamp the SHA-256 of the canonical form of the checkpoint kept with it, its signature and signed attributes must hold, its certificate must be for time-stamping alone and chain to a root supplied at the time the token states, and its policy must be one --tsa-policy <oid> names, when any does; the checkpoint must then match the trail, or the trail was rewritten since. A stamps file larger than 64 MiB fails with stamp-file-too-large; no stamps in it are checked, and the writer refuses appends beyond that bound. " +
			"--tsa-crls <file> supplies certificate revocation lists (PEM or DER; repeatable): a certificate is checked only against a list from its issuer issued at or after the stamp's time and while it was valid, and a stamp no such list speaks for is reported with its status not checked, never as good. " +
			"The records up to the highest checkpoint a trusted stamp covers, with no failed check of the trail at or before it, are stamped as one uninterrupted prefix. Separately, stamps.trusted counts trusted stamps whose individual record checkpoints match even after an earlier chain break. The report gives the lag between each prefix-covered record's at and the first trusted stamp covering it; --require-stamped-through <sequence> fails the verification while the records up to that sequence are not all stamped. " +
			"A stamp shows that its checkpoint existed by the time the authority states, as far as that authority is independent of the operator: not how long before, and a record's at stays the operator's word. " +
			"With --witness-key <file>, the public key of a checkpoint witness the verifier trusts, obtained out of band (64 hexadecimal characters; repeatable, at most 16), held to the same rule as --public-key before anything is read, the statements a witness serves for the trail are read: --witness <file>, one statement per line as the witness serves them (repeatable), and --witness-head <file>, the head the reader fetched from the witness, one statement. " +
			"A witness is a gateway another party runs, which signs the trail's checkpoints and chains its statements per trail (gateway ADR-0013); the runtime fetches nothing. " +
			"The statements are one set, whatever files they came in: each must be of the trail's identity, in the form of statement version 1, with a signature that verifies under the key its keyId names, and two at one index that differ are an equivocation; read from index 0, every index must be present and each statement name the signature before it, checkpoint sequences must increase, a conflict must be at or below the latest checkpoint and a retirement last; and a head must be reached by the statements supplied. " +
			"Each failure is a named finding, witness-malformed, witness-signature-invalid, witness-trail-mismatch, witness-equivocation, witness-chain-broken or witness-head-unreached, and a chain with any of them is credited nothing. " +
			"The checkpoint of every checkpoint statement that verifies is held to the trail as --expect's are, with the same findings; a credited one counts as a held checkpoint, and the coverage's countersigned says how far a credited witness statement reaches; --require-countersigned-through <sequence> fails the verification while the records up to that sequence are not all countersigned. " +
			"A reading with a head is current as of the reader's fetch of it; without one it is historical, ending at the highest statement supplied. " +
			"A verification with no finding at all, given --witness-save <file>, saves a continuation there: the last statement read and the latest checkpoint statement, as signed. --witness-save refuses a destination that is a file the verification reads, by any path or link to it, other than the --witness-resume continuation it advances, and anything already there that is not a regular file holding a continuation, leaving it as it is; the verification is reported either way, a refused destination noted beside a finding. --witness-resume <file> continues from one, checking its two statements again, reading only the statements after its last and holding its checkpoint to the trail again; a statement at or below its last index supplied is refused, and a head below it is witness-head-behind. " +
			"Over 16 keys, 64 MiB of statements files, or 110,000 statements, a continuation's two counted, the reading is refused before any statement is checked, never truncated: a longer chain is read in steps. " +
			"A witness's credited checkpoint statement shows the lines up to its checkpoint are the ones that existed when the witness signed it, as far as that witness is independent of the operator: not who submitted it, and its time is the witness's own clock's. " +
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
			if requireStamped < 0 {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-REQUIRE", "--require-stamped-through must be a sequence from 1.")
			}
			if len(tsaRootPaths) == 0 && (requireStamped > 0 || len(tsaPolicies) > 0 || len(tsaCRLPaths) > 0 || stampsPath != "") {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-STAMPS", "--require-stamped-through, --tsa-policy, --tsa-crls and --stamps apply only with --tsa-roots: no stamp is checked without roots to trust it by.")
			}
			stamps, failure := a.readStampOptions(commandName, format, tsaRootPaths, tsaPolicies, tsaCRLPaths, requireStamped)
			if failure != nil {
				return failure
			}
			if requireCountersigned < 0 {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-REQUIRE", "--require-countersigned-through must be a sequence from 1.")
			}
			if len(witnessKeyPaths) == 0 && (len(witnessPaths) > 0 || witnessHead != "" || witnessResume != "" || witnessSave != "" || requireCountersigned > 0) {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-WITNESS", "--witness, --witness-head, --witness-resume, --witness-save and --require-countersigned-through apply only with --witness-key: no statement is checked without a key to check it by.")
			}
			if witnessSave != "" && (witnessSave == "-" || strings.Contains(witnessSave, "://") || fssecure.IsRemotePath(witnessSave)) {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "The continuation is saved to one local file; pass its path.")
			}
			// Whether the file --witness-save names may be written is decided
			// before anything is written, and reported after the verification,
			// so a refused destination never hides what the verification found.
			var target *continuationTarget
			var refused *saveRefusal
			if witnessSave != "" {
				target, refused = openContinuationTarget(witnessSave)
				if target != nil {
					defer target.dir.Close()
				}
			}
			witness, failure := a.readWitnessOptions(commandName, format, witnessKeyPaths, witnessPaths, witnessHead, witnessResume, requireCountersigned)
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
			opened, failure := a.openTrailFiles(commandName, format, trailPath, configPath, companions{sidecar: signatures != nil, sidecarPath: signaturesPath, stamps: stamps != nil, stampsPath: stampsPath})
			if failure != nil {
				return failure
			}
			defer opened.close()
			// Every input is open now, and recorded: the file --witness-save
			// names is held to them before anything is verified.
			if target != nil && refused == nil {
				refused = a.checkContinuationTarget(target)
			}
			report, locked, failure := a.readTrailWith(commandName, format, opened, audit.Options{Held: held, RequireThrough: requireThrough, Signatures: signatures, Stamps: stamps, Witness: witness})
			if failure != nil {
				return failure
			}
			chain := report.Chain
			if refused != nil {
				if chain.FindingsTotal == 0 {
					return a.operational(commandName, format, refused.exit, refused.code, fmt.Sprintf("--witness-save %s %s. The verification itself found nothing, and nothing was saved.", display.Sanitize(witnessSave), refused.why))
				}
				// The verification found something: it is reported as it would
				// be without --witness-save, which saves nothing then anyway,
				// and the refusal is noted beside it.
				if chain.Witness != nil {
					chain.Witness.SaveRefused = fmt.Sprintf("--witness-save %s %s; nothing was saved", display.Sanitize(witnessSave), refused.why)
				}
			} else if target != nil && report.Continuation != nil {
				if err := target.dir.ReplaceByRename(target.name, report.Continuation, target.existing); err != nil {
					why := "it could not be written"
					if errors.Is(err, fssecure.ErrReplacedChanged) {
						why = "the file there changed after it was checked"
					}
					return a.operational(commandName, format, result.ExitIO, "JPS-AUDIT-WITNESS-SAVE", fmt.Sprintf("The continuation could not be saved to %s: %s. The verification had no finding, and %s is as it was.", display.Sanitize(witnessSave), why, display.Sanitize(witnessSave)))
				}
				chain.Witness.ContinuationSaved = true
			}
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
	command.Flags().StringArrayVar(&tsaRootPaths, "tsa-roots", tsaRootPaths, "a PEM file of root certificates of the time-stamping authorities to trust (repeatable)")
	command.Flags().StringArrayVar(&tsaPolicies, "tsa-policy", tsaPolicies, "a policy OID a stamp may be under, such as 1.2.3.4 (repeatable); without it, any")
	command.Flags().StringArrayVar(&tsaCRLPaths, "tsa-crls", tsaCRLPaths, "a file of certificate revocation lists, PEM or DER, to check the time-stamping certificates against (repeatable)")
	command.Flags().StringVar(&stampsPath, "stamps", stampsPath, "the stamps file to read; without it, the one beside the trail")
	command.Flags().Int64Var(&requireStamped, "require-stamped-through", requireStamped, "fail unless trusted stamps cover every record up to this sequence")
	command.Flags().StringArrayVar(&witnessKeyPaths, "witness-key", witnessKeyPaths, "a file holding the public key of a checkpoint witness to trust, obtained out of band (repeatable, at most 16)")
	command.Flags().StringArrayVar(&witnessPaths, "witness", witnessPaths, "a file of a witness's statements for the trail, one per line, as the witness serves them (repeatable)")
	command.Flags().StringVar(&witnessHead, "witness-head", witnessHead, "a file holding the head you fetched from the witness for the trail: one statement")
	command.Flags().StringVar(&witnessResume, "witness-resume", witnessResume, "a continuation your own earlier successful reading saved with --witness-save, to read on from")
	command.Flags().StringVar(&witnessSave, "witness-save", witnessSave, "where to save a continuation, written only when the verification has no finding at all")
	command.Flags().Int64Var(&requireCountersigned, "require-countersigned-through", requireCountersigned, "fail unless credited witness statements cover every record up to this sequence")
	return command
}

// readWitnessOptions reads the witness keys, statements, head and continuation
// a verification reads, or nil when no witness key was given. The keys are
// counted before any is read and each is held to the public-key rule in
// order, and the files' sizes are bounded together before any of them is
// read; a reading over a bound is refused, never truncated.
func (a *App) readWitnessOptions(command, format string, keyPaths, statementPaths []string, headPath, resumePath string, requireCountersigned int64) (*audit.WitnessOptions, error) {
	if len(keyPaths) == 0 {
		return nil, nil
	}
	refused := func(err error) error {
		var refusal *audit.WitnessRefusal
		if errors.As(err, &refusal) {
			return a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-WITNESS-REFUSED", fmt.Sprintf("The witness's statements are refused before any is checked (%s): %s.", refusal.Reason, refusal.Detail))
		}
		return a.operational(command, format, result.ExitIO, "JPS-AUDIT-WITNESS-READ", "The witness's statements could not be read.")
	}
	if len(keyPaths) > audit.MaxWitnessKeys {
		return nil, refused(&audit.WitnessRefusal{Reason: audit.RefusalKeysOverBound, Detail: fmt.Sprintf("%d witness keys were supplied, and one verification takes at most %d", len(keyPaths), audit.MaxWitnessKeys)})
	}
	supplied := audit.WitnessSupplied{}
	for _, keyPath := range keyPaths {
		if strings.Contains(keyPath, "://") || fssecure.IsRemotePath(keyPath) {
			return nil, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use a local file.")
		}
		data, err := a.readInput(keyPath, 4096, "a --witness-key file")
		if err != nil {
			return nil, a.operational(command, format, result.ExitIO, "JPS-AUDIT-WITNESS-KEY-READ", fmt.Sprintf("The witness key %s could not be read as one bounded regular file.", display.Sanitize(keyPath)))
		}
		public, err := audit.ParsePublicKey(data)
		if err != nil {
			message := fmt.Sprintf("The witness key %s is not one: %s.", display.Sanitize(keyPath), err.Error())
			if reason := audit.WitnessKeyRefusal(err); reason != "" {
				message = fmt.Sprintf("The witness key %s is refused (%s): %s.", display.Sanitize(keyPath), reason, err.Error())
			}
			return nil, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-WITNESS-KEY-INVALID", message)
		}
		supplied.Keys = append(supplied.Keys, public)
	}
	type witnessFile struct {
		path string
		file *os.File
		size int64
	}
	what := func(index int) string {
		switch {
		case index < len(statementPaths):
			return "a --witness file"
		case headPath != "" && index == len(statementPaths):
			return "the --witness-head file"
		}
		return resumeInput
	}
	files := []witnessFile{}
	defer func() {
		for _, each := range files {
			each.file.Close()
		}
	}()
	paths := append([]string{}, statementPaths...)
	for _, extra := range []string{headPath, resumePath} {
		if extra != "" {
			paths = append(paths, extra)
		}
	}
	total := int64(0)
	for index, filePath := range paths {
		if filePath == "-" || strings.Contains(filePath, "://") || fssecure.IsRemotePath(filePath) {
			return nil, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "A witness's statements, its head and a continuation are each read as one local file; pass its path.")
		}
		file, err := a.openInput(filePath, what(index))
		if err != nil {
			return nil, a.operational(command, format, result.ExitIO, "JPS-AUDIT-WITNESS-READ", fmt.Sprintf("The witness file %s could not be opened as one regular file.", display.Sanitize(filePath)))
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, a.operational(command, format, result.ExitIO, "JPS-AUDIT-WITNESS-READ", fmt.Sprintf("The witness file %s could not be opened as one regular file.", display.Sanitize(filePath)))
		}
		files = append(files, witnessFile{path: filePath, file: file, size: info.Size()})
		total += info.Size()
	}
	if total > audit.MaxWitnessBytes {
		return nil, refused(&audit.WitnessRefusal{Reason: audit.RefusalBytesOverBound, Detail: fmt.Sprintf("the witness files hold %d bytes together, more than %d", total, audit.MaxWitnessBytes)})
	}
	read := int64(0)
	contents := make([][]byte, len(files))
	for index, each := range files {
		data, err := io.ReadAll(io.LimitReader(each.file, audit.MaxWitnessBytes-read+1))
		if err != nil {
			return nil, a.operational(command, format, result.ExitIO, "JPS-AUDIT-WITNESS-READ", fmt.Sprintf("The witness file %s could not be read.", display.Sanitize(each.path)))
		}
		read += int64(len(data))
		if read > audit.MaxWitnessBytes {
			// A file grew after it was measured: still over the bound.
			return nil, refused(&audit.WitnessRefusal{Reason: audit.RefusalBytesOverBound, Detail: fmt.Sprintf("the witness files grew past %d bytes together while they were read", audit.MaxWitnessBytes)})
		}
		contents[index] = data
	}
	supplied.Statements = contents[:len(statementPaths)]
	next := len(statementPaths)
	if headPath != "" {
		supplied.Head, supplied.HasHead = contents[next], true
		next++
	}
	if resumePath != "" {
		supplied.Resume, supplied.HasResume = contents[next], true
	}
	input, err := audit.PrepareWitness(supplied)
	if err != nil {
		return nil, refused(err)
	}
	return &audit.WitnessOptions{Input: input, RequireThrough: requireCountersigned}, nil
}

// readStampOptions reads the roots, policies and revocation lists a
// verification checks the stamps by, or nil when no root was given.
func (a *App) readStampOptions(command, format string, rootPaths, policies, crlPaths []string, requireStamped int64) (*audit.StampOptions, error) {
	if len(rootPaths) == 0 {
		return nil, nil
	}
	options := &audit.StampOptions{RequireThrough: requireStamped}
	roots := x509.NewCertPool()
	for _, rootPath := range rootPaths {
		data, failure := a.readTrustFile(command, format, rootPath, "JPS-AUDIT-TSA-ROOTS-READ", "time-stamping roots", "tsa-roots")
		if failure != nil {
			return nil, failure
		}
		found := 0
		for block, rest := pem.Decode(data); block != nil; block, rest = pem.Decode(rest) {
			if block.Type != "CERTIFICATE" {
				continue
			}
			certificate, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-TSA-ROOTS-INVALID", fmt.Sprintf("A certificate in %s could not be read.", display.Sanitize(rootPath)))
			}
			roots.AddCert(certificate)
			found++
		}
		if found == 0 {
			return nil, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-TSA-ROOTS-INVALID", fmt.Sprintf("%s holds no PEM certificate.", display.Sanitize(rootPath)))
		}
	}
	options.Verify.Roots = roots
	for _, text := range policies {
		oid, ok := parseOID(text)
		if !ok {
			return nil, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-STAMPS", fmt.Sprintf("--tsa-policy %s is not an object identifier, such as 1.2.3.4.", display.Sanitize(text)))
		}
		options.Verify.Policies = append(options.Verify.Policies, oid)
	}
	for _, crlPath := range crlPaths {
		data, failure := a.readTrustFile(command, format, crlPath, "JPS-AUDIT-TSA-CRLS-READ", "revocation lists", "tsa-crls")
		if failure != nil {
			return nil, failure
		}
		lists, err := parseCRLs(data)
		if err != nil {
			return nil, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-TSA-CRLS-INVALID", fmt.Sprintf("%s holds a revocation list that could not be read, or none.", display.Sanitize(crlPath)))
		}
		options.Verify.CRLs = append(options.Verify.CRLs, lists...)
	}
	return options, nil
}

// readTrustFile reads one bounded local file a verifier supplies its trust in.
func (a *App) readTrustFile(command, format, filePath, code, what, flag string) ([]byte, error) {
	if strings.Contains(filePath, "://") || fssecure.IsRemotePath(filePath) {
		return nil, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use a local file.")
	}
	data, err := a.readInput(filePath, 16<<20, "a --"+flag+" file")
	if err != nil {
		return nil, a.operational(command, format, result.ExitIO, code, fmt.Sprintf("The %s could not be read as one bounded regular file or standard input stream.", what))
	}
	return data, nil
}

// parseOID reads a dotted object identifier: at least two arcs, each a
// decimal integer.
func parseOID(text string) (asn1.ObjectIdentifier, bool) {
	parts := strings.Split(text, ".")
	if len(parts) < 2 {
		return nil, false
	}
	oid := asn1.ObjectIdentifier{}
	for _, part := range parts {
		arc, err := strconv.Atoi(part)
		if err != nil || arc < 0 || (len(part) > 1 && part[0] == '0') {
			return nil, false
		}
		oid = append(oid, arc)
	}
	return oid, true
}

// parseCRLs reads revocation lists: PEM blocks of type X509 CRL, or one DER
// list.
func parseCRLs(data []byte) ([]*x509.RevocationList, error) {
	lists := []*x509.RevocationList{}
	for block, rest := pem.Decode(data); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "X509 CRL" {
			continue
		}
		list, err := x509.ParseRevocationList(block.Bytes)
		if err != nil {
			return nil, err
		}
		lists = append(lists, list)
	}
	if len(lists) > 0 {
		return lists, nil
	}
	list, err := x509.ParseRevocationList(data)
	if err != nil {
		return nil, err
	}
	return []*x509.RevocationList{list}, nil
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
		data, err := a.readInput(keyPath, 4096, "a --public-key file")
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
		data, err := a.readInput(revokedPath, audit.MaxRevocationBytes, "the --revoked file")
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
	opened, failure := a.openTrailFiles(command, format, trailPath, configPath, companions{})
	if failure != nil {
		return nil, "", failure
	}
	return opened.trail, opened.path, nil
}

// openedTrail is a trail opened for reading and, when signatures or stamps
// are checked, its signature sidecar and its stamps file, each nil when there
// is none.
type openedTrail struct {
	trail   *os.File
	path    string
	sidecar *os.File
	stamps  *os.File
	sizes   []int64
	locked  bool
}

func (o openedTrail) close() {
	for _, file := range []*os.File{o.trail, o.sidecar, o.stamps} {
		if file != nil {
			file.Close()
		}
	}
}

// companions says which of a trail's companion files a command reads: the
// signature sidecar and the stamps file, each either beside the trail or at
// the path given.
type companions struct {
	sidecar, stamps         bool
	sidecarPath, stampsPath string
}

// companion is one companion file's name beside the trail, how a project
// opens it, and how a failure to open it is named.
type companion struct {
	wanted      bool
	explicit    string
	name        string
	label       string
	code        string
	fromProject func(*project.Project) (*os.File, error)
}

// openAuditSnapshot lets tests place a writer between the trail open and lock.
var openAuditSnapshot = fssecure.OpenSnapshot

// openTrailFiles is openTrail, and the companion files asked for too: each at
// the path given, which must be there, or else the one beside the trail,
// opened the way the trail was, which may not be. Existence, identity and sizes
// are captured together under the shared trail lock.
func (a *App) openTrailFiles(command, format, trailPath, configPath string, wanted companions) (openedTrail, error) {
	if trailPath != "" && configPath != "" {
		return openedTrail{}, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-AUDIT-TRAIL", "Pass --trail or --config, not both: one trail is read.")
	}
	var opened openedTrail
	files := []companion{
		{wanted.sidecar, wanted.sidecarPath, audit.SidecarName, "signature sidecar", "JPS-AUDIT-SIGNATURES-READ", (*project.Project).OpenSidecar},
		{wanted.stamps, wanted.stampsPath, audit.StampsName, "stamps file", "JPS-AUDIT-STAMPS-READ", (*project.Project).OpenStamps},
	}
	for _, each := range files {
		if each.explicit != "" && (each.explicit == "-" || strings.Contains(each.explicit, "://") || fssecure.IsRemotePath(each.explicit)) {
			return openedTrail{}, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", fmt.Sprintf("The %s is read as one local file; pass its path.", each.label))
		}
	}
	var openTrail func() (*os.File, error)
	var loaded *project.Project
	if trailPath != "" {
		if trailPath == "-" {
			return openedTrail{}, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-STDIN", "The trail is read as a file, not from standard input; pass its path.")
		}
		if strings.Contains(trailPath, "://") || fssecure.IsRemotePath(trailPath) {
			return openedTrail{}, a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "URL and remote filesystem inputs are not supported; use a local file.")
		}
		opened.path = trailPath
		openTrail = func() (*os.File, error) { return a.openInput(trailPath, "the trail") }
	} else {
		var failure error
		loaded, failure = a.loadProject(configPath, command, format)
		if failure != nil {
			return openedTrail{}, failure
		}
		defer loaded.Close()
		if _, declared := loaded.TrailName(); !declared {
			return openedTrail{}, a.operational(command, format, result.ExitInvocation, "JPS-AUDIT-NOT-DECLARED", notDeclaredMessage)
		}
		opened.path = loaded.TrailPath()
		openTrail = func() (*os.File, error) {
			file, err := loaded.OpenTrail()
			a.noteInput(file, "the trail")
			return file, err
		}
	}
	openers := make([]func() (*os.File, error), len(files))
	for index, each := range files {
		openers[index] = func() (*os.File, error) {
			if !each.wanted {
				return nil, nil
			}
			var file *os.File
			var err error
			switch {
			case each.explicit != "":
				file, err = a.openInput(each.explicit, "the "+each.label)
			case loaded != nil:
				file, err = each.fromProject(loaded)
				a.noteInput(file, "the "+each.label)
			default:
				file, err = a.openInput(filepath.Join(filepath.Dir(trailPath), each.name), "the "+each.label)
			}
			if errors.Is(err, fs.ErrNotExist) && each.explicit == "" {
				return nil, nil
			}
			if err != nil {
				return nil, a.operational(command, format, result.ExitIO, each.code, fmt.Sprintf("The %s could not be opened as one regular file.", each.label))
			}
			return file, nil
		}
	}
	snapshot, sizes, locked, err := openAuditSnapshot(openTrail, openers...)
	if err != nil {
		var handled *handledExit
		if errors.As(err, &handled) {
			return openedTrail{}, err
		}
		return openedTrail{}, a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", "The trail could not be opened and read between writes.")
	}
	opened.trail, opened.sidecar, opened.stamps = snapshot[0], snapshot[1], snapshot[2]
	opened.sizes, opened.locked = sizes, locked
	return opened, nil
}

// readTrail takes a snapshot of an open trail between writes and verifies it.
// The second result says whether the snapshot was taken under the lock.
func (a *App) readTrail(command, format string, file *os.File, options audit.Options) (audit.Report, bool, error) {
	return a.readTrailWith(command, format, openedTrail{trail: file}, options)
}

// readTrailWith is readTrail for a trail and its companion files, whose
// sizes are read under the one shared lock on the trail, so the trail and its
// sidecar fall between two writes: a writer appends to the sidecar under the
// trail's lock. The stamps file is appended under its own lock, and a line a
// write left incomplete when its size was read is read as unreadable.
func (a *App) readTrailWith(command, format string, opened openedTrail, options audit.Options) (audit.Report, bool, error) {
	sizes, locked := opened.sizes, opened.locked
	var err error
	if sizes == nil {
		sizes, locked, err = fssecure.SizesBetweenWrites(opened.trail, opened.sidecar, opened.stamps)
	}
	if err != nil {
		return audit.Report{}, false, a.operational(command, format, result.ExitIO, "JPS-AUDIT-TRAIL-READ", "The trail's size could not be read between writes.")
	}
	if options.Signatures != nil && opened.sidecar != nil {
		options.Signatures.Sidecar, options.Signatures.SidecarSize = opened.sidecar, sizes[1]
	}
	if options.Stamps != nil && opened.stamps != nil {
		options.Stamps.Stamps, options.Stamps.StampsSize = opened.stamps, sizes[2]
	}
	report, err := audit.Verify(opened.trail, sizes[0], options)
	switch {
	case errors.Is(err, audit.ErrSidecarRead):
		return audit.Report{}, false, a.operational(command, format, result.ExitIO, "JPS-AUDIT-SIGNATURES-READ", "The signature sidecar could not be read to its end.")
	case errors.Is(err, audit.ErrStampsRead):
		return audit.Report{}, false, a.operational(command, format, result.ExitIO, "JPS-AUDIT-STAMPS-READ", "The stamps file could not be read to its end.")
	case err != nil:
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
	data, err := a.readInput(expectPath, audit.MaxHeldBytes, "an --expect file")
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
		checkpointed := output.Coverage.Checkpointed
		switch {
		case output.Held != nil && output.Held.Latest != nil && (checkpointed.Status != "through" || checkpointed.Through == output.Held.Latest.Sequence):
			fmt.Fprintf(a.out, "consistent, and witnessed through the held checkpoint at sequence %d: %d line(s)\n", output.Held.Latest.Sequence, output.Lines)
		case checkpointed.Status == "through":
			fmt.Fprintf(a.out, "consistent, and witnessed through the checkpoint a witness statement countersigns at sequence %d: %d line(s)\n", checkpointed.Through, output.Lines)
		case output.Coverage.Chained == 0:
			fmt.Fprintf(a.out, "no chained record: %d line(s), all uncovered\n", output.Lines)
		default:
			fmt.Fprintf(a.out, "consistent through sequence %d; %d uncovered line(s) after it\n", output.Head.Sequence, output.Coverage.Uncovered)
		}
	case "segmented":
		if output.Coverage.Uncovered > 0 {
			fmt.Fprintf(a.out, "SEGMENTED: %d discontinuity record(s); the history is not intact across them; chain-link checks passed through sequence %d, with %d uncovered line(s) after it\n", output.DiscontinuitiesTotal, output.Head.Sequence, output.Coverage.Uncovered)
		} else {
			fmt.Fprintf(a.out, "SEGMENTED: %d discontinuity record(s); the history is not intact across them, and each segment is consistent\n", output.DiscontinuitiesTotal)
		}
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
	stamped := "not checked (no --tsa-roots)"
	switch coverage.Stamped.Status {
	case "through":
		stamped = fmt.Sprintf("through sequence %d", coverage.Stamped.Through)
	case "none":
		stamped = "none"
	}
	fmt.Fprintf(a.out, "records: %d witnessed by a held checkpoint, %d unwitnessed; stamped: %s\n", coverage.Witnessed, coverage.Unwitnessed, stamped)
	if output.Stamps != nil {
		stamps := output.Stamps
		fmt.Fprintf(a.out, "stamps file: %d line(s), %d unreadable, %d trusted; revocation checked for %d, not checked for %d\n",
			stamps.Lines, stamps.Unreadable, stamps.Trusted, stamps.RevocationChecked, stamps.RevocationNotChecked)
		if stamps.CoveredBy != "" {
			fmt.Fprintf(a.out, "stamped records existed by %s, as the authority attests\n", stamps.CoveredBy)
		}
		if lag := stamps.Lag; lag != nil && lag.Records > 0 {
			fmt.Fprintf(a.out, "lag from at to the first trusted stamp: %d record(s), longest %.3fs (sequence %d), shortest %.3fs (sequence %d)\n",
				lag.Records, lag.MaxSeconds, lag.MaxSequence, lag.MinSeconds, lag.MinSequence)
			if lag.AtAfterStamp {
				fmt.Fprintln(a.out, "note: some record's at is later than the time a stamp attests it existed by; at is the operator's word")
			}
		}
	}
	if output.Signatures != nil {
		fmt.Fprintf(a.out, "records: %d with a valid signature of their own, %d without\n", coverage.SignedRecords, coverage.UnsignedRecords)
		fmt.Fprintf(a.out, "signature sidecar: %d line(s), %d unreadable, %d rotation(s) followed; first key %s, key in force %s; %d public key(s) and %d revocation(s) supplied\n",
			output.Signatures.Lines, output.Signatures.Unreadable, output.Signatures.Rotations, output.Signatures.FirstKey, output.Signatures.KeyInForce, output.Signatures.KeysSupplied, output.Signatures.Revocations)
	}
	if output.Held != nil {
		fmt.Fprintf(a.out, "held checkpoints: %d supplied, %d matched, %d failed\n", output.Held.Supplied, output.Held.Matched, output.Held.Failed)
	}
	if witness := output.Witness; witness != nil {
		a.renderWitness(witness, coverage.Countersigned)
	}
	if output.Required != nil {
		fmt.Fprintf(a.out, "required: every record through sequence %d witnessed: %s\n", output.Required.Through, output.Required.Status)
	}
	if output.RequiredSigned != nil {
		fmt.Fprintf(a.out, "required: every record through sequence %d signed: %s\n", output.RequiredSigned.Through, output.RequiredSigned.Status)
	}
	if output.RequiredStamped != nil {
		fmt.Fprintf(a.out, "required: every record through sequence %d stamped: %s\n", output.RequiredStamped.Through, output.RequiredStamped.Status)
	}
	if output.RequiredCountersigned != nil {
		fmt.Fprintf(a.out, "required: every record through sequence %d countersigned: %s\n", output.RequiredCountersigned.Through, output.RequiredCountersigned.Status)
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

// renderWitness reports a witness's statements as read: how many, where the
// reading began and ended, the latest checkpoint statement, the conflicts,
// and how far the countersigned coverage reaches.
func (a *App) renderWitness(witness *result.AuditWitness, countersigned result.AuditCoverageState) {
	began := "read from index 0"
	switch {
	case witness.ContinuedAfter != nil:
		began = fmt.Sprintf("continued after index %d", *witness.ContinuedAfter)
	case witness.Began == "continued":
		began = "continued from a continuation that could not be read"
	}
	fmt.Fprintf(a.out, "witness: %d statement line(s) read, %d statement(s) checked, %d key(s) supplied; %s\n",
		witness.StatementsRead, witness.StatementsChecked, witness.KeysSupplied, began)
	if witness.Status == "read" {
		ended := "historical, ending at the highest index supplied"
		index := int64(0)
		if witness.HighestIndex != nil {
			index = *witness.HighestIndex
		}
		if witness.Reading == "current" && witness.HeadIndex != nil {
			ended, index = "current as of the fetch of the head", *witness.HeadIndex
		}
		fmt.Fprintf(a.out, "witness reading: %s, index %d\n", ended, index)
		if latest := witness.LatestCheckpoint; latest != nil {
			fmt.Fprintf(a.out, "latest checkpoint statement: index %d, sequence %d, witnessed at %s by the witness's clock\n", latest.Index, latest.Sequence, latest.WitnessedAt)
		}
		if witness.ConflictsTotal > 0 {
			fmt.Fprintf(a.out, "conflict statements: %d, at sequence(s) %s\n", witness.ConflictsTotal, joinSequences(witness.Conflicts, witness.ConflictsTotal))
		}
		if witness.Retired {
			fmt.Fprintln(a.out, "the witness's chain for this trail is retired: it ends with a retirement statement")
		}
	} else {
		fmt.Fprintln(a.out, "witness reading: failed, so no statement is credited")
	}
	switch countersigned.Status {
	case "through":
		fmt.Fprintf(a.out, "countersigned: through sequence %d\n", countersigned.Through)
	default:
		fmt.Fprintf(a.out, "countersigned: %s\n", countersigned.Status)
	}
	if witness.ContinuationSaved {
		fmt.Fprintln(a.out, "witness continuation saved")
	}
	if witness.SaveRefused != "" {
		fmt.Fprintln(a.out, "note: "+display.Sanitize(witness.SaveRefused))
	}
}

// joinSequences lists sequences for a line of the human report, saying how
// many more were not listed.
func joinSequences(sequences []int64, total int64) string {
	parts := make([]string, 0, len(sequences))
	for _, sequence := range sequences {
		parts = append(parts, strconv.FormatInt(sequence, 10))
	}
	text := strings.Join(parts, ", ")
	if omitted := total - int64(len(sequences)); omitted > 0 {
		text += fmt.Sprintf(", and %d more, not listed", omitted)
	}
	return text
}

// resumeInput is what the continuation --witness-resume names is recorded as:
// the one input --witness-save may replace, since a reading advances it.
const resumeInput = "the --witness-resume continuation"

// continuationTarget is where --witness-save writes: the directory that is to
// hold the continuation, opened once and held from the check to the rename;
// the continuation's name in it and its path as given; and what was there when
// it was checked, nil when nothing was.
type continuationTarget struct {
	dir      *fssecure.Root
	name     string
	path     string
	existing os.FileInfo
}

// saveRefusal is why the file --witness-save names may not be written: the
// reason, which reads after the destination's path, and the code and exit code
// a refusal answers with when the verification itself found nothing.
type saveRefusal struct {
	code string
	exit int
	why  string
}

// refusedSave is a destination refused as an invocation's mistake.
func refusedSave(why string) *saveRefusal {
	return &saveRefusal{code: "JPS-INVOCATION-AUDIT-WITNESS-SAVE", exit: result.ExitInvocation, why: why}
}

// openContinuationTarget opens the directory the file --witness-save names is
// in. A symbolic link among the directories of its path is followed, since a
// reader may name any directory; once it is opened, the continuation is
// written to that directory, by the rename ReplaceByRename makes.
func openContinuationTarget(target string) (*continuationTarget, *saveRefusal) {
	dir, name := filepath.Split(target)
	if name == "" || name == "." || name == ".." {
		return nil, refusedSave("names no file")
	}
	if dir == "" {
		dir = "."
	}
	opened, err := fssecure.OpenRoot(dir)
	if err != nil {
		return nil, &saveRefusal{code: "JPS-AUDIT-WITNESS-SAVE", exit: result.ExitIO, why: "is in a directory that could not be opened"}
	}
	return &continuationTarget{dir: opened, name: name, path: target}, nil
}

// checkContinuationTarget holds the file --witness-save names to what a
// verification may replace, once every input is open and before anything is
// verified, and answers why it may not be written, or nil:
//
//   - it is none of the files this invocation read, by the identity of the file
//     itself, so another spelling of its path, a symbolic link or a hard link
//     to it is the same file; the continuation --witness-resume read is the one
//     exception, since a reading advances it;
//   - when something is there, it is a regular file, not a symbolic link, that
//     holds a continuation; anything else is left as it is.
//
// The inputs are what the App recorded as it opened them (App.inputs), so an
// input read through the App's readers is held to this without naming it here.
func (a *App) checkContinuationTarget(target *continuationTarget) *saveRefusal {
	refuse := refusedSave
	// The file is looked up by its path, following every link, and through the
	// held directory, following a link that stays in it: either is the file a
	// rename onto the name would replace.
	named := []os.FileInfo{}
	if info, err := os.Stat(target.path); err == nil {
		named = append(named, info)
	}
	if info, err := target.dir.Stat(target.name); err == nil {
		named = append(named, info)
	}
	for _, info := range named {
		for _, input := range a.inputs {
			if input.what != resumeInput && os.SameFile(info, input.info) {
				return refuse(fmt.Sprintf("is %s, which this verification reads, and saving would replace it", input.what))
			}
		}
	}
	info, err := target.dir.Lstat(target.name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return refuse("could not be examined")
	case info.Mode()&os.ModeSymlink != 0:
		return refuse("is a symbolic link; a continuation is saved to a file named by its own path")
	case !info.Mode().IsRegular():
		return refuse("is not a regular file")
	}
	data, err := target.dir.Read(target.name, audit.MaxWitnessBytes)
	if err != nil || !audit.IsContinuation(data) {
		return refuse("holds something other than a continuation, and is left as it is; name a new file, or the continuation a reading saved")
	}
	target.existing = info
	return nil
}
