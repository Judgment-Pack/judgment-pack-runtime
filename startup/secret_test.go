package startup

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"testing"
)

// The value every test here holds: synthetic, and long enough that no
// fragment of an address or a type name matches it by chance.
const syntheticValue = "synthetic-value-qz7k-not-a-credential"

// assertHidden fails when out does not say Redacted or carries any part of
// the value: the whole value, or its bytes as fmt would write them in
// decimal or hexadecimal.
func assertHidden(t *testing.T, name, out string) {
	t.Helper()
	if !strings.Contains(out, Redacted) {
		t.Errorf("%s: got %q, which does not say %s", name, out, Redacted)
	}
	assertNoValue(t, name, out)
}

func assertNoValue(t *testing.T, name, out string) {
	t.Helper()
	for _, form := range []string{
		syntheticValue,
		syntheticValue[:8],
		fmt.Sprintf("%x", syntheticValue)[:16],
		fmt.Sprint([]byte(syntheticValue))[1:16],
	} {
		if strings.Contains(out, form) {
			t.Errorf("%s: got %q, which holds %q from the value", name, out, form)
		}
	}
}

func TestSecretPrintsRedactedUnderEveryVerb(t *testing.T) {
	secret := NewSecret([]byte(syntheticValue))
	pointer := &secret
	type exported struct{ Password Secret }
	type unexported struct{ password Secret }
	values := map[string]any{
		"value":              secret,
		"pointer":            pointer,
		"slice":              []Secret{secret},
		"map":                map[string]Secret{"k": secret},
		"exported field":     exported{secret},
		"pointer to struct":  &exported{secret},
		"interface in slice": []any{secret},
	}
	verbs := []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%10.3s", "%-8v", "% x", "%#x", "%t", "%e", "%U", "%c", "%b", "%o"}
	for name, value := range values {
		for _, verb := range verbs {
			assertHidden(t, name+" "+verb, fmt.Sprintf(verb, value))
		}
	}
	for _, out := range []string{fmt.Sprint(secret), fmt.Sprintln(secret), secret.String()} {
		if strings.TrimSpace(out) != Redacted {
			t.Errorf("got %q, want exactly %s", out, Redacted)
		}
	}
	if got := fmt.Sprintf("%v|%#v|%s|%q|%x", secret, secret, secret, secret, secret); got != strings.Repeat(Redacted+"|", 4)+Redacted {
		t.Errorf("got %q, want %s under each verb", got, Redacted)
	}
	// fmt answers these two before any method runs: %T with the type's name
	// and %p with a notice. Neither may carry the value, including through a
	// field fmt reads by reflection.
	for _, verb := range append([]string{"%T", "%p", "%#p"}, verbs...) {
		for name, value := range map[string]any{"value": secret, "unexported field": unexported{secret}, "pointer to unexported": &unexported{secret}} {
			assertNoValue(t, name+" "+verb, fmt.Sprintf(verb, value))
		}
	}
	var logged bytes.Buffer
	log.New(&logged, "", 0).Printf("password=%v %s", secret, secret)
	assertHidden(t, "log", logged.String())
	if err := fmt.Errorf("wrapped: %v", secret); !strings.Contains(err.Error(), Redacted) {
		t.Errorf("got %q", err)
	}
}

