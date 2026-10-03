package audit

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
)

// The signature sidecar (ADR-0047 §2b). Beside a chained trail, a writer that
// holds a signing key keeps one more file, SidecarName, of compact JSON lines,
// each in its RFC 8785 canonical form: members in code-point order, no
// whitespace, strings that need no escape and integers under 2^53. Two kinds of
// line:
//
//   - a record signature, for one chained record:
//     {"keyId","kind":"record-signature","record","sequence","sidecarVersion":"1","signature","trail"},
//     where record is the SHA-256 of the record's exact line bytes, without its
//     newline, in the "sha256:" form, and signature is Ed25519 over
//     recordSignaturePrefix followed by the canonical form of
//     {"record","sequence","trail"};
//   - a key rotation, made with the key in force and naming the next one:
//     {"at","keyId","kind":"key-rotation","next","sidecarVersion":"1","signature","trail"},
//     where at is the line number of the trail's last line when the rotation
//     was made, next is the next key's public key, and signature is Ed25519
//     over keyRotationPrefix followed by the canonical form of
//     {"at","next","trail"}. Records up to at are signed with the old key,
//     and records after it with the next.
//
// keyId is the gateway's form for a key: the first 32 lowercase hexadecimal
// characters of the SHA-256 of the 32 raw bytes of the public key. A public
// key is 64 lowercase hexadecimal characters and a signature 128. The record
// line itself is never touched, and the sidecar is written after it, under the
// same lock: a record whose signature could not be written is unsigned, never
// a failed decision.
const (
	// SidecarName is the signature sidecar, beside the trail.
	SidecarName = "signatures.jsonl"
	// SidecarVersion is the shape of a sidecar line.
	SidecarVersion = "1"
	// KindRecordSignature and KindKeyRotation are the two kinds of line.
	KindRecordSignature = "record-signature"
	KindKeyRotation     = "key-rotation"
	// SigningKeyEnv names the environment variable that gives the signing
	// key's path. Set and not empty, it is the key, whatever the
	// configuration names.
	SigningKeyEnv = "JPACK_SIGNING_KEY"

	recordSignaturePrefix = "judgment-pack-runtime/record-signature/1:"
	keyRotationPrefix     = "judgment-pack-runtime/key-rotation/1:"
	// maxSidecarLineBytes bounds a sidecar line read whole: a line of either
	// kind is under four hundred bytes, and a longer one is unreadable.
	maxSidecarLineBytes = 4096
	// maxKeyFileBytes bounds what is read of a key file: a seed file is
	// sixty-five bytes.
	maxKeyFileBytes = 4096
)

var (
	publicKeyForm = regexp.MustCompile(`^[0-9a-f]{64}$`)
	keyIDForm     = regexp.MustCompile(`^[0-9a-f]{32}$`)
	signatureForm = regexp.MustCompile(`^[0-9a-f]{128}$`)
)

