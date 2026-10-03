// Package tsatest is a time-stamping authority for tests: a root, a
// time-stamping certificate under it, and tokens and replies made with them, so
// no test ever asks a real authority. Only tests import it.
//
// It encodes RFC 3161 and RFC 5652 with its own structures rather than the
// timestamp package's, so the two meet only at the bytes; a token from an
// independent implementation is checked in beside the timestamp package's
// tests as well.
package tsatest

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"sort"
	"sync"
	"time"
)

var (
	oidSignedData           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidTSTInfo              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}
	oidContentType          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidMessageDigest        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSigningCertificateV2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}
	oidSigningCertificate   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 12}
	oidSigningTime          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}
	oidAlgorithmProtection  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 52}
	oidSHA256               = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384               = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA1                 = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidECPublicKey          = asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
	oidECDSAWithSHA256      = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidECDSAWithSHA384      = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	// Policy is the policy the authority stamps under unless told otherwise.
	Policy = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 1}
)

// Options shape the authority's certificate and its tokens, for the cases a
// test needs to hold a verifier to.
type Options struct {
	// NotBefore and NotAfter bound the time-stamping certificate; zero is a
	// year either side of now.
	NotBefore, NotAfter time.Time
	// Usages replace the certificate's extended key usages, and
	// UsageNotCritical marks the extension not critical.
	Usages           []x509.ExtKeyUsage
	UsageNotCritical bool
	// Policy replaces the policy stamped under.
	Policy asn1.ObjectIdentifier
	// Accuracy is stamped as the token's accuracy, in seconds.
	AccuracySeconds int
	// The faults a token can carry: no certificates; a signing-certificate
	// attribute over other bytes; a content-type attribute naming other
	// content; a message-digest attribute over other content.
	OmitCertificates     bool
	WrongCertificateHash bool
	WrongContentType     bool
	WrongMessageDigest   bool
	// More faults, one check each: another TSTInfo version; an accuracy in
	// milliseconds (past 999 is out of range); a SignerInfo naming a
	// certificate the token does not carry; no signed attributes; a SHA-1
	// digest; an attribute given twice; the signing certificate bound by
	// SHA-1 (signingCertificate) rather than SHA-256; a signature algorithm
	// naming another hash than the digest's; an imprint stated as SHA-384; a
	// second SignerInfo; an encapsulated content and a content type other
	// than TSTInfo and SignedData.
	Version                    int
	AccuracyMillis             int
	WrongSID                   bool
	NoSignedAttributes         bool
	SHA1Digest                 bool
	DuplicateAttribute         bool
	SigningCertificateV1       bool
	SignatureAlgorithmMismatch bool
	ImprintSHA384              bool
	ExtraSigner                bool
	WrongEncapsulatedType      bool
	WrongContentInfoType       bool
	// Departures from DER and from the CMS subset, one each: an element after
	// the last field of the ContentInfo or of the SignedData; another
	// SignedData or SignerInfo version; digest algorithms that are empty or
	// name SHA-384 while the signer uses SHA-256; attribute values as a
	// SEQUENCE rather than a SET; signed attributes out of DER order; and an
	// ESS signingCertificate binding another certificate beside a
	// signingCertificateV2 binding the right one.
	ExtraContentInfoField     bool
	ExtraSignedDataField      bool
	SignedDataVersion         int
	SignerInfoVersion         int
	DigestAlgorithms          string
	AttributeValuesAsSequence bool
	UnsortedAttributes        bool
	AlsoWrongV1Binding        bool
	// More: a critical TSTInfo extension; a genTime written as given; a
	// content-type attribute with two values; no ESS binding at all; and
	// signingCertificateV2's DEFAULT hash, SHA-256, written out.
	CriticalExtension     bool
	GenTimeText           string
	MultiValuedAttribute  bool
	NoBinding             bool
	ExplicitDefaultV2Hash bool
	// What a token may carry beyond what is read, one each: an unsigned
	// attribute (type 1.2.3.4) with the value given, DER as written; a signed
	// attribute of that type and value; revocation data embedded; algorithm
	// parameters written as given for the signer's digest algorithm, its
	// signature algorithm, the imprint's hash (empty is absent) and the
	// SignedData's digest algorithms; an ESS issuerSerial ("right",
	// "wrong-serial", "wrong-issuer"); ESS policies; a second ESS entry with
	// the DEFAULT hash written out; a signing time ("utc", or "bad", not to
	// the second); and an RFC 6211 algorithm protection ("match",
	// "mismatch", or "mac" with a MAC algorithm).
	UnsignedAttributeValue     []byte
	ExtraSignedAttributeValue  []byte
	EmbedCRL                   bool
	SignerDigestParameters     []byte
	SignatureParameters        []byte
	ImprintParameters          []byte
	DigestAlgorithmsParameters []byte
	ESSIssuerSerial            string
	ESSPolicies                bool
	TwoESSEntries              bool
	SigningTime                string
	AlgorithmProtection        string
	// And: SHA-384 as the ESS v2 hash with the parameters given; an ECDSA
	// signature named by id-ecPublicKey.
	ESSHashAlgorithmParameters    []byte
	SignatureAlgorithmECPublicKey bool
}

