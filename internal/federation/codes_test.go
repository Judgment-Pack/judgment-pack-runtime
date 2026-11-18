package federation

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/regsig"
)

// Codes is written in the reader; it must be the Spec's codes.json, which the
// reader never reads: every code, its severity, and its meaning as the
// message, capitalized and ending with a period. A meaning that begins with a
// signature document's member name keeps that name in the schema's case.
func TestCodesAreTheSpecList(t *testing.T) {
	var list struct {
		Companion     string `json:"companion"`
		FormatVersion string `json:"formatVersion"`
		Status        string `json:"status"`
		Codes         []struct {
			Code     string `json:"code"`
			Severity string `json:"severity"`
			Status   string `json:"status"`
			Section  string `json:"section"`
			Meaning  string `json:"meaning"`
		} `json:"codes"`
	}
	raw, err := os.ReadFile("testdata/1.1.0/codes.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if list.Companion != "registry" || list.FormatVersion != "1.1.0" || list.Status != "released" {
		t.Fatalf("codes.json is for %s %s, %s", list.Companion, list.FormatVersion, list.Status)
	}
	members := map[string]bool{}
	for _, kind := range []regsig.Kind{regsig.Lock, regsig.Source} {
		for _, member := range kind.Members() {
			members[member] = true
		}
	}
	federation, lock := 0, 0
	for _, code := range list.Codes {
		info, ok := Codes[code.Code]
		if !ok {
			t.Errorf("codes.json lists %s, which Codes does not", code.Code)
			continue
		}
		if code.Severity != "error" || code.Status != "active" || info.severity != SeverityError {
			t.Errorf("%s is %s and %s in codes.json, %s in Codes", code.Code, code.Severity, code.Status, info.severity)
		}
		want := code.Meaning + "."
		if word, _, _ := strings.Cut(code.Meaning, " "); !members[word] {
			first, size := utf8.DecodeRuneInString(code.Meaning)
			want = string(unicode.ToUpper(first)) + code.Meaning[size:] + "."
		}
		if info.message != want {
			t.Errorf("%s: the message is %q; codes.json's meaning gives %q", code.Code, info.message, want)
		}
		switch {
		case strings.HasPrefix(code.Code, "FED-"):
			federation++
		case strings.HasPrefix(code.Code, "LOCK-"):
			lock++
		default:
			t.Errorf("codes.json lists %s, of neither family", code.Code)
		}
	}
	if federation != 22 || lock != 31 || len(Codes) != len(list.Codes) {
		t.Fatalf("codes.json lists %d FED- and %d LOCK- codes; Codes holds %d", federation, lock, len(Codes))
	}
}
