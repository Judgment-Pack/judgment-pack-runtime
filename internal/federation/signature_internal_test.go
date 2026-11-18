package federation

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/regsig"
)

func codesOf(problems []signatureProblem) []string {
	var out []string
	for _, p := range problems {
		out = append(out, p.code)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// rowSignatureDocuments are every signature document the rows hold.
func rowSignatureDocuments(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	root := filepath.Join("testdata", "1.1.0")
	for _, pattern := range []string{"locks/*.sig.json", "signatures/*.json", "vectors/*/*.sig.json", "vectors/*/*signature*"} {
		paths, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			out[path] = data
		}
	}
	if len(out) < 20 {
		t.Fatalf("only %d signature documents", len(out))
	}
	return out
}

// The verifier's reader and the writer's (readSourceSignature) give a
// source signature document the same codes, and the same identity, on every
// signature document of the rows and on the edits below. They differ on one
// reading only, which no row holds: a number out of registry §2.2's range is
// LOCK-SIG-DOCUMENT to the writer's, which admits numbers, and the member's
// own code to the verifier's, which reads members first.
func TestTheTwoReadersAgree(t *testing.T) {
	documents := rowSignatureDocuments(t)
	standard := string(documents[filepath.Join("testdata", "1.1.0", "signatures", "standard.json")])
	for name, edit := range map[string]string{
		"unknown string": strings.Replace(standard, `{`, `{"comment":"x",`, 1),
		"version number": strings.Replace(standard, `"sourceSignatureVersion":"1.1.0"`, `"sourceSignatureVersion":1`, 1),
		"key number":     strings.Replace(standard, `"publicKey":"`, `"publicKey":7,"x":"`, 1),
		"array":          "[" + standard + "]",
		"white space":    strings.ReplaceAll(standard, `,`, " ,\n\t"),
		"duplicate":      strings.Replace(standard, `{`, `{"algorithm":"ed25519",`, 1),
		"empty":          "",
		"byte order":     "\xef\xbb\xbf" + standard,
		"not UTF-8":      strings.Replace(standard, `ed25519`, "ed\xff5519", 1),
	} {
		documents[name] = []byte(edit)
	}
	for name, data := range documents {
		document := registry.Document{Data: data}
		writer, writerCodes := readSourceSignature(document)
		verifier, problems := readSignature(document, regsig.Source)
		slices.Sort(writerCodes)
		writerCodes = slices.Compact(writerCodes)
		if got := codesOf(problems); !slices.Equal(got, writerCodes) {
			t.Errorf("%s: verifier %v, writer %v", name, got, writerCodes)
		}
		if (writer == nil) != (len(problems) > 0) {
			t.Errorf("%s: one reader reads it and the other does not", name)
		}
		if writer != nil && verifier.identity != writer.identity {
			t.Errorf("%s: identities %s and %s", name, verifier.identity, writer.identity)
		}
	}
}

// A document's key identifier is named once it is read as an object with a
// publicKey of its form, whatever else refuses it, and never for a document
// not read.
func TestASignatureNamesItsKeyOnceRead(t *testing.T) {
	documents := rowSignatureDocuments(t)
	lock := string(documents[filepath.Join("testdata", "1.1.0", "locks", "single-42.lock.json.sig.json")])
	const test1 = "21fe31dfa154a261626bf854046fd227"
	for name, c := range map[string]struct {
		data, keyID string
		codes       []string
		pointer     string
	}{
		"a later version":  {strings.Replace(lock, `"1.1.0"`, `"1.2.0"`, 1), test1, []string{"LOCK-SIG-VERSION"}, "/lockSignatureVersion"},
		"a member":         {strings.Replace(lock, `{`, `{"a/b~c":"x",`, 1), test1, []string{"LOCK-SIG-MEMBER"}, "/a~1b~0c"},
		"a backslash":      {strings.Replace(lock, `ed25519`, "ed"+string(rune(0x5c))+"u0032519", 1), "", []string{"LOCK-SIG-DOCUMENT"}, ""},
		"an uppercase key": {strings.Replace(lock, `d75a98`, `D75A98`, 1), "", []string{"LOCK-SIG-PUBLIC-KEY"}, "/publicKey"},
		"an exponent":      {strings.Replace(lock, `{`, `{"x":1e400,`, 1), "", []string{"LOCK-SIG-DOCUMENT"}, ""},
		"a number member":  {strings.Replace(lock, `{`, `{"x":1,`, 1), test1, []string{"LOCK-SIG-MEMBER"}, "/x"},
		"a refused key":    {strings.Replace(lock, `"publicKey":"d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"`, `"publicKey":1e400`, 1), "", []string{"LOCK-SIG-DOCUMENT"}, ""},
		"a number key":     {strings.Replace(lock, `"publicKey":"d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"`, `"publicKey":1`, 1), "", []string{"LOCK-SIG-PUBLIC-KEY"}, "/publicKey"},
		"over its bound":   {lock + strings.Repeat(" ", regsig.MaxBytes), "", []string{"LOCK-SIG-DOCUMENT"}, ""},
	} {
		read, problems := readSignature(registry.Document{Data: []byte(c.data)}, regsig.Lock)
		if got := codesOf(problems); !slices.Equal(got, c.codes) || problems[0].pointer != c.pointer {
			t.Errorf("%s: %+v, want %v at %q", name, problems, c.codes, c.pointer)
			continue
		}
		keyID := ""
		if read != nil {
			keyID = read.keyID
		}
		if keyID != c.keyID {
			t.Errorf("%s: key identifier %q, want %q", name, keyID, c.keyID)
		}
	}
}