// withParameters is an algorithm identifier with the parameters given, as
// written: nil keeps its own, empty leaves them out.
func withParameters(identifier algorithmIdentifier, parameters []byte) algorithmIdentifier {
	switch {
	case parameters == nil:
		return identifier
	case len(parameters) == 0:
		identifier.Parameters = asn1.RawValue{}
	default:
		identifier.Parameters = asn1.RawValue{FullBytes: parameters}
	}
	return identifier
}

// signatureAlgorithmOf is the signature algorithm the options name.
func signatureAlgorithmOf(options Options) asn1.ObjectIdentifier {
	switch {
	case options.SHA1Digest, options.SignatureAlgorithmECPublicKey:
		return oidECPublicKey
	case options.SignatureAlgorithmMismatch:
		return oidECDSAWithSHA384
	}
	return oidECDSAWithSHA256
}

// genTime is the authority's now as a GeneralizedTime in whole seconds, or
// the text given, as written.
func genTime(now time.Time, text string) asn1.RawValue {
	if text == "" {
		text = now.UTC().Truncate(time.Second).Format("20060102150405Z")
	}
	return asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagGeneralizedTime, Bytes: []byte(text)}
}

// extensions is the TSTInfo's extensions: one critical one when asked.
func extensions(critical bool) []pkix.Extension {
	if !critical {
		return nil
	}
	return []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 3}, Critical: true, Value: []byte{0x05, 0x00}}}
}

// Authority is a root and a time-stamping certificate under it.
type Authority struct {
	Root      *x509.Certificate
	rootKey   *ecdsa.PrivateKey
	Signer    *x509.Certificate
	signerKey *ecdsa.PrivateKey
	options   Options
	mutex     sync.Mutex
	serial    int64
	// Now is the time the authority stamps; nil is the clock.
	Now func() time.Time
	// Requests counts the requests the handler served.
	Requests int
	// Fail, when set, makes the handler answer with that HTTP status.
	Fail int
	// Status, when set, is the status of every reply (2 is rejection).
	Status int
	// WrongDigest and WrongNonce make a reply stamp another digest than the
	// request's, or carry another nonce.
	WrongDigest, WrongNonce bool
	// StatusText puts a text in the reply's status, StatusTextPrintable puts
	// one written as a PrintableString, and StatusExtra puts an element after
	// its fields.
	StatusText, StatusTextPrintable, StatusExtra bool
}

