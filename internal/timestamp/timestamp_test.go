package timestamp

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp/tsatest"
)

// checkpointBytes is the canonical checkpoint of the record signatures' test
// vector (docs/building-with-packs.md), the content the fixtures stamp.
const checkpointBytes = `{"checkpointVersion":"1","recordDigest":"sha256:9305565078dd0531a50e67ee2d227d4fd0cd0d504541b92801660236a0f0eaed","sequence":1,"trail":"00112233445566778899aabbccddeeff"}`

func authority(t *testing.T, options tsatest.Options) *tsatest.Authority {
	t.Helper()
	tsa, err := tsatest.New(options)
	if err != nil {
		t.Fatal(err)
	}
	return tsa
}

func rootsOf(certificates ...*x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, certificate := range certificates {
		pool.AddCert(certificate)
	}
	return pool
}

func digestOf(text string) []byte {
	sum := sha256.Sum256([]byte(text))
	return sum[:]
}

func tokenFrom(t *testing.T, tsa *tsatest.Authority, digest []byte) *Token {
	t.Helper()
	der, err := tsa.Token(digest, big.NewInt(7))
	if err != nil {
		t.Fatal(err)
	}
	token, err := Parse(der)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// Tokens from an independent implementation parse and verify: OpenSSL 3's
// ts -reply, one signed with RSA (rsaEncryption, NULL parameters) and one
// with ECDSA, both binding the certificate by SHA-256
// (signingCertificateV2) and carrying a signing time, with NULL on their
// digest algorithms and imprint, accuracy 1.5 s and a nonce, over the
// checkpoint above, under a root made for them. They were made offline once,
// with:
//
//	openssl ts -query -digest <sha256 of the checkpoint> -sha256 -cert -out query.tsq
//	openssl ts -reply -config ts.cnf -queryfile query.tsq -inkey tsa.key -signer tsa.pem -out reply.tsr
//
// and the TSA certificate carries extendedKeyUsage = critical, timeStamping.
func TestTokensFromAnIndependentImplementationVerify(t *testing.T) {
	rootPEM, err := os.ReadFile(filepath.Join("testdata", "openssl-root.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(rootPEM)
	root, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"openssl-reply-rsa.tsr", "openssl-reply-ec.tsr"} {
		reply, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		der, err := ParseResponse(reply)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		token, err := Parse(der)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(token.HashedMessage, digestOf(checkpointBytes)) || !token.HashAlgorithm.Equal(OIDSHA256) ||
			!token.Policy.Equal(asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 7}) || token.Accuracy != 1500*time.Millisecond || token.Nonce == nil {
			t.Fatalf("%s: %+v", name, token)
		}
		revocation, err := token.Verify(VerifyOptions{Roots: rootsOf(root), Policies: []asn1.ObjectIdentifier{{1, 3, 6, 1, 4, 1, 99999, 7}}})
		if err != nil || revocation != RevocationNotChecked {
			t.Fatalf("%s: %v %v", name, revocation, err)
		}
		if _, err := token.Verify(VerifyOptions{Roots: rootsOf(root), Policies: []asn1.ObjectIdentifier{{1, 3, 6, 1, 4, 1, 99999, 8}}}); !errors.Is(err, ErrPolicy) {
			t.Fatalf("%s: another policy: %v", name, err)
		}
		// Any change to the signed content breaks it.
		tampered := bytes.Clone(der)
		at := bytes.Index(tampered, digestOf(checkpointBytes))
		tampered[at] ^= 1
		if _, err := Parse(tampered); !errors.Is(err, ErrSignature) {
			t.Fatalf("%s: a changed imprint: %v", name, err)
		}
	}
}

// A token the test authority makes parses to what it stamped, and verifies
// under its root at its time.
func TestAValidTokenVerifies(t *testing.T) {
	tsa := authority(t, tsatest.Options{AccuracySeconds: 2})
	digest := digestOf(checkpointBytes)
	token := tokenFrom(t, tsa, digest)
	if !bytes.Equal(token.HashedMessage, digest) || token.Nonce.Int64() != 7 || !token.Policy.Equal(tsatest.Policy) ||
		token.Accuracy != 2*time.Second || !token.ExistedBy().Equal(token.GenTime.Add(2*time.Second)) || token.Signer.SerialNumber.Int64() != 2 {
		t.Fatalf("token = %+v", token)
	}
	revocation, err := token.Verify(VerifyOptions{Roots: rootsOf(tsa.Root)})
	if err != nil || revocation != RevocationNotChecked {
		t.Fatalf("verify: %v %v", revocation, err)
	}
	// A request is DER the authority reads, and its reply carries the
	// token with the request's nonce.
	request, err := Request(digest, big.NewInt(99))
	if err != nil {
		t.Fatal(err)
	}
	reply, err := tsa.Reply(request)
	if err != nil {
		t.Fatal(err)
	}
	der, err := ParseResponse(reply)
	if err != nil {
		t.Fatal(err)
	}
	replied, err := Parse(der)
	if err != nil || replied.Nonce.Int64() != 99 || !bytes.Equal(replied.HashedMessage, digest) {
		t.Fatalf("replied: %+v %v", replied, err)
	}
	if _, err := Request(digest[:31], nil); err == nil {
		t.Fatal("a request is for a SHA-256 digest")
	}
}

// Every part of a token's signature is held: the signature itself, the
// content it signs, the signed content type, the signed digest, the binding
// of the signing certificate, and the certificate's presence.
func TestATokensSignatureIsHeldPartByPart(t *testing.T) {
	digest := digestOf(checkpointBytes)
	der, err := authority(t, tsatest.Options{}).Token(digest, nil)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Clone(der)
	tampered[len(tampered)-1] ^= 1
	if _, err := Parse(tampered); !errors.Is(err, ErrSignature) {
		t.Fatalf("a changed signature: %v", err)
	}
	for name, options := range map[string]tsatest.Options{
		"content type":        {WrongContentType: true},
		"message digest":      {WrongMessageDigest: true},
		"certificate binding": {WrongCertificateHash: true},
	} {
		der, err := authority(t, options).Token(digest, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(der); !errors.Is(err, ErrSignature) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	der, err = authority(t, tsatest.Options{OmitCertificates: true}).Token(digest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(der); !errors.Is(err, ErrMalformed) {
		t.Fatalf("no certificate: %v", err)
	}
	for _, bad := range [][]byte{nil, {0x30, 0x00}, append(bytes.Clone(der), 0)} {
		if _, err := Parse(bad); !errors.Is(err, ErrMalformed) {
			t.Fatalf("%x: %v", bad, err)
		}
	}
}

// The signing certificate must be for time-stamping alone, by a critical
// extension, and chain to a supplied root at the stamp's time.
func TestTheSigningCertificateIsHeldToItsUsageAndChain(t *testing.T) {
	digest := digestOf(checkpointBytes)
	for name, options := range map[string]tsatest.Options{
		"another usage":    {Usages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}},
		"an added usage":   {Usages: []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping, x509.ExtKeyUsageCodeSigning}},
		"a usage not held": {UsageNotCritical: true},
	} {
		tsa := authority(t, options)
		if _, err := tokenFrom(t, tsa, digest).Verify(VerifyOptions{Roots: rootsOf(tsa.Root)}); !errors.Is(err, ErrUsage) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	tsa := authority(t, tsatest.Options{})
	other := authority(t, tsatest.Options{})
	if _, err := tokenFrom(t, tsa, digest).Verify(VerifyOptions{Roots: rootsOf(other.Root)}); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("another root: %v", err)
	}
	if _, err := tokenFrom(t, tsa, digest).Verify(VerifyOptions{}); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("no root: %v", err)
	}
	// Valid then, though expired now: the chain is held at the stamp's time.
	now := time.Now()
	past := authority(t, tsatest.Options{NotBefore: now.Add(-3 * 365 * 24 * time.Hour), NotAfter: now.Add(-365 * 24 * time.Hour)})
	past.Now = func() time.Time { return now.Add(-2 * 365 * 24 * time.Hour) }
	if _, err := tokenFrom(t, past, digest).Verify(VerifyOptions{Roots: rootsOf(past.Root)}); err != nil {
		t.Fatalf("valid at the stamp's time: %v", err)
	}
	// Expired at the stamp's time, and not yet valid at it.
	past.Now = func() time.Time { return now.Add(-6 * 30 * 24 * time.Hour) }
	if _, err := tokenFrom(t, past, digest).Verify(VerifyOptions{Roots: rootsOf(past.Root)}); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("expired at the stamp's time: %v", err)
	}
	past.Now = func() time.Time { return now.Add(-4 * 365 * 24 * time.Hour) }
	if _, err := tokenFrom(t, past, digest).Verify(VerifyOptions{Roots: rootsOf(past.Root)}); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("not yet valid at the stamp's time: %v", err)
	}
}

