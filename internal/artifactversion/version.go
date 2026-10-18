// Package artifactversion provides exact three-component version arithmetic.
// It does not inspect artifacts, decide compatibility or authorize publication.
package artifactversion

import (
	"math/big"
	"regexp"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// This is a tool input bound, not a restriction added to the Core schema.
const MaxVersionBytes = 256

// Pattern is the Core pack version grammar, without prerelease or build suffixes.
const Pattern = `^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`

var grammar = regexp.MustCompile(Pattern)

type Choices struct {
	Patch string `json:"patch"`
	Minor string `json:"minor"`
	Major string `json:"major"`
}

type Report struct {
	OutputVersion        string              `json:"outputVersion"`
	Tool                 result.Tool         `json:"tool"`
	Command              string              `json:"command"`
	Status               string              `json:"status"`
	Scope                string              `json:"scope"`
	MaxVersionBytes      int                 `json:"maxVersionBytes"`
	CompatibilityChecked bool                `json:"compatibilityChecked"`
	Base                 string              `json:"base,omitempty"`
	Choices              *Choices            `json:"choices,omitempty"`
	Candidate            string              `json:"candidate,omitempty"`
	Comparison           string              `json:"comparison,omitempty"`
	Diagnostics          []result.Diagnostic `json:"diagnostics"`
}

// Options returns all bump choices; it never selects a bump for the author.
// Comparison is numeric ordering only, not a publication-admission verdict.
func Options(command, base string, candidate *string) (Report, int) {
	r := Report{OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: command, Status: "refused", Scope: "version-arithmetic", MaxVersionBytes: MaxVersionBytes, Diagnostics: []result.Diagnostic{}}
	refuse := func(path, message string) (Report, int) {
		r.Diagnostics = append(r.Diagnostics, result.ErrorDiagnostic("JPS-ARTIFACT-VERSION", "authoring", path, message))
		return r, result.ExitInvalid
	}
	if !valid(base) {
		return refuse("/base", "Provide a three-component version without leading zeroes or suffixes, within the reported byte limit.")
	}
	if candidate != nil && !valid(*candidate) {
		return refuse("/candidate", "Provide a three-component candidate version without leading zeroes or suffixes, within the reported byte limit.")
	}
	parts := strings.Split(base, ".")
	choices := Choices{
		Patch: parts[0] + "." + parts[1] + "." + increment(parts[2]),
		Minor: parts[0] + "." + increment(parts[1]) + ".0",
		Major: increment(parts[0]) + ".0.0",
	}
	// Every suggested version must fit the grammar and byte bound. A later bump
	// from a boundary value can still be refused when its choices exceed the bound.
	if len(choices.Patch) > MaxVersionBytes || len(choices.Minor) > MaxVersionBytes || len(choices.Major) > MaxVersionBytes {
		return refuse("/base", "The next version exceeds the reported byte limit.")
	}
	r.Status = "available"
	r.Base = base
	r.Choices = &choices
	if candidate != nil {
		r.Candidate = *candidate
		r.Comparison = compare(*candidate, base)
	}
	return r, result.ExitSuccess
}

// Valid checks the existing artifact version grammar and input byte bound.
// It grants no compatibility or publication permission.
func Valid(value string) bool { return len(value) <= MaxVersionBytes && grammar.MatchString(value) }

func valid(value string) bool { return Valid(value) }

// increment is called only with components of a validated version.
func increment(component string) string {
	n, _ := new(big.Int).SetString(component, 10)
	return n.Add(n, big.NewInt(1)).String()
}

func compare(a, b string) string {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := range x {
		if len(x[i]) > len(y[i]) {
			return "greater"
		}
		if len(x[i]) < len(y[i]) {
			return "lower"
		}
		if x[i] > y[i] {
			return "greater"
		}
		if x[i] < y[i] {
			return "lower"
		}
	}
	return "equal"
}