// New makes an authority.
func New(options Options) (*Authority, error) {
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "tsatest root"},
		NotBefore:             now.Add(-10 * 365 * 24 * time.Hour),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		return nil, err
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return nil, err
	}
	signerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	notBefore, notAfter := options.NotBefore, options.NotAfter
	if notBefore.IsZero() {
		notBefore = now.Add(-365 * 24 * time.Hour)
	}
	if notAfter.IsZero() {
		notAfter = now.Add(365 * 24 * time.Hour)
	}
	usages := options.Usages
	if usages == nil {
		usages = []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}
	}
	usageExtension, err := extKeyUsage(usages, !options.UsageNotCritical)
	if err != nil {
		return nil, err
	}
	signerTemplate := &x509.Certificate{
		SerialNumber:    big.NewInt(2),
		Subject:         pkix.Name{CommonName: "tsatest time-stamping"},
		NotBefore:       notBefore,
		NotAfter:        notAfter,
		KeyUsage:        x509.KeyUsageDigitalSignature,
		ExtraExtensions: []pkix.Extension{usageExtension},
	}
	signerDER, err := x509.CreateCertificate(rand.Reader, signerTemplate, root, &signerKey.PublicKey, rootKey)
	if err != nil {
		return nil, err
	}
	signer, err := x509.ParseCertificate(signerDER)
	if err != nil {
		return nil, err
	}
	return &Authority{Root: root, rootKey: rootKey, Signer: signer, signerKey: signerKey, options: options}, nil
}

// extKeyUsage encodes an extended key usage extension, critical or not.
func extKeyUsage(usages []x509.ExtKeyUsage, critical bool) (pkix.Extension, error) {
	oids := map[x509.ExtKeyUsage]asn1.ObjectIdentifier{
		x509.ExtKeyUsageTimeStamping: {1, 3, 6, 1, 5, 5, 7, 3, 8},
		x509.ExtKeyUsageServerAuth:   {1, 3, 6, 1, 5, 5, 7, 3, 1},
		x509.ExtKeyUsageCodeSigning:  {1, 3, 6, 1, 5, 5, 7, 3, 3},
	}
	list := []asn1.ObjectIdentifier{}
	for _, usage := range usages {
		oid, ok := oids[usage]
		if !ok {
			return pkix.Extension{}, errors.New("tsatest: an extended key usage it does not encode")
		}
		list = append(list, oid)
	}
	value, err := asn1.Marshal(list)
	if err != nil {
		return pkix.Extension{}, err
	}
	return pkix.Extension{Id: asn1.ObjectIdentifier{2, 5, 29, 37}, Critical: critical, Value: value}, nil
}

// RootPEM is the root, PEM, as a verifier is given it.
func (a *Authority) RootPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.Root.Raw})
}

// CRL is a revocation list issued by the root at thisUpdate, listing the
// time-stamping certificate revoked at revokedAt for reason (none when
// revokedAt is zero), PEM.
func (a *Authority) CRL(thisUpdate, revokedAt time.Time, reason int) ([]byte, error) {
	return a.CRLListing(thisUpdate, a.Signer.SerialNumber, revokedAt, reason)
}

// CRLExtended is a revocation list issued by the root at thisUpdate with the
// extensions given, and, when entry extensions are given, an entry for
// another certificate carrying them, PEM.
func (a *Authority) CRLExtended(thisUpdate time.Time, list, entry []pkix.Extension) ([]byte, error) {
	template := &x509.RevocationList{
		Number:          big.NewInt(1),
		ThisUpdate:      thisUpdate,
		NextUpdate:      thisUpdate.Add(24 * time.Hour),
		ExtraExtensions: list,
	}
	if len(entry) > 0 {
		template.RevokedCertificateEntries = []x509.RevocationListEntry{{SerialNumber: big.NewInt(999), RevocationTime: thisUpdate.Add(-time.Hour), ExtraExtensions: entry}}
	}
	der, err := x509.CreateRevocationList(rand.Reader, template, a.Root, a.rootKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: der}), nil
}

// CRLListing is CRL listing the certificate of another serial number.
func (a *Authority) CRLListing(thisUpdate time.Time, serial *big.Int, revokedAt time.Time, reason int) ([]byte, error) {
	template := &x509.RevocationList{
		Number:     big.NewInt(1),
		ThisUpdate: thisUpdate,
		NextUpdate: thisUpdate.Add(24 * time.Hour),
	}
	if !revokedAt.IsZero() {
		template.RevokedCertificateEntries = []x509.RevocationListEntry{{SerialNumber: serial, RevocationTime: revokedAt, ReasonCode: reason}}
	}
	der, err := x509.CreateRevocationList(rand.Reader, template, a.Root, a.rootKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: der}), nil
}

type algorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

