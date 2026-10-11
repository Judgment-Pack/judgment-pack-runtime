package timestamp

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

type statusDoer int

func (d statusDoer) Do(request *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: int(d), Header: http.Header{}, Body: http.NoBody, Request: request}, nil
}

// Every 3xx is a RedirectError carrying the status alone; any other status
// that is not 200 stays an unreachable authority.
func TestAskStatusClasses(t *testing.T) {
	for status := 200; status < 600; status += 1 {
		_, _, err := Ask(t.Context(), statusDoer(status), "https://tsa.invalid/stamp", make([]byte, 32))
		var redirect *RedirectError
		switch {
		case status >= 300 && status < 400:
			if !errors.As(err, &redirect) || redirect.Status != status || errors.Is(err, ErrUnreachable) {
				t.Errorf("%d: %v", status, err)
			} else if strings.Contains(err.Error(), "tsa.invalid") {
				t.Errorf("%d: the address is in %q", status, err)
			}
		case status != 200:
			if errors.As(err, &redirect) || !errors.Is(err, ErrUnreachable) {
				t.Errorf("%d: %v", status, err)
			}
		}
	}
}
