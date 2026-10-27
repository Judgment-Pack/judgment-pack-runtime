// Command sync-retained-column imports reviewed bytes from a verified RC2
// archive and its clean immutable source checkout. It performs no network fetch,
// publication verification, registry update or capability selection.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing/fstest"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/retainedcolumn"
)

const archivePrefix = "judgment-pack-spec-0.3.0-rc.2/"
const maxArchiveBytes = 256 << 20 // Maintainer import budget, not a Runtime admission limit.

type options struct{ source, destination, archive, archivePin, revision, inventoryPin, sourceRef string }

func main() {
	var o options
	flag.StringVar(&o.source, "source", "", "clean official Spec checkout at the verified release tag")
	flag.StringVar(&o.destination, "destination", "", "new leaf artifact directory (must not exist)")
	flag.StringVar(&o.archive, "archive", "", "independently verified official RC2 tar.gz release asset")
	flag.StringVar(&o.archivePin, "archive-sha256", "", "independently verified sha256: archive digest")
	flag.StringVar(&o.inventoryPin, "inventory-sha256", "", "reviewed sha256: release inventory digest")
	flag.StringVar(&o.revision, "source-commit", "", "verified immutable 40-hex source commit")
	flag.StringVar(&o.sourceRef, "source-ref", "", "exact release tag v0.3.0-rc.2")
	flag.Parse()
	pin, err := run(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Imported retained-column artifacts; independent provenance pin:", pin)
	fmt.Println("No selector, supported identity or conformance claim was changed.")
}
func git(source string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", source}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("verify immutable Spec source (%s): %w", args[0], err)
	}
	return out, nil
}
func verifySource(o options) error { return verifySourceWith(o, git) }
func verifySourceWith(o options, read func(string, ...string) ([]byte, error)) error {
	if o.source == "" || o.sourceRef != retainedcolumn.Release {
		return errors.New("clean official source checkout and exact RC2 release tag are required")
	}
	origin, err := read(o.source, "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	switch strings.TrimSpace(string(origin)) {
	case retainedcolumn.Repository, retainedcolumn.Repository + ".git", "git@github.com:Judgment-Pack/judgment-pack-spec.git":
	default:
		return errors.New("source origin is not the official Spec repository")
	}
	status, err := read(o.source, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return err
	}
	if len(status) != 0 {
		return errors.New("source worktree must be clean")
	}
	for _, ref := range []string{"HEAD", "refs/tags/" + o.sourceRef} {
		commit, err := read(o.source, "rev-parse", "--verify", ref+"^{commit}")
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(commit)) != o.revision {
			return errors.New("source checkout and immutable release tag must equal the verified source commit")
		}
	}
	return nil
}
func readArchive(raw []byte, pin, revision string) (fstest.MapFS, error) {
	if retainedcolumn.Digest(raw) != pin {
		return nil, errors.New("archive differs from independently verified digest")
	}
	compressed, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer compressed.Close()
	expanded := &io.LimitedReader{R: compressed, N: maxArchiveBytes + 1}
	reader := tar.NewReader(expanded)
	files := fstest.MapFS{}
	seen := map[string]bool{}
	var total int64
	var entries int
	for {
		before := expanded.N
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		// git archive emits one leading PAX global comment naming the source
		// commit. It is metadata, never a file or a path override. Go returns
		// this header after parsing its records, unlike per-file PAX headers.
		// Admit only that exact bounded form; retain all member safety checks.
		if header.Typeflag == tar.TypeXGlobalHeader {
			if entries != 0 || header.Name != "pax_global_header" || header.Format != tar.FormatPAX ||
				expanded.N < before-1024 || len(header.PAXRecords) != 1 ||
				len(revision) != 40 || header.PAXRecords["comment"] != revision {
				return nil, errors.New("archive has unsupported global metadata")
			}
			entries++
			continue
		}
		entries++
		name := strings.TrimPrefix(header.Name, archivePrefix)
		directory := header.Typeflag == tar.TypeDir
		if !strings.HasPrefix(header.Name, archivePrefix) || strings.Contains(name, "\\") {
			return nil, errors.New("archive has an invalid release prefix or path")
		}
		if directory {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "" && directory {
			continue
		}
		if !fs.ValidPath(name) || seen[name] {
			return nil, errors.New("archive has an invalid or duplicate member")
		}
		seen[name] = true
		if directory {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > maxArchiveBytes-total {
			return nil, errors.New("archive contains a nonregular or oversized member")
		}
		total += header.Size
		data, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != header.Size {
			return nil, errors.New("archive member size differs")
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	// Consume through the gzip trailer so corrupt/truncated gzip streams fail,
	// even when the tar end marker was already reached. Bound tar padding too.
	if _, err := io.Copy(io.Discard, expanded); err != nil {
		return nil, err
	}
	if expanded.N <= 0 {
		return nil, errors.New("expanded archive exceeds import budget")
	}
	return files, nil
}

// prepare is also exercised with explicitly synthetic, handwritten archives;
// it accepts no capability report as an installation trust anchor.
func prepare(raw []byte, o options, sourceFile func(string) ([]byte, error)) (fstest.MapFS, string, error) {
	files, err := readArchive(raw, o.archivePin, o.revision)
	if err != nil {
		return nil, "", err
	}
	identity, inventory, err := retainedcolumn.Inspect(files, o.revision, o.inventoryPin)
	if err != nil {
		return nil, "", err
	}
	names := []string{retainedcolumn.InventoryPath, "release-identity.json"}
	for name := range inventory.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	selected := fstest.MapFS{}
	provenance := retainedcolumn.Provenance{SourceRepository: retainedcolumn.Repository, SourceRelease: retainedcolumn.Release, SourceRevision: o.revision, SourceKind: "immutable-git-ref", SourceArchiveSHA256: o.archivePin, InventorySHA256: o.inventoryPin, Identity: identity}
	for _, name := range names {
		data := files[name].Data
		if name != "release-identity.json" {
			original, err := sourceFile(name)
			if err != nil {
				return nil, "", err
			}
			if !bytes.Equal(data, original) {
				return nil, "", fmt.Errorf("archive file differs from immutable tagged source: %s", name)
			}
		}
		selected[name] = &fstest.MapFile{Data: data}
		provenance.Files = append(provenance.Files, retainedcolumn.File{Path: name, ByteLength: len(data), SHA256: retainedcolumn.Digest(data)})
	}
	manifest, err := json.MarshalIndent(provenance, "", "  ")
	if err != nil {
		return nil, "", err
	}
	manifest = append(manifest, '\n')
	selected["provenance.json"] = &fstest.MapFile{Data: manifest}
	pin := retainedcolumn.Digest(manifest)
	if err := retainedcolumn.Verify(selected, pin, identity); err != nil {
		return nil, "", err
	}
	return selected, pin, nil
}
func run(o options) (string, error) {
	if o.destination == "" || o.archive == "" {
		return "", errors.New("archive and new destination are required")
	}
	destination, err := filepath.Abs(o.destination)
	if err != nil {
		return "", err
	}
	source, err := filepath.Abs(o.source)
	if err != nil {
		return "", err
	}
	if destination == source || strings.HasPrefix(destination, source+string(os.PathSeparator)) {
		return "", errors.New("destination must be outside the Spec checkout")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("destination must not exist")
	}
	if err := verifySource(o); err != nil {
		return "", err
	}
	file, err := os.Open(o.archive)
	if err != nil {
		return "", err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > maxArchiveBytes {
		return "", errors.New("archive must be a bounded regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxArchiveBytes+1))
	if err != nil {
		return "", err
	}
	selected, pin, err := prepare(raw, o, func(name string) ([]byte, error) { return git(o.source, "show", o.revision+":"+name) })
	if err != nil {
		return "", err
	}
	// Stage only after all source/byte checks, then publish one complete new directory.
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".retained-column-import-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	for name, file := range selected {
		path := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, file.Data, 0644); err != nil {
			return "", err
		}
	}
	// Recheck immediately before publishing. As with sync-spec-artifacts, portable
	// Go rename leaves a window for a concurrently created empty directory; a
	// nonempty destination is refused by rename. Run with exclusive destination ownership.
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("destination appeared while preparing import")
	}
	if err := retainedcolumn.Verify(os.DirFS(stage), pin, importIdentity(selected)); err != nil {
		return "", err
	}
	if err := os.Rename(stage, destination); err != nil {
		return "", err
	}
	return pin, nil
}

func importIdentity(files fstest.MapFS) retainedcolumn.Identity {
	var p retainedcolumn.Provenance
	_ = json.Unmarshal(files["provenance.json"].Data, &p) // generated and verified before staging
	return p.Identity
}