type imprint struct {
	HashAlgorithm algorithmIdentifier
	HashedMessage []byte
}

type accuracy struct {
	Seconds int `asn1:"optional"`
	Millis  int `asn1:"optional,tag:0"`
}

type tstInfo struct {
	Version        int
	Policy         asn1.ObjectIdentifier
	MessageImprint imprint
	SerialNumber   *big.Int
	GenTime        asn1.RawValue
	Accuracy       accuracy         `asn1:"optional"`
	Nonce          *big.Int         `asn1:"optional"`
	Extensions     []pkix.Extension `asn1:"optional,tag:1"`
}

type attribute struct {
	Type   asn1.ObjectIdentifier
	Values []asn1.RawValue `asn1:"set"`
}

type generalNames struct {
	Names  []asn1.RawValue
	Serial *big.Int
}

type essCertIDv2 struct {
	HashAlgorithm algorithmIdentifier `asn1:"optional"`
	CertHash      []byte
	IssuerSerial  generalNames `asn1:"optional"`
}

type signingCertificateV2 struct {
	Certs    []essCertIDv2
	Policies []asn1.RawValue `asn1:"optional"`
}

type protection struct {
	DigestAlgorithm    algorithmIdentifier
	SignatureAlgorithm algorithmIdentifier `asn1:"tag:1"`
	MAC                asn1.RawValue       `asn1:"optional"`
}

type essCertID struct {
	CertHash     []byte
	IssuerSerial generalNames `asn1:"optional"`
}

type signingCertificateV1 struct {
	Certs []essCertID
}

type issuerAndSerial struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

type signerInfo struct {
	Version            int
	SID                issuerAndSerial
	DigestAlgorithm    algorithmIdentifier
	SignedAttrs        asn1.RawValue
	SignatureAlgorithm algorithmIdentifier
	Signature          []byte
	UnsignedAttrs      asn1.RawValue `asn1:"optional"`
}

type encapsulated struct {
	EContentType asn1.ObjectIdentifier
	EContent     asn1.RawValue
}

type signedData struct {
	Version          int
	DigestAlgorithms []algorithmIdentifier `asn1:"set"`
	EncapContentInfo encapsulated
	Certificates     asn1.RawValue
	CRLs             asn1.RawValue `asn1:"optional"`
	SignerInfos      []signerInfo  `asn1:"set"`
}

type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue
}

// explicit wraps DER in a [tag] EXPLICIT context-specific tag.
func explicit(tag int, der []byte) asn1.RawValue {
	return asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: tag, IsCompound: true, Bytes: der}
}