// The refusals of a signing key, and of signing with one. None carries any of
// the key's contents.
var (
	// ErrKeyNotAbsolute is a key path that is not absolute: a key outside the
	// project is named by where it is, never relative to anything.
	ErrKeyNotAbsolute = errors.New("the signing key's path is not absolute")
	// ErrKeyInsideProject is a key file inside the project's directory, where
	// whatever can edit the project can read it.
	ErrKeyInsideProject = errors.New("the signing key is inside the project's directory")
	// ErrKeyThroughLink is a key whose path goes through a symbolic link at
	// any component: what a link names can change between a check and an
	// open, so a key is named by its real path.
	ErrKeyThroughLink = errors.New("the signing key's path goes through a symbolic link; name the key by its real path")
	// ErrKeyPathChanged is a key whose path changed while it was opened.
	ErrKeyPathChanged = errors.New("the signing key's path changed while it was opened")
	// ErrKeyLinked is a key file with more than one name, another of which
	// could be anywhere, the project included.
	ErrKeyLinked = errors.New("the signing key has more than one name (a hard link)")
	// ErrKeyNotRegular is a key that is not one regular file.
	ErrKeyNotRegular = errors.New("the signing key is not one regular file")
	// ErrKeyPrivacyUnchecked is any key on a platform where this runtime
	// cannot establish that a key is its owner's alone: Windows, whose ACLs it
	// does not read, and any platform without unix ownership and modes.
	ErrKeyPrivacyUnchecked = errors.New("this platform cannot show that a signing key is readable by its owner alone (Windows ACLs are not read), so no key signs here")
	// ErrKeyNotOwned is a key file another user owns.
	ErrKeyNotOwned = errors.New("the signing key is not owned by the user this runtime runs as")
	// ErrKeyTooOpen is a key file its group or other users can read or write.
	ErrKeyTooOpen = errors.New("the signing key can be read or written by its group or by other users")
	// ErrKeyMalformed is a key file that is not one Ed25519 seed: 64
	// hexadecimal characters, with nothing else but surrounding whitespace.
	ErrKeyMalformed = errors.New("the signing key is not 64 hexadecimal characters")
	// ErrKeyNotInForce is a key that is not the key in force at the end of
	// the sidecar: another key signed last, or a rotation named another. Such
	// a key signs nothing, so the record stays unsigned.
	ErrKeyNotInForce = errors.New("the signing key is not the key in force in the signature sidecar")
	// ErrNoSigningKey is a rotation asked of a writer that holds no key.
	ErrNoSigningKey = errors.New("the project has no signing key")
	// ErrRotationNoTrail is a rotation asked for before the trail has a
	// chained record: there is no trail identity to bind it to.
	ErrRotationNoTrail = errors.New("the trail has no chained record to rotate the key after")
	// ErrRotationSameKey is a rotation to the key already in force.
	ErrRotationSameKey = errors.New("the next key is the key already in force")
)

// KeyRefusal says why a signing key was refused, in words that carry nothing
// read from the key's file.
func KeyRefusal(err error) string {
	for _, known := range []error{ErrKeyNotAbsolute, ErrKeyPrivacyUnchecked, ErrKeyInsideProject, ErrKeyThroughLink, ErrKeyPathChanged, ErrKeyLinked, ErrKeyNotRegular, ErrKeyNotOwned, ErrKeyTooOpen, ErrKeyMalformed} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "no file is there"
	}
	return "it could not be opened as one regular file named by its own path"
}

// Signer signs with one Ed25519 key. It gives out only the key's public half
// and its keyId.
type Signer struct {
	private ed25519.PrivateKey
	public  ed25519.PublicKey
}

// PublicKey is the signer's public key, 64 lowercase hexadecimal characters.
func (s *Signer) PublicKey() string { return hex.EncodeToString(s.public) }

// KeyID is the signer's keyId.
func (s *Signer) KeyID() string { return KeyID(s.public) }

// KeyID is a public key's id in the gateway's form: the first 32 lowercase
// hexadecimal characters of the SHA-256 of its 32 raw bytes.
func KeyID(public ed25519.PublicKey) string {
	sum := sha256.Sum256(public)
	return hex.EncodeToString(sum[:])[:32]
}

// NewSignerFromSeed is a signer for a 32-byte seed.
func NewSignerFromSeed(seed []byte) (*Signer, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, ErrKeyMalformed
	}
	private := ed25519.NewKeyFromSeed(seed)
	return &Signer{private: private, public: private.Public().(ed25519.PublicKey)}, nil
}

// ParseSeed reads a seed file's contents: 64 hexadecimal characters, with
// nothing else but surrounding whitespace, which is the form the gateway's
// keygen and jpack audit key generate write.
func ParseSeed(data []byte) ([]byte, error) {
	text := strings.ToLower(strings.TrimSpace(string(data)))
	if !publicKeyForm.MatchString(text) {
		return nil, ErrKeyMalformed
	}
	seed, err := hex.DecodeString(text)
	if err != nil {
		return nil, ErrKeyMalformed
	}
	return seed, nil
}

// ParsePublicKey reads a public key file's contents: 64 hexadecimal
// characters, with nothing else but surrounding whitespace, that
// CheckPublicKey accepts.
func ParsePublicKey(data []byte) (ed25519.PublicKey, error) {
	text := strings.ToLower(strings.TrimSpace(string(data)))
	if !publicKeyForm.MatchString(text) {
		return nil, ErrPublicKeyForm
	}
	public, err := hex.DecodeString(text)
	if err != nil {
		return nil, ErrPublicKeyForm
	}
	if err := CheckPublicKey(public); err != nil {
		return nil, err
	}
	return ed25519.PublicKey(public), nil
}

