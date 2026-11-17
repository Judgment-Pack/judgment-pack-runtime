// Command jpack-emit-ts-constants is the reference TypeScript constants
// emitter of spec RFC 0036 §6 item 2 (registry companion §11.6), run under
// the emitter contract of RFC 0036 (companion §11). ADR-0103 records its
// design.
//
// It reads one emit request on standard input, of version 1.0.0 and in the
// resolved form, for one environment, and writes one response on standard
// output. The response holds one TypeScript file for the environment: for
// each kind the request pins, a type made from the kind's schema and a
// read-only constant object of the kind's resolved entries, by entry id.
// Every member at a secret position of the kind (its secrets patterns) is
// left out of both the type and the value.
//
// The program is a pure function of the request bytes. It opens, writes and
// deletes no file, and reads no clock, environment variable or network. A
// request it cannot read makes it write one fixed line on standard error,
// which never quotes the request, write nothing on standard output, and exit
// 1. A request it can read but refuses (a declaration that does not match its
// pin, a schema it does not generate, a name it cannot form) gets a response
// of no files and error diagnostics, each a code and a location with no
// value, and the host fails the run.
package main

import (
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr))
}

// run is the program: the request on in, the response on out, and on a
// refusal one fixed line on errOut and exit status 1.
func run(in io.Reader, out, errOut io.Writer) int {
	request, err := io.ReadAll(io.LimitReader(in, maxRequestBytes+1))
	if err != nil || len(request) > maxRequestBytes {
		return refuse(errOut, errRead)
	}
	response, err := emit(request)
	if err != nil {
		return refuse(errOut, err)
	}
	if _, err := out.Write(response); err != nil {
		return refuse(errOut, errWrite)
	}
	return 0
}

// refuse writes the refusal's fixed line. An error that is not one of the
// emitter's refusals is written as the internal-error line, so no text from
// the request or from a library reaches standard error.
func refuse(errOut io.Writer, err error) int {
	line, ok := err.(refusal)
	if !ok {
		line = errInternal
	}
	_, _ = io.WriteString(errOut, string(line)+"\n")
	return 1
}

// refusal is one of the emitter's fixed refusal lines.
type refusal string

func (r refusal) Error() string { return string(r) }

// The refusals of a request the emitter cannot read. Each is a fixed text:
// none quotes the request.
const (
	errRead     refusal = "jpack-emit-ts-constants: the request could not be read"
	errRequest  refusal = "jpack-emit-ts-constants: the request is not a resolved-form request of version 1.0.0 for one environment"
	errOptions  refusal = "jpack-emit-ts-constants: the options are not this emitter's options"
	errWrite    refusal = "jpack-emit-ts-constants: the response could not be written"
	errInternal refusal = "jpack-emit-ts-constants: internal error"
)