// Token is a time-stamp token over a SHA-256 digest at the authority's now,
// in whole seconds, as encoding/asn1 writes a GeneralizedTime, DER. A nil
// nonce leaves the nonce out.
func (a *Authority) Token(digest []byte, nonce *big.Int) ([]byte, error) {
	a.mutex.Lock()
	a.serial++
	serial := a.serial
	a.mutex.Unlock()
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	options := a.options
	policy := Policy
	if options.Policy != nil {
		policy = options.Policy
	}
	version := 1
	if options.Version != 0 {
		version = options.Version
	}
	imprintAlgorithm := oidSHA256
	if options.ImprintSHA384 {
		imprintAlgorithm = oidSHA384
	}
	content, err := asn1.Marshal(tstInfo{
		Version:        version,
		Policy:         policy,
		MessageImprint: imprint{HashAlgorithm: withParameters(algorithmIdentifier{Algorithm: imprintAlgorithm, Parameters: asn1.NullRawValue}, options.ImprintParameters), HashedMessage: digest},
		SerialNumber:   big.NewInt(serial),
		GenTime:        genTime(now, options.GenTimeText),
		Accuracy:       accuracy{Seconds: options.AccuracySeconds, Millis: options.AccuracyMillis},
		Nonce:          nonce,
		Extensions:     extensions(options.CriticalExtension),
	})
	if err != nil {
		return nil, err
	}
	digestAlgorithm, digestHash := oidSHA256, crypto.SHA256
	if options.SHA1Digest {
		digestAlgorithm, digestHash = oidSHA1, crypto.SHA1
	}
	sum := func(data []byte) []byte {
		h := digestHash.New()
		h.Write(data)
		return h.Sum(nil)
	}
	contentDigest := sum(content)
	if options.WrongMessageDigest {
		contentDigest = sum(append(bytes.Clone(content), 0))
	}
	declared := oidTSTInfo
	if options.WrongContentType {
		declared = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	}
	bound := a.Signer.Raw
	if options.WrongCertificateHash {
		bound = a.Root.Raw
	}
	rawValue := func(value any) (asn1.RawValue, error) {
		der, err := asn1.Marshal(value)
		return asn1.RawValue{FullBytes: der}, err
	}
	contentType, err := rawValue(declared)
	if err != nil {
		return nil, err
	}
	messageDigest, err := rawValue(contentDigest)
	if err != nil {
		return nil, err
	}
	var signingCertificate asn1.RawValue
	certificateAttribute := oidSigningCertificateV2
	named := generalNames{}
	if options.ESSIssuerSerial != "" {
		issuer, serial := a.Signer.RawIssuer, a.Signer.SerialNumber
		if options.ESSIssuerSerial == "wrong-serial" {
			serial = new(big.Int).Add(serial, big.NewInt(1))
		}
		if options.ESSIssuerSerial == "wrong-issuer" {
			issuer = a.Signer.RawSubject
		}
		named = generalNames{Names: []asn1.RawValue{{Class: asn1.ClassContextSpecific, Tag: 4, IsCompound: true, Bytes: issuer}}, Serial: serial}
	}
	if options.SigningCertificateV1 {
		certHash := sha1.Sum(bound)
		certificateAttribute = oidSigningCertificate
		ids := []essCertID{{CertHash: certHash[:], IssuerSerial: named}}
		if options.TwoESSEntries {
			rootHash := sha1.Sum(a.Root.Raw)
			ids = append(ids, essCertID{CertHash: rootHash[:]})
		}
		signingCertificate, err = rawValue(signingCertificateV1{Certs: ids})
	} else {
		certHash := sha256.Sum256(bound)
		id := essCertIDv2{CertHash: certHash[:], IssuerSerial: named}
		if options.ExplicitDefaultV2Hash {
			id.HashAlgorithm = algorithmIdentifier{Algorithm: oidSHA256}
		}
		if options.ESSHashAlgorithmParameters != nil {
			long := sha512.Sum384(bound)
			id.CertHash = long[:]
			id.HashAlgorithm = algorithmIdentifier{Algorithm: oidSHA384, Parameters: asn1.RawValue{FullBytes: options.ESSHashAlgorithmParameters}}
		}
		value := signingCertificateV2{Certs: []essCertIDv2{id}}
		if options.TwoESSEntries {
			rootHash := sha256.Sum256(a.Root.Raw)
			value.Certs = append(value.Certs, essCertIDv2{HashAlgorithm: algorithmIdentifier{Algorithm: oidSHA256}, CertHash: rootHash[:]})
		}
		if options.ESSPolicies {
			policy, _ := asn1.Marshal(struct{ Policy asn1.ObjectIdentifier }{Policy})
			value.Policies = []asn1.RawValue{{FullBytes: policy}}
		}
		signingCertificate, err = rawValue(value)
	}
	if err != nil {
		return nil, err
	}
	attributeList := []attribute{
		{Type: oidContentType, Values: []asn1.RawValue{contentType}},
		{Type: oidMessageDigest, Values: []asn1.RawValue{messageDigest}},
	}
	if !options.NoBinding {
		attributeList = append(attributeList, attribute{Type: certificateAttribute, Values: []asn1.RawValue{signingCertificate}})
	}
	if options.ExtraSignedAttributeValue != nil {
		attributeList = append(attributeList, attribute{Type: asn1.ObjectIdentifier{1, 2, 3, 4}, Values: []asn1.RawValue{{FullBytes: options.ExtraSignedAttributeValue}}})
	}
	switch options.SigningTime {
	case "utc":
		attributeList = append(attributeList, attribute{Type: oidSigningTime, Values: []asn1.RawValue{{Class: asn1.ClassUniversal, Tag: asn1.TagUTCTime, Bytes: []byte(now.UTC().Format("060102150405Z"))}}})
	case "bad":
		attributeList = append(attributeList, attribute{Type: oidSigningTime, Values: []asn1.RawValue{{Class: asn1.ClassUniversal, Tag: asn1.TagUTCTime, Bytes: []byte(now.UTC().Format("0601021504Z"))}}})
	case "two":
		one := asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagUTCTime, Bytes: []byte(now.UTC().Format("060102150405Z"))}
		other := asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagUTCTime, Bytes: []byte(now.UTC().Add(time.Hour).Format("060102150405Z"))}
		attributeList = append(attributeList, attribute{Type: oidSigningTime, Values: []asn1.RawValue{one, other}})
	}
	if options.AlgorithmProtection != "" {
		value := protection{
			DigestAlgorithm:    withParameters(algorithmIdentifier{Algorithm: digestAlgorithm}, options.SignerDigestParameters),
			SignatureAlgorithm: withParameters(algorithmIdentifier{Algorithm: signatureAlgorithmOf(options)}, options.SignatureParameters),
		}
		switch options.AlgorithmProtection {
		case "mismatch":
			value.DigestAlgorithm = algorithmIdentifier{Algorithm: oidSHA384}
		case "mismatch-signature":
			value.SignatureAlgorithm = algorithmIdentifier{Algorithm: oidECDSAWithSHA384}
		case "mac":
			value.MAC = asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 2, IsCompound: true, Bytes: []byte{0x06, 0x01, 0x2a}}
		}
		encoded, err := rawValue(value)
		if err != nil {
			return nil, err
		}
		attributeList = append(attributeList, attribute{Type: oidAlgorithmProtection, Values: []asn1.RawValue{encoded}})
	}
	if options.MultiValuedAttribute {
		other, err := rawValue(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1})
		if err != nil {
			return nil, err
		}
		attributeList[0].Values = append(attributeList[0].Values, other)
	}
	if options.DuplicateAttribute {
		attributeList = append(attributeList, attribute{Type: oidContentType, Values: []asn1.RawValue{contentType}})
	}
	if options.AlsoWrongV1Binding {
		wrong := sha1.Sum(a.Root.Raw)
		v1, err := rawValue(signingCertificateV1{Certs: []essCertID{{CertHash: wrong[:]}}})
		if err != nil {
			return nil, err
		}
		attributeList = append(attributeList, attribute{Type: oidSigningCertificate, Values: []asn1.RawValue{v1}})
	}
	attributes, err := encodeAttributes(attributeList, options)
	if err != nil {
		return nil, err
	}
	signedOver := attributes
	if options.NoSignedAttributes {
		signedOver = content
	}
	signature, err := a.signerKey.Sign(rand.Reader, sum(signedOver), digestHash)
	if err != nil {
		return nil, err
	}
	signatureAlgorithm := signatureAlgorithmOf(options)
	signerVersion := 1
	if options.SignerInfoVersion != 0 {
		signerVersion = options.SignerInfoVersion
	}
	info := signerInfo{
		Version:            signerVersion,
		SID:                issuerAndSerial{Issuer: asn1.RawValue{FullBytes: a.Signer.RawIssuer}, Serial: a.Signer.SerialNumber},
		DigestAlgorithm:    withParameters(algorithmIdentifier{Algorithm: digestAlgorithm}, options.SignerDigestParameters),
		SignedAttrs:        asn1.RawValue{FullBytes: append([]byte{0xa0}, attributes[1:]...)},
		SignatureAlgorithm: withParameters(algorithmIdentifier{Algorithm: signatureAlgorithm}, options.SignatureParameters),
		Signature:          signature,
	}
	if options.UnsignedAttributeValue != nil {
		unsigned, err := asn1.MarshalWithParams([]attribute{{Type: asn1.ObjectIdentifier{1, 2, 3, 4}, Values: []asn1.RawValue{{FullBytes: options.UnsignedAttributeValue}}}}, "set")
		if err != nil {
			return nil, err
		}
		info.UnsignedAttrs = asn1.RawValue{FullBytes: append([]byte{0xa1}, unsigned[1:]...)}
	}
	if options.WrongSID {
		info.SID.Serial = new(big.Int).Add(a.Signer.SerialNumber, big.NewInt(100))
	}
	infos := []signerInfo{info}
	if options.ExtraSigner {
		infos = append(infos, info)
	}
	var signed []byte
	// The certificate set in DER order, as a SET OF is.
	pair := [][]byte{a.Root.Raw, a.Signer.Raw}
	sort.Slice(pair, func(i, j int) bool { return bytes.Compare(pair[i], pair[j]) < 0 })
	certificates := bytes.Join(pair, nil)
	if options.OmitCertificates {
		certificates = []byte{}
	}
	encapsulatedType := oidTSTInfo
	if options.WrongEncapsulatedType {
		encapsulatedType = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	}
	encapsulatedContent := encapsulated{EContentType: encapsulatedType, EContent: explicit(0, mustOctets(content))}
	certificateSet := asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: certificates}
	digestAlgorithms := []algorithmIdentifier{withParameters(algorithmIdentifier{Algorithm: digestAlgorithm}, options.DigestAlgorithmsParameters)}
	switch options.DigestAlgorithms {
	case "empty":
		digestAlgorithms = []algorithmIdentifier{}
	case "sha384":
		digestAlgorithms = []algorithmIdentifier{{Algorithm: oidSHA384}}
	}
	signedVersion := 3
	if options.SignedDataVersion != 0 {
		signedVersion = options.SignedDataVersion
	}
	if options.NoSignedAttributes {
		signed, err = asn1.Marshal(signedDataBare{
			Version: 3, DigestAlgorithms: digestAlgorithms, EncapContentInfo: encapsulatedContent, Certificates: certificateSet,
			SignerInfos: []signerInfoBare{{Version: 1, SID: info.SID, DigestAlgorithm: info.DigestAlgorithm, SignatureAlgorithm: info.SignatureAlgorithm, Signature: signature}},
		})
	} else if options.ExtraSignedDataField {
		signed, err = asn1.Marshal(signedDataExtra{
			Version: signedVersion, DigestAlgorithms: digestAlgorithms, EncapContentInfo: encapsulatedContent, Certificates: certificateSet, SignerInfos: infos, Extra: 1,
		})
	} else {
		var crls asn1.RawValue
		if options.EmbedCRL {
			crls = asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 1, IsCompound: true, Bytes: []byte{0x30, 0x00}}
		}
		signed, err = asn1.Marshal(signedData{
			Version: signedVersion, DigestAlgorithms: digestAlgorithms, EncapContentInfo: encapsulatedContent, Certificates: certificateSet, CRLs: crls, SignerInfos: infos,
		})
	}
	if err != nil {
		return nil, err
	}
	contentInfoType := oidSignedData
	if options.WrongContentInfoType {
		contentInfoType = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	}
	if options.ExtraContentInfoField {
		return asn1.Marshal(contentInfoExtra{ContentType: contentInfoType, Content: explicit(0, signed), Extra: 1})
	}
	return asn1.Marshal(contentInfo{ContentType: contentInfoType, Content: explicit(0, signed)})
}

