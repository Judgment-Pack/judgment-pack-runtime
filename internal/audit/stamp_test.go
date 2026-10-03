package audit

import (
	"bytes"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp/tsatest"
)

func testAuthority(t *testing.T, options tsatest.Options) *tsatest.Authority {
	t.Helper()
	tsa, err := tsatest.New(options)
	if err != nil {
		t.Fatal(err)
	}
	return tsa
}

// stampOf is a stamps line keeping a token of checkpoint from tsa, made at
// the authority's now.
func stampOf(t *testing.T, tsa *tsatest.Authority, checkpoint result.AuditCheckpoint) []byte {
	t.Helper()
	token, err := tsa.Token(CheckpointDigest(checkpoint), big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	return encodeStampLine(checkpoint, token)
}

// verifyStamped verifies a trail held in memory with its stamps under the
// roots of the authorities given.
func verifyStamped(t *testing.T, trail, stamps []byte, options StampOptions, roots ...*tsatest.Authority) result.AuditChain {
	t.Helper()
	pool := x509.NewCertPool()
	for _, tsa := range roots {
		pool.AddCert(tsa.Root)
	}
	options.Verify.Roots = pool
	options.Stamps, options.StampsSize = bytes.NewReader(stamps), int64(len(stamps))
	report, err := Verify(bytes.NewReader(trail), int64(len(trail)), Options{Stamps: &options})
	if err != nil {
		t.Fatal(err)
	}
	return report.Chain
}

func checkpointAt(t *testing.T, trail []byte, sequence int64) result.AuditCheckpoint {
	t.Helper()
	kept := keptCheckpoints(t, trail, sequence)
	if len(kept) != 1 {
		t.Fatalf("no checkpoint at %d", sequence)
	}
	return kept[0]
}

// A trusted stamp of the trail's checkpoint covers every line up to it: the
// coverage is real, with the time the records existed by, the lag from their
// at, and the fixed sentences of what a stamp does and does not establish.
func TestATrustedStampCoversTheLinesUpToItsCheckpoint(t *testing.T) {
	_, _, trail := chainedTrail(t, 3)
	tsa := testAuthority(t, tsatest.Options{AccuracySeconds: 1})
	stamps := stampOf(t, tsa, checkpointAt(t, trail, 3))
	chain := verifyStamped(t, trail, stamps, StampOptions{}, tsa)
	if chain.Status != "valid" || chain.Coverage.Stamped != (result.AuditCoverageState{Status: "through", Through: 3}) ||
		chain.Stamps.Trusted != 1 || chain.Stamps.RevocationNotChecked != 1 || chain.Stamps.Lines != 1 || chain.Stamps.Lag.Records != 3 || chain.Stamps.CoveredBy == "" {
		t.Fatalf("verification: %v %+v %+v %+v", findingNames(chain), chain.Coverage, chain.Stamps, chain.Stamps.Lag)
	}
	coveredBy, err := time.Parse(time.RFC3339Nano, chain.Stamps.CoveredBy)
	if err != nil || coveredBy.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("covered by %q: %v", chain.Stamps.CoveredBy, err)
	}
	if !containsString(chain.Establishes, fmt.Sprintf(establishesStamped, 3, chain.Stamps.CoveredBy)) ||
		!containsString(chain.DoesNotEstablish, fmt.Sprintf(notStampedAfter, 3)) || !containsString(chain.DoesNotEstablish, notBeforeStamp) ||
		!containsString(chain.DoesNotEstablish, notAgainstAuthority) || !containsString(chain.DoesNotEstablish, fmt.Sprintf(notRevocationChecked, 1)) ||
		containsString(chain.DoesNotEstablish, notStampedUnchecked) {
		t.Fatalf("statements: %q %q", chain.Establishes, chain.DoesNotEstablish)
	}
	// Without roots no stamp is checked, and the report says so.
	unchecked := verifyBytes(t, trail, nil)
	if unchecked.Coverage.Stamped.Status != "not-checked" || unchecked.Stamps != nil || !containsString(unchecked.DoesNotEstablish, notStampedUnchecked) {
		t.Fatalf("unchecked: %+v %q", unchecked.Coverage, unchecked.DoesNotEstablish)
	}
	// A stamp of an earlier checkpoint covers the lines up to it only.
	chain = verifyStamped(t, trail, stampOf(t, tsa, checkpointAt(t, trail, 2)), StampOptions{RequireThrough: 3}, tsa)
	if chain.Coverage.Stamped.Through != 2 || !slices.Equal(findingNames(chain), []string{"stamp-coverage-missing@3"}) || chain.RequiredStamped.Status != "unmet" {
		t.Fatalf("an earlier checkpoint: %v %+v", findingNames(chain), chain.Coverage)
	}
	chain = verifyStamped(t, trail, stamps, StampOptions{RequireThrough: 3}, tsa)
	if chain.Status != "valid" || chain.RequiredStamped.Status != "met" {
		t.Fatalf("required through 3: %v %+v", findingNames(chain), chain.RequiredStamped)
	}
}

