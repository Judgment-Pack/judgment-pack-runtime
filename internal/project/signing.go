package project

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// UnsignedCode is the warning a deciding run reports when the signing key its
// project names will not sign the record the run writes (ADR-0071 §1). The
// record is written and the disposition reported as before; the warning is
// how the caller learns, at decision time, what packs validate would say.
const UnsignedCode = "JPS-AUDIT-UNSIGNED"

// SigningRequiredCode is the refusal a project whose audit member sets
// requireSigning gives a deciding run whose record would not be signed
// (ADR-0071 §2): before any evaluation, with nothing recorded.
const SigningRequiredCode = "JPS-AUDIT-SIGNING-REQUIRED"

// Unsigned is why the signing key a project names will not sign the records
// written now: the key is refused, the signature sidecar could not be read,
// or the key is not the key in force there. It is the reason packs validate's
// audit-signing-key check fails with, held in parts so that the check, the
// warning a deciding run reports and the refusal requireSigning gives are one
// wording.
type Unsigned struct {
	// subject is what is wrong, as the clause a sentence opens with.
	subject string
	// consequence is what it does to the records.
	consequence string
	// reason says why, or what fixes it; it may be empty.
	reason string
}

// Detail is the reason as one sentence, exactly as packs validate's
// audit-signing-key check gives it.
func (u *Unsigned) Detail() string {
	return capitalized(u.subject) + ", " + u.consequence + u.because() + "."
}

// Diagnostic is the warning a deciding run reports: UnsignedCode, with the
// check's detail as its message.
func (u *Unsigned) Diagnostic() result.Diagnostic {
	return result.WarningDiagnostic(UnsignedCode, "audit", "", u.Detail())
}

// clause is the reason as the clause of a refusal, which records nothing and
// so says nothing of what records it leaves unsigned.
func (u *Unsigned) clause() string {
	return u.subject + u.because()
}

func (u *Unsigned) because() string {
	if u.reason == "" {
		return ""
	}
	return ": " + u.reason
}

