package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	const p = "Release immutability check failed: "
	tests := []struct {
		name   string
		mode   string
		status int
		body   string
		ok     string
		fail   string
	}{
		{"latest immutable", "previous-release", 200, `[{"tag_name":"v2","immutable":true,"draft":false,"prerelease":false}]`, "The latest published release is immutable.", ""},
		{"drafts and prereleases skipped", "previous-release", 200, `[{"immutable":false,"draft":true},{"immutable":false,"prerelease":true},{"immutable":true}]`, "The latest published release is immutable.", ""},
		{"latest not immutable", "previous-release", 200, `[{"immutable":false},{"immutable":true}]`, "", p + "the latest published release is not immutable: turn on release immutability in the repository settings before cutting this one."},
		{"latest missing field", "previous-release", 200, `[{"tag_name":"v2"}]`, "", p + `the latest published release has no "immutable" field in the API answer, so it cannot be confirmed and the release stops.`},
		{"latest wrong type", "previous-release", 200, `[{"immutable":"yes"}]`, "", p + `the "immutable" field of the latest published release is not true or false, so it cannot be confirmed and the release stops.`},
		{"no releases", "previous-release", 200, `[]`, "No published release exists yet; the check after publication is the only enforcement of release immutability for this first release.", ""},
		{"only drafts", "previous-release", 200, `[{"draft":true}]`, "No published release exists yet; the check after publication is the only enforcement of release immutability for this first release.", ""},
		{"list api error", "previous-release", 500, `{}`, "", p + "the API answered HTTP 500 when reading the list of published releases, so it cannot be confirmed and the release stops."},
		{"list permission refusal", "previous-release", 403, `{}`, "", p + "the API refused the workflow token (HTTP 403) when reading the list of published releases, so it cannot be confirmed and the release stops."},
		{"list not json list", "previous-release", 200, `{"immutable":true}`, "", p + "the API answer for the list of published releases is not a JSON list, so it cannot be confirmed and the release stops."},
		{"release immutable", "release", 200, `{"tag_name":"v1.0.0","immutable":true}`, "The published release reports immutable true.", ""},
		{"release not immutable", "release", 200, `{"immutable":false}`, "", p + "the published release reports immutable false; it was published without immutability and must be replaced by a new version, not edited."},
		{"release missing field", "release", 200, `{"tag_name":"v1.0.0"}`, "", p + `the published release has no "immutable" field in the API answer, so it cannot be confirmed and the release stops.`},
		{"release api error", "release", 502, ``, "", p + "the API answered HTTP 502 when reading the published release, so it cannot be confirmed and the release stops."},
		{"release permission refusal", "release", 403, `{}`, "", p + "the API refused the workflow token (HTTP 403) when reading the published release, so it cannot be confirmed and the release stops."},
		{"unknown mode", "repository", 200, `{}`, "", p + "--mode must be previous-release or release."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := check(test.mode, test.status, []byte(test.body))
			if test.fail == "" {
				if err != nil || got != test.ok {
					t.Fatalf("got (%q, %v), want %q", got, err, test.ok)
				}
				return
			}
			if err == nil || err.Error() != test.fail {
				t.Fatalf("got (%q, %v), want failure %q", got, err, test.fail)
			}
		})
	}
}

func TestRunExitCodesAndStreams(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--mode", "previous-release", "--status", "200"}, strings.NewReader(`[{"immutable":true}]`), &out, &errb); code != 0 || errb.Len() != 0 || out.Len() == 0 {
		t.Fatalf("enabled: code %d out %q err %q", code, out.String(), errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := run([]string{"--mode", "previous-release", "--status", "200"}, strings.NewReader(`[{"immutable":false}]`), &out, &errb); code != 1 || out.Len() != 0 || strings.Count(errb.String(), "\n") != 1 {
		t.Fatalf("disabled: code %d out %q err %q", code, out.String(), errb.String())
	}
	errb.Reset()
	if code := run([]string{"--mode", "release"}, strings.NewReader(`{"immutable":true}`), &out, &errb); code != 1 {
		t.Fatalf("missing status must fail closed (status 0), got %d", code)
	}
	if code := run([]string{"--bogus"}, strings.NewReader(``), &out, &errb); code != 2 {
		t.Fatalf("bad usage: got %d", code)
	}
}