// Every check of a token is a named finding, and a stamp that fails one
// covers nothing: a token over another digest, with its signature changed,
// under another root, from a certificate not for time-stamping alone, under
// another policy, from a certificate expired at the stamp's time, or revoked
// as of it.
func TestEveryCheckOfATokenIsAFinding(t *testing.T) {
	_, _, trail := chainedTrail(t, 2)
	checkpoint := checkpointAt(t, trail, 2)
	tsa := testAuthority(t, tsatest.Options{})
	now := time.Now()
	expired := testAuthority(t, tsatest.Options{NotBefore: now.Add(-2 * 365 * 24 * time.Hour), NotAfter: now.Add(-365 * 24 * time.Hour)})
	wrongUsage := testAuthority(t, tsatest.Options{Usages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	otherDigest, err := tsa.Token(CheckpointDigest(checkpointAt(t, trail, 1)), nil)
	if err != nil {
		t.Fatal(err)
	}
	good := stampOf(t, tsa, checkpoint)
	tampered := tamperToken(t, good)
	revokedList, err := tsa.CRL(now.Add(time.Hour), now.Add(-time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		stamps  []byte
		options StampOptions
		roots   []*tsatest.Authority
		want    string
	}{
		{"another digest", encodeStampLine(checkpoint, otherDigest), StampOptions{}, []*tsatest.Authority{tsa}, "stamp-imprint-mismatch@2"},
		{"a changed signature", tampered, StampOptions{}, []*tsatest.Authority{tsa}, "stamp-signature-invalid@2"},
		{"another root", good, StampOptions{}, []*tsatest.Authority{testAuthority(t, tsatest.Options{})}, "stamp-untrusted@2"},
		{"a usage not time-stamping", stampOf(t, wrongUsage, checkpoint), StampOptions{}, []*tsatest.Authority{wrongUsage}, "stamp-usage-invalid@2"},
		{"another policy", good, StampOptions{Verify: timestamp.VerifyOptions{Policies: []asn1.ObjectIdentifier{{1, 2, 3}}}}, []*tsatest.Authority{tsa}, "stamp-policy-mismatch@2"},
		{"expired at the stamp's time", stampOf(t, expired, checkpoint), StampOptions{}, []*tsatest.Authority{expired}, "stamp-untrusted@2"},
		{"revoked as of the stamp's time", good, StampOptions{Verify: timestamp.VerifyOptions{CRLs: []*x509.RevocationList{parseList(t, revokedList)}}}, []*tsatest.Authority{tsa}, "stamp-revoked@2"},
		{"not a token", encodeStampLine(checkpoint, []byte{0x30, 0x03, 0x02, 0x01, 0x01}), StampOptions{}, []*tsatest.Authority{tsa}, "stamp-malformed@2"},
	}
	for _, c := range cases {
		chain := verifyStamped(t, trail, c.stamps, c.options, c.roots...)
		if !slices.Equal(findingNames(chain), []string{c.want}) || chain.Coverage.Stamped.Status != "none" || chain.Stamps.Trusted != 0 ||
			!containsString(chain.DoesNotEstablish, notStampedNone) {
			t.Fatalf("%s: %v %+v", c.name, findingNames(chain), chain.Coverage)
		}
	}
	// A clean list issued after the stamp checks the status.
	cleanList, err := tsa.CRL(now.Add(time.Hour), time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	chain := verifyStamped(t, trail, good, StampOptions{Verify: timestamp.VerifyOptions{CRLs: []*x509.RevocationList{parseList(t, cleanList)}}}, tsa)
	if chain.Status != "valid" || chain.Stamps.RevocationChecked != 1 || chain.Stamps.RevocationNotChecked != 0 ||
		slices.ContainsFunc(chain.DoesNotEstablish, func(s string) bool { return strings.Contains(s, "no revocation list supplied") }) {
		t.Fatalf("a clean list: %v %+v %q", findingNames(chain), chain.Stamps, chain.DoesNotEstablish)
	}
}

// tamperToken changes the last byte of a stamps line's token, the last byte
// of its signature, keeping the line's shape.
func tamperToken(t *testing.T, line []byte) []byte {
	t.Helper()
	parsed := parseStampLine(bytes.TrimSuffix(line, []byte("\n")))
	if !parsed.readable {
		t.Fatal("not a stamps line")
	}
	token := bytes.Clone(parsed.token)
	token[len(token)-1] ^= 1
	return encodeStampLine(parsed.checkpoint, token)
}

func parseList(t *testing.T, encoded []byte) *x509.RevocationList {
	t.Helper()
	block, _ := pem.Decode(encoded)
	list, err := x509.ParseRevocationList(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// A trusted stamp the trail no longer matches shows the trail was rewritten
// since: the last line edited, which the chain alone cannot see, or the
// trail cut short before the stamped record.
func TestATrustedStampShowsATrailRewrittenSince(t *testing.T) {
	_, _, trail := chainedTrail(t, 3)
	tsa := testAuthority(t, tsatest.Options{})
	stamps := stampOf(t, tsa, checkpointAt(t, trail, 3))
	lines := splitLines(trail)
	lines[2] = bytes.Replace(lines[2], []byte(`"n":2`), []byte(`"n":8`), 1)
	edited := joinLines(lines)
	if plain := verifyBytes(t, edited, nil); plain.Status != "valid" {
		t.Fatalf("the chain alone does not see the last line edited: %v", findingNames(plain))
	}
	chain := verifyStamped(t, edited, stamps, StampOptions{}, tsa)
	if !slices.Equal(findingNames(chain), []string{"stamp-checkpoint-mismatch@3"}) || chain.Coverage.Stamped.Status != "none" {
		t.Fatalf("an edited last line: %v %+v", findingNames(chain), chain.Coverage)
	}
	chain = verifyStamped(t, joinLines(splitLines(trail)[:2]), stamps, StampOptions{}, tsa)
	if !slices.Equal(findingNames(chain), []string{"stamp-checkpoint-mismatch@3"}) {
		t.Fatalf("a trail cut short: %v", findingNames(chain))
	}
	// A stamp of another trail's checkpoint names a record of another trail.
	_, _, other := chainedTrail(t, 3)
	chain = verifyStamped(t, trail, stampOf(t, tsa, checkpointAt(t, other, 3)), StampOptions{}, tsa)
	if !slices.Equal(findingNames(chain), []string{"stamp-checkpoint-mismatch@3"}) {
		t.Fatalf("another trail: %v", findingNames(chain))
	}
	// A failed check of the trail before a stamp's checkpoint voids its
	// coverage, as it voids a checkpoint's.
	lines = splitLines(trail)
	lines[0] = bytes.Replace(lines[0], []byte(`"n":0`), []byte(`"n":9`), 1)
	chain = verifyStamped(t, joinLines(lines), stampOf(t, tsa, checkpointAt(t, trail, 3)), StampOptions{}, tsa)
	if chain.Coverage.Stamped.Status != "none" || !slices.Contains(findingNames(chain), "previous-mismatch@2") {
		t.Fatalf("a break before the stamp: %v %+v", findingNames(chain), chain.Coverage)
	}
	// A stamp that fails its own checks voids neither the trail's checks nor
	// a held checkpoint's coverage.
	options := StampOptions{}
	pool := x509.NewCertPool()
	pool.AddCert(testAuthority(t, tsatest.Options{}).Root)
	options.Verify.Roots = pool
	options.Stamps, options.StampsSize = bytes.NewReader(stamps), int64(len(stamps))
	report := verifyWith(t, trail, Options{Held: keptCheckpoints(t, trail, 3), Stamps: &options})
	if !slices.Equal(findingNames(report.Chain), []string{"stamp-untrusted@3"}) || report.Chain.Coverage.Checkpointed.Through != 3 {
		t.Fatalf("an untrusted stamp and a held checkpoint: %v %+v", findingNames(report.Chain), report.Chain.Coverage)
	}
}

// The lag is from each covered record's at to the earliest time a trusted
// stamp at or after it attests: the first stamp covering it, which may be a
// later checkpoint's earlier stamp. A record whose at is after that time is
// reported, since at is the operator's word.
func TestTheLagIsFromAtToTheFirstStampCoveringTheRecord(t *testing.T) {
	_, _, trail := chainedTrail(t, 4)
	ats := []time.Time{}
	for _, line := range splitLines(trail) {
		members, err := exactObject(line)
		if err != nil {
			t.Fatal(err)
		}
		var at string
		if decodeString(members["at"], &at) != nil {
			t.Fatal("a record has an at")
		}
		parsed, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			t.Fatal(err)
		}
		ats = append(ats, parsed)
	}
	tsa := testAuthority(t, tsatest.Options{})
	stampAt := func(sequence int64, when time.Time) []byte {
		tsa.Now = func() time.Time { return when }
		return stampOf(t, tsa, checkpointAt(t, trail, sequence))
	}
	// The test authority states whole seconds, so the stamps' times are
	// whole seconds here too.
	base := ats[3].Truncate(time.Second)
	stamps := slices.Concat(stampAt(2, base.Add(time.Hour)), stampAt(4, base.Add(2*time.Hour)))
	chain := verifyStamped(t, trail, stamps, StampOptions{}, tsa)
	lag := chain.Stamps.Lag
	near := func(got float64, want time.Duration) bool {
		return got > want.Seconds()-0.01 && got < want.Seconds()+0.01
	}
	if chain.Status != "valid" || lag.Records != 4 || lag.MaxSequence != 3 || !near(lag.MaxSeconds, base.Add(2*time.Hour).Sub(ats[2])) ||
		lag.MinSequence != 2 || !near(lag.MinSeconds, base.Add(time.Hour).Sub(ats[1])) || lag.AtAfterStamp || chain.Coverage.Stamped.Through != 4 {
		t.Fatalf("two stamps: %+v %+v", lag, chain.Coverage)
	}
	// The later checkpoint stamped earlier covers the records before it
	// first.
	stamps = slices.Concat(stampAt(2, base.Add(3*time.Hour)), stampAt(4, base.Add(time.Hour)))
	lag = verifyStamped(t, trail, stamps, StampOptions{}, tsa).Stamps.Lag
	if lag.MaxSequence != 1 || !near(lag.MaxSeconds, base.Add(time.Hour).Sub(ats[0])) ||
		lag.MinSequence != 4 || !near(lag.MinSeconds, base.Add(time.Hour).Sub(ats[3])) {
		t.Fatalf("a later checkpoint stamped earlier: %+v", lag)
	}
	// A stamp before the records' at.
	lag = verifyStamped(t, trail, stampAt(4, ats[0].Add(-time.Hour)), StampOptions{}, tsa).Stamps.Lag
	if !lag.AtAfterStamp || lag.MinSeconds >= 0 {
		t.Fatalf("at after the stamp: %+v", lag)
	}
}

// Keeping a stamp is idempotent by the checkpoint's digest: a second token
// for the same checkpoint is not kept, a line a failed write left incomplete
// is ended so it stays unreadable, and lines of no shape are counted
// unreadable, not failed.
func TestKeepingAStampIsIdempotentByTheCheckpoint(t *testing.T) {
	root, dir, trail := chainedTrail(t, 2)
	writer := NewWriter(root, "audit", true)
	tsa := testAuthority(t, tsatest.Options{})
	checkpoint := checkpointAt(t, trail, 2)
	if stamped, err := writer.Stamped(checkpoint); err != nil || stamped {
		t.Fatalf("nothing stamped yet: %v %v", stamped, err)
	}
	token, err := tsa.Token(CheckpointDigest(checkpoint), nil)
	if err != nil {
		t.Fatal(err)
	}
	if appended, err := writer.RecordStamp(checkpoint, token); err != nil || !appended {
		t.Fatalf("first: %v %v", appended, err)
	}
	again, err := tsa.Token(CheckpointDigest(checkpoint), nil)
	if err != nil {
		t.Fatal(err)
	}
	if appended, err := writer.RecordStamp(checkpoint, again); err != nil || appended {
		t.Fatalf("a retry: %v %v", appended, err)
	}
	if stamped, err := writer.Stamped(checkpoint); err != nil || !stamped {
		t.Fatalf("stamped: %v %v", stamped, err)
	}
	stampsPath := filepath.Join(dir, "audit", StampsName)
	data, err := os.ReadFile(stampsPath)
	if err != nil || len(splitLines(data)) != 1 {
		t.Fatalf("one line: %q %v", data, err)
	}
	// A line for the checkpoint whose token does not hold is not a stamp.
	if err := os.WriteFile(stampsPath, tamperToken(t, data), 0o600); err != nil {
		t.Fatal(err)
	}
	if stamped, err := writer.Stamped(checkpoint); err != nil || stamped {
		t.Fatalf("a token that does not hold: %v %v", stamped, err)
	}
	// A torn last line, ended before the next.
	torn := append(bytes.Clone(data[:len(data)-1]), []byte("")...)
	if err := os.WriteFile(stampsPath, torn, 0o600); err != nil {
		t.Fatal(err)
	}
	if appended, err := writer.RecordStamp(checkpoint, token); err != nil || !appended {
		t.Fatalf("after a torn line: %v %v", appended, err)
	}
	data, err = os.ReadFile(stampsPath)
	if err != nil {
		t.Fatal(err)
	}
	if lines := splitLines(data); len(lines) != 2 || !bytes.HasSuffix(lines[0], []byte("~")) {
		t.Fatalf("the torn line is ended and kept: %q", data)
	}
	chain := verifyStamped(t, trail, append(data, []byte("not a stamp\n")...), StampOptions{}, tsa)
	if chain.Status != "valid" || chain.Stamps.Lines != 3 || chain.Stamps.Unreadable != 2 || chain.Stamps.Trusted != 1 {
		t.Fatalf("verification: %v %+v", findingNames(chain), chain.Stamps)
	}
	if appended, err := NewWriter(root, "audit", true).RecordStamp(checkpoint, token); err != nil || appended {
		t.Fatalf("a stamp kept after the torn line is held: %v %v", appended, err)
	}
}

// A stamps line is read by its exact shape.
func TestAStampsLineOfAnotherShapeIsUnreadable(t *testing.T) {
	_, _, trail := chainedTrail(t, 1)
	tsa := testAuthority(t, tsatest.Options{})
	line := bytes.TrimSuffix(stampOf(t, tsa, checkpointAt(t, trail, 1)), []byte("\n"))
	if !parseStampLine(line).readable {
		t.Fatal("the writer's line is readable")
	}
	for name, edit := range map[string][2]string{
		"version":    {`"stampVersion":"1"`, `"stampVersion":"2"`},
		"extra":      {`"stampVersion":`, `"x":1,"stampVersion":`},
		"checkpoint": {`"checkpointVersion":"1"`, `"checkpointVersion":"2"`},
		"base64":     {`"token":"`, `"token":"*`},
		"missing":    {`,"stampVersion":"1"`, ``},
	} {
		edited := bytes.Replace(line, []byte(edit[0]), []byte(edit[1]), 1)
		if bytes.Equal(edited, line) || parseStampLine(edited).readable {
			t.Fatalf("%s: %s is unreadable", name, edited)
		}
	}
	if parseStampLine(append(bytes.Repeat([]byte(" "), MaxStampLineBytes), line...)).readable {
		t.Fatal("a line past the bound is unreadable")
	}
}

// An imprint taken with another hash is not the checkpoint's digest, whatever
// its bytes.
func TestAnImprintOfAnotherHashIsNotTheCheckpoints(t *testing.T) {
	_, _, trail := chainedTrail(t, 1)
	tsa := testAuthority(t, tsatest.Options{ImprintSHA384: true})
	chain := verifyStamped(t, trail, stampOf(t, tsa, checkpointAt(t, trail, 1)), StampOptions{}, tsa)
	if !slices.Equal(findingNames(chain), []string{"stamp-imprint-mismatch@1"}) {
		t.Fatalf("a SHA-384 imprint: %v", findingNames(chain))
	}
}

// A trusted stamp of another record at a sequence is a failed check of the
// trail: it voids a held checkpoint's coverage after it, as a break would.
func TestATrustedStampMismatchVoidsCoverageAfterIt(t *testing.T) {
	_, _, trail := chainedTrail(t, 3)
	_, _, other := chainedTrail(t, 3)
	tsa := testAuthority(t, tsatest.Options{})
	stamps := stampOf(t, tsa, checkpointAt(t, other, 2))
	options := StampOptions{}
	pool := x509.NewCertPool()
	pool.AddCert(tsa.Root)
	options.Verify.Roots = pool
	options.Stamps, options.StampsSize = bytes.NewReader(stamps), int64(len(stamps))
	chain := verifyWith(t, trail, Options{Held: keptCheckpoints(t, trail, 3), Stamps: &options}).Chain
	if !slices.Equal(findingNames(chain), []string{"stamp-checkpoint-mismatch@2"}) || chain.Coverage.Checkpointed.Status != "failed" {
		t.Fatalf("a mismatch before a held checkpoint: %v %+v", findingNames(chain), chain.Coverage)
	}
}

// A covered record whose at cannot be read is counted, not given a lag.
func TestARecordWhoseAtCannotBeReadIsCounted(t *testing.T) {
	_, _, trail := chainedTrail(t, 2)
	lines := splitLines(trail)
	members, err := exactObject(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	var at string
	if decodeString(members["at"], &at) != nil {
		t.Fatal("a record has an at")
	}
	lines[1] = bytes.Replace(lines[1], []byte(`"at":"`+at+`"`), []byte(`"at":"yesterday"`), 1)
	edited := joinLines(lines)
	tsa := testAuthority(t, tsatest.Options{})
	chain := verifyStamped(t, edited, stampOf(t, tsa, checkpointAt(t, edited, 2)), StampOptions{}, tsa)
	if chain.Status != "valid" || chain.Stamps.Lag.AtUnreadable != 1 || chain.Stamps.Lag.Records != 1 {
		t.Fatalf("an unreadable at: %v %+v", findingNames(chain), chain.Stamps.Lag)
	}
}

// A checkpoint is held whole: one whose record digest is the line's but whose
// trail is another's does not match, and without roots no stamp is read at
// all.
func TestAStampsCheckpointIsHeldWholeAndOnlyWithRoots(t *testing.T) {
	_, _, trail := chainedTrail(t, 2)
	tsa := testAuthority(t, tsatest.Options{})
	checkpoint := checkpointAt(t, trail, 2)
	checkpoint.Trail = strings.Repeat("ab", 16)
	chain := verifyStamped(t, trail, stampOf(t, tsa, checkpoint), StampOptions{}, tsa)
	if !slices.Equal(findingNames(chain), []string{"stamp-checkpoint-mismatch@2"}) {
		t.Fatalf("another trail named over this record: %v", findingNames(chain))
	}
	stamps := stampOf(t, tsa, checkpointAt(t, trail, 2))
	options := StampOptions{Stamps: bytes.NewReader(stamps), StampsSize: int64(len(stamps))}
	report := verifyWith(t, trail, Options{Stamps: &options})
	if report.Chain.Status != "valid" || report.Chain.Coverage.Stamped.Status != "not-checked" || report.Chain.Stamps != nil {
		t.Fatalf("no roots: %v %+v", findingNames(report.Chain), report.Chain.Coverage)
	}
}

// A stamp lends its time only to the records the chain links to its
// checkpoint. A trail whose first record was changed after its third was
// stamped, with the changed record stamped later: the chain breaks at line 2,
// so the third record's stamp, though it still matches, lends its earlier
// time to nothing, and neither the time the stamped records existed by, nor
// the lag, nor the count of unreadable at, reaches past the break.
func TestAStampBeyondABreakLendsNoTime(t *testing.T) {
	_, _, trail := chainedTrail(t, 3)
	tsa := testAuthority(t, tsatest.Options{})
	lines := splitLines(trail)
	members, err := exactObject(lines[2])
	if err != nil {
		t.Fatal(err)
	}
	var at string
	if decodeString(members["at"], &at) != nil {
		t.Fatal("a record has an at")
	}
	lines[2] = bytes.Replace(lines[2], []byte(`"at":"`+at+`"`), []byte(`"at":"later"`), 1)
	early := time.Now().Add(-time.Hour).Truncate(time.Second)
	tsa.Now = func() time.Time { return early }
	third := stampOf(t, tsa, checkpointAt(t, joinLines(lines), 3))
	lines[0] = bytes.Replace(lines[0], []byte(`"n":0`), []byte(`"n":7`), 1)
	edited := joinLines(lines)
	late := early.Add(time.Hour)
	tsa.Now = func() time.Time { return late }
	first := stampOf(t, tsa, checkpointAt(t, edited, 1))
	chain := verifyStamped(t, edited, slices.Concat(third, first), StampOptions{}, tsa)
	if !slices.Equal(findingNames(chain), []string{"previous-mismatch@2"}) || chain.Coverage.Stamped != (result.AuditCoverageState{Status: "through", Through: 1}) {
		t.Fatalf("coverage: %v %+v", findingNames(chain), chain.Coverage)
	}
	if chain.Stamps.CoveredBy != late.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("the changed record existed by %s, not by the earlier stamp's time: %s", late.UTC(), chain.Stamps.CoveredBy)
	}
	if lag := chain.Stamps.Lag; lag.Records != 1 || lag.AtUnreadable != 0 || lag.MaxSequence != 1 {
		t.Fatalf("the lag reaches past the break: %+v", lag)
	}
	if !containsString(chain.Establishes, fmt.Sprintf(establishesStamped, 1, late.UTC().Format(time.RFC3339Nano))) {
		t.Fatalf("statements: %q", chain.Establishes)
	}
	// An unmet requirement is not a break: it does not pull the stamps'
	// coverage back.
	_, _, intact := chainedTrail(t, 2)
	options := StampOptions{}
	pool := x509.NewCertPool()
	pool.AddCert(tsa.Root)
	options.Verify.Roots = pool
	stamps := stampOf(t, tsa, checkpointAt(t, intact, 2))
	options.Stamps, options.StampsSize = bytes.NewReader(stamps), int64(len(stamps))
	report := verifyWith(t, intact, Options{Held: keptCheckpoints(t, intact, 1), RequireThrough: 2, Stamps: &options})
	if report.Chain.Coverage.Stamped.Through != 2 || report.Chain.Stamps.Lag.Records != 2 {
		t.Fatalf("an unmet checkpoint requirement: %v %+v", findingNames(report.Chain), report.Chain.Coverage)
	}
}

// Every token audit stamp can receive fits one stamps line a reader reads
// back, and a token that would not is refused before the stamps file is
// opened, so nothing is kept that a retry would not see.
func TestAStampTooLargeToReadBackIsRefusedBeforeWriting(t *testing.T) {
	root, dir, trail := chainedTrail(t, 1)
	checkpoint := checkpointAt(t, trail, 1)
	if size := len(encodeStampLine(checkpoint, make([]byte, timestamp.MaxReplyBytes))); size > MaxStampLineBytes {
		t.Fatalf("the largest reply makes a line of %d bytes, past %d", size, MaxStampLineBytes)
	}
	writer := NewWriter(root, "audit", true)
	if appended, err := writer.RecordStamp(checkpoint, make([]byte, MaxStampLineBytes)); !errors.Is(err, ErrStampTooLarge) || appended {
		t.Fatalf("a token past the bound: %v %v", appended, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "audit", StampsName)); !os.IsNotExist(err) {
		t.Fatalf("no stamps file is created: %v", err)
	}
}
