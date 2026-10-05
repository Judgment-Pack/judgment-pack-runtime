package audit

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// A checkpoint witness (gateway ADR-0013; the gateway's SPEC.md §8) is a
// gateway, run by a party other than the trail's operator, that signs this
// runtime's checkpoints and chains its statements per trail. This file reads
// what such a witness serves: each statement held to its form, its signature
// checked under a key the reader supplies, and the chain walked from its first
// statement, or from a continuation the reader's own earlier reading saved.
// Verify then holds the trail to the checkpoint of every statement that
// verified, as it holds it to a holder's; only a chain with no witness finding
// is credited. The runtime fetches nothing: the reader brings the statements,
// the head it fetched, and the keys it trusts.
//
// A statement is one JSON object of exactly eight members:
//
//	{"checkpoint":{…},"index":I,"keyId":"…","kind":"…","prevSignature":null|"…","signature":"…","witnessVersion":"1","witnessedAt":"…"}
//
// and its signature is Ed25519 over witnessPrefix followed by the canonical
// form of the statement without its signature member. Every value is ASCII
// hexadecimal, a fixed string or an integer below 2^53, so that canonical form
// is built here from the values read, as RecordMessage builds a record
// signature's.

const (
	// WitnessVersion is the statement format this runtime reads.
	WitnessVersion = "1"
	// ContinuationVersion is the shape of a continuation.
	ContinuationVersion = "1"
	// MaxWitnessKeys bounds the keys one verification takes.
	MaxWitnessKeys = 16
	// MaxWitnessBytes bounds the statements files, the head file and the
	// continuation of one verification, together.
	MaxWitnessBytes = 64 << 20
	// MaxWitnessStatements bounds the statement lines of one verification,
	// the continuation's two counted. A longer chain is read in steps.
	MaxWitnessStatements = 110000

	// witnessPrefix is the domain of what a statement signs: neither a
	// receipt's nor a seal's, so none of the three is replayed as another.
	witnessPrefix = "judgment-pack-gateway/witness/1:"
)

// witnessStatementBound is MaxWitnessStatements, which a test lowers to read
// a chain in steps without writing one of its length.
var witnessStatementBound = MaxWitnessStatements

// The kinds of statement.
const (
	WitnessKindCheckpoint = "checkpoint"
	WitnessKindConflict   = "conflict"
	WitnessKindRetirement = "retirement"
)

// The refusals of a reading, made before any statement is checked: no reading
// at all, never a reading of part. They are the gateway's names, and stable.
const (
	RefusalKeysOverBound       = "keys-over-bound"
	RefusalKeyNotCanonical     = "key-not-canonical"
	RefusalKeySmallOrder       = "key-small-order"
	RefusalKeyNotOnCurve       = "key-not-on-curve"
	RefusalBytesOverBound      = "bytes-over-bound"
	RefusalStatementsOverBound = "statements-over-bound"
	// RefusalStatementBeforeContinuation is a statements file holding a
	// statement at or below the continuation's last index: a reading that
	// continues reads only what follows, and nothing supplied is passed over.
	RefusalStatementBeforeContinuation = "statement-before-continuation"
)

