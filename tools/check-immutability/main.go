// Command check-immutability enforces the GitHub Release Immutability
// prerequisite of docs/releasing.md. It is read-only and fails closed: every
// answer it cannot read as an explicit "on" is a failure with one sentence.
//
// The caller fetches the API answer and passes its HTTP status and body, so
// this program needs no network:
//
//	check-immutability --mode previous-release --status 200 < releases.json
//	check-immutability --mode release           --status 200 < published-release.json
//
// The repository setting itself (GET /repos/{owner}/{repo}/immutable-releases)
// needs the Administration permission, which a workflow token cannot hold. Mode
// "previous-release" therefore reads GET /repos/{owner}/{repo}/releases?per_page=10
// (a list), takes the first release that is neither a draft nor a prerelease,
// and requires its "immutable" field to be true; with no such release it passes,
// because a first release has only the post-publish check. Mode "release" reads
// GET /repos/{owner}/{repo}/releases/tags/{tag} (field "immutable") and is the
// enforcement.
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
		fmt.Fprintln(stderr, "Release immutability check failed: usage is --mode previous-release|release --status HTTP_STATUS with the API body on stdin.")
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
	var subject string
	switch mode {
	case "previous-release":
		subject = "list of published releases"
	case "release":
		subject = "published release"
	default:
		return "", errors.New("Release immutability check failed: --mode must be previous-release or release.")
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

	var object map[string]json.RawMessage
	if mode == "previous-release" {
		var list []map[string]json.RawMessage
		if err := json.Unmarshal(body, &list); err != nil {
			return fail("the API answer for the %s is not a JSON list, so it cannot be confirmed and the release stops.", subject)
		}
		for _, entry := range list {
			if isTrue(entry["draft"]) || isTrue(entry["prerelease"]) {
				continue
			}
			object = entry
			break
		}
		if object == nil {
			return "No published release exists yet; the check after publication is the only enforcement of release immutability for this first release.", nil
		}
	} else if err := json.Unmarshal(body, &object); err != nil {
		return fail("the API answer for the %s is not a JSON object, so it cannot be confirmed and the release stops.", subject)
	}

	what := "the " + subject
	if mode == "previous-release" {
		what = "the latest published release"
	}
	raw, ok := object["immutable"]
	if !ok {
		return fail("%s has no \"immutable\" field in the API answer, so it cannot be confirmed and the release stops.", what)
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return fail("the \"immutable\" field of %s is not true or false, so it cannot be confirmed and the release stops.", what)
	}
	if !value {
		if mode == "previous-release" {
			return fail("the latest published release is not immutable: turn on release immutability in the repository settings before cutting this one.")
		}
		return fail("the published release reports immutable false; it was published without immutability and must be replaced by a new version, not edited.")
	}
	if mode == "previous-release" {
		return "The latest published release is immutable.", nil
	}
	return "The published release reports immutable true.", nil
}

func isTrue(raw json.RawMessage) bool {
	var v bool
	return json.Unmarshal(raw, &v) == nil && v
}
