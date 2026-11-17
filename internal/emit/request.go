package emit

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// Inputs are what a request is formed from (RFC 0036 §2).
type Inputs struct {
	// Registry is the validated registry, or nil when none was given (only
	// in the facts-contracts form).
	Registry *registry.Result
	// Contracts are the authored facts contracts the caller named, each read
	// by registry.ReadFactsContract. Only in the facts-contracts form.
	Contracts []*registry.FactsContractDoc
	// Options is the admitted options document, or nil for none, which is
	// the empty object.
	Options any
}

// Request is one formed request: its selector, its RFC 8785 bytes, and the
// environment it holds for a manifest whose environments are "each".
type Request struct {
	Environment string
	Version     string
	Bytes       []byte
}

// Digest is the request's "sha256:" digest.
func (r Request) Digest() string { return digestOf(r.Bytes) }

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// FormRequests forms every request a manifest's run takes: one per
// environment for "each", and one otherwise. It refuses before any emitter
// starts: a registry that is not valid, a pinned kind the manifest reads
// whose id differs or whose version is outside the range, options that fail
// the schema, a request version that cannot be written, and a request whose
// RFC 8785 bytes exceed MaxRequestBytes (EMIT-REQUEST), any one of "each".
//
// The kind filter is applied as each document is built, not after: a pin, a
// declaration or an entry of a kind the manifest does not read is never
// copied into a request, and a generated contract is made only for a kind it
// reads.
//
// The resolved form is {"emitRequestVersion", "emitter", "options",
// "declarations", "resolved"} (registry companion §11.2): declarations holds
// the declaration of each kind whose pins remain, as admitted, sorted by
// kind, and each SHA-256 of its RFC 8785 form is its pin's digest. A request
// whose pins and declarations do not match one to one is refused
// (EMIT-REQUEST). The facts-contracts form has no declarations.
func FormRequests(m *Manifest, in Inputs) ([]Request, []Diagnostic) {
	var diagnostics []Diagnostic
	reads := map[string]Read{}
	for _, r := range m.Reads {
		reads[r.Kind] = r
	}
	keep := func(kind string) bool {
		_, ok := reads[kind]
		return ok
	}

	if in.Registry != nil && !in.Registry.Report.Valid {
		return nil, []Diagnostic{diagnostic("EMIT-REQUEST", Location{Document: DocumentRegistry})}
	}
	if in.Registry == nil && m.Input == InputResolved {
		return nil, []Diagnostic{diagnostic("EMIT-REQUEST", Location{Document: DocumentRegistry})}
	}
	var readPins []pinAt
	if in.Registry != nil {
		for i, pin := range in.Registry.Pins() {
			r, read := reads[pin.Kind]
			at := Location{Document: DocumentRegistry, Pointer: "/kinds/" + strconv.Itoa(i)}
			switch {
			case !read:
				continue
			case pin.ID != r.ID:
				diagnostics = append(diagnostics, diagnostic("EMIT-KIND-ID", at))
			case !r.Versions.Contains(pin.Version):
				diagnostics = append(diagnostics, diagnostic("EMIT-KIND-RANGE", at))
			default:
				readPins = append(readPins, pinAt{Pin: pin, index: i})
			}
		}
	}

	options := in.Options
	if options == nil {
		options = map[string]any{}
	}
	for _, finding := range m.Options.Validate(options) {
		diagnostics = append(diagnostics, diagnostic("EMIT-OPTIONS", Location{Document: DocumentOptions, Pointer: finding.Pointer}))
	}

	version, ok := requestVersion(m)
	if !ok {
		diagnostics = append(diagnostics, diagnostic("EMIT-REQUEST-VERSION", Location{Document: DocumentManifest, Pointer: "/requestVersions"}))
	}
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}

	document := func(member string, value any) map[string]any {
		return map[string]any{
			"emitRequestVersion": version,
			"emitter":            map[string]any{"id": m.ID, "version": m.Version},
			"options":            options,
			member:               value,
		}
	}
	encode := func(environment string, value map[string]any) (Request, bool) {
		data, err := jcs.EncodeDocumentWithin(value, MaxRequestBytes)
		if err != nil {
			diagnostics = append(diagnostics, diagnostic("EMIT-REQUEST", Location{Document: DocumentRequest, Environment: environment}))
			return Request{}, false
		}
		return Request{Environment: environment, Version: version, Bytes: data}, true
	}

	if m.Input == InputFactsContracts {
		return factsContractsRequest(m, in, readPins, version, document, encode)
	}

	// The registry's resolved format must be one the request carries; a
	// registry is never written down to an older one.
	if requestVersions[version].resolved != registry.ResolvedFormatVersion {
		return nil, []Diagnostic{diagnostic("EMIT-REQUEST-VERSION", Location{Document: DocumentManifest, Pointer: "/requestVersions"})}
	}
	// The read kinds' declarations, by the pins' own filter (registry
	// companion §11.2): the same in every request of the run.
	declarations, digests, ok := readDeclarations(in.Registry, keep)
	if !ok {
		return nil, []Diagnostic{diagnostic("EMIT-REQUEST", Location{Document: DocumentRegistry})}
	}
	resolvedDocument := func(resolved []any) map[string]any {
		value := document("resolved", resolved)
		value["declarations"] = declarations
		return value
	}
	var resolved []any
	var requests []Request
	for _, environment := range in.Registry.Environments() {
		value, err := in.Registry.ResolvedFor(environment, keep)
		if err != nil || !declaresPins(value, digests) {
			return nil, []Diagnostic{diagnostic("EMIT-REQUEST", Location{Document: DocumentRegistry, Environment: environment})}
		}
		if m.Environments == EnvironmentsAll {
			resolved = append(resolved, value)
			continue
		}
		request, ok := encode(environment, resolvedDocument([]any{value}))
		if !ok {
			return nil, diagnostics
		}
		requests = append(requests, request)
	}
	if m.Environments == EnvironmentsAll {
		if resolved == nil {
			resolved = []any{}
		}
		request, ok := encode("", resolvedDocument(resolved))
		if !ok {
			return nil, diagnostics
		}
		requests = append(requests, request)
	}
	return requests, nil
}

