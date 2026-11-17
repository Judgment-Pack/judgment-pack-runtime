package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// Check 4 (kind filtering), as ADR-0099 decides it: the harness forms the
// request again from the vector's registry with an entry of a synthetic kind
// the manifest does not read (its declaration and pin added too), restores
// every registry digest in it to the original request's, and requires the
// result to be the original request's bytes; only then does it run the
// emitter on it, and require the response to be unchanged. The digest is
// restored because it is the registry's, which the synthetic entry changes,
// and an emitter may write it into a generated file's header (RFC 0036 §4).

// The synthetic kind and its entry. The kind's name is taken from these, with
// a number added while the registry pins, or the manifest reads, one of that
// name.
const (
	syntheticKindName = "jpack-harness-undeclared"
	syntheticKindBase = "https://example.invalid/jpack-harness/kinds/"
	syntheticVersion  = "1.0.0"
	syntheticEntryID  = "jpack-harness-entry"
)

var errSynthetic = errors.New("conformance: the registry with the synthetic kind could not be formed")

// synthetic is a vector's registry with the synthetic kind added.
type synthetic struct {
	registry registry.Document
	kinds    []registry.Document
	// id is the synthetic kind's id, which no request may hold.
	id string
}

// withSyntheticKind adds the synthetic kind's declaration, its pin and one
// entry of it to a vector's registry, each in the order the registry's rules
// require (pins by kind name, entries by kind and id). The registry must be
// valid. The entry's owner, and the kind's, is the registry's first owner.
func withSyntheticKind(v *Vector, m *emit.Manifest) (*synthetic, error) {
	value, code, _ := registry.AdmitValue(v.Registry.Data, registry.MaxRegistryBytes)
	if code != "" {
		return nil, errSynthetic
	}
	root, _ := value.(map[string]any)
	pins, pinsOK := root["kinds"].([]any)
	entries, entriesOK := root["entries"].([]any)
	owners, _ := root["owners"].([]any)
	if !pinsOK || !entriesOK || len(owners) == 0 {
		return nil, errSynthetic
	}
	owner, _ := owners[0].(string)
	taken := map[string]bool{}
	for _, pin := range pins {
		object, _ := pin.(map[string]any)
		name, _ := object["kind"].(string)
		taken[name] = true
	}
	for _, r := range m.Reads {
		taken[r.Kind] = true
	}
	name := syntheticKindName
	for i := 2; taken[name]; i++ {
		name = syntheticKindName + "-" + strconv.Itoa(i)
	}
	id := syntheticKindBase + name
	declaration := map[string]any{
		"kindFormatVersion": registry.KindFormatVersion,
		"id":                id,
		"version":           syntheticVersion,
		"kind":              name,
		"owner":             owner,
		"schema": map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{"note": map[string]any{"type": "string"}},
		},
	}
	declarationBytes, err := jcs.EncodeDocument(declaration)
	if err != nil {
		return nil, errSynthetic
	}
	digest, err := registry.Digest(declaration)
	if err != nil {
		return nil, errSynthetic
	}
	pin := map[string]any{"kind": name, "id": id, "version": syntheticVersion, "digest": digest}
	at := len(pins)
	for i, item := range pins {
		object, _ := item.(map[string]any)
		if kind, _ := object["kind"].(string); kind > name {
			at = i
			break
		}
	}
	root["kinds"] = insert(pins, at, pin)
	entry := map[string]any{"kind": name, "id": syntheticEntryID, "owner": owner, "value": map[string]any{"note": "jpack-harness"}}
	at = len(entries)
	for i, item := range entries {
		object, _ := item.(map[string]any)
		kind, _ := object["kind"].(string)
		entryID, _ := object["id"].(string)
		if kind > name || kind == name && entryID > syntheticEntryID {
			at = i
			break
		}
	}
	root["entries"] = insert(entries, at, entry)
	registryBytes, err := jcs.EncodeDocument(root)
	if err != nil || len(registryBytes) > registry.MaxRegistryBytes {
		return nil, errSynthetic
	}
	kinds := append(append([]registry.Document{}, v.Kinds...), registry.Document{Data: declarationBytes})
	return &synthetic{registry: registry.Document{Data: registryBytes}, kinds: kinds, id: id}, nil
}

