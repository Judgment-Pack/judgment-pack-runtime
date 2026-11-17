// Command jpack-emit-go-loader is the reference Go typed loader emitter of
// spec RFC 0036 §6, item 1 (registry companion §11.6), run under the emitter
// contract of RFC 0036 (companion §11). ADR-0102 records its design.
//
// It reads one emit request on standard input, of version 1.0.0 and in the
// resolved form, and writes one response on standard output. For each kind
// the request declares, the response holds one Go file: a type generated from
// the kind's schema, in which every member at one of the kind's secret
// positions is a startup.Secret, and the Registry methods that decode an
// entry of the kind. A second file, loader.go, holds Registry, Load and
// LoadFile, which read one resolved document through the start-up module
// (github.com/Judgment-Pack/judgment-pack-runtime/startup), and the decoder
// the kind files share. The generated code imports the standard library and
// the start-up module only.
//
// Before it generates anything, it checks every pin of every environment in
// the request against the declarations: each pin must have the declaration
// of its kind, whose RFC 8785 form's SHA-256 is the pin's digest, and each
// declaration a pin.
//
// The program is a pure function of the request bytes. It opens, writes and
// deletes no file, and reads no clock, environment variable or network. It
// refuses in one of two ways, and never quotes the request in either:
//   - a request it does not read (its form, its options, or declarations and
//     pins that do not match one to one) makes it write one fixed line on
//     standard error, write nothing on standard output, and exit 1;
//   - a declaration it reads but cannot generate code for (a schema shape it
//     does not generate, a secret position that is not a string, or Go names
//     that cannot be formed or would collide) makes it write a response with
//     no file and one error diagnostic, located at the kind, which fails the
//     run.
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
	errRead        refusal = "jpack-emit-go-loader: the request could not be read"
	errRequest     refusal = "jpack-emit-go-loader: the request is not a resolved-form request of version 1.0.0"
	errOptions     refusal = "jpack-emit-go-loader: the options are not this emitter's options"
	errDeclaration refusal = "jpack-emit-go-loader: a declaration is not a kind declaration of format 1.0.0"
	errPin         refusal = "jpack-emit-go-loader: the declarations do not match the pins one to one, by kind, id, version and digest"
	errBounds      refusal = "jpack-emit-go-loader: the generated files exceed the response bounds"
	errWrite       refusal = "jpack-emit-go-loader: the response could not be written"
	errInternal    refusal = "jpack-emit-go-loader: internal error"
)

// finding is a declaration the emitter reads but cannot generate code for.
// It is the code of the response's one error diagnostic, located at the
// kind (findingAt).
type finding string

func (f finding) Error() string { return string(f) }

// The findings, each an emitter diagnostic code (companion §11.3).
const (
	// findSchema: the kind's schema has a shape the emitter does not
	// generate (ADR-0102, "Refused shapes").
	findSchema finding = "x-go-loader-schema"
	// findSecret: a secret pattern of the kind matches a position that is not
	// a string, matches only some members or elements of a map or an array,
	// or matches no position of the type.
	findSecret finding = "x-go-loader-secret"
	// findName: a Go name cannot be formed from a kind name, a member name or
	// a value.
	findName finding = "x-go-loader-name"
	// findCollide: two generated Go names collide.
	findCollide finding = "x-go-loader-collide"
)

// findingAt is a finding and the kind it is located at.
type findingAt struct {
	code finding
	kind string
}

func (f *findingAt) Error() string { return string(f.code) }