func TestSecretEncodesRedactedAsJSON(t *testing.T) {
	secret := NewSecret([]byte(syntheticValue))
	type document struct {
		Password Secret            `json:"password"`
		Pointer  *Secret           `json:"pointer"`
		Many     []Secret          `json:"many"`
		ByKey    map[string]Secret `json:"byKey"`
		Keyed    map[Secret]int    `json:"keyed"`
	}
	out, err := json.Marshal(document{secret, &secret, []Secret{secret}, map[string]Secret{"k": secret}, map[Secret]int{secret: 1}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"password":"[secret]","pointer":"[secret]","many":["[secret]"],"byKey":{"k":"[secret]"},"keyed":{"[secret]":1}}`
	if string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
	direct, err := json.Marshal(secret)
	if err != nil || string(direct) != `"[secret]"` {
		t.Errorf("got %s, %v", direct, err)
	}
	indented, err := json.MarshalIndent(map[string]any{"s": secret}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	assertHidden(t, "indented", string(indented))
}

func TestSecretEncodesRedactedAsText(t *testing.T) {
	secret := NewSecret([]byte(syntheticValue))
	text, err := secret.MarshalText()
	if err != nil || string(text) != Redacted {
		t.Errorf("got %q, %v", text, err)
	}
	type element struct {
		XMLName  xml.Name `xml:"service"`
		Password Secret   `xml:"password"`
		Token    Secret   `xml:"token,attr"`
	}
	out, err := xml.Marshal(element{Password: secret, Token: secret})
	if err != nil {
		t.Fatal(err)
	}
	if want := `<service token="[secret]"><password>[secret]</password></service>`; string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
	// encoding/gob does not use MarshalText for a struct with no exported
	// fields: it refuses, and writes nothing of the value.
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(struct{ Password Secret }{secret}); err == nil {
		t.Error("gob encoded a Secret")
	}
	assertNoValue(t, "gob", encoded.String())
}

func TestSecretLogsRedactedThroughSlog(t *testing.T) {
	secret := NewSecret([]byte(syntheticValue))
	type config struct {
		Password Secret
		Name     string
	}
	if got := secret.LogValue(); got.Kind() != slog.KindString || got.String() != Redacted {
		t.Errorf("got %v", got)
	}
	for name, handler := range map[string]func(*bytes.Buffer) slog.Handler{
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	} {
		var out bytes.Buffer
		logger := slog.New(handler(&out))
		logger.Info("start", "password", secret, "pointer", &secret, slog.Any("config", config{secret, "n"}),
			slog.Group("db", slog.Any("password", secret)))
		logger.With("bound", secret).Warn("again")
		line := out.String()
		assertHidden(t, name, line)
		if count := strings.Count(line, Redacted); count != 5 {
			t.Errorf("%s: got %d redactions in %q, want 5", name, count, line)
		}
	}
}

func TestSecretRevealReturnsACopy(t *testing.T) {
	value := []byte(syntheticValue)
	secret := NewSecret(value)
	value[0] = 'X'
	revealed := secret.Reveal()
	if string(revealed) != syntheticValue {
		t.Fatalf("NewSecret kept the caller's slice: got %q", revealed)
	}
	revealed[0] = 'Y'
	if string(secret.Reveal()) != syntheticValue {
		t.Fatal("Reveal gave the stored bytes, not a copy")
	}
	if secret.IsZero() {
		t.Error("a Secret holding a value reported zero")
	}
	var zero Secret
	if !zero.IsZero() || zero.Reveal() != nil {
		t.Error("the zero Secret holds a value")
	}
	assertHidden(t, "zero", fmt.Sprintf("%v %#v", zero, zero))
}

func TestSecretRefusesToBeDecoded(t *testing.T) {
	var document struct{ Password Secret }
	reference := `{"Password":"secret:gcp-sm:projects/example-prod/secrets/n/versions/1"}`
	if err := json.Unmarshal([]byte(reference), &document); !errors.Is(err, ErrNotDecodable) {
		t.Errorf("json: got %v, want ErrNotDecodable", err)
	}
	for _, input := range []string{`{"Password":{}}`, `{"Password":[]}`, `{"Password":7}`} {
		document.Password = Secret{}
		if err := json.Unmarshal([]byte(input), &document); !errors.Is(err, ErrNotDecodable) {
			t.Errorf("%s: got %v, want ErrNotDecodable", input, err)
		}
		if !document.Password.IsZero() {
			t.Errorf("%s: decoding set a value", input)
		}
	}
	var element struct {
		Password Secret `xml:"password"`
	}
	if err := xml.Unmarshal([]byte(`<x><password>v</password></x>`), &element); !errors.Is(err, ErrNotDecodable) {
		t.Errorf("xml: got %v, want ErrNotDecodable", err)
	}
	var secret Secret
	if err := secret.UnmarshalText([]byte("v")); !errors.Is(err, ErrNotDecodable) {
		t.Errorf("text: got %v", err)
	}
}
