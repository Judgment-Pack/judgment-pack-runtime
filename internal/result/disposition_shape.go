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
// map[string]any (the value member's pack-defined contents) is passed to the
// canonical encoder unchanged. Every struct field must be exported, not embedded,
// and named by a json tag whose only option is omitempty. Anything else is refused,
// so the members written are exactly the ones the audit-formats fingerprint reads
// from the same type.
func encodeDeclared(value any) ([]byte, error) {
	members, err := declaredValue(reflect.ValueOf(value))
	if err != nil {
		return nil, err
	}
	return jcs.Encode(members)
}

func declaredValue(value reflect.Value) (any, error) {
	switch value.Kind() {
	case reflect.String:
		return value.String(), nil
	case reflect.Slice:
		items := make([]any, value.Len())
		for index := range items {
			item, err := declaredValue(value.Index(index))
			if err != nil {
				return nil, err
			}
			items[index] = item
		}
		return items, nil
	case reflect.Map:
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
			if options == "omitempty" && omitted(member) {
				continue
			}
			encoded, err := declaredValue(member)
			if err != nil {
				return nil, err
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