type contentInfoExtra struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue
	Extra       int
}

type signedDataExtra struct {
	Version          int
	DigestAlgorithms []algorithmIdentifier `asn1:"set"`
	EncapContentInfo encapsulated
	Certificates     asn1.RawValue
	SignerInfos      []signerInfo `asn1:"set"`
	Extra            int
}

type attributeSequence struct {
	Type   asn1.ObjectIdentifier
	Values []asn1.RawValue
}

// encodeAttributes is the SET OF the signed attributes: in DER order, as
// encoding/asn1 sorts it, unless the options ask for its values as a
// SEQUENCE or for the set out of order.
func encodeAttributes(list []attribute, options Options) ([]byte, error) {
	if !options.AttributeValuesAsSequence && !options.UnsortedAttributes {
		return asn1.MarshalWithParams(list, "set")
	}
	elements := [][]byte{}
	for _, each := range list {
		var element []byte
		var err error
		if options.AttributeValuesAsSequence {
			element, err = asn1.Marshal(attributeSequence{Type: each.Type, Values: each.Values})
		} else {
			element, err = asn1.Marshal(each)
		}
		if err != nil {
			return nil, err
		}
		elements = append(elements, element)
	}
	if options.UnsortedAttributes {
		sort.Slice(elements, func(i, j int) bool { return bytes.Compare(elements[i], elements[j]) > 0 })
	}
	return asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: bytes.Join(elements, nil)})
}

