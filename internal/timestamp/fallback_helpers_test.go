package timestamp

import "crypto/x509"

// setFallbackRoots makes pool the process's roots.
func setFallbackRoots(pool *x509.CertPool) { x509.SetFallbackRoots(pool) }

// x509VerifyOptions is what crypto/x509 alone would verify a token's signer
// with: the given roots, nil for the process's own.
func x509VerifyOptions(token *Token, roots *x509.CertPool) x509.VerifyOptions {
	intermediates := x509.NewCertPool()
	for _, certificate := range token.Certificates {
		if certificate != token.Signer {
			intermediates.AddCert(certificate)
		}
	}
	return x509.VerifyOptions{Roots: roots, Intermediates: intermediates, CurrentTime: token.GenTime, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}}
}
