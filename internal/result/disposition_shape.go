package result

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// CanonicalDispositionShape is the persisted disposition: Disposition.CanonicalWith
// fills one and encodes it with encodeDeclared, so a member is written only when
// this type declares it (#342). The audit-formats check (#327) fingerprints this
// type as the disposition of every evaluations.jsonl record, so declaring a new
// member changes the fingerprint and needs a format decision. The check also holds
// exemplar dispositions to it: a declared member no exemplar writes fails. Members
// are listed in the canonical (code-point) order.
//
// It is never encoded by encoding/json. Here omitempty omits an empty string and a
// nil slice or map, and a non-nil empty slice is written as []: Core 0.3 writes
// routes as [] for a routed pack that selected no route.
type CanonicalDispositionShape struct {
	Handoff struct {
		Routes      []HandoffTarget `json:"routes,omitempty"`
		State       string          `json:"state"`
		TriggeredBy []string        `json:"triggeredBy,omitempty"`
	} `json:"handoff"`
	Kind      string         `json:"kind"`
	OutcomeID string         `json:"outcomeId,omitempty"`
	Reasons   []string       `json:"reasons"`
	Value     map[string]any `json:"value,omitempty"`
}

// encodeDeclared is the RFC 8785 form of value, built from value's Go type: a struct
// is an object of its declared members, a slice an array, a string itself, and a
// map[string]any is passed to the canonical encoder unchanged. A map is taken only
// as the top-level value member, the pack-defined contents the fingerprint reads as
// one opaque map (#350): a map member anywhere else, nil or not, is refused, since
// its keys would be written without the fingerprint naming them. Every struct field
// must be exported, not embedded, and named by a json tag whose only option is
// omitempty. Anything else is refused, so the members written are exactly the ones
// the audit-formats fingerprint reads from the same type.
func encodeDeclared(value any) ([]byte, error) {
	members, err := declaredValue(reflect.ValueOf(value), "")
	if err != nil {
		return nil, err
	}
	return jcs.Encode(members)
}

// valueMember is the one path at which a map is taken: the top-level value member.
const valueMember = "value"

// declaredValue encodes value, found at path: the json names from the top level,
// joined by ".", with "[]" for a slice's items ("" is the top level itself).
func declaredValue(value reflect.Value, path string) (any, error) {
	switch value.Kind() {
	case reflect.String:
		return value.String(), nil
	case reflect.Slice:
		items := make([]any, value.Len())
		for index := range items {
			item, err := declaredValue(value.Index(index), path+"[]")
			if err != nil {
				return nil, err
			}
			items[index] = item
		}
		return items, nil
	case reflect.Map:
		if path != valueMember {
			where := path
			if where == "" {
				where = "the top level"
			}
			return nil, fmt.Errorf("disposition shape: a member of kind map at %s is taken only as the top-level %s member", where, valueMember)
		}
		if contents, ok := value.Interface().(map[string]any); ok {
			return contents, nil
		}
	case reflect.Struct:
		object := map[string]any{}
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
			if !field.IsExported() || field.Anonymous || name == "" || name == "-" || (options != "" && options != "omitempty") {
				return nil, fmt.Errorf("disposition shape: field %s of %s is not an exported member named by a json tag with only omitempty", field.Name, value.Type())
			}
			member := value.Field(index)
			memberPath := name
			if path != "" {
				memberPath = path + "." + name
			}
			// The member is encoded before omitempty is applied, so a member the
			// shape refuses is refused even when it is empty.
			encoded, err := declaredValue(member, memberPath)
			if err != nil {
				return nil, err
			}
			if options == "omitempty" && omitted(member) {
				continue
			}
			object[name] = encoded
		}
		return object, nil
	}
	return nil, fmt.Errorf("disposition shape: a member of kind %s is outside the canonical value space", value.Kind())
}

// omitted is the omitempty rule above: an empty string, a nil slice or a nil map.
func omitted(member reflect.Value) bool {
	switch member.Kind() {
	case reflect.String:
		return member.Len() == 0
	case reflect.Slice, reflect.Map:
		return member.IsNil()
	}
	return false
}
