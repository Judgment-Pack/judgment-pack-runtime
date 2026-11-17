// Command jpack-emit-go-facts is the reference Go typed facts builder emitter
// of spec RFC 0037 §6 (registry companion §10.6), run under the emitter
// contract of RFC 0036 (companion §11). ADR-0100 records its design.
//
// It reads one emit request on standard input, of version 1.0.0 and in the
// facts-contracts form, and writes one response on standard output. For each
// facts contract the response holds one Go file: a struct per object position,
// a constructor whose arguments are the members the contract always holds,
// typed constants for each closed list of string values, and a Build method
// that returns the facts document as RFC 8785 canonical JSON. A second file,
// facts_support.go, holds the Decimal type and the canonical writer the
// contract files share.
//
// The program is a pure function of the request bytes. It opens, writes and
// deletes no file, and reads no clock, environment variable or network. A
// request it cannot handle makes it write one fixed line on standard error,
// which never quotes the request, write nothing on standard output, and exit
// 1.
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

// The refusals. Each is a fixed text: none quotes the request.
const (
	errRead        refusal = "jpack-emit-go-facts: the request could not be read"
	errRequest     refusal = "jpack-emit-go-facts: the request is not a facts-contracts request of version 1.0.0"
	errOptions     refusal = "jpack-emit-go-facts: the options are not this emitter's options"
	errContract    refusal = "jpack-emit-go-facts: a facts contract is not of the facts contract form"
	errUnsupported refusal = "jpack-emit-go-facts: a facts contract holds a position this emitter does not generate"
	errName        refusal = "jpack-emit-go-facts: a Go name cannot be formed from a contract id, pointer token or value"
	errCollide     refusal = "jpack-emit-go-facts: two generated Go names collide"
	errBounds      refusal = "jpack-emit-go-facts: the generated files exceed the response bounds"
	errWrite       refusal = "jpack-emit-go-facts: the response could not be written"
	errInternal    refusal = "jpack-emit-go-facts: internal error"
)