func insert(items []any, at int, item any) []any {
	out := make([]any, 0, len(items)+1)
	out = append(out, items[:at]...)
	out = append(out, item)
	return append(out, items[at:]...)
}

// filteredRequest compares a request formed with the synthetic kind with the
// original: every registry digest the changed request holds is set to the
// original's at the same place, and the result must be the original's bytes.
// It returns the restored bytes, and "" or the reason the comparison fails.
func filteredRequest(original, changed []byte, syntheticID string) ([]byte, string) {
	decode := func(data []byte) (map[string]any, bool) {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var value map[string]any
		if decoder.Decode(&value) != nil || decoder.More() {
			return nil, false
		}
		return value, true
	}
	originalValue, ok1 := decode(original)
	changedValue, ok2 := decode(changed)
	if !ok1 || !ok2 {
		return nil, reasonFilterBytes
	}
	restoreDigests(changedValue, originalValue)
	restored, err := jcs.EncodeDocument(changedValue)
	switch {
	case err != nil:
		return nil, reasonFilterBytes
	case bytes.Equal(restored, original):
		return restored, ""
	case strings.Contains(string(restored), `"`+syntheticID+`"`):
		return nil, reasonFilterHolds
	}
	return nil, reasonFilterBytes
}

// restoreDigests sets each registry digest of a request to the original
// request's at the same place: /resolved/N/registry/digest, and, in the facts
// form, /factsContracts/N/source/registry/digest where a contract's source
// carries the registry's digest. Nothing else is changed.
func restoreDigests(changed, original map[string]any) {
	for _, member := range []string{"resolved", "factsContracts"} {
		changedItems, _ := changed[member].([]any)
		originalItems, _ := original[member].([]any)
		for i := range min(len(changedItems), len(originalItems)) {
			a, _ := changedItems[i].(map[string]any)
			b, _ := originalItems[i].(map[string]any)
			if member == "factsContracts" {
				a, _ = a["source"].(map[string]any)
				b, _ = b["source"].(map[string]any)
			}
			ra, _ := a["registry"].(map[string]any)
			rb, _ := b["registry"].(map[string]any)
			if ra == nil || rb == nil {
				continue
			}
			_, has := ra["digest"]
			digest, had := rb["digest"]
			if has && had {
				ra["digest"] = digest
			}
		}
	}
}

// kindFiltering is check 4.
func (h *harness) kindFiltering(run *vectorRun, row Row) (Row, *emit.Diagnostic) {
	switch {
	case !run.v.hasRegistry():
		row.Status, row.Reason = StatusSkip, reasonNoRegistry
		return row, nil
	case run.refused != nil:
		row.Status, row.Reason = StatusSkip, reasonRefused
		return row, nil
	}
	bound := func() (Row, *emit.Diagnostic) {
		d := vectorDiagnostic("EMIT-HARNESS-BOUND", run.v.Name+"/"+fileRegistry, "")
		return row, &d
	}
	added, err := withSyntheticKind(run.v, h.m)
	if err != nil {
		return bound()
	}
	result := registry.ValidateDocuments(added.registry, added.kinds)
	if !result.Report.Valid {
		return bound()
	}
	changed, refused := emit.FormRequests(h.m, emit.Inputs{Registry: result, Options: run.v.Options})
	if len(refused) > 0 {
		row.Status, row.Reason, row.Diagnostics = StatusFail, reasonFilterRefused, bounded(refused)
		return row, nil
	}
	if len(changed) != len(run.requests) {
		row.Status, row.Reason = StatusFail, reasonFilterBytes
		return row, nil
	}
	restored := make([]emit.Request, len(changed))
	for i, request := range changed {
		data, reason := filteredRequest(run.requests[i].Bytes, request.Bytes, added.id)
		if reason != "" {
			row.Status, row.Reason, row.Environment = StatusFail, reason, request.Environment
			return row, nil
		}
		restored[i] = emit.Request{Environment: request.Environment, Version: request.Version, Bytes: data}
	}
	// Only once the request is the original's does the emitter run on it:
	// the restored bytes are the original request's bytes exactly, so this
	// run repeats run 1, and the byte comparison above carries the check.
	for i, request := range restored {
		if !sameOutcome(run.results[i], h.execute(request, nil)) {
			row.Status, row.Reason, row.Environment = StatusFail, reasonFilterResponse, request.Environment
			return row, nil
		}
	}
	row.Status = StatusPass
	return row, nil
}
