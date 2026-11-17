package carrier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
)

// referenceDecode is decode as it was before it read valid JSON from its bytes
// and built a location only for a refusal: every value read through the
// standard decoder's tokens, every location copied for every value. It is
// the behavior decode must keep, value for value and refusal for refusal.
func referenceDecode(data []byte, limits Limits, rootScan bool) (any, *Failure) {
	if !utf8.Valid(data) {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", "", "Input is not valid UTF-8 JSON.")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	p := referenceParser{decoder: decoder, limits: limits, data: data, rootScan: rootScan}
	value, failure := p.value(nil, 0)
	if failure != nil {
		return nil, failure
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", "", "Input contains trailing data after the JSON value.")
	}
	if offset, found := UnpairedSurrogateEscape(data); found {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", "",
			fmt.Sprintf("Input contains an unpaired surrogate escape at byte offset %d. RFC 8785 §3.2.2.2 makes such a value invalid rather than replaceable, and this decoder refuses it rather than substituting U+FFFD.", offset))
	}
	return value, nil
}

type referenceParser struct {
	decoder  *json.Decoder
	limits   Limits
	nodes    int
	data     []byte
	rootScan bool
}

func (p *referenceParser) value(location []string, depth int) (any, *Failure) {
	if p.nodes >= p.limits.MaxNodes {
		return nil, resource("JPS-RESOURCE-NODE-LIMIT", Pointer(location), "Input exceeds the configured JSON node limit.")
	}
	p.nodes++
	token, err := p.decoder.Token()
	if err != nil {
		return nil, p.invalidJSON(Pointer(location), "", err)
	}
	switch typed := token.(type) {
	case json.Delim:
		if depth >= p.limits.MaxDepth {
			return nil, resource("JPS-RESOURCE-DEPTH-LIMIT", Pointer(location), "Input exceeds the configured JSON nesting limit.")
		}
		switch typed {
		case '{':
			return p.object(location, depth+1)
		case '[':
			return p.array(location, depth+1)
		default:
			return nil, p.invalidJSON(Pointer(location), "unexpected JSON delimiter", nil)
		}
	case string:
		if len(typed) > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-STRING-LIMIT", Pointer(location), "Input contains a string exceeding the configured limit.")
		}
		return typed, nil
	case json.Number:
		if len(typed.String()) > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-NUMBER-LIMIT", Pointer(location), "Input contains a number token exceeding the configured limit.")
		}
		return typed, nil
	case bool, nil:
		return typed, nil
	default:
		return nil, p.invalidJSON(Pointer(location), "value outside the JSON data model", nil)
	}
}

func (p *referenceParser) object(location []string, depth int) (map[string]any, *Failure) {
	value := map[string]any{}
	var seen map[string]bool
	if p.rootScan {
		seen = map[string]bool{}
	}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return nil, p.invalidJSON(Pointer(location), "", err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, p.invalidJSON(Pointer(location), "object member name is not a string", nil)
		}
		memberLocation := referenceAppendLocation(location, key)
		if len(key) > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-STRING-LIMIT", Pointer(memberLocation), "Input contains an object member name exceeding the configured limit.")
		}
		_, exists := value[key]
		if p.rootScan {
			exists = seen[key]
		}
		if exists {
			return nil, invalid("JPS-CARRIER-DUPLICATE-MEMBER", Pointer(memberLocation), "Object member name is duplicated.")
		}
		if p.rootScan {
			seen[key] = true
		}
		item, failure := p.value(memberLocation, depth)
		if failure != nil {
			return nil, failure
		}
		if !p.rootScan || depth == 1 && (key == "specVersion" || key == "profile") {
			value[key] = item
		}
	}
	if token, err := p.decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, p.invalidJSON(Pointer(location), "expected the end of the object", err)
	}
	return value, nil
}

func (p *referenceParser) array(location []string, depth int) ([]any, *Failure) {
	value := []any{}
	for index := 0; p.decoder.More(); index++ {
		itemLocation := referenceAppendLocation(location, fmt.Sprint(index))
		item, failure := p.value(itemLocation, depth)
		if failure != nil {
			return nil, failure
		}
		if !p.rootScan {
			value = append(value, item)
		}
	}
	if token, err := p.decoder.Token(); err != nil || token != json.Delim(']') {
		return nil, p.invalidJSON(Pointer(location), "expected the end of the array", err)
	}
	return value, nil
}

func referenceAppendLocation(location []string, part string) []string {
	copyOfLocation := append([]string(nil), location...)
	return append(copyOfLocation, part)
}

