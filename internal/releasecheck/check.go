package releasecheck

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
)

const specificationRepository = "https://github.com/protossai/judgment-pack-spec"

var (
	versionTagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	commitPattern     = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

// CheckEmbedded validates the tag and the integrity and provenance of the
// specification artifacts compiled into a release candidate.
func CheckEmbedded(tag string) error {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		return fmt.Errorf("load embedded specification artifacts: %w", err)
	}
	return Validate(tag, set.Lock())
}

// Validate applies release-only invariants to a verified artifact lock.
func Validate(tag string, lock artifacts.Lock) error {
	if !versionTagPattern.MatchString(tag) {
		return errors.New("CLI release tag must be an exact SemVer tag such as v0.1.0 or v0.2.0-rc.1")
	}
	if lock.Source.Repository != specificationRepository {
		return errors.New("bundled specification repository is not the official JPS repository")
	}
	if lock.Source.Kind != "immutable-git-ref" {
		return errors.New("bundled specification source is not an immutable Git ref")
	}
	if lock.Source.WorktreeDirty {
		return errors.New("bundled specification source records a dirty worktree")
	}
	if !commitPattern.MatchString(lock.Source.BaseCommit) {
		return errors.New("bundled specification source has no exact commit digest")
	}
	if !immutableRef(lock.Source.Ref, lock.Source.BaseCommit) {
		return errors.New("bundled specification ref must be an exact release tag or the recorded commit digest")
	}
	if lock.BundleDigest.Algorithm != "sha256-length-prefixed-v1" || len(lock.BundleDigest.Value) != 64 {
		return errors.New("bundled specification has no supported SHA-256 bundle digest")
	}
	if lock.SpecVersion == "" || len(lock.Files) == 0 {
		return errors.New("bundled specification artifact set is empty")
	}
	return nil
}

func immutableRef(ref, commit string) bool {
	if ref == commit && commitPattern.MatchString(ref) {
		return true
	}
	if strings.HasPrefix(ref, "refs/tags/") {
		ref = strings.TrimPrefix(ref, "refs/tags/")
	}
	return versionTagPattern.MatchString(ref)
}
