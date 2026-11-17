package main

import (
	"errors"
	"strings"
	"testing"
)

// The request's nesting is counted in containers, as the Go loader counts it:
// 34 containers are read whatever the innermost value is, and 35 are refused
// even when the innermost is an empty object.
func TestTheDepthBoundCountsContainers(t *testing.T) {
	nested := func(containers int, innermost string) []byte {
		return []byte(strings.Repeat(`{"a":`, containers) + innermost + strings.Repeat(`}`, containers))
	}
	for _, tc := range []struct {
		name string
		data []byte
		ok   bool
	}{
		{"34 containers, a string innermost", nested(34, `"x"`), true},
		{"34 containers, the innermost empty", nested(33, `{}`), true},
		{"35 containers, a string innermost", nested(35, `"x"`), false},
		{"35 containers, the innermost empty", nested(34, `{}`), false},
		{"35 containers, an array innermost", nested(34, `[]`), false},
	} {
		_, err := decodeStrict(tc.data)
		if tc.ok && err != nil {
			t.Errorf("%s: %v, want it read", tc.name, err)
		}
		if !tc.ok && !errors.Is(err, errRequest) {
			t.Errorf("%s: %v, want the request refused", tc.name, err)
		}
	}
}