// LoadSigner reads the signing key at keyPath for the project whose directory
// is project (nil for none), the key a writer signs with. The key must be named
// by an absolute path, and it is read only where this platform can establish
// that it is its owner's alone (keyPrivacyChecked): on unix, by readKey's
// rules. Elsewhere, Windows among them, every key is refused
// (ErrKeyPrivacyUnchecked), so records there are unsigned. No error carries any
// of the key's contents.
func LoadSigner(keyPath string, project os.FileInfo) (*Signer, error) {
	if !filepath.IsAbs(keyPath) {
		return nil, ErrKeyNotAbsolute
	}
	if !keyPrivacyChecked {
		return nil, ErrKeyPrivacyUnchecked
	}
	return readKey(filepath.Clean(keyPath), project)
}

// ReadKey reads a seed file only to show its public half, as jpack audit key
// generate and public do: held to every rule LoadSigner holds a key to that
// this platform can check, except being outside a project, and not refused
// where its privacy cannot be checked, since nothing is signed with it.
func ReadKey(keyPath string) (*Signer, error) {
	if !filepath.IsAbs(keyPath) {
		return nil, ErrKeyNotAbsolute
	}
	return readKey(filepath.Clean(keyPath), nil)
}

// readSeedFrom reads an opened key file: at most maxKeyFileBytes, one seed.
func readSeedFrom(file io.Reader) (*Signer, error) {
	data, err := io.ReadAll(io.LimitReader(file, maxKeyFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxKeyFileBytes {
		return nil, ErrKeyMalformed
	}
	seed, err := ParseSeed(data)
	if err != nil {
		return nil, err
	}
	return NewSignerFromSeed(seed)
}

// RecordMessage is what a record signature signs: the domain prefix, then the
// canonical form of {"record","sequence","trail"}. Every value is hexadecimal
// or the "sha256:" form and the sequence an integer under 2^53, so these bytes
// are that canonical form exactly.
func RecordMessage(trail string, sequence int64, record string) []byte {
	return []byte(recordSignaturePrefix + `{"record":"` + record + `","sequence":` + strconv.FormatInt(sequence, 10) + `,"trail":"` + trail + `"}`)
}

// RotationMessage is what a key rotation signs: the domain prefix, then the
// canonical form of {"at","next","trail"}.
func RotationMessage(trail string, at int64, next string) []byte {
	return []byte(keyRotationPrefix + `{"at":` + strconv.FormatInt(at, 10) + `,"next":"` + next + `","trail":"` + trail + `"}`)
}

// recordSignatureLine is a record signature's sidecar line, its members in
// code-point order.
type recordSignatureLine struct {
	KeyID          string `json:"keyId"`
	Kind           string `json:"kind"`
	Record         string `json:"record"`
	Sequence       int64  `json:"sequence"`
	SidecarVersion string `json:"sidecarVersion"`
	Signature      string `json:"signature"`
	Trail          string `json:"trail"`
}

// keyRotationLine is a key rotation's sidecar line, its members in code-point
// order.
type keyRotationLine struct {
	At             int64  `json:"at"`
	KeyID          string `json:"keyId"`
	Kind           string `json:"kind"`
	Next           string `json:"next"`
	SidecarVersion string `json:"sidecarVersion"`
	Signature      string `json:"signature"`
	Trail          string `json:"trail"`
}

// SignRecordLine is the sidecar line, newline included, that signs one record
// line given by its exact bytes without its newline. The line must be a
// chained record (chainedLine's rule), whose trail and sequence it binds.
func (s *Signer) SignRecordLine(line []byte) ([]byte, error) {
	found, _, ok := readLink(line)
	if !ok {
		return nil, errors.New("the line is not a chained record")
	}
	record := Digest(line)
	return canonicalLine(recordSignatureLine{
		KeyID:          s.KeyID(),
		Kind:           KindRecordSignature,
		Record:         record,
		Sequence:       found.sequence,
		SidecarVersion: SidecarVersion,
		Signature:      hex.EncodeToString(ed25519.Sign(s.private, RecordMessage(found.trail, found.sequence, record))),
		Trail:          found.trail,
	}), nil
}

// RotationLine is the sidecar line, newline included, by which s hands the
// trail's signing over to next after the trail's line at.
func (s *Signer) RotationLine(trail string, at int64, next ed25519.PublicKey) []byte {
	nextKey := hex.EncodeToString(next)
	return canonicalLine(keyRotationLine{
		At:             at,
		KeyID:          s.KeyID(),
		Kind:           KindKeyRotation,
		Next:           nextKey,
		SidecarVersion: SidecarVersion,
		Signature:      hex.EncodeToString(ed25519.Sign(s.private, RotationMessage(trail, at, nextKey))),
		Trail:          trail,
	})
}

// canonicalLine encodes a sidecar line. Its values need no escape, so the
// standard encoder writes the canonical form.
func canonicalLine(value any) []byte {
	encoded, _ := json.Marshal(value)
	return append(encoded, '\n')
}

// sidecarItem is one sidecar line as read: readable when it is one of the two
// kinds, of its shape exactly.
type sidecarItem struct {
	readable bool
	kind     string
	keyID    string
	trail    string
	// sequence is a record signature's, and at a rotation's.
	sequence  int64
	at        int64
	record    string
	next      ed25519.PublicKey
	signature []byte
}

// parseSidecarLine reads one sidecar line, without its newline. The rule is
// about the JSON value, as chainedLine's is: one JSON object naming exactly
// the seven members of its kind, each once and of its form. A line of another
// kind, another sidecarVersion or another shape is unreadable: it asserts
// nothing, so it signs nothing.
func parseSidecarLine(line []byte) sidecarItem {
	if len(line) == 0 || len(line) > maxSidecarLineBytes || !json.Valid(line) {
		return sidecarItem{}
	}
	members, err := exactObject(line)
	if err != nil || len(members) != 7 {
		return sidecarItem{}
	}
	var item sidecarItem
	var version, signature string
	if decodeString(members["sidecarVersion"], &version) != nil || version != SidecarVersion ||
		decodeString(members["kind"], &item.kind) != nil ||
		decodeString(members["keyId"], &item.keyID) != nil || !keyIDForm.MatchString(item.keyID) ||
		decodeString(members["trail"], &item.trail) != nil || !trailForm.MatchString(item.trail) ||
		decodeString(members["signature"], &signature) != nil || !signatureForm.MatchString(signature) {
		return sidecarItem{}
	}
	item.signature, _ = hex.DecodeString(signature)
	switch item.kind {
	case KindRecordSignature:
		if decodeInteger(members["sequence"], &item.sequence) != nil || item.sequence < 1 || item.sequence >= maxSafeInteger ||
			decodeString(members["record"], &item.record) != nil || !previousForm.MatchString(item.record) {
			return sidecarItem{}
		}
	case KindKeyRotation:
		var next string
		if decodeInteger(members["at"], &item.at) != nil || item.at < 1 || item.at >= maxSafeInteger ||
			decodeString(members["next"], &next) != nil || !publicKeyForm.MatchString(next) {
			return sidecarItem{}
		}
		item.next, _ = hex.DecodeString(next)
	default:
		return sidecarItem{}
	}
	item.readable = true
	return item
}

// SignWith gives the writer a key to sign its chained records with (ADR-0047
// §2b). nil, the zero value, signs nothing. A writer that does not chain signs
// nothing either: a signature binds a record's trail and sequence.
func (w *Writer) SignWith(signer *Signer) {
	if w == nil {
		return
	}
	w.signer = signer
}

// Signs says whether the writer will sign the chained records it appends.
func (w *Writer) Signs() bool {
	return w != nil && w.chain && w.signer != nil
}

// sidecarPath is the sidecar's name beneath the project's root.
func (w *Writer) sidecarPath() string { return path.Join(w.dir, SidecarName) }

// signThen is the step AppendLockedThen runs after the trail's lines were
// written and synced, still under the trail's lock: it signs every chained
// line among them. It signs nothing when no lock was taken, since the
// sidecar's order is the trail's only because one lock covers both.
func (w *Writer) signThen(appended *[]byte) func(bool) error {
	if !w.Signs() {
		return nil
	}
	return func(locked bool) error {
		if !locked {
			return nil
		}
		return w.signLines(*appended)
	}
}

// signLines appends a signature for every chained record line in appended,
// the exact bytes just appended to the trail, to the sidecar. Nothing is
// signed when the writer's key is not the key in force.
func (w *Writer) signLines(appended []byte) error {
	var lines []byte
	for _, line := range bytes.Split(bytes.TrimSuffix(appended, []byte("\n")), []byte("\n")) {
		if _, _, ok := readLink(line); !ok {
			continue
		}
		signed, err := w.signer.SignRecordLine(line)
		if err != nil {
			return err
		}
		lines = append(lines, signed...)
	}
	if len(lines) == 0 {
		return nil
	}
	return w.appendSidecar(lines)
}

// appendSidecar appends lines to the sidecar when the writer's key is the key
// in force there. A sidecar whose last line a failed write left without its
// newline is ended first, with tornLineEnd, so that line stays a line of its
// own and stays unreadable, whatever it holds, and the new ones stay whole.
func (w *Writer) appendSidecar(lines []byte) error {
	file, err := w.root.Open(w.sidecarPath())
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		inForce, torn, err := keyInForce(file, w.signer.public)
		file.Close()
		if err != nil {
			return err
		}
		if !inForce {
			return ErrKeyNotInForce
		}
		if torn {
			lines = append([]byte(tornLineEnd), lines...)
		}
	}
	return w.root.AppendSynced(w.sidecarPath(), lines)
}

// tornLineEnd ends a sidecar line a failed write left without its newline. A
// complete rotation or signature that lost only its newline was never
// written, as far as a verifier reads it, and ending it with a bare newline
// would make it readable: the byte before the newline is one no JSON text can
// end with, so the line can never be read as a sidecar line, and it stays what
// it was to a verifier, unreadable.
const tornLineEnd = "~\n"

// keyInForce says whether public is the key in force at the end of a sidecar,
// as a writer reads it, and whether the sidecar's last line is incomplete. The
// last readable line decides, among the lines a newline ends, as a verifier
// reads them, so a line a failed write left without its newline decides
// nothing, however whole its JSON: when there is none, any key is in force and
// becomes the first; when it is a rotation, only the key it names next; when
// it is a record signature, only the key that made it. So a rotated-away key
// stops signing the moment the rotation is written, and a key put in place
// without a rotation never starts. The writer does not verify the sidecar;
// jpack audit verify does.
func keyInForce(file *os.File, public ed25519.PublicKey) (bool, bool, error) {
	info, err := file.Stat()
	if err != nil {
		return false, false, err
	}
	size := info.Size()
	torn := false
	if size > 0 {
		var final [1]byte
		if err := readAt(file, final[:], size-1); err != nil {
			return false, false, err
		}
		torn = final[0] != '\n'
	}
	last, err := lastReadableSidecarLine(file, size)
	if err != nil {
		return false, false, err
	}
	switch {
	case !last.readable:
		return true, torn, nil
	case last.kind == KindKeyRotation:
		return bytes.Equal(last.next, public), torn, nil
	default:
		return last.keyID == KeyID(public), torn, nil
	}
}

// KeyInForce says whether signer is the key in force at the end of the
// project's sidecar, which is whether the writer would sign with it.
func (w *Writer) KeyInForce() (bool, error) {
	if !w.Signs() {
		return false, ErrNoSigningKey
	}
	file, err := w.root.Open(w.sidecarPath())
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	inForce, _, err := keyInForce(file, w.signer.public)
	return inForce, err
}

// lastReadableSidecarLine reads a sidecar back from its end, a chunk at a
// time, to its last readable line, and answers the zero item when it has none.
// Only lines a newline ends are read, as a verifier reads them: what follows
// the last newline is a write that did not complete, or nothing. A run of more
// than maxSidecarLineBytes without a newline is unreadable whatever it holds
// and is passed over without being kept.
func lastReadableSidecarLine(contents io.ReaderAt, size int64) (sidecarItem, error) {
	var pending []byte
	overlong := false
	// tail is true until the bytes after the last newline are passed over.
	tail := true
	chunk := make([]byte, readChunk)
	for cursor := size; ; {
		for {
			index := bytes.LastIndexByte(pending, '\n')
			if index < 0 {
				break
			}
			if !overlong && !tail {
				if item := parseSidecarLine(pending[index+1:]); item.readable {
					return item, nil
				}
			}
			overlong, tail, pending = false, false, pending[:index]
		}
		if cursor == 0 {
			if !overlong && !tail {
				if item := parseSidecarLine(pending); item.readable {
					return item, nil
				}
			}
			return sidecarItem{}, nil
		}
		if len(pending) > maxSidecarLineBytes {
			overlong, pending = true, nil
		}
		n := min(int64(len(chunk)), cursor)
		if err := readAt(contents, chunk[:n], cursor-n); err != nil {
			return sidecarItem{}, err
		}
		pending = append(append([]byte{}, chunk[:n]...), pending...)
		cursor -= n
	}
}

// RotationReport is what a rotation wrote: the trail line it follows, the
// trail, and the two keys by keyId, with the next key's public key.
type RotationReport struct {
	At            int64
	Trail         string
	From          string
	Next          string
	NextPublicKey string
}

// Rotate hands the trail's signing over from the writer's key to next
// (ADR-0047 §2b): under the trail's lock it appends to the sidecar a
// key-rotation line, made with the writer's key, naming next, at the trail's
// last line. The records after it are signed with next, and the writer's key,
// no longer in force, signs nothing more. It is refused when the writer holds
// no key or does not chain, when its key is not the key in force, when next is
// that key, where no lock can be taken, and before the trail has a chained
// record or while its last line is incomplete.
func (w *Writer) Rotate(next *Signer) (RotationReport, error) {
	if w == nil || w.signer == nil {
		return RotationReport{}, ErrNoSigningKey
	}
	if !w.chain {
		return RotationReport{}, ErrRepairUnchained
	}
	if next == nil || bytes.Equal(next.public, w.signer.public) {
		return RotationReport{}, ErrRotationSameKey
	}
	name := path.Join(w.dir, FileName)
	existing, err := w.root.Open(name)
	if err != nil {
		return RotationReport{}, errors.Join(ErrNoTrail, err)
	}
	existing.Close()
	var report RotationReport
	thenErr, err := w.root.AppendLockedThen(name, func(state fssecure.AppendState) ([]byte, error) {
		if !state.Locked {
			return nil, ErrRepairNeedsLock
		}
		trail, at, err := trailHead(state.Contents, state.Size)
		if err != nil {
			return nil, err
		}
		report = RotationReport{At: at, Trail: trail, From: w.signer.KeyID(), Next: next.KeyID(), NextPublicKey: next.PublicKey()}
		return nil, nil
	}, func(bool) error {
		return w.appendSidecar(w.signer.RotationLine(report.Trail, report.At, next.public))
	})
	if err != nil {
		return RotationReport{}, err
	}
	if thenErr != nil {
		return RotationReport{}, thenErr
	}
	return report, nil
}

// trailHead is the identity of a trail's last chained line and the line
// number of its last line, which a rotation follows. A trail with no chained
// line has no identity to bind a rotation to (ErrRotationNoTrail), and one
// whose last line is incomplete is refused as the writer refuses it.
func trailHead(contents io.ReaderAt, size int64) (string, int64, error) {
	if size == 0 {
		return "", 0, ErrRotationNoTrail
	}
	line, err := lastLine(contents, size)
	if err != nil {
		return "", 0, err
	}
	if trail, sequence, ok := chainedLine(line); ok {
		return trail, sequence, nil
	}
	_, lines, trail, err := readPrefix(contents, size)
	if err != nil {
		return "", 0, err
	}
	if trail == "" {
		return "", 0, ErrRotationNoTrail
	}
	return trail, lines, nil
}