// A token's policy is held to the ones supplied, and to none when none is.
func TestATokensPolicyIsHeld(t *testing.T) {
	tsa := authority(t, tsatest.Options{Policy: asn1.ObjectIdentifier{1, 2, 3, 4}})
	token := tokenFrom(t, tsa, digestOf(checkpointBytes))
	if _, err := token.Verify(VerifyOptions{Roots: rootsOf(tsa.Root), Policies: []asn1.ObjectIdentifier{tsatest.Policy}}); !errors.Is(err, ErrPolicy) {
		t.Fatalf("another policy: %v", err)
	}
	if _, err := token.Verify(VerifyOptions{Roots: rootsOf(tsa.Root), Policies: []asn1.ObjectIdentifier{tsatest.Policy, {1, 2, 3, 4}}}); err != nil {
		t.Fatalf("one of those supplied: %v", err)
	}
	if _, err := token.Verify(VerifyOptions{Roots: rootsOf(tsa.Root)}); err != nil {
		t.Fatalf("none supplied: %v", err)
	}
}

func crlOf(t *testing.T, tsa *tsatest.Authority, thisUpdate, revokedAt time.Time, reason int) *x509.RevocationList {
	t.Helper()
	encoded, err := tsa.CRL(thisUpdate, revokedAt, reason)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(encoded)
	list, err := x509.ParseRevocationList(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func listOf(t *testing.T, tsa *tsatest.Authority, thisUpdate time.Time, serial *big.Int, revokedAt time.Time) *x509.RevocationList {
	t.Helper()
	encoded, err := tsa.CRLListing(thisUpdate, serial, revokedAt, 0)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(encoded)
	list, err := x509.ParseRevocationList(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// Revocation status is as of the stamp's time, and only from a list that can
// speak for it: issued by the certificate's issuer, signed by it, at or after
// the stamp and while the certificate was valid. Without one it is not
// checked, never assumed good.
func TestRevocationIsCheckedOnlyWhereAListCanSpeakForTheStamp(t *testing.T) {
	tsa := authority(t, tsatest.Options{})
	token := tokenFrom(t, tsa, digestOf(checkpointBytes))
	at := token.GenTime
	roots := rootsOf(tsa.Root)
	check := func(lists ...*x509.RevocationList) (Revocation, error) {
		return token.Verify(VerifyOptions{Roots: roots, CRLs: lists})
	}
	cases := []struct {
		name  string
		lists []*x509.RevocationList
		want  Revocation
		err   error
	}{
		{"no list", nil, RevocationNotChecked, nil},
		{"a clean list after the stamp", []*x509.RevocationList{crlOf(t, tsa, at.Add(time.Hour), time.Time{}, 0)}, RevocationChecked, nil},
		{"a list from before the stamp", []*x509.RevocationList{crlOf(t, tsa, at.Add(-time.Hour), time.Time{}, 0)}, RevocationNotChecked, nil},
		{"a list after the certificate expired", []*x509.RevocationList{crlOf(t, tsa, tsa.Signer.NotAfter.Add(time.Hour), time.Time{}, 0)}, RevocationNotChecked, nil},
		{"another authority's list", []*x509.RevocationList{crlOf(t, authority(t, tsatest.Options{}), at.Add(time.Hour), time.Time{}, 0)}, RevocationNotChecked, nil},
		{"revoked before the stamp", []*x509.RevocationList{crlOf(t, tsa, at.Add(time.Hour), at.Add(-time.Minute), 0)}, "", ErrRevoked},
		{"revoked at the stamp", []*x509.RevocationList{crlOf(t, tsa, at.Add(time.Hour), at, 0)}, "", ErrRevoked},
		{"revoked after the stamp", []*x509.RevocationList{crlOf(t, tsa, at.Add(time.Hour), at.Add(time.Minute), 4)}, RevocationChecked, nil},
		{"a key compromised after the stamp", []*x509.RevocationList{crlOf(t, tsa, at.Add(time.Hour), at.Add(time.Minute), 1)}, "", ErrRevoked},
		{"a CA compromised after the stamp", []*x509.RevocationList{crlOf(t, tsa, at.Add(time.Hour), at.Add(time.Minute), 2)}, "", ErrRevoked},
		{"another certificate revoked", []*x509.RevocationList{listOf(t, tsa, at.Add(time.Hour), big.NewInt(999), at.Add(-time.Hour))}, RevocationChecked, nil},
	}
	for _, c := range cases {
		got, err := check(c.lists...)
		if got != c.want || !errors.Is(err, c.err) {
			t.Fatalf("%s: %q %v", c.name, got, err)
		}
	}
}

// A reply is read for its token only when it grants the request.
func TestAReplyIsReadForItsTokenOnlyWhenGranted(t *testing.T) {
	tsa := authority(t, tsatest.Options{})
	request, err := Request(digestOf(checkpointBytes), big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	tsa.Status = 2
	rejection, err := tsa.Reply(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseResponse(rejection); !errors.Is(err, ErrRejected) {
		t.Fatalf("a rejection: %v", err)
	}
	tsa.Status = 0
	reply, err := tsa.Reply(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseResponse(append(bytes.Clone(reply), 0)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("trailing bytes: %v", err)
	}
	granted, _ := asn1.Marshal(struct{ Status struct{ Status int } }{})
	if _, err := ParseResponse(granted); !errors.Is(err, ErrMalformed) {
		t.Fatalf("granted without a token: %v", err)
	}
}

// Every rule of the token's shape is held, one fault at a time: what is not of
// the shape is malformed, and what is of it but does not hold is a signature
// that does not.
func TestEveryRuleOfATokensShapeIsHeld(t *testing.T) {
	digest := digestOf(checkpointBytes)
	cases := []struct {
		name    string
		options tsatest.Options
		want    error
	}{
		{"another TSTInfo version", tsatest.Options{Version: 2}, ErrMalformed},
		{"an accuracy out of range", tsatest.Options{AccuracyMillis: 1500}, ErrMalformed},
		{"a signer it does not carry", tsatest.Options{WrongSID: true}, ErrMalformed},
		{"no signed attributes", tsatest.Options{NoSignedAttributes: true}, ErrMalformed},
		{"a SHA-1 digest", tsatest.Options{SHA1Digest: true}, ErrMalformed},
		{"an attribute given twice", tsatest.Options{DuplicateAttribute: true}, ErrMalformed},
		{"two signers", tsatest.Options{ExtraSigner: true}, ErrMalformed},
		{"another encapsulated content", tsatest.Options{WrongEncapsulatedType: true}, ErrMalformed},
		{"another content type", tsatest.Options{WrongContentInfoType: true}, ErrMalformed},
		{"a signature algorithm of another hash", tsatest.Options{SignatureAlgorithmMismatch: true}, ErrSignature},
		{"a SHA-1 certificate binding to another certificate", tsatest.Options{SigningCertificateV1: true, WrongCertificateHash: true}, ErrSignature},
	}
	for _, c := range cases {
		der, err := authority(t, c.options).Token(digest, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if _, err := Parse(der); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	// The SHA-1 binding is read when it binds the signer, and an accuracy
	// in milliseconds is read.
	for name, options := range map[string]tsatest.Options{
		"a SHA-1 certificate binding": {SigningCertificateV1: true},
		"an accuracy of 250 ms":       {AccuracyMillis: 250},
	} {
		der, err := authority(t, options).Token(digest, nil)
		if err != nil {
			t.Fatal(err)
		}
		token, err := Parse(der)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if options.AccuracyMillis == 250 && token.Accuracy != 250*time.Millisecond {
			t.Fatalf("%s: %v", name, token.Accuracy)
		}
	}
	// The imprint's hash is stated as the token states it.
	der, err := authority(t, tsatest.Options{ImprintSHA384: true}).Token(digest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token, err := Parse(der); err != nil || token.HashAlgorithm.Equal(OIDSHA256) {
		t.Fatalf("an imprint stated as SHA-384: %+v %v", token, err)
	}
}

// An RSA signature from the independent implementation is checked as an
// ECDSA one is: changed, it does not verify.
func TestAnRSASignatureIsVerified(t *testing.T) {
	reply, err := os.ReadFile(filepath.Join("testdata", "openssl-reply-rsa.tsr"))
	if err != nil {
		t.Fatal(err)
	}
	der, err := ParseResponse(reply)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Clone(der)
	tampered[len(tampered)-1] ^= 1
	if _, err := Parse(tampered); !errors.Is(err, ErrSignature) {
		t.Fatalf("a changed RSA signature: %v", err)
	}
}

// The subset is held as a class: a token outside DER, or outside the CMS
// versions and digest algorithms read here, is refused, whatever its
// signature, which each of these carries correctly.
func TestATokenOutsideDERAndTheCMSSubsetIsRefused(t *testing.T) {
	digest := digestOf(checkpointBytes)
	for name, options := range map[string]tsatest.Options{
		"an element after the ContentInfo's fields":   {ExtraContentInfoField: true},
		"an element after the SignedData's fields":    {ExtraSignedDataField: true},
		"SignedData version 1":                        {SignedDataVersion: 1},
		"SignerInfo version 99":                       {SignerInfoVersion: 99},
		"SignerInfo version 3 naming by serial":       {SignerInfoVersion: 3},
		"no digest algorithms":                        {DigestAlgorithms: "empty"},
		"digest algorithms without the signer's":      {DigestAlgorithms: "sha384"},
		"attribute values as a SEQUENCE":              {AttributeValuesAsSequence: true},
		"signed attributes out of DER order":          {UnsortedAttributes: true},
		"an accuracy no time span holds":              {AccuracySeconds: 9223372037},
		"an ESS binding of another certificate first": {AlsoWrongV1Binding: true},
		"no ESS binding":                              {NoBinding: true},
		"a critical TSTInfo extension":                {CriticalExtension: true},
		"a genTime with a trailing zero":              {GenTimeText: "20261003023902.50Z"},
		"a genTime with a zone":                       {GenTimeText: "20261003023902+0000"},
		"a genTime to the minute":                     {GenTimeText: "202610030239Z"},
		"a content type with two values":              {MultiValuedAttribute: true},
		"the default ESS hash written out":            {ExplicitDefaultV2Hash: true},
	} {
		der, err := authority(t, options).Token(digest, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		token, err := Parse(der)
		if err == nil {
			t.Fatalf("%s: read, existed by %v", name, token.ExistedBy())
		}
		want := ErrMalformed
		if options.AlsoWrongV1Binding || options.NoBinding {
			want = ErrSignature
		}
		if !errors.Is(err, want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// A fraction of a second, as DER writes it, is read.
	der, err := authority(t, tsatest.Options{GenTimeText: "20261003023902.5Z"}).Token(digest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token, err := Parse(der); err != nil || token.GenTime.Nanosecond() != 500000000 {
		t.Fatalf("a fraction: %v %v", err, token)
	}
	// The largest accuracy a time span holds is read, and adds to the time.
	der, err = authority(t, tsatest.Options{AccuracySeconds: 9223372035}).Token(digest, nil)
	if err != nil {
		t.Fatal(err)
	}
	token, err := Parse(der)
	if err != nil || !token.ExistedBy().After(token.GenTime) {
		t.Fatalf("a long accuracy: %v %v", err, token)
	}
}

// A revocation list that cannot speak for every certificate of its issuer is
// no evidence of status: a delta list, a scoped one, an indirect one, and one
// with a critical extension, of the list or of an entry, not read here. The
// stamp's status is then not checked, never good.
func TestOnlyACompleteRevocationListIsEvidence(t *testing.T) {
	tsa := authority(t, tsatest.Options{})
	token := tokenFrom(t, tsa, digestOf(checkpointBytes))
	after := token.GenTime.Add(time.Hour)
	baseNumber, _ := asn1.Marshal(big.NewInt(1))
	scope, _ := asn1.Marshal(struct {
		OnlyUser bool `asn1:"optional,tag:1"`
	}{OnlyUser: true})
	issuer, _ := asn1.Marshal([]asn1.RawValue{{Class: asn1.ClassContextSpecific, Tag: 4, IsCompound: true, Bytes: tsa.Root.RawSubject}})
	unknown := asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 9}
	for name, extensions := range map[string][2][]pkix.Extension{
		"a delta list":                      {{{Id: asn1.ObjectIdentifier{2, 5, 29, 27}, Critical: true, Value: baseNumber}}, nil},
		"a delta list not marked critical":  {{{Id: asn1.ObjectIdentifier{2, 5, 29, 27}, Value: baseNumber}}, nil},
		"a scoped list":                     {{{Id: asn1.ObjectIdentifier{2, 5, 29, 28}, Critical: true, Value: scope}}, nil},
		"a scoped list not marked critical": {{{Id: asn1.ObjectIdentifier{2, 5, 29, 28}, Value: scope}}, nil},
		"a critical list extension unread":  {{{Id: unknown, Critical: true, Value: []byte{0x05, 0x00}}}, nil},
		"an indirect entry":                 {nil, {{Id: asn1.ObjectIdentifier{2, 5, 29, 29}, Critical: true, Value: issuer}}},
		"a critical entry extension unread": {nil, {{Id: unknown, Critical: true, Value: []byte{0x05, 0x00}}}},
	} {
		encoded, err := tsa.CRLExtended(after, extensions[0], extensions[1])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		block, _ := pem.Decode(encoded)
		list, err := x509.ParseRevocationList(block.Bytes)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got, err := token.Verify(VerifyOptions{Roots: rootsOf(tsa.Root), CRLs: []*x509.RevocationList{list}}); err != nil || got != RevocationNotChecked {
			t.Fatalf("%s: %q %v", name, got, err)
		}
	}
	// An extension is allowed critical only where it is decoded and
	// applied: an entry's critical invalidity date is not applied, whatever
	// its value, so the list is not evidence; the same date not marked
	// critical may be passed over, and the list is.
	entryList := func(entry pkix.Extension) *x509.RevocationList {
		t.Helper()
		encoded, err := tsa.CRLExtended(after, nil, []pkix.Extension{entry})
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(encoded)
		list, err := x509.ParseRevocationList(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		return list
	}
	invalidityDate := asn1.ObjectIdentifier{2, 5, 29, 24}
	date, _ := asn1.MarshalWithParams(after.Add(-2*time.Hour).UTC().Truncate(time.Second), "generalized")
	for name, value := range map[string][]byte{"a date": date, "NULL": {0x05, 0x00}, "text": []byte("not a date")} {
		list := entryList(pkix.Extension{Id: invalidityDate, Critical: true, Value: value})
		if got, err := token.Verify(VerifyOptions{Roots: rootsOf(tsa.Root), CRLs: []*x509.RevocationList{list}}); err != nil || got != RevocationNotChecked {
			t.Fatalf("a critical invalidity date of %s: %q %v", name, got, err)
		}
	}
	list := entryList(pkix.Extension{Id: invalidityDate, Value: date})
	if got, err := token.Verify(VerifyOptions{Roots: rootsOf(tsa.Root), CRLs: []*x509.RevocationList{list}}); err != nil || got != RevocationChecked {
		t.Fatalf("a complete list: %q %v", got, err)
	}
}

// What verification does not need is refused, not carried unchecked: an
// unsigned attribute or an unread signed attribute, whatever its value (the
// reviewer's inputs: a non-minimal nested length, a non-minimal INTEGER, a
// BOOLEAN TRUE written 01, a SET OF reversed, and a well-formed one);
// revocation data embedded in the token; algorithm parameters other than
// absent or 05 00 where NULL is allowed; ESS policies; and an ESS binding of
// two certificates, the second writing out its DEFAULT hash.
func TestWhatVerificationDoesNotNeedIsRefused(t *testing.T) {
	digest := digestOf(checkpointBytes)
	values := map[string][]byte{
		"a non-minimal nested length": {0x30, 0x81, 0x03, 0x02, 0x01, 0x01},
		"a non-minimal INTEGER":       {0x02, 0x02, 0x00, 0x01},
		"BOOLEAN TRUE written 01":     {0x01, 0x01, 0x01},
		"a SET OF reversed":           {0x31, 0x06, 0x02, 0x01, 0x02, 0x02, 0x01, 0x01},
		"a well-formed INTEGER":       {0x02, 0x01, 0x01},
	}
	cases := map[string]tsatest.Options{
		"embedded revocation data":                        {EmbedCRL: true},
		"parameters on the signer's digest algorithm":     {SignerDigestParameters: []byte{0x04, 0x00}},
		"a NULL with content on the digest algorithm":     {SignerDigestParameters: []byte{0x05, 0x01, 0x00}},
		"NULL on an ECDSA signature algorithm":            {SignatureParameters: []byte{0x05, 0x00}},
		"parameters on the imprint's hash":                {ImprintParameters: []byte{0x02, 0x01, 0x00}},
		"parameters on the SignedData's digest algorithm": {DigestAlgorithmsParameters: []byte{0x30, 0x00}},
		"ESS policies":              {ESSPolicies: true},
		"two ESS v1 entries":        {SigningCertificateV1: true, TwoESSEntries: true},
		"parameters on an ESS hash": {ESSHashAlgorithmParameters: []byte{0x04, 0x00}},
		"two signing times":         {SigningTime: "two"},
		"an ECDSA signature named by its key's algorithm": {SignatureAlgorithmECPublicKey: true},
		"two ESS entries":                    {TwoESSEntries: true},
		"a signing time not to the second":   {SigningTime: "bad"},
		"an algorithm protection with a MAC": {AlgorithmProtection: "mac"},
	}
	for name, value := range values {
		cases["an unsigned attribute of "+name] = tsatest.Options{UnsignedAttributeValue: value}
		cases["an unread signed attribute of "+name] = tsatest.Options{ExtraSignedAttributeValue: value}
	}
	for name, options := range cases {
		der, err := authority(t, options).Token(digest, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := Parse(der); !errors.Is(err, ErrMalformed) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// What is read is held to what it states.
	for name, options := range map[string]tsatest.Options{
		"an ESS issuerSerial of another serial":        {ESSIssuerSerial: "wrong-serial"},
		"an ESS v1 issuerSerial of another serial":     {SigningCertificateV1: true, ESSIssuerSerial: "wrong-serial"},
		"an algorithm protection of another signature": {AlgorithmProtection: "mismatch-signature"},
		"an ESS issuerSerial of another issuer":        {ESSIssuerSerial: "wrong-issuer"},
		"an algorithm protection of another hash":      {AlgorithmProtection: "mismatch"},
	} {
		der, err := authority(t, options).Token(digest, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := Parse(der); !errors.Is(err, ErrSignature) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// And the common shapes a real authority writes are read.
	for name, options := range map[string]tsatest.Options{
		"NULL on the signer's digest algorithm":         {SignerDigestParameters: []byte{0x05, 0x00}},
		"the imprint's hash without parameters":         {ImprintParameters: []byte{}},
		"NULL on the SignedData's digest algorithm":     {DigestAlgorithmsParameters: []byte{0x05, 0x00}},
		"an ESS issuerSerial naming the signer":         {ESSIssuerSerial: "right"},
		"an ESS v1 issuerSerial naming the signer":      {SigningCertificateV1: true, ESSIssuerSerial: "right"},
		"SHA-384 as the ESS hash with NULL":             {ESSHashAlgorithmParameters: []byte{0x05, 0x00}},
		"a signing time":                                {SigningTime: "utc"},
		"an algorithm protection naming the algorithms": {AlgorithmProtection: "match"},
	} {
		der, err := authority(t, options).Token(digest, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := Parse(der); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// A reply's envelope is decoded whole: its status's text is read, and an
// element after its fields is refused.
func TestAReplyEnvelopeIsDecodedWhole(t *testing.T) {
	tsa := authority(t, tsatest.Options{})
	request, err := Request(digestOf(checkpointBytes), big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	tsa.StatusText = true
	reply, err := tsa.Reply(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseResponse(reply); err != nil {
		t.Fatalf("a status text: %v", err)
	}
	tsa.StatusText, tsa.StatusTextPrintable = false, true
	reply, err = tsa.Reply(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseResponse(reply); !errors.Is(err, ErrMalformed) {
		t.Fatalf("a status text not UTF-8 strings: %v", err)
	}
	tsa.StatusTextPrintable, tsa.StatusExtra = false, true
	reply, err = tsa.Reply(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseResponse(reply); !errors.Is(err, ErrMalformed) {
		t.Fatalf("an element after the status's fields: %v", err)
	}
}

// The RSA signature algorithm's parameters are held as the ECDSA ones are:
// absent or NULL. They are outside what is signed, so a token with others
// still verifies, and is refused for them.
func TestAnRSASignatureAlgorithmsParametersAreHeld(t *testing.T) {
	reply, err := os.ReadFile(filepath.Join("testdata", "openssl-reply-rsa.tsr"))
	if err != nil {
		t.Fatal(err)
	}
	der, err := ParseResponse(reply)
	if err != nil {
		t.Fatal(err)
	}
	rsaWithNull := []byte{0x06, 0x09, 0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x01, 0x01, 0x05, 0x00}
	// The SignerInfo comes last, after the certificates, whose keys name the
	// algorithm too.
	at := bytes.LastIndex(der, rsaWithNull)
	if at < 0 {
		t.Fatal("the fixture names rsaEncryption with NULL")
	}
	changed := bytes.Clone(der)
	changed[at+11] = 0x04
	if _, err := Parse(changed); !errors.Is(err, ErrMalformed) {
		t.Fatalf("other parameters on RSA: %v", err)
	}
}
