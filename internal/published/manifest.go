package published

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
)

// ManifestVersion is the one manifest format this runtime writes and reads.
// The manifest is a closed input: a reader refuses a member it does not know,
// so adding one moves this version.
const ManifestVersion = "1"

// ErrManifestVersion is a manifest that declares a manifestVersion this
// runtime does not read: a later format, not a damaged one.
var ErrManifestVersion = errors.New("the manifest declares a manifestVersion this runtime does not read")

// MaxManifestBytes bounds a manifest, read or written: the hard input limit.
// A manifest that would encode past it is not written, because no run could
// read it back.
const MaxManifestBytes = carrier.HardMaxBytes

// Manifest pins every published identity to the SHA-256 of the bytes its
// first tag published.
type Manifest struct {
	ManifestVersion string          `json:"manifestVersion"`
	Documents       []ManifestEntry `json:"documents"`
}

// ManifestEntry is one pinned identity.
type ManifestEntry struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Version  string `json:"version"`
	Digest   string `json:"digest"`
	FirstTag string `json:"firstTag"`
}

func (e ManifestEntry) identity() Identity {
	return Identity{Kind: e.Kind, ID: e.ID, Version: e.Version}
}

func sortEntries(entries []ManifestEntry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].identity().less(entries[j].identity()) })
}

// EncodeManifest writes a manifest's deterministic bytes: the entries sorted
// by kind, id and version, one entry per line, members in a fixed order, a
// final line feed. The same entries always give the same bytes.
func EncodeManifest(m Manifest) []byte {
	entries := append([]ManifestEntry(nil), m.Documents...)
	sortEntries(entries)
	var out bytes.Buffer
	out.WriteString("{\n  \"manifestVersion\": " + jsonString(ManifestVersion) + ",\n  \"documents\": [")
	for index, entry := range entries {
		if index > 0 {
			out.WriteString(",")
		}
		out.WriteString("\n    {\"kind\": " + jsonString(entry.Kind) +
			", \"id\": " + jsonString(entry.ID) +
			", \"version\": " + jsonString(entry.Version) +
			", \"digest\": " + jsonString(entry.Digest) +
			", \"firstTag\": " + jsonString(entry.FirstTag) + "}")
	}
	if len(entries) > 0 {
		out.WriteString("\n  ")
	}
	out.WriteString("]\n}\n")
	return out.Bytes()
}

func jsonString(value string) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return string(bytes.TrimSuffix(buffer.Bytes(), []byte("\n")))
}

// ReadManifest reads a manifest's bytes. It refuses anything this runtime
// would not write: a duplicate or unknown member, another manifestVersion, a
// kind other than pack or dialect, an empty id, version or firstTag, a
// digest not of the form sha256: and sixty-four lowercase hexadecimal
// digits, or two entries for one identity.
func ReadManifest(data []byte) (*Manifest, error) {
	if int64(len(data)) > MaxManifestBytes {
		return nil, fmt.Errorf("the manifest is larger than %d bytes", MaxManifestBytes)
	}
	decoded, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		return nil, fmt.Errorf("the manifest is not acceptable JSON: %s", failure.Diagnostic.Message)
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("the manifest must be an object with exactly the members manifestVersion and documents")
	}
	// The version is compared before the member set, as the baseline reader
	// does: a later format may add members, and is a later format, not damage.
	if declared, isString := root["manifestVersion"].(string); isString && declared != ManifestVersion {
		return nil, fmt.Errorf("%w (it reads %q)", ErrManifestVersion, ManifestVersion)
	}
	if !exactMembers(root, "manifestVersion", "documents") {
		return nil, fmt.Errorf("the manifest must be an object with exactly the members manifestVersion and documents")
	}
	if _, isString := root["manifestVersion"].(string); !isString {
		return nil, fmt.Errorf("the manifest's manifestVersion must be a string")
	}
	documents, ok := root["documents"].([]any)
	if !ok {
		return nil, fmt.Errorf("the manifest's documents member must be an array")
	}
	m := Manifest{ManifestVersion: ManifestVersion, Documents: make([]ManifestEntry, 0, len(documents))}
	for index, item := range documents {
		object, ok := item.(map[string]any)
		if !ok || !exactMembers(object, "kind", "id", "version", "digest", "firstTag") {
			return nil, fmt.Errorf("manifest entry %d must be an object with exactly the members kind, id, version, digest and firstTag", index)
		}
		var entry ManifestEntry
		for name, target := range map[string]*string{"kind": &entry.Kind, "id": &entry.ID, "version": &entry.Version, "digest": &entry.Digest, "firstTag": &entry.FirstTag} {
			value, isString := object[name].(string)
			if !isString {
				return nil, fmt.Errorf("manifest entry %d: %s must be a string", index, name)
			}
			*target = value
		}
		m.Documents = append(m.Documents, entry)
	}
	seen := map[Identity]bool{}
	for index, entry := range m.Documents {
		if entry.Kind != KindPack && entry.Kind != KindDialect {
			return nil, fmt.Errorf("manifest entry %d: kind must be %q or %q", index, KindPack, KindDialect)
		}
		if entry.ID == "" || entry.Version == "" || entry.FirstTag == "" {
			return nil, fmt.Errorf("manifest entry %d: id, version and firstTag must be non-empty strings", index)
		}
		if !validDigest(entry.Digest) {
			return nil, fmt.Errorf("manifest entry %d: digest must be sha256: and sixty-four lowercase hexadecimal digits", index)
		}
		if seen[entry.identity()] {
			return nil, fmt.Errorf("manifest entry %d: a second entry for one identity", index)
		}
		seen[entry.identity()] = true
	}
	return &m, nil
}

func exactMembers(object map[string]any, names ...string) bool {
	if len(object) != len(names) {
		return false
	}
	for _, name := range names {
		if _, present := object[name]; !present {
			return false
		}
	}
	return true
}

func validDigest(digest string) bool {
	const prefix = "sha256:"
	if len(digest) != len(prefix)+64 || digest[:len(prefix)] != prefix {
		return false
	}
	for index := len(prefix); index < len(digest); index++ {
		if c := digest[index]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
