// Package timestamp is the part of RFC 3161, and of the RFC 5652 SignedData
// that carries its token, that this runtime needs: to ask a time-stamping
// authority to stamp a digest, and to verify the token it returns, offline,
// against roots the verifier supplies (ADR-0047 §2a, C2).
//
// It is written on the standard library's encoding/asn1 and crypto/x509 rather
// than on a CMS library, for a verifier whose answer is a claim: the subset is
// small and fixed, and every rule it holds a token to is here to read. A token
// is read in DER only, and is held to:
//
//   - a ContentInfo of SignedData, carrying exactly one SignerInfo and an
//     encapsulated TSTInfo (version 1);
//   - signed attributes that name the TSTInfo content type, carry the digest of
//     the encapsulated TSTInfo, and bind the signing certificate by its hash
//     (ESS signingCertificate or signingCertificateV2);
//   - a signature over those attributes, by the certificate the SignerInfo
//     names among the token's own certificates, in RSA PKCS #1 v1.5 or ECDSA
//     with SHA-256, SHA-384 or SHA-512.
//
// Verify then holds the signing certificate to the RFC 3161 usage (only
// id-kp-timeStamping, critical), its chain to a supplied root at the time the
// token states, its policy to the ones supplied, and, where certificate
// revocation lists are supplied that can speak for that time, its status.
package timestamp

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"reflect"
	"regexp"
	"time"

	// The hashes a token may name, registered for crypto.Hash.New.
	_ "crypto/sha256"
	_ "crypto/sha512"
)

var (
	oidSignedData           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidTSTInfo              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}
	oidContentType          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidMessageDigest        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSigningCertificate   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 12}
	oidSigningCertificateV2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}
	// OIDSHA256 names SHA-256, the one hash a message imprint is taken with.
	OIDSHA256          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	oidRSAEncryption   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidSHA256WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidSHA384WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	oidSHA512WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	oidECPublicKey     = asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
	oidECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidECDSAWithSHA384 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	oidECDSAWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}
	oidExtKeyUsage     = asn1.ObjectIdentifier{2, 5, 29, 37}
)

// The ways a token fails. They are distinct so a report can name which check
// failed.
var (
	// ErrMalformed is a token, or a reply, not of the shape this package
	// reads.
	ErrMalformed = errors.New("the time-stamp token is not of the shape RFC 3161 gives it")
	// ErrSignature is a token whose signature, or signed attributes, do not
	// hold for its own content and signing certificate.
	ErrSignature = errors.New("the time-stamp token's signature does not hold")
	// ErrUsage is a signing certificate not for time-stamping alone, by a
	// critical extended key usage.
	ErrUsage = errors.New("the time-stamping certificate is not for time-stamping alone")
	// ErrUntrusted is a signing certificate that does not chain to a supplied
	// root at the time the token states.
	ErrUntrusted = errors.New("the time-stamping certificate does not chain to a supplied root at the stamp's time")
	// ErrPolicy is a token under a policy other than the ones supplied.
	ErrPolicy = errors.New("the time-stamp token's policy is not one supplied")
	// ErrRevoked is a certificate a supplied revocation list shows revoked at
	// or before the stamp's time, or for a compromised key at any time.
	ErrRevoked = errors.New("a certificate of the time-stamp token was revoked as of the stamp's time")
	// ErrRejected is a reply whose status refuses the request.
	ErrRejected = errors.New("the time-stamping authority refused the request")
)

// messageImprint is a digest and the hash it was taken with.
type messageImprint struct {
	HashAlgorithm pkix.AlgorithmIdentifier
	HashedMessage []byte
}

// timeStampReq is RFC 3161's request, without extensions.
type timeStampReq struct {
	Version        int
	MessageImprint messageImprint
	ReqPolicy      asn1.ObjectIdentifier `asn1:"optional"`
	Nonce          *big.Int              `asn1:"optional"`
	CertReq        bool                  `asn1:"optional,default:false"`
}

