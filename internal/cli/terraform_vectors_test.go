package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/terraformfile"
)

// The terraform-file reading's vectors, vendored and pinned in
// internal/terraformfile/testdata (its README and provenance.json). That
// package runs the read rows through the reader; this test runs the rows the
// reader does not reach, through jpack terraform facts: the batch rows, and the
// rows whose declaration is malformed, which the command's flags stand for.
var tfVectorsDir = filepath.Join("..", "terraformfile", "testdata", "1.0.0")

type tfVectorRow struct {
	ID          string          `json:"id"`
	Operation   string          `json:"operation"`
	Declaration string          `json:"declaration"`
	Input       string          `json:"input"`
	Generate    json.RawMessage `json:"generate"`
	Expected    string          `json:"expected"`
}

func tfVectorFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(tfVectorsDir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// tfVectorInput is a row's input file: its own under vectors/, or, for a row
// with a generate rule, a file of the template's bytes with its one replace
// text replaced by repeat written times times, quoted when the rule says so.
// A rule has those members and no other.
func tfVectorInput(t *testing.T, row tfVectorRow) (string, []byte) {
	t.Helper()
	if row.Generate == nil {
		return filepath.Join(tfVectorsDir, filepath.FromSlash(row.Input)), tfVectorFile(t, row.Input)
	}
	var rule struct {
		Template string `json:"template"`
		Replace  string `json:"replace"`
		With     struct {
			Repeat string `json:"repeat"`
			Times  int    `json:"times"`
			Quoted bool   `json:"quoted"`
		} `json:"with"`
	}
	decoder := json.NewDecoder(bytes.NewReader(row.Generate))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rule); err != nil {
		t.Fatalf("%s: the generate rule: %v", row.ID, err)
	}
	template := tfVectorFile(t, "vectors/"+rule.Template)
	if row.Input != "" || bytes.Count(template, []byte(rule.Replace)) != 1 || rule.With.Repeat == "" || rule.With.Times < 1 {
		t.Fatalf("%s: the generate rule does not name one replacement of a committed template", row.ID)
	}
	with := strings.Repeat(rule.With.Repeat, rule.With.Times)
	if rule.With.Quoted {
		with = `"` + with + `"`
	}
	data := bytes.Replace(template, []byte(rule.Replace), []byte(with), 1)
	return tfFile(t, "generated.json", data), data
}

// tfVectorFlags writes a declaration as the command line that stands for it:
// --plan or --state for document, with the input's path; one --type per
// type; --mode, --address and --actions as written; and --max and --max-bytes
// with the number's literal as written, so 1.0 and 1e3 stay what they are. A
// declaration a command line cannot write returns the reason instead: one that
// is not an object, a member §2 does not have, a document other than plan and
// state, or a member of another JSON type, since every flag value is text.
func tfVectorFlags(t *testing.T, raw []byte, input string) ([]string, string) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("the declaration is not JSON: %v", err)
	}
	declaration, ok := decoded.(map[string]any)
	if !ok {
		return nil, "the declaration is not an object, and the flags are one source's members"
	}
	args := []string{"terraform", "facts"}
	for _, name := range sortedNames(declaration) {
		if name != "document" && name != "select" && name != "maxBytes" {
			return nil, fmt.Sprintf("%s is not a member of §2, and no flag names it (the file is the value of --plan or --state)", name)
		}
	}
	if document, present := declaration["document"]; present {
		switch document {
		case terraformfile.DocumentPlan:
			args = append(args, "--plan", input)
		case terraformfile.DocumentState:
			args = append(args, "--state", input)
		default:
			if _, isString := document.(string); isString {
				return nil, "--plan and --state are the only documents a command line names"
			}
			return nil, "document is not a string, and --plan and --state take none"
		}
	}
	if selection, present := declaration["select"]; present {
		members, ok := selection.(map[string]any)
		if !ok {
			return nil, "select is not an object, and its flags are its members"
		}
		for _, name := range sortedNames(members) {
			value := members[name]
			switch name {
			case "types":
				types, ok := tfStrings(value)
				if !ok {
					return nil, "types is not an array of strings, which --type writes"
				}
				if len(types) == 0 {
					args = append(args, "--type=")
				}
				for _, typ := range types {
					args = append(args, "--type="+typ)
				}
			case "actions":
				actions, ok := tfStrings(value)
				if !ok {
					return nil, "actions is not an array of strings, which --actions writes"
				}
				args = append(args, "--actions="+strings.Join(actions, ","))
			case "mode", "address":
				text, ok := value.(string)
				if !ok {
					return nil, name + " is not a string, which --" + name + " writes"
				}
				args = append(args, "--"+name+"="+text)
			case "max":
				number, ok := value.(json.Number)
				if !ok {
					return nil, "max is not a number, and --max takes its literal"
				}
				args = append(args, "--max="+number.String())
			default:
				return nil, fmt.Sprintf("select.%s is not a member of §5, and no flag names it", name)
			}
		}
	}
	if maxBytes, present := declaration["maxBytes"]; present {
		number, ok := maxBytes.(json.Number)
		if !ok {
			return nil, "maxBytes is not a number, and --max-bytes takes its literal"
		}
		args = append(args, "--max-bytes="+number.String())
	}
	return args, ""
}