// The findings of a reading. They are stable: a reader may branch on them.
const (
	// FindingWitnessMalformed is a statement line, a head file or a
	// continuation that is not of its form.
	FindingWitnessMalformed = "witness-malformed"
	// FindingWitnessSignatureInvalid is a statement whose keyId names no key
	// supplied, or whose signature does not verify under the key it names.
	FindingWitnessSignatureInvalid = "witness-signature-invalid"
	// FindingWitnessTrailMismatch is a statement of another trail than the
	// one being verified.
	FindingWitnessTrailMismatch = "witness-trail-mismatch"
	// FindingWitnessEquivocation is two statements that verify, of one
	// index, that differ: the witness signed two chains.
	FindingWitnessEquivocation = "witness-equivocation"
	// FindingWitnessChainBroken is a chain that does not begin at index 0,
	// or at the index after the continuation's last, or is not contiguous
	// and linked, or breaks a rule of its kinds.
	FindingWitnessChainBroken = "witness-chain-broken"
	// FindingWitnessHeadUnreached is a head more than one index past every
	// other statement supplied: the statements do not reach it.
	FindingWitnessHeadUnreached = "witness-head-unreached"
	// FindingWitnessHeadBehind is a head below the continuation's last
	// index: the witness serves an older head than a statement this reader's
	// own earlier reading checked, or the head supplied is stale.
	FindingWitnessHeadBehind = "witness-head-behind"
	// FindingCountersignedCoverageMissing is a countersigned coverage the
	// verification was told to require and the credited statements do not
	// give.
	FindingCountersignedCoverageMissing = "countersigned-coverage-missing"
)

var (
	// witnessedAtForm is the form of witnessedAt: a digit where each letter
	// but T and Z stands. It is held to that and nothing else: a reader
	// compares it with no clock and with no other statement's.
	witnessedAtForm  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)
	statementMembers = map[string]bool{
		"checkpoint": true, "index": true, "keyId": true, "kind": true,
		"prevSignature": true, "signature": true, "witnessVersion": true, "witnessedAt": true,
	}
	witnessKinds          = map[string]bool{WitnessKindCheckpoint: true, WitnessKindConflict: true, WitnessKindRetirement: true}
	checkpointMemberNames = map[string]bool{"checkpointVersion": true, "trail": true, "sequence": true, "recordDigest": true}
	continuationMembers   = map[string]bool{"continuationVersion": true, "last": true, "latestCheckpoint": true}
)

// WitnessRefusal is a reading refused before any statement is checked, with
// its reason, one of the Refusal names, and what was over its bound.
type WitnessRefusal struct {
	Reason string
	Detail string
}

func (r *WitnessRefusal) Error() string { return r.Reason + ": " + r.Detail }

// WitnessKeyRefusal is the refusal a witness key gets from the key rule, by
// the error CheckPublicKey or ParsePublicKey gave it, or "" for a key file
// that is not 64 hexadecimal characters at all.
func WitnessKeyRefusal(err error) string {
	switch {
	case errors.Is(err, ErrPublicKeyNotCanonical):
		return RefusalKeyNotCanonical
	case errors.Is(err, ErrPublicKeySmallOrder):
		return RefusalKeySmallOrder
	case errors.Is(err, ErrPublicKeyNotOnCurve):
		return RefusalKeyNotOnCurve
	}
	return ""
}

// witnessStatement is one statement that holds its form.
type witnessStatement struct {
	kind        string
	checkpoint  result.AuditCheckpoint
	index       int64
	prev        string // "" for null
	witnessedAt string
	keyID       string
	signature   string
	// whole is the SHA-256 of the statement's canonical bytes: two copies
	// with the same are one statement, however each was spelled.
	whole [sha256.Size]byte
	// from says where the statement was first read, for a finding.
	from string
}

// canonical is the statement's canonical form, with its signature member or,
// for what the signature covers, without it.
func (s *witnessStatement) canonical(withSignature bool) string {
	prev := "null"
	if s.prev != "" {
		prev = `"` + s.prev + `"`
	}
	signature := ""
	if withSignature {
		signature = `,"signature":"` + s.signature + `"`
	}
	return `{"checkpoint":` + string(bytes.TrimSuffix(EncodeCheckpoint(s.checkpoint), []byte("\n"))) +
		`,"index":` + strconv.FormatInt(s.index, 10) + `,"keyId":"` + s.keyID + `","kind":"` + s.kind +
		`","prevSignature":` + prev + signature + `,"witnessVersion":"` + WitnessVersion + `","witnessedAt":"` + s.witnessedAt + `"}`
}