// readDeclarations returns the declarations of the kinds keep admits, sorted
// by kind, as the request's declarations member, and each one's digest by
// kind name: the SHA-256 of its RFC 8785 form, which is the form the request
// holds it in, taken here rather than trusted from the reader.
func readDeclarations(r *registry.Result, keep func(kind string) bool) ([]any, map[string]string, bool) {
	declarations, err := r.Declarations(keep)
	if err != nil {
		return nil, nil, false
	}
	values := make([]any, 0, len(declarations))
	digests := make(map[string]string, len(declarations))
	for _, declaration := range declarations {
		digest, err := registry.Digest(declaration.Value)
		if err != nil {
			return nil, nil, false
		}
		values = append(values, declaration.Value)
		digests[declaration.Kind] = digest
	}
	return values, digests, true
}

// declaresPins reports whether a resolved document's pins and the
// declarations are of exactly the same kinds, each pin's digest its
// declaration's. A request that fails it is never written: an emitter must
// refuse a pin with no declaration or another's (§11.2), and a declaration
// with no pin is of a kind the emitter does not read.
func declaresPins(resolved map[string]any, digests map[string]string) bool {
	pins, _ := resolved["kinds"].([]any)
	if len(pins) != len(digests) {
		return false
	}
	seen := make(map[string]bool, len(pins))
	for _, item := range pins {
		pin, _ := item.(map[string]any)
		kind, _ := pin["kind"].(string)
		digest, _ := pin["digest"].(string)
		if declared, ok := digests[kind]; !ok || declared != digest || seen[kind] {
			return false
		}
		seen[kind] = true
	}
	return true
}

// pinAt is a pin and its index in the registry's kinds.
type pinAt struct {
	registry.Pin
	index int
}

func factsContractsRequest(_ *Manifest, in Inputs, readPins []pinAt, version string, document func(string, any) map[string]any, encode func(string, map[string]any) (Request, bool)) ([]Request, []Diagnostic) {
	if requestVersions[version].contract != registry.FactsContractVersion {
		return nil, []Diagnostic{diagnostic("EMIT-REQUEST-VERSION", Location{Document: DocumentManifest, Pointer: "/requestVersions"})}
	}
	type contract struct {
		id, version string
		value       any
	}
	var contracts []contract
	var diagnostics []Diagnostic
	for _, pin := range readPins {
		at := Location{Document: DocumentRegistry, Pointer: "/kinds/" + strconv.Itoa(pin.index)}
		data, findings, err := in.Registry.FactsContract(pin.Kind)
		if err != nil || len(findings) > 0 {
			diagnostics = append(diagnostics, diagnostic("EMIT-REQUEST", at))
			continue
		}
		value, code, _ := registry.AdmitValue(data, registry.MaxContractBytes)
		if code != "" {
			diagnostics = append(diagnostics, diagnostic("EMIT-REQUEST", at))
			continue
		}
		contracts = append(contracts, contract{id: pin.ID, version: pin.Version, value: value})
	}
	for _, authored := range in.Contracts {
		contracts = append(contracts, contract{id: authored.ID, version: authored.Version, value: authored.Document})
	}
	slices.SortStableFunc(contracts, func(a, b contract) int {
		return cmp.Or(cmp.Compare(a.id, b.id), cmp.Compare(a.version, b.version))
	})
	for i := 1; i < len(contracts); i++ {
		if contracts[i].id == contracts[i-1].id && contracts[i].version == contracts[i-1].version {
			diagnostics = append(diagnostics, diagnostic("EMIT-REQUEST", Location{Document: DocumentContract, Pointer: "/factsContracts/" + strconv.Itoa(i)}))
		}
	}
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	values := make([]any, len(contracts))
	for i, c := range contracts {
		values[i] = c.value
	}
	request, ok := encode("", document("factsContracts", values))
	if !ok {
		return nil, []Diagnostic{diagnostic("EMIT-REQUEST", Location{Document: DocumentRequest})}
	}
	return []Request{request}, nil
}

// requestVersion is the highest request version the manifest lists that this
// host writes.
func requestVersion(m *Manifest) (string, bool) {
	best := ""
	for _, v := range m.RequestVersions {
		if _, supported := requestVersions[v]; supported && (best == "" || compareVersions(v, best) > 0) {
			best = v
		}
	}
	return best, best != ""
}