func (p *referenceParser) invalidJSON(location, detail string, err error) *Failure {
	offset := p.decoder.InputOffset()
	line, column := lineColumn(p.data, offset)
	message := fmt.Sprintf("Input is not valid JSON at line %d, column %d (byte offset %d)", line, column, offset)
	if err != nil {
		detail = display.Sanitize(err.Error())
	}
	if detail != "" {
		message += ": " + detail
	}
	return invalid("JPS-CARRIER-INVALID-JSON", location, message+".")
}

// sameAsReference fails t when decode and the reference read data apart:
// a different value, a different refusal, or one refusing what the other
// accepts. Both the full decode and the root scan are compared.
func sameAsReference(t *testing.T, name string, data []byte, limits Limits) {
	t.Helper()
	for _, rootScan := range []bool{false, true} {
		got, gotFailure := decode(data, limits, rootScan)
		want, wantFailure := referenceDecode(data, limits, rootScan)
		if !reflect.DeepEqual(gotFailure, wantFailure) {
			t.Fatalf("%s (limits %+v, root scan %v): refusal %+v, want %+v", name, limits, rootScan, describe(gotFailure), describe(wantFailure))
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s (limits %+v, root scan %v): the decoded value differs from the reference", name, limits, rootScan)
		}
	}
}

func describe(failure *Failure) any {
	if failure == nil {
		return nil
	}
	return *failure
}

// tightLimits draws limits at and near what the document needs, so that each
// limit refuses at a location somewhere inside it.
func tightLimits(random *rand.Rand, data []byte) Limits {
	return Limits{
		MaxDepth:       1 + random.IntN(8),
		MaxNodes:       1 + random.IntN(1+len(data)/2),
		MaxStringBytes: random.IntN(1 + min(len(data), 64)),
	}
}

