package sdk_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
)

const modulePath = "github.com/Judgment-Pack/judgment-pack-runtime"

// moduleRoot is this module's root, one directory above the package under
// test, which is where go test runs it.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// parsePackage parses the non-test Go files of one directory of this module.
func parsePackage(t *testing.T, dir string) (*token.FileSet, []*ast.File) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fileset := token.NewFileSet()
	var files []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		t.Fatalf("no Go files in %s", dir)
	}
	return fileset, files
}

// ambientImports are the standard-library packages through which a program
// reaches files, the environment, processes or the network.
func ambientImport(path string) bool {
	switch path {
	case "net/netip", "net/url":
		return false // parsing only
	case "os", "syscall", "io/ioutil", "path/filepath", "plugin", "unsafe", "runtime/cgo", "net":
		return true
	}
	return strings.HasPrefix(path, "os/") || strings.HasPrefix(path, "net/") || strings.HasPrefix(path, "syscall/")
}

// The import closure of package sdk is read from source, without the go
// command: no package of this module in it imports a standard-library
// package that reaches files, the environment, processes or the network, and
// the third-party packages in it are exactly the schema validator's, whose
// schema loading the validation package holds offline.
func TestTheImportClosureReachesNoAmbientIO(t *testing.T) {
	root := moduleRoot(t)
	visited := map[string]bool{}
	third := map[string]bool{}
	queue := []string{modulePath + "/sdk"}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true
		_, files := parsePackage(t, filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(current, modulePath+"/"))))
		for _, file := range files {
			for _, spec := range file.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				switch {
				case strings.HasPrefix(path, modulePath+"/"):
					queue = append(queue, path)
				case strings.Contains(strings.Split(path, "/")[0], "."):
					third[path] = true
				case ambientImport(path):
					t.Errorf("%s imports %q", current, path)
				}
			}
		}
	}
	thirdParty := make([]string, 0, len(third))
	for path := range third {
		thirdParty = append(thirdParty, path)
	}
	slices.Sort(thirdParty)
	want := []string{"github.com/dlclark/regexp2", "github.com/santhosh-tekuri/jsonschema/v6", "github.com/santhosh-tekuri/jsonschema/v6/kind"}
	if !slices.Equal(thirdParty, want) {
		t.Errorf("third-party packages in the closure = %v, want %v", thirdParty, want)
	}
	if !visited[modulePath+"/internal/evaluation"] {
		t.Error("the closure walk did not reach the shared evaluator; the walk is broken")
	}
}

// No exported identifier of package sdk or of package sdk/view names a type
// from an internal package: an exported function's parameters and results, an
// exported type's definition, and an exported field of an exported struct all
// use only these packages' types and the standard library's.
func TestTheExportedAPINamesNoInternalType(t *testing.T) {
	_, files := parsePackage(t, filepath.Join(moduleRoot(t), "sdk"))
	_, viewFiles := parsePackage(t, filepath.Join(moduleRoot(t), "sdk", "view"))
	files = append(files, viewFiles...)
	checked := 0
	for _, file := range files {
		internal := map[string]bool{}
		for _, spec := range file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if !strings.Contains(path, "/internal/") {
				continue
			}
			name := path[strings.LastIndex(path, "/")+1:]
			if spec.Name != nil {
				name = spec.Name.Name
			}
			internal[name] = true
		}
		leaks := func(where string, node ast.Node) {
			checked++
			ast.Inspect(node, func(node ast.Node) bool {
				if selector, ok := node.(*ast.SelectorExpr); ok {
					if ident, ok := selector.X.(*ast.Ident); ok && internal[ident.Name] {
						t.Errorf("%s names %s.%s", where, ident.Name, selector.Sel.Name)
					}
				}
				return true
			})
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if !declaration.Name.IsExported() {
					continue
				}
				if declaration.Recv != nil {
					receiver := declaration.Recv.List[0].Type
					if star, ok := receiver.(*ast.StarExpr); ok {
						receiver = star.X
					}
					if ident, ok := receiver.(*ast.Ident); !ok || !ident.IsExported() {
						continue
					}
				}
				leaks("func "+declaration.Name.Name, declaration.Type)
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if !spec.Name.IsExported() {
							continue
						}
						structure, ok := spec.Type.(*ast.StructType)
						if !ok {
							leaks("type "+spec.Name.Name, spec.Type)
							continue
						}
						for _, field := range structure.Fields.List {
							exported := len(field.Names) == 0 // an embedded field is promoted
							for _, name := range field.Names {
								exported = exported || name.IsExported()
							}
							if exported {
								leaks("field of "+spec.Name.Name, field.Type)
							}
						}
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if name.IsExported() && spec.Type != nil {
								leaks(name.Name, spec.Type)
							}
						}
					}
				}
			}
		}
	}
	if checked < 10 {
		t.Fatalf("checked %d exported declarations; the walk is broken", checked)
	}
	// The exported constants take their values from the internal result
	// package and are untyped string constants, so no internal type escapes
	// through them either.
	var class any = sdk.ClassMalformedInput
	if _, ok := class.(string); !ok {
		t.Fatalf("ClassMalformedInput has type %T", class)
	}
}

// ambientChild is set in the environment of the child process the next test
// starts; nothing else in this package's tests reads it.
const ambientChild = "SDK_AMBIENT_IO_CHILD"

// TestMain runs the child's half of the next test when it is the child.
func TestMain(m *testing.M) {
	if os.Getenv(ambientChild) == "1" {
		os.Exit(ambientChildRun())
	}
	os.Exit(m.Run())
}

// Load, Evaluate and Describe run in a process whose environment holds
// nothing else and whose working directory is a temporary directory
// holding a project file that cannot be read as one, which JPACK_CONFIG also
// names. A surface that consulted either would refuse; this package consults
// neither, so the child evaluates. The child is this test binary itself.
func TestNoWorkingDirectoryOrEnvironmentIsConsulted(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	broken := filepath.Join(dir, "jpack.json")
	if err := os.WriteFile(broken, []byte("{ not a project"), 0o600); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^$")
	child.Dir = dir
	// GOCOVERDIR is for the test binary alone, which warns on stderr without it
	// when built with -cover; the runtime reads none of these.
	child.Env = []string{ambientChild + "=1", "JPACK_CONFIG=" + broken, "GOCOVERDIR=" + t.TempDir()}
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "evaluated" {
		t.Fatalf("child output %q", got)
	}
}

func ambientChildRun() int {
	pack, err := sdk.Load(context.Background(), []byte(thresholdPack), sdk.LoadOptions{})
	if err != nil {
		os.Stdout.WriteString("Load: " + err.Error() + "\n")
		return 1
	}
	evaluated, err := pack.Evaluate(context.Background(), []byte(`{"request":{"amount":"1000"}}`), []byte(`{}`))
	if err != nil {
		os.Stdout.WriteString("Evaluate: " + err.Error() + "\n")
		return 1
	}
	if string(evaluated.CanonicalDisposition()) != atOrAbove {
		os.Stdout.WriteString("disposition: " + string(evaluated.CanonicalDisposition()) + "\n")
		return 1
	}
	if _, err := sdk.Describe(); err != nil {
		os.Stdout.WriteString("Describe: " + err.Error() + "\n")
		return 1
	}
	os.Stdout.WriteString("evaluated\n")
	return 0
}
