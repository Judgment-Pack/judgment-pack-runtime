package timestamp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp/tsatest"
)

// Asking an authority over HTTP answers its token only when the reply is for
// the request: a granted reply, over the digest asked for, with the nonce
// asked with. No error names the address, which may hold credentials. The
// authority here is a local test server; no test asks a real one.
func TestAskingAnAuthorityHoldsItsReplyToTheRequest(t *testing.T) {
	tsa := authority(t, tsatest.Options{})
	server := httptest.NewServer(tsa)
	defer server.Close()
	digest := digestOf(checkpointBytes)
	der, token, err := Ask(context.Background(), server.Client(), server.URL, digest)
	if err != nil || !bytes.Equal(token.HashedMessage, digest) || len(der) == 0 || tsa.Requests != 1 {
		t.Fatalf("ask: %v %d", err, tsa.Requests)
	}
	if _, err := Parse(der); err != nil {
		t.Fatalf("the DER answered is the token: %v", err)
	}
	cases := []struct {
		name  string
		setup func()
		want  error
	}{
		{"an HTTP failure", func() { tsa.Fail = http.StatusInternalServerError }, ErrUnreachable},
		{"a rejection", func() { tsa.Status = 2 }, ErrRejected},
		{"another digest", func() { tsa.WrongDigest = true }, ErrNotAsked},
		{"another nonce", func() { tsa.WrongNonce = true }, ErrNotAsked},
	}
	for _, c := range cases {
		tsa.Fail, tsa.Status, tsa.WrongDigest, tsa.WrongNonce = 0, 0, false, false
		c.setup()
		if _, _, err := Ask(context.Background(), server.Client(), server.URL, digest); !errors.Is(err, c.want) || strings.Contains(err.Error(), server.URL) {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	big := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(bytes.Repeat([]byte{0x30}, MaxReplyBytes+1))
	}))
	defer big.Close()
	if _, _, err := Ask(context.Background(), big.Client(), big.URL, digest); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("a reply past the bound: %v", err)
	}
	garbage := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("not a reply"))
	}))
	defer garbage.Close()
	if _, _, err := Ask(context.Background(), garbage.Client(), garbage.URL, digest); !errors.Is(err, ErrMalformed) {
		t.Fatalf("not a reply: %v", err)
	}
	closed := httptest.NewServer(tsa)
	address := closed.URL
	closed.Close()
	if _, _, err := Ask(context.Background(), http.DefaultClient, address, digest); !errors.Is(err, ErrUnreachable) || strings.Contains(err.Error(), address) {
		t.Fatalf("nothing listening: %v", err)
	}
	if _, _, err := Ask(context.Background(), http.DefaultClient, "http://user:secret@\x7f", digest); !errors.Is(err, ErrUnreachable) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("an address no request can go to: %v", err)
	}
}