// capitalized is text with its first letter in upper case. Every subject
// opens with an ASCII letter.
func capitalized(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

// signingKeyNamer says which input named the signing key, for a reason.
func signingKeyNamer(source string) string {
	if source == SigningKeyFromEnvironment {
		return audit.SigningKeyEnv
	}
	return "the audit member's signingKey"
}

// expectedKeyNamer says which input named the expected public key, for a
// reason.
func expectedKeyNamer(source string) string {
	if source == SigningKeyFromEnvironment {
		return audit.SigningPublicKeyEnv
	}
	return "the audit member's signingPublicKey"
}

// signing loads the signing key the project names and says whether it signs
// the records written now. It answers the key whenever it loaded, as the
// writer is given it, and the reason it will not sign, or nil when it will.
// Both are nil when no key is named, when the trail is not chained, which no
// key signs, and when the configuration declares no audit member. It applies
// packs validate's rules in packs validate's order: the key is loaded under
// audit.LoadSigner's rules, and a key that loads must also be the key in force
// at the end of the signature sidecar, which a sidecar that cannot be read
// does not show.
func (p *Project) signing() (*audit.Signer, *Unsigned) {
	keyPath, source := p.SigningKeyPath()
	if keyPath == "" || !p.Config.Audit.Chains() {
		return nil, nil
	}
	named := signingKeyNamer(source)
	expected, expectedSource := p.ExpectedSigningKey()
	signer, err := p.loadKey(keyPath, expected)
	switch {
	case errors.Is(err, audit.ErrExpectedKeyMalformed), errors.Is(err, audit.ErrKeyNotExpected):
		// What is refused is which key it is, not where it is, so neither the
		// path nor either key is named: the reason can be relayed as it is.
		return nil, &Unsigned{
			subject:     fmt.Sprintf("the signing key %s names is refused", named),
			consequence: "so records are written unsigned",
			reason:      fmt.Sprintf("%s (%s names the expected key)", audit.KeyRefusal(err), expectedKeyNamer(expectedSource)),
		}
	case err != nil:
		return nil, &Unsigned{
			subject:     fmt.Sprintf("the signing key %s names, %s, is refused", named, display.Sanitize(keyPath)),
			consequence: "so records are written unsigned",
			reason:      display.Sanitize(audit.KeyRefusal(err)),
		}
	}
	writer := audit.NewWriter(p.root, p.Config.Audit.Dir, true)
	writer.SignWith(signer)
	inForce, err := writer.KeyInForce()
	switch {
	case err != nil:
		return signer, &Unsigned{
			subject:     fmt.Sprintf("the signature sidecar could not be read to see whether key %s is the key in force", signer.KeyID()),
			consequence: "so records may be written unsigned",
		}
	case !inForce:
		return signer, &Unsigned{
			subject:     fmt.Sprintf("key %s is not the key in force in the signature sidecar", signer.KeyID()),
			consequence: "so it signs nothing and records are written unsigned",
			reason:      "rotate to it from the key in force with jpack audit key rotate, or name the key in force",
		}
	}
	return signer, nil
}

// DecisionWriter is AuditWriter for a deciding run, with the reason the
// record it writes will be unsigned (ADR-0071). The writer is the one
// AuditWriter makes, given the key whenever it loads, so what is recorded and
// how it is signed are unchanged; the key is loaded once, and the reason is
// about that key. The reason is nil when the key signs, when no key is named,
// when the trail is not chained, and when the configuration declares no audit
// member, so nil means nothing to report, never that a record will be signed.
//
// It is read before anything is evaluated, as packs validate reads it. What
// changes between then and the write is not seen: a rotation another process
// writes in between, or a signature that cannot be written, still leaves the
// record unsigned with nothing reported, and audit verify
// --require-signed-through is the reader's check for those.
func (p *Project) DecisionWriter() (*audit.Writer, *Unsigned) {
	if p == nil || p.Config.Audit == nil {
		return nil, nil
	}
	writer := audit.NewWriter(p.root, p.Config.Audit.Dir, p.Config.Audit.Chains())
	signer, unsigned := p.signing()
	writer.SignWith(signer)
	return writer, unsigned
}

// RequiresSigning says whether this project's audit member asks its deciding
// surfaces to refuse a decision whose record would not be signed (ADR-0071
// §2). The nil project, and one with no audit member, ask for nothing.
func (p *Project) RequiresSigning() bool {
	return p != nil && p.Config.Audit != nil && p.Config.Audit.RequireSigning
}

// RequireSigning is what a deciding surface does, with the reason
// DecisionWriter gave, for a project whose audit member sets requireSigning
// (ADR-0071 §2): nil when the named key signs, and otherwise the refusal,
// before any evaluation, so nothing is recorded. It refuses when no signing
// key is named and when the key named will not sign, for the reason packs
// validate gives. A declared rehearsal records nothing and is not a decision,
// and the surfaces never ask this of one.
//
// It binds as far as requireReviewed does (ADR-0044 §5): whoever chooses
// which configuration a run reads chooses whether this applies, and whoever
// can edit the configuration can turn it off, which the reviewed-set lock,
// pinning the configuration's digest, sees as an amendment. Whoever can write
// the signature sidecar can make every decision refused, as a forged rotation
// stops the named key signing.
func (p *Project) RequireSigning(unsigned *Unsigned) *Failure {
	if !p.RequiresSigning() {
		return nil
	}
	config := display.Sanitize(p.ConfigPath)
	if keyPath, _ := p.SigningKeyPath(); keyPath == "" {
		return &Failure{
			Code: SigningRequiredCode,
			Message: fmt.Sprintf("This evaluation was refused because %s requires every decision's record to be signed, and no signing key is named. Name one by the audit member's signingKey or by %s, or declare the run a rehearsal.",
				config, audit.SigningKeyEnv),
			ExitCode: result.ExitInvalid,
		}
	}
	if !p.Config.Audit.Chains() {
		// The schema refuses requireSigning beside chain false; this is the
		// rule, not the schema, saying so.
		return &Failure{
			Code: SigningRequiredCode,
			Message: fmt.Sprintf("This evaluation was refused because %s requires every decision's record to be signed, and its trail is not chained, so no record is signed. Remove chain false, or declare the run a rehearsal.",
				config),
			ExitCode: result.ExitInvalid,
		}
	}
	if unsigned == nil {
		return nil
	}
	return &Failure{
		Code: SigningRequiredCode,
		Message: fmt.Sprintf("This evaluation was refused because %s requires every decision's record to be signed, and %s. Make the named key one that signs, or declare the run a rehearsal.",
			config, unsigned.clause()),
		ExitCode: result.ExitInvalid,
	}
}
