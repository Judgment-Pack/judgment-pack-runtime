package audit

import (
	"crypto/ed25519"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/regsig"
)

// SignRegistryLock is the lock signature document (registry format 1.1.0
// §10.1, proposed), its final line feed included, by which s signs the lock
// whose digest is lockDigest: Ed25519 over regsig.Lock's message, never over
// the lock's bytes, so the signature is one of a lock and of nothing else. A
// digest not of its form is regsig.ErrDigest, and nothing is signed.
func (s *Signer) SignRegistryLock(lockDigest string) ([]byte, error) {
	return s.signRegistry(regsig.Lock, lockDigest)
}

// SignRegistrySource is the source signature document (§10.2), by which s
// signs the source statement whose digest is sourceDigest, over
// regsig.Source's message.
func (s *Signer) SignRegistrySource(sourceDigest string) ([]byte, error) {
	return s.signRegistry(regsig.Source, sourceDigest)
}

func (s *Signer) signRegistry(kind regsig.Kind, digest string) ([]byte, error) {
	message, err := kind.Message(digest)
	if err != nil {
		return nil, err
	}
	return kind.Encode(s.public, digest, ed25519.Sign(s.private, message))
}