// signed is what the statement's signature covers.
func (s *witnessStatement) signed() []byte {
	return []byte(witnessPrefix + s.canonical(false))
}

// parseWitnessStatement reads one statement by its JSON value and holds it to
// its form, or reports that it is malformed. Whitespace, the order of members
// and the escapes in a string are its spelling: a name given twice is refused,
// every form is held on the value decoded, and an integer is digits alone, so
// -0, 1.0 and 1e0 are not one. It is read a member at a time, and the first
// member the form does not allow ends the reading (streamedObject).
func parseWitnessStatement(raw []byte, from string) (*witnessStatement, bool) {
	decoder := streamDecoder(raw)
	statement, ok := readWitnessStatement(decoder, from)
	if !ok || !streamEnded(decoder) {
		return nil, false
	}
	return statement, true
}

// readWitnessStatement reads one statement object from a stream, as
// parseWitnessStatement does.
func readWitnessStatement(decoder *json.Decoder, from string) (*witnessStatement, bool) {
	var checkpoint map[string]json.RawMessage
	members, ok := streamedObject(decoder, statementMembers, func(name string) (json.RawMessage, bool) {
		if name != "checkpoint" {
			return streamedScalar(decoder)
		}
		var ok bool
		checkpoint, ok = streamedObject(decoder, checkpointMemberNames, func(string) (json.RawMessage, bool) {
			return streamedScalar(decoder)
		})
		return nil, ok
	})
	if !ok {
		return nil, false
	}
	statement := &witnessStatement{from: from}
	var version string
	if decodeString(members["witnessVersion"], &version) != nil || version != WitnessVersion ||
		decodeString(members["kind"], &statement.kind) != nil || !witnessKinds[statement.kind] ||
		decodeInteger(members["index"], &statement.index) != nil || statement.index >= maxSafeInteger ||
		decodeString(members["witnessedAt"], &statement.witnessedAt) != nil || !witnessedAtForm.MatchString(statement.witnessedAt) ||
		decodeString(members["keyId"], &statement.keyID) != nil || !keyIDForm.MatchString(statement.keyID) ||
		decodeString(members["signature"], &statement.signature) != nil || !signatureForm.MatchString(statement.signature) {
		return nil, false
	}
	if string(members["prevSignature"]) != "null" {
		if decodeString(members["prevSignature"], &statement.prev) != nil || !signatureForm.MatchString(statement.prev) {
			return nil, false
		}
	}
	var err error
	if statement.checkpoint, err = checkpointOfMembers(checkpoint); err != nil {
		return nil, false
	}
	statement.whole = sha256.Sum256([]byte(statement.canonical(true)))
	return statement, true
}

// parseContinuation reads a continuation: one JSON object of exactly
// continuationVersion ("1"), last and latestCheckpoint, each a statement of
// its form, read a member at a time as a statement is.
func parseContinuation(raw []byte) (last, latest *witnessStatement, ok bool) {
	decoder := streamDecoder(raw)
	members, ok := streamedObject(decoder, continuationMembers, func(name string) (json.RawMessage, bool) {
		var ok bool
		switch name {
		case "last":
			last, ok = readWitnessStatement(decoder, "the continuation's last statement")
		case "latestCheckpoint":
			latest, ok = readWitnessStatement(decoder, "the continuation's latest checkpoint statement")
		default:
			return streamedScalar(decoder)
		}
		return nil, ok
	})
	var version string
	if !ok || !streamEnded(decoder) || decodeString(members["continuationVersion"], &version) != nil || version != ContinuationVersion {
		return nil, nil, false
	}
	return last, latest, true
}

// IsContinuation says whether a document is a continuation in its form, by
// the rules a reading that resumes from it holds it to before anything is
// checked: what --witness-save may replace.
func IsContinuation(document []byte) bool {
	_, _, ok := parseContinuation(document)
	return ok
}

