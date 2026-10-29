// Command audit-formats regenerates the audit trail's line-format catalog and
// checks it, the committed change records, the format status and the writers'
// exemplar lines (#327). Run it
// from the repository root. -write rewrites the generated catalog only; change
// records and the status are written by hand.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/auditformat"
)

func main() {
	dir := flag.String("dir", filepath.Join("internal", "auditformat"), "directory holding the catalog, change records and status")
	write := flag.Bool("write", false, "rewrite the generated catalog before checking")
	flag.Parse()
	os.Exit(run(*dir, *write))
}

func run(dir string, write bool) int {
	generated, _ := auditformat.Generate()
	catalogPath := filepath.Join(dir, auditformat.CatalogFile)
	if write {
		current, _ := os.ReadFile(catalogPath)
		if encoded := auditformat.Encode(generated); !bytes.Equal(current, encoded) {
			if err := os.WriteFile(catalogPath, encoded, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "audit-formats:", err)
				return 1
			}
			fmt.Println("audit-formats: rewrote", catalogPath)
		}
	}
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			fmt.Fprintln(os.Stderr, "audit-formats:", err)
		}
		return raw
	}
	problems := auditformat.CheckFiles(read(auditformat.CatalogFile), read(auditformat.ChangesFile), read(auditformat.StatusFile))
	for _, problem := range problems {
		fmt.Fprintln(os.Stderr, "audit-formats:", problem)
	}
	if len(problems) != 0 {
		return 1
	}
	fmt.Printf("audit-formats: %d persisted line types match their committed format decisions\n", len(generated.Lines))
	return 0
}
