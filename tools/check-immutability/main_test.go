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
		{"setting enabled", "repository", 200, `{"enabled":true,"enforced_by_owner":false}`, "GitHub Release Immutability is enabled for this repository.", ""},
		{"setting disabled", "repository", 200, `{"enabled":false}`, "", p + "the GitHub Release Immutability setting reports enabled false; enable it in the repository settings before releasing."},
		{"setting api error", "repository", 500, `{"message":"boom"}`, "", p + "the API answered HTTP 500 when reading the GitHub Release Immutability setting, so it cannot be confirmed and the release stops."},
		{"setting not found", "repository", 404, `{}`, "", p + "the API answered HTTP 404 when reading the GitHub Release Immutability setting, so it cannot be confirmed and the release stops."},
		{"setting permission refusal", "repository", 403, `{"message":"Resource not accessible by integration"}`, "", p + "the API refused the workflow token (HTTP 403) when reading the GitHub Release Immutability setting, so it cannot be confirmed and the release stops."},
		{"setting unauthorized", "repository", 401, ``, "", p + "the API refused the workflow token (HTTP 401) when reading the GitHub Release Immutability setting, so it cannot be confirmed and the release stops."},
		{"setting missing field", "repository", 200, `{"enforced_by_owner":false}`, "", p + `the API answer for the GitHub Release Immutability setting has no "enabled" field, so it cannot be confirmed and the release stops.`},
		{"setting wrong type", "repository", 200, `{"enabled":"yes"}`, "", p + `the "enabled" field of the GitHub Release Immutability setting is not true or false, so it cannot be confirmed and the release stops.`},
		{"setting not json", "repository", 200, `<html>`, "", p + "the API answer for the GitHub Release Immutability setting is not a JSON object, so it cannot be confirmed and the release stops."},
		{"release immutable", "release", 200, `{"tag_name":"v1.0.0","immutable":true}`, "The published release reports immutable true.", ""},
		{"release not immutable", "release", 200, `{"immutable":false}`, "", p + "the published release reports immutable false; the release was published without immutability and must be replaced by a new version, not edited."},
		{"release missing field", "release", 200, `{"tag_name":"v1.0.0"}`, "", p + `the API answer for the published release has no "immutable" field, so it cannot be confirmed and the release stops.`},
		{"release api error", "release", 502, ``, "", p + "the API answered HTTP 502 when reading the published release, so it cannot be confirmed and the release stops."},
		{"release permission refusal", "release", 403, `{}`, "", p + "the API refused the workflow token (HTTP 403) when reading the published release, so it cannot be confirmed and the release stops."},
		{"unknown mode", "other", 200, `{}`, "", p + "--mode must be repository or release."},
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
	if code := run([]string{"--mode", "repository", "--status", "200"}, strings.NewReader(`{"enabled":true}`), &out, &errb); code != 0 || errb.Len() != 0 || out.Len() == 0 {
		t.Fatalf("enabled: code %d out %q err %q", code, out.String(), errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := run([]string{"--mode", "repository", "--status", "200"}, strings.NewReader(`{"enabled":false}`), &out, &errb); code != 1 || out.Len() != 0 || strings.Count(errb.String(), "\n") != 1 {
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