// streamDecoder reads one JSON text a token at a time, numbers kept as they
// are spelled.
func streamDecoder(raw []byte) *json.Decoder {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder
}

// streamEnded says whether nothing but JSON whitespace follows what was read.
func streamEnded(decoder *json.Decoder) bool {
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

// streamedObject reads one JSON object from a stream a member at a time, each
// value read by value, which answers it as JSON text (or nil, for a value it
// read in place) and whether it may stand. The reading ends at the first
// member whose name is not among names, whose name was given before, or whose
// value may not stand, before anything after it is read: an object of many
// members costs no more than the members its form allows. Every name must be
// given.
func streamedObject(decoder *json.Decoder, names map[string]bool, value func(name string) (json.RawMessage, bool)) (map[string]json.RawMessage, bool) {
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, false
	}
	members := make(map[string]json.RawMessage, len(names))
	for decoder.More() {
		token, err := decoder.Token()
		name, isName := token.(string)
		if err != nil || !isName || !names[name] {
			return nil, false
		}
		if _, given := members[name]; given {
			return nil, false
		}
		raw, ok := value(name)
		if !ok {
			return nil, false
		}
		members[name] = raw
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || len(members) != len(names) {
		return nil, false
	}
	return members, true
}

// streamedScalar reads one value that is a string, a number, true, false or
// null, as its JSON text, and refuses an object or an array without reading
// into it: no member of a statement holds one but its checkpoint.
func streamedScalar(decoder *json.Decoder) (json.RawMessage, bool) {
	token, err := decoder.Token()
	if err != nil {
		return nil, false
	}
	switch value := token.(type) {
	case string:
		encoded, err := json.Marshal(value)
		return encoded, err == nil
	case json.Number:
		return json.RawMessage(value), true
	case bool:
		if value {
			return json.RawMessage("true"), true
		}
		return json.RawMessage("false"), true
	case nil:
		return json.RawMessage("null"), true
	}
	return nil, false
}

// encodeContinuation is a continuation's canonical line, newline included:
// the last statement a reading read, of any kind, and the latest checkpoint
// statement at or before it, both as signed.
func encodeContinuation(last, latest *witnessStatement) []byte {
	return []byte(`{"continuationVersion":"` + ContinuationVersion + `","last":` + last.canonical(true) +
		`,"latestCheckpoint":` + latest.canonical(true) + "}\n")
}

// witnessLine is one statement line as split from a file: its bytes, the file
// it is in (from 1, in the order given; 0 for the head file) and its line
// number there.
type witnessLine struct {
	text []byte
	file int
	line int64
}

func (l witnessLine) from() string {
	if l.file == 0 {
		return "the head file"
	}
	return fmt.Sprintf("witness file %d, line %d", l.file, l.line)
}

// splitStatementLines appends a file's statement lines to into, counting each
// as it is split: its lines are its bytes up to each 0x0A, the piece after the
// last one included, less those that are empty or hold only spaces, tabs and
// carriage returns. At the line past limit it stops, keeps nothing more and
// answers false, so a file of many short lines costs no more than limit
// statements do.
func splitStatementLines(file []byte, number int, counted *int, limit int, into *[]witnessLine) bool {
	var line int64
	for len(file) > 0 {
		text := file
		if index := bytes.IndexByte(file, '\n'); index >= 0 {
			text, file = file[:index], file[index+1:]
		} else {
			file = nil
		}
		line++
		if len(bytes.Trim(text, " \t\r")) == 0 {
			continue
		}
		*counted++
		if *counted > limit {
			return false
		}
		*into = append(*into, witnessLine{text: text, file: number, line: line})
	}
	return true
}

// WitnessSupplied is what a reader supplies to read a witness's statements:
// the keys it trusts, each 32 bytes, in the order given; the bytes of each
// statements file, in order; the head it fetched from the witness, when it
// supplies one; and a continuation its own earlier reading saved, when it
// resumes from one.
type WitnessSupplied struct {
	Keys       [][]byte
	Statements [][]byte
	Head       []byte
	HasHead    bool
	Resume     []byte
	HasResume  bool
}

// WitnessInput is a reading prepared within its bounds: the keys passed the
// key rule, the lines are split and each held to its form, and nothing is yet
// checked.
type WitnessInput struct {
	keys      map[string]ed25519.PublicKey
	keyCount  int
	lines     []witnessLine
	headLines []witnessLine
	hasHead   bool
	// counted is the statement lines read, the continuation's two included.
	counted int
	// parsed is each distinct line's statement, by the SHA-256 of the line's
	// bytes, or nil for a line that is malformed: a line given again is the
	// statement it was without being read twice.
	parsed map[[sha256.Size]byte]*witnessStatement
	// The continuation: resumed says one was supplied, and last and latest
	// are its statements, nil when it is not of its shape.
	resumed      bool
	last, latest *witnessStatement
}

// PrepareWitness holds what was supplied to the bounds of one reading, in this
// order, and refuses it with a *WitnessRefusal at the first it is over: more
// than MaxWitnessKeys keys; a key the key rule refuses, the keys taken in
// their order; more than MaxWitnessBytes bytes in the files together; more
// than MaxWitnessStatements statement lines, the continuation's two counted
// first and the lines counted as the files are split, the statements files in
// their order and the head file last; and, with a continuation, a statements
// file holding a statement at or below its last index. No signature is
// checked before all of them pass.
func PrepareWitness(supplied WitnessSupplied) (*WitnessInput, error) {
	if len(supplied.Keys) > MaxWitnessKeys {
		return nil, &WitnessRefusal{Reason: RefusalKeysOverBound, Detail: fmt.Sprintf("%d witness keys were supplied, and one verification takes at most %d", len(supplied.Keys), MaxWitnessKeys)}
	}
	input := &WitnessInput{keys: map[string]ed25519.PublicKey{}, keyCount: len(supplied.Keys), parsed: map[[sha256.Size]byte]*witnessStatement{}}
	for index, key := range supplied.Keys {
		if err := CheckPublicKey(key); err != nil {
			reason := WitnessKeyRefusal(err)
			if reason == "" {
				reason = RefusalKeyNotOnCurve
			}
			return nil, &WitnessRefusal{Reason: reason, Detail: fmt.Sprintf("witness key %d %v", index+1, err)}
		}
		input.keys[KeyID(key)] = ed25519.PublicKey(key)
	}
	total := int64(len(supplied.Head)) + int64(len(supplied.Resume))
	for _, file := range supplied.Statements {
		total += int64(len(file))
	}
	if total > MaxWitnessBytes {
		return nil, &WitnessRefusal{Reason: RefusalBytesOverBound, Detail: fmt.Sprintf("the witness files hold %d bytes together, more than %d", total, MaxWitnessBytes)}
	}
	overStatements := &WitnessRefusal{Reason: RefusalStatementsOverBound, Detail: fmt.Sprintf("the witness files hold more than %d statements, the continuation's two counted; read a longer chain in steps, each continuing from what the step before saved", witnessStatementBound)}
	if supplied.HasResume {
		input.resumed = true
		input.counted = 2
	}
	for index, file := range supplied.Statements {
		if !splitStatementLines(file, index+1, &input.counted, witnessStatementBound, &input.lines) {
			return nil, overStatements
		}
	}
	if supplied.HasHead {
		input.hasHead = true
		if !splitStatementLines(supplied.Head, 0, &input.counted, witnessStatementBound, &input.headLines) {
			return nil, overStatements
		}
	}
	if input.resumed {
		input.last, input.latest, _ = parseContinuation(supplied.Resume)
	}
	for _, lines := range [][]witnessLine{input.lines, input.headLines} {
		for _, line := range lines {
			key := sha256.Sum256(line.text)
			if _, read := input.parsed[key]; read {
				continue
			}
			statement, ok := parseWitnessStatement(line.text, line.from())
			if !ok {
				statement = nil
			}
			input.parsed[key] = statement
		}
	}
	if input.last != nil {
		for _, line := range input.lines {
			if statement := input.parsed[sha256.Sum256(line.text)]; statement != nil && statement.index <= input.last.index {
				return nil, &WitnessRefusal{Reason: RefusalStatementBeforeContinuation, Detail: fmt.Sprintf("%s is the statement at index %d, at or below the continuation's last, index %d: a reading that continues is given only the statements after it", line.from(), statement.index, input.last.index)}
			}
		}
	}
	return input, nil
}

// checkpointSequences is the sequence of every statement read that holds its
// form and is a checkpoint statement: the lines of the trail a verification
// keeps, to hold them to these checkpoints once the trail is read.
func (in *WitnessInput) checkpointSequences() []int64 {
	sequences := []int64{}
	for _, statement := range in.parsed {
		if statement != nil && statement.kind == WitnessKindCheckpoint {
			sequences = append(sequences, statement.checkpoint.Sequence)
		}
	}
	if in.latest != nil {
		sequences = append(sequences, in.latest.checkpoint.Sequence)
	}
	if in.last != nil && in.last.kind == WitnessKindCheckpoint {
		sequences = append(sequences, in.last.checkpoint.Sequence)
	}
	return sequences
}

// witnessReading is what a reading found. With no finding, it is clean: it
// says how the chain read ends, and its checkpoint statements are credited.
type witnessReading struct {
	findings []result.AuditFinding
	// verified is every checkpoint statement that holds its form, verifies
	// under a key supplied and is of the trail being verified, in index
	// order: each one's checkpoint is held against the trail, credited or
	// not.
	verified []*witnessStatement
	clean    bool
	read     int
	checked  int
	// What a clean reading reports: whether a head was supplied, the head,
	// the last statement read and the latest checkpoint statement at or
	// before it, the conflict statements' sequences in index order, and
	// whether the chain is retired.
	head      *witnessStatement
	last      *witnessStatement
	latest    *witnessStatement
	conflicts []int64
	retired   bool
}

// read reads the chain against the identity of the trail being verified
// (SPEC.md §8.6, and gateway ADR-0013 §6 for a continuation):
//
//  1. The statements of every file, the head and the continuation's two are
//     one set, two with the same canonical bytes being one. Each is checked
//     once, against its form (witness-malformed, a head file of other than
//     one statement line too), its signature under the key its keyId names
//     among those supplied (witness-signature-invalid), and its trail
//     (witness-trail-mismatch), taking the first of these it fails and no
//     other. A statement that fails is never passed over.
//  2. Two that passed, of one index, that differ: witness-equivocation.
//  3. A set in which 1 or 2 found anything is not walked. Otherwise the chain
//     is the set less a head more than one index past every other statement,
//     and less, with a continuation, the statements at or below its last
//     index. Taken in index order it begins at index 0 with prevSignature
//     null, or after a continuation at the index after its last with that
//     statement's signature; every index follows the one before, each
//     prevSignature the signature before it; each checkpoint statement's
//     sequence exceeds the latest checkpoint statement's before it, the
//     continuation's latest the first of them; a conflict statement has a
//     checkpoint statement before it and a sequence at or below its; and a
//     retirement statement has one before it, repeats its checkpoint and is
//     last, so nothing follows a continuation whose last is one. The
//     continuation's latestCheckpoint is a checkpoint statement at or before
//     its last, and is the latest one: its last itself when that is a
//     checkpoint statement, at or above a conflict's sequence, the one a
//     retirement repeats. Otherwise witness-chain-broken.
//  4. A head left out of the chain is witness-head-unreached, and a head
//     below the continuation's last index witness-head-behind.
func (in *WitnessInput) read(trail string) *witnessReading {
	reading := &witnessReading{read: in.counted}
	finding := func(name, detail string) {
		reading.findings = append(reading.findings, result.AuditFinding{Name: name, Detail: detail})
	}
	seen := map[[sha256.Size]byte]*witnessStatement{}
	var set []*witnessStatement
	add := func(statement *witnessStatement) *witnessStatement {
		if earlier, ok := seen[statement.whole]; ok {
			return earlier
		}
		seen[statement.whole] = statement
		set = append(set, statement)
		return statement
	}
	reported := map[[sha256.Size]byte]bool{}
	addLine := func(line witnessLine) *witnessStatement {
		key := sha256.Sum256(line.text)
		statement := in.parsed[key]
		if statement == nil {
			if !reported[key] {
				reported[key] = true
				finding(FindingWitnessMalformed, line.from()+" is not a witness statement of version 1 in its form")
			}
			return nil
		}
		return add(statement)
	}
	var last, latest *witnessStatement
	if in.resumed {
		if in.last == nil {
			finding(FindingWitnessMalformed, "the continuation is not one of version 1 in its form, holding a last statement and a latest checkpoint statement")
		} else {
			last, latest = add(in.last), add(in.latest)
		}
	}
	for _, line := range in.lines {
		addLine(line)
	}
	var head *witnessStatement
	if in.hasHead {
		if len(in.headLines) != 1 {
			finding(FindingWitnessMalformed, fmt.Sprintf("the head file holds %d statement lines, and a head is one", len(in.headLines)))
		} else {
			head = addLine(in.headLines[0])
		}
	}
	reading.checked = len(set)

	passing := []*witnessStatement{}
	for _, statement := range set {
		key, known := in.keys[statement.keyID]
		switch {
		case !known:
			finding(FindingWitnessSignatureInvalid, statement.from+": its keyId names no witness key supplied")
		case !ed25519.Verify(key, statement.signed(), statementSignature(statement.signature)):
			finding(FindingWitnessSignatureInvalid, statement.from+": its signature does not verify under the witness key its keyId names")
		case statement.checkpoint.Trail != trail:
			finding(FindingWitnessTrailMismatch, statement.from+": it is a statement for another trail than the one being verified")
		default:
			passing = append(passing, statement)
		}
	}
	sort.SliceStable(passing, func(i, j int) bool { return passing[i].index < passing[j].index })
	for _, statement := range passing {
		if statement.kind == WitnessKindCheckpoint {
			reading.verified = append(reading.verified, statement)
		}
	}
	for index := 1; index < len(passing); index++ {
		if passing[index].index == passing[index-1].index {
			finding(FindingWitnessEquivocation, fmt.Sprintf("%s and %s are two statements at index %d that differ, both verifying: the witness signed two chains", passing[index-1].from, passing[index].from, passing[index].index))
		}
	}
	if len(reading.findings) > 0 {
		return reading
	}

	start, previous := int64(0), ""
	broken := ""
	if last != nil {
		switch {
		case latest.kind != WitnessKindCheckpoint || latest.index > last.index:
			broken = "the continuation's latest checkpoint statement is not a checkpoint statement at or before its last"
		case last.kind == WitnessKindCheckpoint && latest != last:
			broken = "the continuation's last statement is a checkpoint statement, and its latest checkpoint statement is another"
		case last.kind == WitnessKindConflict && last.checkpoint.Sequence > latest.checkpoint.Sequence:
			broken = "the continuation's last statement is a conflict above its latest checkpoint statement's sequence"
		case last.kind == WitnessKindRetirement && last.checkpoint != latest.checkpoint:
			broken = "the continuation's last statement is a retirement that does not repeat its latest checkpoint statement's checkpoint"
		}
		start, previous = last.index+1, last.signature
	}
	behind := last != nil && head != nil && head.index < last.index
	highestOther := start - 1
	for _, statement := range passing {
		if statement != head && statement.index > highestOther {
			highestOther = statement.index
		}
	}
	unreached := head != nil && !behind && head.index > highestOther+1
	chain := []*witnessStatement{}
	for _, statement := range passing {
		if statement.index >= start && !(unreached && statement == head) {
			chain = append(chain, statement)
		}
	}
	switch {
	case broken != "":
	case last == nil && len(chain) == 0:
		broken = "no statement begins the chain at index 0: a chain is read from its first statement"
	case last != nil && last.kind == WitnessKindRetirement && len(chain) > 0:
		broken = fmt.Sprintf("%s follows the continuation's last statement, a retirement, which is the last of its chain", chain[0].from)
	}
	conflicts := []int64{}
	for position, statement := range chain {
		if broken != "" {
			break
		}
		switch {
		case statement.index != start:
			if last == nil && position == 0 {
				broken = fmt.Sprintf("the statements begin at index %d, and a chain is read from its first statement, index 0", statement.index)
			} else {
				broken = fmt.Sprintf("no statement at index %d was supplied, and %s is at index %d", start, statement.from, statement.index)
			}
		case statement.prev != previous:
			broken = fmt.Sprintf("%s: its prevSignature is not the signature of the statement before it", statement.from)
			if previous == "" {
				broken = fmt.Sprintf("%s: it is at index 0 and names a previous signature", statement.from)
			}
		case statement.kind == WitnessKindCheckpoint && latest != nil && statement.checkpoint.Sequence <= latest.checkpoint.Sequence:
			broken = fmt.Sprintf("%s: its sequence, %d, does not exceed the latest checkpoint statement's, %d", statement.from, statement.checkpoint.Sequence, latest.checkpoint.Sequence)
		case statement.kind != WitnessKindCheckpoint && latest == nil:
			broken = fmt.Sprintf("%s: it is a %s with no checkpoint statement before it", statement.from, statement.kind)
		case statement.kind == WitnessKindConflict && statement.checkpoint.Sequence > latest.checkpoint.Sequence:
			broken = fmt.Sprintf("%s: a conflict at sequence %d, above the latest checkpoint statement's, %d", statement.from, statement.checkpoint.Sequence, latest.checkpoint.Sequence)
		case statement.kind == WitnessKindRetirement && statement.checkpoint != latest.checkpoint:
			broken = fmt.Sprintf("%s: a retirement that does not repeat the latest checkpoint statement's checkpoint", statement.from)
		case statement.kind == WitnessKindRetirement && position != len(chain)-1:
			broken = fmt.Sprintf("%s: a retirement that is not the last of its chain", statement.from)
		}
		switch statement.kind {
		case WitnessKindCheckpoint:
			latest = statement
		case WitnessKindConflict:
			conflicts = append(conflicts, statement.checkpoint.Sequence)
		}
		start, previous = statement.index+1, statement.signature
	}
	if broken != "" {
		finding(FindingWitnessChainBroken, broken)
	}
	if unreached {
		finding(FindingWitnessHeadUnreached, fmt.Sprintf("the head is at index %d, more than one past every other statement supplied: they do not reach it", head.index))
	}
	if behind {
		finding(FindingWitnessHeadBehind, fmt.Sprintf("the head is at index %d, below the continuation's last, index %d: the witness serves an older head than a statement this reader's own earlier reading checked, or the head supplied is stale", head.index, last.index))
	}
	if len(reading.findings) > 0 {
		return reading
	}
	if len(chain) > 0 {
		last = chain[len(chain)-1]
	}
	reading.clean = true
	reading.head, reading.last, reading.latest, reading.conflicts = head, last, latest, conflicts
	reading.retired = last != nil && last.kind == WitnessKindRetirement
	return reading
}

// statementSignature decodes a signature a statement's form has already held.
func statementSignature(text string) []byte {
	decoded, _ := hex.DecodeString(text)
	return decoded
}
