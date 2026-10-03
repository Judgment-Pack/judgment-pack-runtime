package timestamp

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp/tsatest"
)

// fallbackHelper marks a copy of this test binary as the process that sets
// fallback roots, which a process can do once.
const fallbackHelper = "JPACK_TIMESTAMP_FALLBACK_HELPER"

// With no roots supplied, nothing is trusted, even where the process's own
// trust store would trust the authority: a verifier's trust is what it
// supplies. A copy of this test binary makes the test authority's root the
// process's roots (x509.SetFallbackRoots, forced by
// GODEBUG=x509usefallbackroots=1), so that crypto/x509 alone would accept the
// token, and Verify must still refuse it.
func TestNoRootsSuppliedTrustsNothing(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("this test binary cannot be named: %v", err)
	}
	command := exec.Command(self, "-test.run=^TestFallbackRootsHelper$", "-test.count=1", "-test.v")
	command.Env = append(os.Environ(), fallbackHelper+"=1", "GODEBUG=x509usefallbackroots=1")
	output, err := command.CombinedOutput()
	text := string(output)
	if strings.Contains(text, "fallback roots not in effect") {
		t.Skip("this platform does not take fallback roots here")
	}
	if err != nil || !strings.Contains(text, "fallback helper: refused") {
		t.Fatalf("helper: %v\n%s", err, text)
	}
}

// TestFallbackRootsHelper is the process TestNoRootsSuppliedTrustsNothing
// starts.
func TestFallbackRootsHelper(t *testing.T) {
	if os.Getenv(fallbackHelper) != "1" {
		t.Skip("run by TestNoRootsSuppliedTrustsNothing")
	}
	tsa, err := tsatest.New(tsatest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	pool := rootsOf(tsa.Root)
	setFallbackRoots(pool)
	token := tokenFrom(t, tsa, digestOf(checkpointBytes))
	if _, err := token.Signer.Verify(x509VerifyOptions(token, nil)); err != nil {
		fmt.Println("fallback roots not in effect:", err)
		return
	}
	if _, err := token.Verify(VerifyOptions{}); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("no roots supplied: %v", err)
	}
	fmt.Println("fallback helper: refused", time.Now().Unix())
}