// signerInfoBare and signedDataBare are a SignerInfo without signed
// attributes, and the SignedData that carries one.
type signerInfoBare struct {
	Version            int
	SID                issuerAndSerial
	DigestAlgorithm    algorithmIdentifier
	SignatureAlgorithm algorithmIdentifier
	Signature          []byte
}

type signedDataBare struct {
	Version          int
	DigestAlgorithms []algorithmIdentifier `asn1:"set"`
	EncapContentInfo encapsulated
	Certificates     asn1.RawValue
	SignerInfos      []signerInfoBare `asn1:"set"`
}

// mustOctets is DER bytes as an OCTET STRING.
func mustOctets(content []byte) []byte {
	der, _ := asn1.Marshal(content)
	return der
}

type timeStampReq struct {
	Version        int
	MessageImprint imprint
	ReqPolicy      asn1.ObjectIdentifier `asn1:"optional"`
	Nonce          *big.Int              `asn1:"optional"`
	CertReq        bool                  `asn1:"optional,default:false"`
}

type statusInfo struct {
	Status int
	Text   []asn1.RawValue `asn1:"optional"`
}

type statusInfoExtra struct {
	Status int
	Extra  int
}

type replyExtra struct {
	Status statusInfoExtra
	Token  asn1.RawValue `asn1:"optional"`
}