// Every JSON document in the repository, the conformance vectors among them,
// decodes to the same value or refusal as the reference: under the default
// limits, and under tight ones that refuse inside it.
func TestDecodeMatchesTheReferenceOverTheRepository(t *testing.T) {
	random := rand.New(rand.NewPCG(464, 1))
	documents := 0
	err := filepath.WalkDir(filepath.Join("..", ".."), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		documents++
		sameAsReference(t, path, data, DefaultLimits())
		for range 3 {
			sameAsReference(t, path, data, tightLimits(random, data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if documents < 500 {
		t.Fatalf("read %d documents, expected the repository's corpus", documents)
	}
	t.Logf("%d documents", documents)
}

// randomDocument writes a document of every kind of value: escapes of every
// kind in names and strings (paired and unpaired surrogates, reserved pointer
// characters), duplicate names, numbers of every layout, and whitespace
// around every token.
func randomDocument(random *rand.Rand, builder *strings.Builder, depth int) {
	space := func() {
		builder.WriteString([]string{"", "", "", " ", "\n", "\t", "\r\n  "}[random.IntN(7)])
	}
	space()
	kind := random.IntN(10)
	if depth > 5 && kind < 4 {
		kind += 4
	}
	switch kind {
	case 0, 1:
		builder.WriteByte('{')
		names := []string{}
		for i := range random.IntN(5) {
			if i > 0 {
				builder.WriteByte(',')
			}
			space()
			name := randomString(random)
			if len(names) > 0 && random.IntN(8) == 0 {
				name = names[random.IntN(len(names))]
			}
			if depth == 0 && random.IntN(3) == 0 {
				name = []string{`"specVersion"`, `"profile"`, `"specVersion"`}[random.IntN(3)]
			}
			names = append(names, name)
			builder.WriteString(name)
			space()
			builder.WriteByte(':')
			randomDocument(random, builder, depth+1)
		}
		space()
		builder.WriteByte('}')
	case 2, 3:
		builder.WriteByte('[')
		for i := range random.IntN(14) {
			if i > 0 {
				builder.WriteByte(',')
			}
			randomDocument(random, builder, depth+1)
		}
		space()
		builder.WriteByte(']')
	case 4, 5:
		builder.WriteString(randomString(random))
	case 6, 7:
		builder.WriteString([]string{"0", "-0", "1", "12", "-3.25", "1e20", "1E+2", "2.5e-3", "0.0", "123456789012345678901234567890",
			"1.7976931348623157e309", "4.9e-325", "-0.000001", "10", "99999"}[random.IntN(15)])
	default:
		builder.WriteString([]string{"true", "false", "null"}[random.IntN(3)])
	}
	space()
}

func randomString(random *rand.Rand) string {
	parts := []string{"a", "b", "~", "/", "~1", "é", "😀", `\"`, `\\`, `\/`, `\b`, `\f`, `\n`, `\r`, `\t`,
		`A`, `é`, `\u0000`, `😀`, `😀`, `\ud800`, `\udc00`, `\ud800A`,
		`\ud800\ud800`, `􏿿`, `�`, `￿`, "\u007f", "specVersion", "profile"}
	var builder strings.Builder
	builder.WriteByte('"')
	for range random.IntN(5) {
		builder.WriteString(parts[random.IntN(len(parts))])
	}
	builder.WriteByte('"')
	return builder.String()
}

// mutate damages a document: a cut, a changed byte, an inserted or a removed
// one, so that both readers meet documents that are not JSON, or are other
// JSON than was written.
func mutate(random *rand.Rand, data []byte) []byte {
	out := append([]byte(nil), data...)
	if len(out) == 0 {
		return out
	}
	alphabet := []byte("{}[],:\"\\ u0123456789abcdefeE.-+tfnrl\n\xff\xc3")
	for range 1 + random.IntN(3) {
		if len(out) == 0 {
			break
		}
		at := random.IntN(len(out))
		switch random.IntN(4) {
		case 0:
			out = out[:at]
		case 1:
			out[at] = alphabet[random.IntN(len(alphabet))]
		case 2:
			out = append(out[:at], append([]byte{alphabet[random.IntN(len(alphabet))]}, out[at:]...)...)
		default:
			out = append(out[:at], out[at+1:]...)
		}
	}
	return out
}

// Random documents, and damaged copies of them, decode to the same value or
// refusal as the reference under default and tight limits.
func TestDecodeMatchesTheReferenceOverRandomDocuments(t *testing.T) {
	random := rand.New(rand.NewPCG(464, 2))
	accepted, refused := 0, 0
	for range 20000 {
		var builder strings.Builder
		randomDocument(random, &builder, 0)
		data := []byte(builder.String())
		if random.IntN(2) == 0 {
			data = mutate(random, data)
		}
		for _, limits := range []Limits{DefaultLimits(), tightLimits(random, data)} {
			sameAsReference(t, string(data), data, limits)
			if _, failure := decode(data, limits, false); failure == nil {
				accepted++
			} else {
				refused++
			}
		}
	}
	if accepted < 5000 || refused < 5000 {
		t.Fatalf("%d accepted, %d refused: the corpus no longer reaches both", accepted, refused)
	}
}

// Nesting past what the standard library's validity scan accepts is read
// through the tokens, as before, and is refused by the configured limit at the
// same location.
func TestDecodeMatchesTheReferenceBeyondTheValidityScan(t *testing.T) {
	data := []byte(strings.Repeat(`{"a":[`, 6000) + "1" + strings.Repeat("]}", 6000))
	if json.Valid(data) {
		t.Fatal("the validity scan accepted the nesting this test needs it to refuse")
	}
	sameAsReference(t, "deep", data, Limits{MaxDepth: 20000, MaxNodes: 1 << 20, MaxStringBytes: 16})
	sameAsReference(t, "deep", data, DefaultLimits())
}

func FuzzDecodeMatchesReference(f *testing.F) {
	for _, seed := range []string{`{"a":[1,"b",{"c":null}]}`, `{"specVersion":"0.5.0","profile":"x"}`,
		`["😀","\ud800","~/"]`, `{"a":1,"a":2}`, `[1e400,-0,0.5e-3]`, `{"a" 1}`, `[1,]`, ` 7 `} {
		f.Add([]byte(seed), uint8(4), uint16(64), uint16(64))
	}
	f.Fuzz(func(t *testing.T, data []byte, depth uint8, nodes, stringBytes uint16) {
		sameAsReference(t, "fuzzed", data, DefaultLimits())
		sameAsReference(t, "fuzzed", data, Limits{MaxDepth: int(depth), MaxNodes: int(nodes), MaxStringBytes: int(stringBytes)})
	})
}

// Reading a valid document allocates for its values, not for each value's
// location or for an error per scalar: a deep array of small numbers, where
// a location built for each value costs hundreds of bytes per two-byte
// element, allocates about 50 times its size, most of it the array's growth.
// Before, the same document allocated about 375 times its size.
func TestDecodeBuildsNoLocationForAnAcceptedValue(t *testing.T) {
	const depth = 32
	data := []byte(strings.Repeat("[", depth) + strings.TrimSuffix(strings.Repeat("7,", 1<<16), ",") + strings.Repeat("]", depth))
	limits := DefaultLimits()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, failure := Decode(data, limits); failure != nil {
		t.Fatal(failure)
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("%d bytes allocated %d bytes, %.1f per input byte", len(data), allocated, float64(allocated)/float64(len(data)))
	if allocated > 64*uint64(len(data)) {
		t.Fatalf("decoding allocated %d bytes, more than 64 times its %d bytes", allocated, len(data))
	}
}
