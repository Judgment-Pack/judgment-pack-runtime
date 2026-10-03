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
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
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
	GenTime        time.Time `asn1:"generalized"`
	Accuracy       accuracy  `asn1:"optional"`
	Nonce          *big.Int  `asn1:"optional"`
}

type attribute struct {
	Type   asn1.ObjectIdentifier
	Values []asn1.RawValue `asn1:"set"`
}

type essCertIDv2 struct {
	CertHash []byte
}

type signingCertificateV2 struct {
	Certs []essCertIDv2
}

type essCertID struct {
	CertHash []byte
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
	SignerInfos      []signerInfo `asn1:"set"`
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
		MessageImprint: imprint{HashAlgorithm: algorithmIdentifier{Algorithm: imprintAlgorithm, Parameters: asn1.NullRawValue}, HashedMessage: digest},
		SerialNumber:   big.NewInt(serial),
		GenTime:        now.UTC().Truncate(time.Second),
		Accuracy:       accuracy{Seconds: options.AccuracySeconds, Millis: options.AccuracyMillis},
		Nonce:          nonce,
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
	if options.SigningCertificateV1 {
		certHash := sha1.Sum(bound)
		certificateAttribute = oidSigningCertificate
		signingCertificate, err = rawValue(signingCertificateV1{Certs: []essCertID{{CertHash: certHash[:]}}})
	} else {
		certHash := sha256.Sum256(bound)
		signingCertificate, err = rawValue(signingCertificateV2{Certs: []essCertIDv2{{CertHash: certHash[:]}}})
	}
	if err != nil {
		return nil, err
	}
	attributeList := []attribute{
		{Type: oidContentType, Values: []asn1.RawValue{contentType}},
		{Type: oidMessageDigest, Values: []asn1.RawValue{messageDigest}},
		{Type: certificateAttribute, Values: []asn1.RawValue{signingCertificate}},
	}
	if options.DuplicateAttribute {
		attributeList = append(attributeList, attribute{Type: oidContentType, Values: []asn1.RawValue{contentType}})
	}
	attributes, err := asn1.MarshalWithParams(attributeList, "set")
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
	signatureAlgorithm := oidECDSAWithSHA256
	if options.SignatureAlgorithmMismatch {
		signatureAlgorithm = oidECDSAWithSHA384
	}
	if options.SHA1Digest {
		signatureAlgorithm = oidECPublicKey
	}
	info := signerInfo{
		Version:            1,
		SID:                issuerAndSerial{Issuer: asn1.RawValue{FullBytes: a.Signer.RawIssuer}, Serial: a.Signer.SerialNumber},
		DigestAlgorithm:    algorithmIdentifier{Algorithm: digestAlgorithm},
		SignedAttrs:        asn1.RawValue{FullBytes: append([]byte{0xa0}, attributes[1:]...)},
		SignatureAlgorithm: algorithmIdentifier{Algorithm: signatureAlgorithm},
		Signature:          signature,
	}
	if options.WrongSID {
		info.SID.Serial = new(big.Int).Add(a.Signer.SerialNumber, big.NewInt(100))
	}
	infos := []signerInfo{info}
	if options.ExtraSigner {
		infos = append(infos, info)
	}
	var signed []byte
	certificates := append(bytes.Clone(a.Root.Raw), a.Signer.Raw...)
	if options.OmitCertificates {
		certificates = []byte{}
	}
	encapsulatedType := oidTSTInfo
	if options.WrongEncapsulatedType {
		encapsulatedType = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	}
	encapsulatedContent := encapsulated{EContentType: encapsulatedType, EContent: explicit(0, mustOctets(content))}
	certificateSet := asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: certificates}
	digestAlgorithms := []algorithmIdentifier{{Algorithm: digestAlgorithm}}
	if options.NoSignedAttributes {
		signed, err = asn1.Marshal(signedDataBare{
			Version: 3, DigestAlgorithms: digestAlgorithms, EncapContentInfo: encapsulatedContent, Certificates: certificateSet,
			SignerInfos: []signerInfoBare{{Version: 1, SID: info.SID, DigestAlgorithm: info.DigestAlgorithm, SignatureAlgorithm: info.SignatureAlgorithm, Signature: signature}},
		})
	} else {
		signed, err = asn1.Marshal(signedData{
			Version: 3, DigestAlgorithms: digestAlgorithms, EncapContentInfo: encapsulatedContent, Certificates: certificateSet, SignerInfos: infos,
		})
	}
	if err != nil {
		return nil, err
	}
	contentInfoType := oidSignedData
	if options.WrongContentInfoType {
		contentInfoType = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	}
	return asn1.Marshal(contentInfo{ContentType: contentInfoType, Content: explicit(0, signed)})
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
	return asn1.Marshal(reply{Status: statusInfo{Status: 0}, Token: asn1.RawValue{FullBytes: token}})
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