func sortedNames(object map[string]any) []string {
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// tfStrings is an array of strings with no comma in any, which a flag can
// write; ok is false for anything else.
func tfStrings(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := []string{}
	for _, item := range items {
		text, ok := item.(string)
		if !ok || strings.Contains(text, ",") {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

// Every batch row through jpack terraform facts, with the flags its
// declaration stands for: the exact bytes of its lines, or its refusal, with
// no line. Every row whose declaration is malformed through the same flags:
// an unusable command line (exit 3) that never reads the file, which is not
// JSON, so a command that read it would refuse it TF-JSON instead. A malformed
// declaration a command line cannot write is not run, for the reason logged.
func TestTerraformFactsSpecVectors(t *testing.T) {
	var m struct {
		Companion string        `json:"companion"`
		Reading   string        `json:"reading"`
		Vectors   []tfVectorRow `json:"vectors"`
	}
	if err := json.Unmarshal(tfVectorFile(t, "manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	if m.Companion != "terraform-file" || m.Reading != "1.0.0" {
		t.Fatalf("the manifest is for %s %s", m.Companion, m.Reading)
	}
	// The malformed declarations' input, read under a well-formed one, is
	// refused: so their exit 3 is the command line's, not the file's.
	notJSON := filepath.Join(tfVectorsDir, "vectors", "declarations", "not-json.txt")
	code, out, stderr := runTest(t, []string{"terraform", "facts", "--plan", notJSON, "--type", "google_sql_user"}, "")
	tfRefused(t, code, out, stderr, tfVectorFile(t, "vectors/declarations/not-json.txt"), terraformfile.CodeJSON)
	batch, malformed := 0, 0
	var notRun []string
	for _, row := range m.Vectors {
		expectedFile := tfVectorFile(t, row.Expected)
		var expected struct {
			Declaration string `json:"declaration"`
			Refusal     *struct {
				Code   string      `json:"code"`
				SHA256 string      `json:"sha256"`
				Bytes  json.Number `json:"bytes"`
			} `json:"refusal"`
		}
		if !strings.HasSuffix(row.Expected, ".jsonl") {
			decoder := json.NewDecoder(bytes.NewReader(expectedFile))
			decoder.UseNumber()
			if err := decoder.Decode(&expected); err != nil {
				t.Fatalf("%s: %v", row.ID, err)
			}
		}
		switch {
		case row.Operation == "batch":
		case expected.Declaration == "malformed":
		default:
			continue
		}
		path, data := tfVectorInput(t, row)
		args, skip := tfVectorFlags(t, tfVectorFile(t, row.Declaration), path)
		if skip != "" {
			if row.Operation == "batch" {
				t.Fatalf("%s: a batch row's declaration has no command line: %s", row.ID, skip)
			}
			notRun = append(notRun, row.ID+": "+skip)
			continue
		}
		t.Run(row.ID, func(t *testing.T) {
			code, out, stderr := runTest(t, args, "")
			switch {
			case expected.Declaration == "malformed":
				if code != 3 || out != "" || !strings.HasPrefix(stderr, "error: ") || strings.Contains(stderr, "TF-") || strings.Contains(stderr, "FACTS-") {
					t.Fatalf("%v: exit %d, stdout %q, stderr %q; a malformed declaration is an unusable command line", args, code, out, stderr)
				}
				malformed++
			case strings.HasSuffix(row.Expected, ".jsonl"):
				if code != 0 || stderr != "" || out != string(expectedFile) {
					t.Fatalf("%v: exit %d, stderr %q\n got %q\nwant %q", args, code, stderr, first(out, 2000), first(string(expectedFile), 2000))
				}
				batch++
			case expected.Refusal != nil:
				want := expected.Refusal
				sum := sha256.Sum256(data)
				if want.SHA256 != hex.EncodeToString(sum[:]) || want.Bytes.String() != fmt.Sprint(len(data)) {
					t.Fatalf("the row's refusal names %s and %s bytes, not its input", want.SHA256, want.Bytes)
				}
				message := terraformfile.Message(want.Code)
				if message == "" {
					message = terraformFactsMessage(want.Code)
				}
				refusal := `{"bytes":` + want.Bytes.String() + `,"code":` + string(mustJCS(t, want.Code)) + `,"message":` + string(mustJCS(t, message)) + `,"sha256":"` + want.SHA256 + `"}` + "\n"
				if code != 2 || out != "" || stderr != refusal {
					t.Fatalf("%v: exit %d, stdout %q\n got %q\nwant %q", args, code, first(out, 400), stderr, refusal)
				}
				batch++
			default:
				t.Fatalf("expected result %s is neither a batch nor a refusal", row.Expected)
			}
		})
	}
	for _, reason := range notRun {
		t.Logf("not run: %s", reason)
	}
	t.Logf("%d batch rows and %d malformed declarations run; %d malformed declarations not run", batch, malformed, len(notRun))
	if batch != 10 || malformed != 21 || len(notRun) != 16 {
		t.Fatalf("%d batch rows, %d malformed declarations run and %d not run; the manifest at the pinned revision has 10 batch rows, and 37 malformed declarations of which 21 have a command line",
			batch, malformed, len(notRun))
	}
}