// Request is the DER of a request for a stamp over a SHA-256 digest, with a
// nonce and a request for the authority's certificate in the token, so the
// token can be verified offline.
func Request(digest []byte, nonce *big.Int) ([]byte, error) {
	if len(digest) != 32 {
		return nil, errors.New("a stamp is asked for a SHA-256 digest")
	}
	return asn1.Marshal(timeStampReq{
		Version: 1,
		MessageImprint: messageImprint{
			HashAlgorithm: pkix.AlgorithmIdentifier{Algorithm: OIDSHA256, Parameters: asn1.NullRawValue},
			HashedMessage: digest,
		},
		Nonce:   nonce,
		CertReq: true,
	})
}

type timeStampResp struct {
	Status         asn1.RawValue
	TimeStampToken asn1.RawValue `asn1:"optional"`
}

// ParseResponse reads a reply and answers the token in it: the reply must be
// DER with nothing after it, its status granted (0) or granted with
// modifications (1), and a token present.
func ParseResponse(reply []byte) ([]byte, error) {
	var response timeStampResp
	rest, err := asn1.Unmarshal(reply, &response)
	if err != nil || len(rest) != 0 {
		return nil, ErrMalformed
	}
	// The status's text and failure bits follow its number; encoding/asn1
	// passes over elements after the last field, and nothing here reads them.
	var status struct {
		Status int
	}
	if _, err := asn1.Unmarshal(response.Status.FullBytes, &status); err != nil {
		return nil, ErrMalformed
	}
	if status.Status != 0 && status.Status != 1 {
		return nil, fmt.Errorf("%w (status %d)", ErrRejected, status.Status)
	}
	if len(response.TimeStampToken.FullBytes) == 0 {
		return nil, ErrMalformed
	}
	return response.TimeStampToken.FullBytes, nil
}

// strictly unmarshals der into value, which must be a pointer to a struct or
// slice of fixed shape, and holds der to DER exactly: nothing after it, and
// encoding/asn1's own encoding of what was read must be der byte for byte.
// That one rule refuses what encoding/asn1 alone would read past: an element
// after the last field of a SEQUENCE, a non-minimal or indefinite length, a
// DEFAULT value encoded, and a SET OF out of DER order. Every field that must
// be read whole and kept as given is a RawValue, which encodes as it was read.
func strictly(der []byte, value any, params string) error {
	rest, err := asn1.UnmarshalWithParams(der, value, params)
	if err != nil || len(rest) != 0 {
		return ErrMalformed
	}
	again, err := asn1.MarshalWithParams(reflect.ValueOf(value).Elem().Interface(), params)
	if err != nil || !bytes.Equal(again, der) {
		return fmt.Errorf("%w: it is not in DER", ErrMalformed)
	}
	return nil
}

