package startup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

// Redacted is what every printed or encoded form of a [Secret] is.
const Redacted = "[secret]"

// ErrNotDecodable is returned when something tries to decode a [Secret] from
// JSON or text. A Secret is made by a [Reader] or by [NewSecret], never decoded:
// a secret position of a resolved registry holds a reference, not a value.
var ErrNotDecodable = errors.New("startup: a Secret is read through a Reader, never decoded")

// Secret holds one secret value. Its printed and encoded forms are all
// [Redacted]:
//   - every fmt verb that fmt passes to the value, %v, %+v, %#v, %s, %q, %x and
//     %d included. fmt answers %T with the type's name and %p with a notice
//     that holds an address, before any method of the value runs, so neither
//     reaches the bytes;
//   - String, for code that prints through fmt.Stringer;
//   - encoding/json, as the JSON string "[secret]";
//   - encoding.TextMarshaler, which encoding/xml uses;
//   - log/slog, through slog.LogValuer.
//
// The bytes are reached only through [Secret.Reveal]. The zero Secret holds no
// value.
type Secret struct {
	// fmt prints a value by reflection, without calling its methods, when it
	// sits in an unexported struct field, and it then follows a pointer to a
	// struct when a verb does not fit it. So the bytes are held where
	// reflection cannot reach them: captured by a function, whose captured
	// variables reflection cannot read, and which fmt prints as an address.
	// The pointer keeps Secret comparable.
	box *secretBox
}

type secretBox struct {
	value func() []byte
}

// NewSecret returns a Secret holding a copy of value. A reader in another
// package, such as a later secret-store reader, makes its Secret with it.
func NewSecret(value []byte) Secret {
	held := bytes.Clone(value)
	return Secret{box: &secretBox{value: func() []byte { return held }}}
}

// Reveal returns a copy of the secret's bytes, or nil for the zero Secret.
// It is the only way to reach them.
func (s Secret) Reveal() []byte {
	if s.box == nil {
		return nil
	}
	return bytes.Clone(s.box.value())
}

// IsZero reports whether s is the zero Secret, which holds no value.
func (s Secret) IsZero() bool {
	return s.box == nil
}

// Format implements fmt.Formatter: every verb prints [Redacted], whatever its
// flags, width or precision.
func (Secret) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, Redacted)
}

// String implements fmt.Stringer.
func (Secret) String() string {
	return Redacted
}

// MarshalJSON implements json.Marshaler.
func (Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"` + Redacted + `"`), nil
}

// MarshalText implements encoding.TextMarshaler.
func (Secret) MarshalText() ([]byte, error) {
	return []byte(Redacted), nil
}

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value {
	return slog.StringValue(Redacted)
}

// UnmarshalJSON implements json.Unmarshaler by refusing, with ErrNotDecodable.
func (*Secret) UnmarshalJSON([]byte) error {
	return ErrNotDecodable
}

// UnmarshalText implements encoding.TextUnmarshaler by refusing, with
// ErrNotDecodable.
func (*Secret) UnmarshalText([]byte) error {
	return ErrNotDecodable
}
