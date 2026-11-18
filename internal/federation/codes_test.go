package federation

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// Codes is written in the reader; it must be the Spec's codes.json, which the
// reader never reads: every code, its severity, and its meaning as the
// message, capitalized and ending with a period.
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
		first, size := utf8.DecodeRuneInString(code.Meaning)
		if want := string(unicode.ToUpper(first)) + code.Meaning[size:] + "."; info.message != want {
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