type reply struct {
	Status statusInfo
	Token  asn1.RawValue `asn1:"optional"`
}

// Reply answers a request as an authority does: the token over the request's
// digest, with its nonce, in a granted reply, or a rejection when Status says.
func (a *Authority) Reply(request []byte) ([]byte, error) {
	var parsed timeStampReq
	if rest, err := asn1.Unmarshal(request, &parsed); err != nil || len(rest) != 0 {
		return nil, errors.New("tsatest: not a time-stamp request")
	}
	if a.Status != 0 {
		return asn1.Marshal(reply{Status: statusInfo{Status: a.Status}})
	}
	digest, nonce := parsed.MessageImprint.HashedMessage, parsed.Nonce
	if a.WrongDigest {
		sum := sha256.Sum256(digest)
		digest = sum[:]
	}
	if a.WrongNonce && nonce != nil {
		nonce = new(big.Int).Add(nonce, big.NewInt(1))
	}
	token, err := a.Token(digest, nonce)
	if err != nil {
		return nil, err
	}
	if a.StatusExtra {
		return asn1.Marshal(replyExtra{Status: statusInfoExtra{Status: 0, Extra: 1}, Token: asn1.RawValue{FullBytes: token}})
	}
	status := statusInfo{Status: 0}
	if a.StatusText {
		status.Text = []asn1.RawValue{{Class: asn1.ClassUniversal, Tag: asn1.TagUTF8String, Bytes: []byte("granted")}}
	}
	if a.StatusTextPrintable {
		status.Text = []asn1.RawValue{{Class: asn1.ClassUniversal, Tag: asn1.TagPrintableString, Bytes: []byte("granted")}}
	}
	return asn1.Marshal(reply{Status: status, Token: asn1.RawValue{FullBytes: token}})
}

// ServeHTTP answers RFC 3161 requests over HTTP, as a test server's handler.
func (a *Authority) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	a.mutex.Lock()
	a.Requests++
	fail := a.Fail
	a.mutex.Unlock()
	if fail != 0 {
		http.Error(writer, "refused", fail)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 1<<16))
	if err != nil || request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/timestamp-query" {
		http.Error(writer, "bad request", http.StatusBadRequest)
		return
	}
	answer, err := a.Reply(body)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	writer.Header().Set("Content-Type", "application/timestamp-reply")
	_, _ = io.Copy(writer, bytes.NewReader(answer))
}