// contentInfo keeps its [0] EXPLICIT content as one RawValue, read as given.
type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"tag:0"`
}

type encapsulatedContentInfo struct {
	EContentType asn1.ObjectIdentifier
	EContent     []byte `asn1:"explicit,optional,tag:0"`
}

type signedData struct {
	Version          int
	DigestAlgorithms []pkix.AlgorithmIdentifier `asn1:"set"`
	EncapContentInfo encapsulatedContentInfo
	Certificates     []asn1.RawValue `asn1:"optional,set,tag:0"`
	CRLs             asn1.RawValue   `asn1:"optional,tag:1"`
	SignerInfos      []signerInfo    `asn1:"set"`
}

type signerInfo struct {
	Version            int
	SID                asn1.RawValue
	DigestAlgorithm    pkix.AlgorithmIdentifier
	SignedAttrs        asn1.RawValue `asn1:"optional,tag:0"`
	SignatureAlgorithm pkix.AlgorithmIdentifier
	Signature          []byte
	UnsignedAttrs      asn1.RawValue `asn1:"optional,tag:1"`
}

type issuerAndSerialNumber struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

type attribute struct {
	Type   asn1.ObjectIdentifier
	Values []asn1.RawValue `asn1:"set"`
}

type essCertID struct {
	CertHash     []byte
	IssuerSerial asn1.RawValue `asn1:"optional"`
}

type signingCertificate struct {
	Certs    []essCertID
	Policies asn1.RawValue `asn1:"optional"`
}

type essCertIDv2 struct {
	HashAlgorithm pkix.AlgorithmIdentifier `asn1:"optional"`
	CertHash      []byte
	IssuerSerial  asn1.RawValue `asn1:"optional"`
}

type signingCertificateV2 struct {
	Certs    []essCertIDv2
	Policies asn1.RawValue `asn1:"optional"`
}

// accuracy keeps each of its parts as given, a zero among them, so it
// encodes as it was read.
type accuracy struct {
	Seconds *big.Int `asn1:"optional"`
	Millis  *big.Int `asn1:"optional,tag:0"`
	Micros  *big.Int `asn1:"optional,tag:1"`
}

// tstInfo keeps its genTime as given and reads it apart (parseGenTime):
// encoding/asn1 writes a GeneralizedTime without its fraction, so its own
// encoding of a time with one would not be the bytes read.
type tstInfo struct {
	Version        int
	Policy         asn1.ObjectIdentifier
	MessageImprint messageImprint
	SerialNumber   *big.Int
	GenTime        asn1.RawValue
	Accuracy       accuracy         `asn1:"optional"`
	Ordering       bool             `asn1:"optional,default:false"`
	Nonce          *big.Int         `asn1:"optional"`
	TSA            asn1.RawValue    `asn1:"optional,tag:0"`
	Extensions     []pkix.Extension `asn1:"optional,tag:1"`
}

// Token is a time-stamp token whose signature has been checked against its
// own signing certificate. Nothing about whether that certificate is to be
// trusted is known until Verify.
type Token struct {
	// GenTime is the time the authority states, and Accuracy how far off it
	// says that time may be: the content existed by GenTime + Accuracy.
	GenTime  time.Time
	Accuracy time.Duration
	Policy   asn1.ObjectIdentifier
	// HashedMessage is the digest stamped, taken with HashAlgorithm.
	HashAlgorithm asn1.ObjectIdentifier
	HashedMessage []byte
	Nonce         *big.Int
	SerialNumber  *big.Int
	// Signer is the certificate that signed, and Certificates every
	// certificate the token carries, the signer among them.
	Signer       *x509.Certificate
	Certificates []*x509.Certificate
}

// ExistedBy is the latest time the token attests its content existed by: the
// stated time and its stated accuracy.
func (t *Token) ExistedBy() time.Time { return t.GenTime.Add(t.Accuracy) }

// Parse reads a token, DER, and checks its signature against the signing
// certificate it carries. Every structure is held to DER exactly (strictly),
// and to the subset read here:
//
//   - a ContentInfo of SignedData, version 3, whose digest algorithms name the
//     signer's, carrying an encapsulated TSTInfo, version 1, with no critical
//     extension, a genTime in DER's GeneralizedTime form, and an accuracy
//     that a time.Duration holds;
//   - exactly one SignerInfo, version 1 naming its certificate by issuer and
//     serial number or version 3 naming it by subject key identifier, the
//     certificate among the token's own;
//   - signed attributes, one value each for those read, naming the TSTInfo
//     content type, carrying the digest of the encapsulated TSTInfo, and
//     binding the signing certificate by its hash in every ESS binding
//     present (signingCertificate, signingCertificateV2), at least one;
//   - a signature over those attributes that verifies.
//
// Whether the certificate is to be trusted is Verify's.
func Parse(der []byte) (*Token, error) {
	var info contentInfo
	if err := strictly(der, &info, ""); err != nil {
		return nil, err
	}
	if !info.ContentType.Equal(oidSignedData) || info.Content.Class != asn1.ClassContextSpecific || !info.Content.IsCompound {
		return nil, ErrMalformed
	}
	var signed signedData
	if err := strictly(info.Content.Bytes, &signed, ""); err != nil {
		return nil, err
	}
	if signed.Version != 3 || !signed.EncapContentInfo.EContentType.Equal(oidTSTInfo) || len(signed.EncapContentInfo.EContent) == 0 || len(signed.SignerInfos) != 1 {
		return nil, ErrMalformed
	}
	signer := signed.SignerInfos[0]
	declared := false
	for _, algorithm := range signed.DigestAlgorithms {
		declared = declared || algorithm.Algorithm.Equal(signer.DigestAlgorithm.Algorithm)
	}
	if !declared {
		return nil, fmt.Errorf("%w: the signer's digest algorithm is not among the SignedData's", ErrMalformed)
	}
	var content tstInfo
	if err := strictly(signed.EncapContentInfo.EContent, &content, ""); err != nil {
		return nil, err
	}
	if content.Version != 1 {
		return nil, ErrMalformed
	}
	for _, extension := range content.Extensions {
		if extension.Critical {
			return nil, fmt.Errorf("%w: the TSTInfo has a critical extension", ErrMalformed)
		}
	}
	genTime, err := parseGenTime(content.GenTime)
	if err != nil {
		return nil, err
	}
	accuracy, err := durationOf(content.Accuracy)
	if err != nil {
		return nil, err
	}
	certificates, err := parseCertificates(signed.Certificates)
	if err != nil {
		return nil, err
	}
	certificate, err := findSigner(signer, certificates)
	if err != nil {
		return nil, err
	}
	if err := checkSignedAttributes(signer, signed.EncapContentInfo.EContent, certificate); err != nil {
		return nil, err
	}
	return &Token{
		GenTime:       genTime,
		Accuracy:      accuracy,
		Policy:        content.Policy,
		HashAlgorithm: content.MessageImprint.HashAlgorithm.Algorithm,
		HashedMessage: content.MessageImprint.HashedMessage,
		Nonce:         content.Nonce,
		SerialNumber:  content.SerialNumber,
		Signer:        certificate,
		Certificates:  certificates,
	}, nil
}

// generalizedTimeForm is DER's GeneralizedTime: to the second, in UTC, with a
// fraction only when it is not zero and with no trailing zero.
var generalizedTimeForm = regexp.MustCompile(`^[0-9]{14}(\.[0-9]*[1-9])?Z$`)

// parseGenTime reads a TSTInfo's genTime, DER's GeneralizedTime.
func parseGenTime(raw asn1.RawValue) (time.Time, error) {
	if raw.Class != asn1.ClassUniversal || raw.Tag != asn1.TagGeneralizedTime || raw.IsCompound || !generalizedTimeForm.Match(raw.Bytes) {
		return time.Time{}, fmt.Errorf("%w: the genTime is not a DER GeneralizedTime", ErrMalformed)
	}
	layout := "20060102150405Z"
	if bytes.IndexByte(raw.Bytes, '.') >= 0 {
		layout = "20060102150405.999999999Z"
	}
	parsed, err := time.Parse(layout, string(raw.Bytes))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: the genTime is not a time", ErrMalformed)
	}
	return parsed, nil
}

// durationOf is an accuracy as a time.Duration: each part not negative,
// milliseconds and microseconds at most 999, and the whole within what a
// time.Duration holds, or the token is refused rather than given a time that
// overflowed.
func durationOf(stated accuracy) (time.Duration, error) {
	total := new(big.Int)
	for _, part := range []struct {
		value *big.Int
		unit  int64
		max   int64
	}{{stated.Seconds, int64(time.Second), -1}, {stated.Millis, int64(time.Millisecond), 999}, {stated.Micros, int64(time.Microsecond), 999}} {
		if part.value == nil {
			continue
		}
		if part.value.Sign() < 0 || (part.max >= 0 && part.value.Cmp(big.NewInt(part.max)) > 0) {
			return 0, fmt.Errorf("%w: the accuracy is out of range", ErrMalformed)
		}
		total.Add(total, new(big.Int).Mul(part.value, big.NewInt(part.unit)))
	}
	if !total.IsInt64() {
		return 0, fmt.Errorf("%w: the accuracy is longer than a time span this runtime holds", ErrMalformed)
	}
	return time.Duration(total.Int64()), nil
}

// parseCertificates reads the token's certificates: each must be an X.509
// certificate, the one choice of CertificateChoices read here.
func parseCertificates(raw []asn1.RawValue) ([]*x509.Certificate, error) {
	certificates := []*x509.Certificate{}
	for _, element := range raw {
		if element.Class != asn1.ClassUniversal || element.Tag != asn1.TagSequence {
			return nil, ErrMalformed
		}
		certificate, err := x509.ParseCertificate(element.FullBytes)
		if err != nil {
			return nil, ErrMalformed
		}
		certificates = append(certificates, certificate)
	}
	return certificates, nil
}

// findSigner is the certificate a SignerInfo names, held to the version its
// way of naming takes: version 1 by issuer and serial number, version 3 by
// subject key identifier.
func findSigner(signer signerInfo, certificates []*x509.Certificate) (*x509.Certificate, error) {
	sid := signer.SID
	switch {
	case sid.Class == asn1.ClassUniversal && sid.Tag == asn1.TagSequence && signer.Version == 1:
		var named issuerAndSerialNumber
		if err := strictly(sid.FullBytes, &named, ""); err != nil || named.Serial == nil {
			return nil, ErrMalformed
		}
		for _, certificate := range certificates {
			if bytes.Equal(certificate.RawIssuer, named.Issuer.FullBytes) && certificate.SerialNumber.Cmp(named.Serial) == 0 {
				return certificate, nil
			}
		}
	case sid.Class == asn1.ClassContextSpecific && sid.Tag == 0 && !sid.IsCompound && signer.Version == 3:
		for _, certificate := range certificates {
			if len(certificate.SubjectKeyId) > 0 && bytes.Equal(certificate.SubjectKeyId, sid.Bytes) {
				return certificate, nil
			}
		}
	default:
		return nil, fmt.Errorf("%w: the SignerInfo's version is not the one its identifier takes", ErrMalformed)
	}
	return nil, fmt.Errorf("%w: the token does not carry the certificate that signed it", ErrMalformed)
}

// hashOf is the hash an algorithm identifier names, among those read here.
func hashOf(algorithm asn1.ObjectIdentifier) (crypto.Hash, bool) {
	switch {
	case algorithm.Equal(OIDSHA256):
		return crypto.SHA256, true
	case algorithm.Equal(oidSHA384):
		return crypto.SHA384, true
	case algorithm.Equal(oidSHA512):
		return crypto.SHA512, true
	}
	return 0, false
}

// singleValue is an attribute's one value, read strictly into value.
func singleValue(attr attribute, value any) error {
	if len(attr.Values) != 1 {
		return fmt.Errorf("%w: a signed attribute read here has more than one value", ErrMalformed)
	}
	return strictly(attr.Values[0].FullBytes, value, "")
}

// checkSignedAttributes holds a SignerInfo's signed attributes to RFC 3161 and
// RFC 5652 and verifies the signature over them.
func checkSignedAttributes(signer signerInfo, content []byte, certificate *x509.Certificate) error {
	if len(signer.SignedAttrs.FullBytes) == 0 || signer.SignedAttrs.Class != asn1.ClassContextSpecific || !signer.SignedAttrs.IsCompound {
		return fmt.Errorf("%w: the token has no signed attributes", ErrMalformed)
	}
	digestHash, ok := hashOf(signer.DigestAlgorithm.Algorithm)
	if !ok {
		return fmt.Errorf("%w: the token's digest algorithm is not one read here", ErrMalformed)
	}
	// The signature is over the attributes as a SET OF, not as the [0]
	// IMPLICIT that carries them: the tag is the one byte that differs.
	encoded := append([]byte{0x31}, signer.SignedAttrs.FullBytes[1:]...)
	var attributes []attribute
	if err := strictly(encoded, &attributes, "set"); err != nil {
		return err
	}
	seen := map[string]bool{}
	var contentTypeOK, digestOK bool
	bindings, bound := 0, 0
	for _, attr := range attributes {
		key := attr.Type.String()
		if seen[key] {
			return fmt.Errorf("%w: a signed attribute is given twice", ErrMalformed)
		}
		seen[key] = true
		switch {
		case attr.Type.Equal(oidContentType):
			var value asn1.ObjectIdentifier
			if err := singleValue(attr, &value); err != nil {
				return err
			}
			contentTypeOK = value.Equal(oidTSTInfo)
		case attr.Type.Equal(oidMessageDigest):
			var value []byte
			if err := singleValue(attr, &value); err != nil {
				return err
			}
			sum := digestHash.New()
			sum.Write(content)
			digestOK = bytes.Equal(value, sum.Sum(nil))
		case attr.Type.Equal(oidSigningCertificate):
			var value signingCertificate
			if err := singleValue(attr, &value); err != nil || len(value.Certs) == 0 {
				return ErrMalformed
			}
			bindings++
			sum := sha1.Sum(certificate.Raw)
			if bytes.Equal(value.Certs[0].CertHash, sum[:]) {
				bound++
			}
		case attr.Type.Equal(oidSigningCertificateV2):
			var value signingCertificateV2
			if err := singleValue(attr, &value); err != nil || len(value.Certs) == 0 {
				return ErrMalformed
			}
			certHash := crypto.SHA256
			if algorithm := value.Certs[0].HashAlgorithm.Algorithm; len(algorithm) > 0 {
				// SHA-256 is the DEFAULT, which DER leaves out.
				if certHash, ok = hashOf(algorithm); !ok || certHash == crypto.SHA256 {
					return fmt.Errorf("%w: the signing certificate's hash is not one read here, or is the default written out", ErrMalformed)
				}
			}
			bindings++
			sum := certHash.New()
			sum.Write(certificate.Raw)
			if bytes.Equal(value.Certs[0].CertHash, sum.Sum(nil)) {
				bound++
			}
		}
	}
	switch {
	case !contentTypeOK:
		return fmt.Errorf("%w: the signed content type is not a TSTInfo", ErrSignature)
	case !digestOK:
		return fmt.Errorf("%w: the signed digest is not the digest of the TSTInfo", ErrSignature)
	case bindings == 0 || bound != bindings:
		return fmt.Errorf("%w: the signed attributes do not bind the certificate that signed, in every binding they carry", ErrSignature)
	}
	sum := digestHash.New()
	sum.Write(encoded)
	if err := verifySignature(certificate, signer.SignatureAlgorithm.Algorithm, digestHash, sum.Sum(nil), signer.Signature); err != nil {
		return err
	}
	return nil
}

// verifySignature verifies a signature over a digest with a certificate's key,
// by the signature algorithm the SignerInfo names, which must agree with the
// digest's hash.
func verifySignature(certificate *x509.Certificate, algorithm asn1.ObjectIdentifier, digestHash crypto.Hash, digest, signature []byte) error {
	named := map[string]crypto.Hash{
		oidSHA256WithRSA.String(): crypto.SHA256, oidSHA384WithRSA.String(): crypto.SHA384, oidSHA512WithRSA.String(): crypto.SHA512,
		oidECDSAWithSHA256.String(): crypto.SHA256, oidECDSAWithSHA384.String(): crypto.SHA384, oidECDSAWithSHA512.String(): crypto.SHA512,
	}
	if want, ok := named[algorithm.String()]; ok && want != digestHash {
		return fmt.Errorf("%w: the signature algorithm's hash is not the digest's", ErrSignature)
	}
	switch key := certificate.PublicKey.(type) {
	case *rsa.PublicKey:
		if !algorithm.Equal(oidRSAEncryption) && !algorithm.Equal(oidSHA256WithRSA) && !algorithm.Equal(oidSHA384WithRSA) && !algorithm.Equal(oidSHA512WithRSA) {
			return fmt.Errorf("%w: the signature algorithm is not RSA PKCS #1 v1.5", ErrMalformed)
		}
		if rsa.VerifyPKCS1v15(key, digestHash, digest, signature) != nil {
			return ErrSignature
		}
	case *ecdsa.PublicKey:
		if !algorithm.Equal(oidECPublicKey) && !algorithm.Equal(oidECDSAWithSHA256) && !algorithm.Equal(oidECDSAWithSHA384) && !algorithm.Equal(oidECDSAWithSHA512) {
			return fmt.Errorf("%w: the signature algorithm is not ECDSA", ErrMalformed)
		}
		if !ecdsa.VerifyASN1(key, digest, signature) {
			return ErrSignature
		}
	default:
		return fmt.Errorf("%w: the signing key is neither RSA nor ECDSA", ErrMalformed)
	}
	return nil
}

// Revocation is what supplied revocation lists show about a token's chain.
type Revocation string

// The revocation statuses.
const (
	// RevocationChecked is every certificate of the chain but its root
	// covered by a supplied revocation list that speaks for the stamp's time
	// and does not list it.
	RevocationChecked Revocation = "checked"
	// RevocationNotChecked is at least one certificate of the chain no
	// supplied list speaks for: its status at the stamp's time is not known.
	RevocationNotChecked Revocation = "not-checked"
)

// VerifyOptions is what a token is held to: the roots its signing certificate
// must chain to, the policies it may be under (any, when none), and the
// revocation lists the verifier holds.
type VerifyOptions struct {
	Roots    *x509.CertPool
	Policies []asn1.ObjectIdentifier
	CRLs     []*x509.RevocationList
}

// Verify holds a parsed token to its trust:
//
//   - its signing certificate carries the extended key usage extension,
//     critical, with id-kp-timeStamping as its only purpose (RFC 3161 §2.3);
//   - that certificate chains to a supplied root, through the certificates
//     the token carries, with every certificate valid at the token's GenTime
//     and the chain allowing time-stamping;
//   - its policy is one supplied, when any is;
//   - and, for every certificate of the chain but its root, a supplied
//     revocation list issued by that certificate's issuer, signed by it,
//     issued at or after GenTime and while the certificate was still valid
//     (so a revocation would still be on it), and complete (completeList),
//     does not list the certificate as revoked at or before GenTime, nor for
//     a compromised key at any time.
//
// It answers RevocationChecked when every such certificate was covered by such
// a list, and RevocationNotChecked otherwise; a revocation shown is ErrRevoked.
func (t *Token) Verify(options VerifyOptions) (Revocation, error) {
	if err := timeStampingOnly(t.Signer); err != nil {
		return "", err
	}
	if options.Roots == nil {
		return "", ErrUntrusted
	}
	intermediates := x509.NewCertPool()
	for _, certificate := range t.Certificates {
		if certificate != t.Signer {
			intermediates.AddCert(certificate)
		}
	}
	chains, err := t.Signer.Verify(x509.VerifyOptions{
		Roots:         options.Roots,
		Intermediates: intermediates,
		CurrentTime:   t.GenTime,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
	})
	if err != nil {
		var invalid x509.CertificateInvalidError
		if errors.As(err, &invalid) && invalid.Reason == x509.Expired {
			return "", fmt.Errorf("%w: a certificate of its chain was not valid at the stamp's time", ErrUntrusted)
		}
		return "", fmt.Errorf("%w: %v", ErrUntrusted, err)
	}
	if len(options.Policies) > 0 {
		found := false
		for _, policy := range options.Policies {
			found = found || policy.Equal(t.Policy)
		}
		if !found {
			return "", ErrPolicy
		}
	}
	return revocationOf(chains[0], t.GenTime, options.CRLs)
}

// timeStampingOnly holds a certificate to RFC 3161's usage: the extended key
// usage extension, critical, naming id-kp-timeStamping and nothing else.
func timeStampingOnly(certificate *x509.Certificate) error {
	critical := false
	for _, extension := range certificate.Extensions {
		if extension.Id.Equal(oidExtKeyUsage) {
			critical = extension.Critical
		}
	}
	if !critical || len(certificate.ExtKeyUsage) != 1 || certificate.ExtKeyUsage[0] != x509.ExtKeyUsageTimeStamping || len(certificate.UnknownExtKeyUsage) != 0 {
		return ErrUsage
	}
	return nil
}

// revocationOf is what the supplied lists show about a verified chain at the
// stamp's time.
func revocationOf(chain []*x509.Certificate, at time.Time, lists []*x509.RevocationList) (Revocation, error) {
	status := RevocationChecked
	for index := 0; index < len(chain)-1; index++ {
		certificate, issuer := chain[index], chain[index+1]
		covered := false
		for _, list := range lists {
			if !bytes.Equal(list.RawIssuer, certificate.RawIssuer) || list.ThisUpdate.Before(at) || list.ThisUpdate.After(certificate.NotAfter) {
				continue
			}
			if list.CheckSignatureFrom(issuer) != nil || !completeList(list) {
				continue
			}
			covered = true
			for _, entry := range list.RevokedCertificateEntries {
				if entry.SerialNumber.Cmp(certificate.SerialNumber) != 0 {
					continue
				}
				// keyCompromise (1) and cACompromise (2): a key that was
				// compromised could have signed this token at any time.
				if !entry.RevocationTime.After(at) || entry.ReasonCode == 1 || entry.ReasonCode == 2 {
					return "", ErrRevoked
				}
			}
		}
		if !covered {
			status = RevocationNotChecked
		}
	}
	return status, nil
}

var (
	oidDeltaCRLIndicator        = asn1.ObjectIdentifier{2, 5, 29, 27}
	oidIssuingDistributionPoint = asn1.ObjectIdentifier{2, 5, 29, 28}
	oidCertificateIssuer        = asn1.ObjectIdentifier{2, 5, 29, 29}
	oidAuthorityKeyIdentifier   = asn1.ObjectIdentifier{2, 5, 29, 35}
	oidCRLNumber                = asn1.ObjectIdentifier{2, 5, 29, 20}
	oidReasonCode               = asn1.ObjectIdentifier{2, 5, 29, 21}
	oidInvalidityDate           = asn1.ObjectIdentifier{2, 5, 29, 24}
	recognisedListExtensions    = []asn1.ObjectIdentifier{oidAuthorityKeyIdentifier, oidCRLNumber}
	recognisedEntryExtensions   = []asn1.ObjectIdentifier{oidReasonCode, oidInvalidityDate}
)

// completeList says whether a revocation list can speak for every
// certificate its issuer issued: not a delta list, which lists only what
// changed since a base it does not carry; not a scoped one, which an issuing
// distribution point limits to some certificates or some reasons; not an
// indirect one, whose entries name another issuer; and with no critical
// extension, of the list or of an entry, that is not read here.
func completeList(list *x509.RevocationList) bool {
	recognised := func(id asn1.ObjectIdentifier, known []asn1.ObjectIdentifier) bool {
		for _, each := range known {
			if id.Equal(each) {
				return true
			}
		}
		return false
	}
	for _, extension := range list.Extensions {
		if extension.Id.Equal(oidDeltaCRLIndicator) || extension.Id.Equal(oidIssuingDistributionPoint) {
			return false
		}
		if extension.Critical && !recognised(extension.Id, recognisedListExtensions) {
			return false
		}
	}
	for _, entry := range list.RevokedCertificateEntries {
		for _, extension := range entry.Extensions {
			if extension.Id.Equal(oidCertificateIssuer) || (extension.Critical && !recognised(extension.Id, recognisedEntryExtensions)) {
				return false
			}
		}
	}
	return true
}

// MaxReplyBytes bounds an authority's reply: a token with its certificates is
// a few kilobytes.
const MaxReplyBytes = 1 << 20

// The ways asking an authority fails, apart from a token that does not hold.
var (
	// ErrUnreachable is an authority that could not be asked, or did not
	// answer an HTTP 200 with a reply within MaxReplyBytes.
	ErrUnreachable = errors.New("the time-stamping authority could not be asked, or did not answer")
	// ErrNotAsked is a reply that is not for the request: another digest, or
	// another nonce.
	ErrNotAsked = errors.New("the time-stamping authority's reply is not for this request")
)

// Doer sends an HTTP request, as *http.Client does.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Ask asks the authority at url to stamp a SHA-256 digest, over RFC 3161's
// HTTP transport, and answers the token's DER and the token, checked against
// its own signing certificate and held to the request: the digest stamped and
// the nonce returned must be the ones asked for. Whether the authority is to
// be trusted is for a verifier with roots to decide. No error carries the
// url, which may hold credentials.
func Ask(ctx context.Context, client Doer, url string, digest []byte) ([]byte, *Token, error) {
	nonce, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 63))
	if err != nil {
		return nil, nil, err
	}
	nonce.Add(nonce, big.NewInt(1))
	body, err := Request(digest, nonce)
	if err != nil {
		return nil, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: the address is not one a request can be sent to", ErrUnreachable)
	}
	request.Header.Set("Content-Type", "application/timestamp-query")
	request.Header.Set("Accept", "application/timestamp-reply")
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: the request was not answered", ErrUnreachable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%w: it answered HTTP %d", ErrUnreachable, response.StatusCode)
	}
	reply, err := io.ReadAll(io.LimitReader(response.Body, MaxReplyBytes+1))
	if err != nil || len(reply) > MaxReplyBytes {
		return nil, nil, fmt.Errorf("%w: its reply could not be read whole", ErrUnreachable)
	}
	der, err := ParseResponse(reply)
	if err != nil {
		return nil, nil, err
	}
	token, err := Parse(der)
	if err != nil {
		return nil, nil, err
	}
	if !token.HashAlgorithm.Equal(OIDSHA256) || !bytes.Equal(token.HashedMessage, digest) {
		return nil, nil, fmt.Errorf("%w: it stamped another digest", ErrNotAsked)
	}
	if token.Nonce == nil || token.Nonce.Cmp(nonce) != 0 {
		return nil, nil, fmt.Errorf("%w: its nonce is not the one asked with", ErrNotAsked)
	}
	return der, token, nil
}
