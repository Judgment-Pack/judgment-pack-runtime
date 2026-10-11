// Command check-immutability enforces the GitHub Release Immutability
// prerequisite of docs/releasing.md. It is read-only and fails closed: every
// answer it cannot read as an explicit "on" is a failure with one sentence.
//
// The caller fetches the API answer and passes its HTTP status and body, so
// this program needs no network:
//
//	check-immutability --mode repository --status 200 < repository-setting.json
//	check-immutability --mode release    --status 200 < published-release.json
//
// Mode "repository" reads GET /repos/{owner}/{repo}/immutable-releases (field
// "enabled"); mode "release" reads GET /repos/{owner}/{repo}/releases/tags/{tag}
// (field "immutable").
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check-immutability", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	mode := fs.String("mode", "", "repository or release")
	status := fs.Int("status", 0, "HTTP status of the API answer")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "Release immutability check failed: usage is --mode repository|release --status HTTP_STATUS with the API body on stdin.")
		return 2
	}
	body, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintln(stderr, "Release immutability check failed: the API answer could not be read.")
		return 1
	}
	message, err := check(*mode, *status, body)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	fmt.Fprintln(stdout, message)
	return 0
}

// check returns the success sentence, or an error whose text is the one
// failure sentence.
func check(mode string, status int, body []byte) (string, error) {
	var field, subject, remedy string
	switch mode {
	case "repository":
		field, subject = "enabled", "GitHub Release Immutability setting"
		remedy = "enable it in the repository settings before releasing"
	case "release":
		field, subject = "immutable", "published release"
		remedy = "the release was published without immutability and must be replaced by a new version, not edited"
	default:
		return "", errors.New("Release immutability check failed: --mode must be repository or release.")
	}
	fail := func(format string, a ...any) (string, error) {
		return "", fmt.Errorf("Release immutability check failed: "+format, a...)
	}

	switch {
	case status == 401 || status == 403:
		return fail("the API refused the workflow token (HTTP %d) when reading the %s, so it cannot be confirmed and the release stops.", status, subject)
	case status < 200 || status > 299:
		return fail("the API answered HTTP %d when reading the %s, so it cannot be confirmed and the release stops.", status, subject)
	}

	var answer map[string]json.RawMessage
	if err := json.Unmarshal(body, &answer); err != nil {
		return fail("the API answer for the %s is not a JSON object, so it cannot be confirmed and the release stops.", subject)
	}
	raw, ok := answer[field]
	if !ok {
		return fail("the API answer for the %s has no %q field, so it cannot be confirmed and the release stops.", subject, field)
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return fail("the %q field of the %s is not true or false, so it cannot be confirmed and the release stops.", field, subject)
	}
	if !value {
		return fail("the %s reports %s false; %s.", subject, field, remedy)
	}
	if mode == "repository" {
		return "GitHub Release Immutability is enabled for this repository.", nil
	}
	return "The published release reports immutable true.", nil
}
