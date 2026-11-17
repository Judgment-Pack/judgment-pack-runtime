package terraformfile

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// The documents a source can name.
const (
	DocumentPlan  = "plan"
	DocumentState = "state"
)

// The resource modes select.mode can name.
const (
	ModeManaged = "managed"
	ModeData    = "data"
)

// planActions are the plan actions select.actions can name.
var planActions = map[string]bool{"create": true, "update": true, "delete": true, "read": true, "no-op": true}

// Source is a terraform-file source's members (RFC 0038 §1). The file is not
// one of them: the caller binds it at evaluation time.
//
// MaxBytes 0 means the member is absent, and reads as DefaultMaxBytes. A
// dialect reader holds a declared maxBytes to 1..MaxBytesLimit before it fills
// this field, since 0 is not a value the member can take.
type Source struct {
	Document string
	Select   Select
	MaxBytes int64
}

// Select is a source's select member (RFC 0038 §4).
//
//   - Mode "" means the member is absent, and reads as ModeManaged.
//   - Address nil means absent: every address. A non-nil Address is a pattern,
//     and the empty pattern matches the empty address only.
//   - Actions nil means absent: every change. A non-nil empty Actions is an
//     empty array, which selects no change.
//   - Max 0 means absent, and reads as DefaultSelectMax. As for MaxBytes, a
//     dialect reader holds a declared max to 1..SelectMaxLimit first.
type Select struct {
	Types   []string
	Mode    string
	Address *string
	Actions []string
	Max     int
}

// ErrInvalidSource is the error Validate wraps. A malformed source makes the
// dialect invalid, not the file (RFC 0038 §4), so it is never a Refusal.
var ErrInvalidSource = errors.New("terraformfile: invalid source")

func invalid(member, rule string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidSource, member, rule)
}

// Validate reports whether a source's members have the forms RFC 0038 §1 and
// §4 give them. The error names the member and the rule broken, never a value.
func (s Source) Validate() error {
	if s.Document != DocumentPlan && s.Document != DocumentState {
		return invalid("document", "is not plan or state")
	}
	if s.MaxBytes < 0 || s.MaxBytes > MaxBytesLimit {
		return invalid("maxBytes", "is outside 1 to 268435456")
	}
	selection := s.Select
	if len(selection.Types) == 0 {
		return invalid("select.types", "is empty")
	}
	if len(selection.Types) > MaxTypes {
		return invalid("select.types", "lists more than 64 type names")
	}
	seen := make(map[string]bool, len(selection.Types))
	for _, name := range selection.Types {
		if name == "" || !utf8.ValidString(name) {
			return invalid("select.types", "holds a name that is empty or not UTF-8")
		}
		if seen[name] {
			return invalid("select.types", "lists a type name twice")
		}
		seen[name] = true
	}
	switch selection.Mode {
	case "", ModeManaged, ModeData:
	default:
		return invalid("select.mode", "is not managed or data")
	}
	if selection.Address != nil {
		pattern := *selection.Address
		if !utf8.ValidString(pattern) {
			return invalid("select.address", "is not UTF-8")
		}
		if utf8.RuneCountInString(pattern) > MaxPatternChars {
			return invalid("select.address", "is longer than 512 characters")
		}
	}
	if selection.Actions != nil {
		if s.Document != DocumentPlan {
			return invalid("select.actions", "is declared on a state source")
		}
		for _, action := range selection.Actions {
			if !planActions[action] {
				return invalid("select.actions", "holds a value that is not create, update, delete, read or no-op")
			}
		}
	}
	if selection.Max < 0 || selection.Max > SelectMaxLimit {
		return invalid("select.max", "is outside 1 to 10000")
	}
	return nil
}

func (s Source) maxBytes() int64 {
	if s.MaxBytes == 0 {
		return DefaultMaxBytes
	}
	return s.MaxBytes
}

func (s Select) mode() string {
	if s.Mode == "" {
		return ModeManaged
	}
	return s.Mode
}

func (s Select) max() int {
	if s.Max == 0 {
		return DefaultSelectMax
	}
	return s.Max
}
