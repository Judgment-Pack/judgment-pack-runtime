package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/retainedcolumn"
)

// Handwritten synthetic archive inputs; no published/adopted release is used.
func syntheticArchive(t *testing.T) ([]byte, options, fstest.MapFS) {
	t.Helper()
	files := fstest.MapFS{}
	inv := retainedcolumn.Inventory{Contract: retainedcolumn.Contract, Distribution: retainedcolumn.Release, Maturity: "draft", Schema: retainedcolumn.SchemaPath, Corpus: retainedcolumn.CorpusPath, Claim: "selected-leaf-support", Files: map[string]string{}}
	for _, name := range []string{retainedcolumn.SchemaPath, retainedcolumn.CorpusPath, "spec/retained-column.md", "conformance/retained-column/1/manifest.json", "conformance/retained-column/1/capabilities.json", "conformance/retained-column/1/examples.json", "conformance/retained-column/1/preparations.json"} {
		raw := []byte("synthetic: " + name + "\n")
		files[name] = &fstest.MapFile{Data: raw}
		inv.Files[name] = retainedcolumn.Digest(raw)
	}
	inv.SchemaSHA256 = inv.Files[inv.Schema]
	inv.CorpusSHA256 = inv.Files[inv.Corpus]
	encode := func(name string, v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	encode(retainedcolumn.InventoryPath, inv)
	revision := strings.Repeat("1", 40)
	encode("release-identity.json", retainedcolumn.ReleaseIdentity{ReleaseVersion: retainedcolumn.Version, Tag: retainedcolumn.Release, SpecVersion: "0.3.0", Maturity: "draft", Distribution: "release-candidate", SourceCommit: revision})
	raw := makeArchive(t, files, nil)
	return raw, options{archivePin: retainedcolumn.Digest(raw), revision: revision, inventoryPin: retainedcolumn.Digest(files[retainedcolumn.InventoryPath].Data)}, files
}
func makeArchive(t *testing.T, files fstest.MapFS, extra *tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	zip := gzip.NewWriter(&out)
	writer := tar.NewWriter(zip)
	for name, file := range files {
		if err := writer.WriteHeader(&tar.Header{Name: archivePrefix + name, Mode: 0644, Size: int64(len(file.Data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(file.Data); err != nil {
			t.Fatal(err)
		}
	}
	if extra != nil {
		if err := writer.WriteHeader(extra); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func TestPrepareSyntheticArchiveAndRefusals(t *testing.T) {
	for _, name := range []string{"valid", "archive pin", "inventory pin", "source identity", "source bytes", "missing source", "missing schema", "wrong schema", "wrong inventory identity", "missing release identity", "placeholder source", "bad gzip checksum", "traversal", "symlink", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			raw, o, files := syntheticArchive(t)
			source := func(name string) ([]byte, error) {
				file, ok := files[name]
				if !ok {
					return nil, fmt.Errorf("missing source")
				}
				return file.Data, nil
			}
			rearchive := func() { raw = makeArchive(t, files, nil); o.archivePin = retainedcolumn.Digest(raw) }
			switch name {
			case "archive pin":
				o.archivePin = "sha256:" + strings.Repeat("0", 64)
			case "inventory pin":
				o.inventoryPin = "sha256:" + strings.Repeat("0", 64)
			case "source identity":
				o.revision = strings.Repeat("2", 40)
			case "source bytes":
				source = func(string) ([]byte, error) { return []byte("mismatch"), nil }
			case "missing source":
				source = func(string) ([]byte, error) { return nil, fmt.Errorf("source missing") }
			case "missing schema":
				delete(files, retainedcolumn.SchemaPath)
				rearchive()
			case "wrong schema":
				files[retainedcolumn.SchemaPath].Data = []byte("{}")
				rearchive()
			case "wrong inventory identity":
				var inv retainedcolumn.Inventory
				_ = json.Unmarshal(files[retainedcolumn.InventoryPath].Data, &inv)
				inv.Contract = "wrong/1"
				files[retainedcolumn.InventoryPath].Data, _ = json.Marshal(inv)
				o.inventoryPin = retainedcolumn.Digest(files[retainedcolumn.InventoryPath].Data)
				rearchive()
			case "missing release identity":
				delete(files, "release-identity.json")
				rearchive()
			case "placeholder source":
				o.revision = "main"
			case "bad gzip checksum":
				raw[len(raw)-8] ^= 128
				o.archivePin = retainedcolumn.Digest(raw)
			case "traversal", "symlink", "duplicate":
				extra := &tar.Header{Name: archivePrefix + "../outside", Mode: 0644}
				if name == "symlink" {
					extra.Name = archivePrefix + "link"
					extra.Typeflag = tar.TypeSymlink
					extra.Linkname = "outside"
				}
				if name == "duplicate" {
					extra.Name = archivePrefix + retainedcolumn.SchemaPath
				}
				raw = makeArchive(t, files, extra)
				o.archivePin = retainedcolumn.Digest(raw)
			}
			selected, pin, err := prepare(raw, o, source)
			if name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				if len(selected) != len(files)+1 || pin != retainedcolumn.Digest(selected["provenance.json"].Data) {
					t.Fatal("incomplete synthetic import")
				}
				if err := retainedcolumn.Verify(selected, pin, importIdentity(selected)); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe synthetic import accepted")
			}
		})
	}
}
func TestVerifySourceRefusals(t *testing.T) {
	for _, name := range []string{"valid", "moving ref", "wrong origin", "dirty", "untracked", "wrong HEAD", "wrong tag"} {
		t.Run(name, func(t *testing.T) {
			o := options{source: "synthetic-source", sourceRef: retainedcolumn.Release, revision: strings.Repeat("1", 40)}
			if name == "moving ref" {
				o.sourceRef = "main"
			}
			read := func(_ string, args ...string) ([]byte, error) {
				switch args[0] {
				case "remote":
					if name == "wrong origin" {
						return []byte("https://example.invalid/spec"), nil
					}
					return []byte(retainedcolumn.Repository), nil
				case "status":
					if name == "dirty" {
						return []byte(" M schema.json\n"), nil
					}
					if name == "untracked" {
						return []byte("?? new.json\n"), nil
					}
					return nil, nil
				case "rev-parse":
					if name == "wrong HEAD" && args[2] == "HEAD^{commit}" || name == "wrong tag" && strings.HasPrefix(args[2], "refs/tags/") {
						return []byte(strings.Repeat("2", 40)), nil
					}
					return []byte(o.revision), nil
				}
				return nil, fmt.Errorf("unexpected source command")
			}
			err := verifySourceWith(o, read)
			if name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe source accepted")
			}
		})
	}
}

func TestRunRefusesExistingDestination(t *testing.T) {
	destination := t.TempDir()
	sentinel := filepath.Join(destination, "existing.txt")
	if err := os.WriteFile(sentinel, []byte("preserve existing artifact bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(options{destination: destination, archive: "unused-synthetic-input"}); err == nil || !strings.Contains(err.Error(), "destination must not exist") {
		t.Fatal("existing destination was not refused", err)
	}
	raw, err := os.ReadFile(sentinel)
	if err != nil || string(raw) != "preserve existing artifact bytes" {
		t.Fatal("existing destination changed", err)
	}
}

// These headers model git archive's PAX comment by hand. Metadata cannot
// introduce archive members, override paths, or replace the source identity.
func TestGitArchiveGlobalMetadata(t *testing.T) {
	revision := strings.Repeat("1", 40)
	for _, name := range []string{"valid", "wrong comment", "path override", "unknown record", "wrong name", "duplicate", "late", "member traversal", "member symlink", "member duplicate", "member special"} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			zip := gzip.NewWriter(&out)
			writer := tar.NewWriter(zip)
			metadata := &tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": revision}, Format: tar.FormatPAX}
			switch name {
			case "wrong comment":
				metadata.PAXRecords["comment"] = strings.Repeat("2", 40)
			case "path override":
				metadata.PAXRecords["path"] = "../outside"
			case "unknown record":
				metadata.PAXRecords["unknown"] = "value"
			case "wrong name":
				metadata.Name = archivePrefix + "metadata"
			}
			write := func(h *tar.Header) {
				t.Helper()
				if err := writer.WriteHeader(h); err != nil {
					t.Fatal(err)
				}
			}
			member := &tar.Header{Name: archivePrefix + "safe", Typeflag: tar.TypeReg, Mode: 0644}
			if name == "late" {
				write(member)
			}
			write(metadata)
			if name == "duplicate" {
				write(metadata)
			}
			write(member)
			switch name {
			case "member traversal":
				write(&tar.Header{Name: archivePrefix + "../outside", Typeflag: tar.TypeReg})
			case "member symlink":
				write(&tar.Header{Name: archivePrefix + "link", Typeflag: tar.TypeSymlink, Linkname: "outside"})
			case "member duplicate":
				write(member)
			case "member special":
				write(&tar.Header{Name: archivePrefix + "fifo", Typeflag: tar.TypeFifo})
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := zip.Close(); err != nil {
				t.Fatal(err)
			}
			raw := out.Bytes()
			files, err := readArchive(raw, retainedcolumn.Digest(raw), revision)
			if name == "valid" {
				if err != nil || len(files) != 1 || files["safe"] == nil {
					t.Fatal("Git metadata fixture refused", err)
				}
			} else if err == nil {
				t.Fatal("unsafe metadata/member accepted")
			}
		})
	}
}

func TestGitArchiveGlobalMetadataByteBudget(t *testing.T) {
	revision := strings.Repeat("1", 40)
	// Repeated identical PAX keys collapse in Go's parsed map. A handwritten
	// payload ensures the metadata budget still counts all serialized records.
	var plain bytes.Buffer
	writer := tar.NewWriter(&plain)
	if err := writer.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, Format: tar.FormatPAX, PAXRecords: map[string]string{"comment": revision}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	header := append([]byte(nil), plain.Bytes()[:512]...)
	payload := []byte(strings.Repeat("52 comment="+revision+"\n", 30))
	copy(header[124:136], []byte(fmt.Sprintf("%011o\x00", len(payload))))
	copy(header[148:156], []byte("        "))
	sum := 0
	for _, b := range header {
		sum += int(b)
	}
	copy(header[148:156], []byte(fmt.Sprintf("%06o\x00 ", sum)))
	var compressed bytes.Buffer
	zip := gzip.NewWriter(&compressed)
	for _, part := range [][]byte{header, payload, make([]byte, (512-len(payload)%512)%512), make([]byte, 1024)} {
		if _, err := zip.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	// Validate this hand-built fixture reaches the parsed one-comment shape.
	unzip, err := gzip.NewReader(bytes.NewReader(compressed.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := tar.NewReader(unzip).Next()
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "pax_global_header" || len(parsed.PAXRecords) != 1 || parsed.PAXRecords["comment"] != revision {
		t.Fatal("invalid handcrafted metadata fixture")
	}
	_, _ = io.Copy(io.Discard, unzip)
	_ = unzip.Close()
	if _, err := readArchive(compressed.Bytes(), retainedcolumn.Digest(compressed.Bytes()), revision); err == nil {
		t.Fatal("oversized metadata accepted")
	}
}

// Construct the per-file extension bytes by hand: tar.Writer derives known PAX
// fields from Header, so a caller's conflicting path record is not a safe-target
// override fixture. The extension below precedes an ordinary in-prefix member.
func perFilePAXArchive(t *testing.T, key, value string, directory bool) []byte {
	t.Helper()
	var regular bytes.Buffer
	writer := tar.NewWriter(&regular)
	typ := byte(tar.TypeReg)
	if directory {
		typ = tar.TypeDir
	}
	if err := writer.WriteHeader(&tar.Header{Name: archivePrefix + "original", Typeflag: typ, Mode: 0644}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	record := ""
	if key != "" {
		body := " " + key + "=" + value + "\n"
		length := len(body) + 1
		for {
			next := len(fmt.Sprint(length)) + len(body)
			if next == length {
				break
			}
			length = next
		}
		record = fmt.Sprint(length) + body
	}
	header := append([]byte(nil), regular.Bytes()[:512]...)
	clear(header[:100])
	copy(header[:100], "pax_file_header")
	header[156] = tar.TypeXHeader
	copy(header[124:136], []byte(fmt.Sprintf("%011o\x00", len(record))))
	copy(header[148:156], []byte("        "))
	sum := 0
	for _, b := range header {
		sum += int(b)
	}
	copy(header[148:156], []byte(fmt.Sprintf("%06o\x00 ", sum)))
	var out bytes.Buffer
	zip := gzip.NewWriter(&out)
	for _, part := range [][]byte{header, []byte(record), make([]byte, (512-len(record)%512)%512), regular.Bytes()} {
		if _, err := zip.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestPerFilePAXMetadataRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
		directory        bool
	}{
		{"unknown", "unknown", "value", false},
		{"safe path override", "path", archivePrefix + "safe", false},
		{"per-file source comment", "comment", strings.Repeat("1", 40), false},
		{"empty header", "", "", false},
		{"directory safe path override", "path", archivePrefix + "safe", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := perFilePAXArchive(t, tc.key, tc.value, tc.directory)
			zip, err := gzip.NewReader(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			header, err := tar.NewReader(zip).Next()
			if err != nil {
				t.Fatal(err)
			}
			if header.Format != tar.FormatPAX || header.Typeflag == tar.TypeXHeader {
				t.Fatal("fixture did not reach effective PAX member")
			}
			if tc.key != "" && header.PAXRecords[tc.key] != tc.value {
				t.Fatal("fixture lost per-file record")
			}
			if tc.key == "path" && header.Name != archivePrefix+"safe" {
				t.Fatal("fixture did not apply safe path override")
			}
			_ = zip.Close()
			if _, err := readArchive(raw, retainedcolumn.Digest(raw), strings.Repeat("1", 40)); err == nil || !strings.Contains(err.Error(), "unsupported per-file metadata") {
				t.Fatal("unsupported per-file metadata admitted", err)
			}
		})
	}
}

// GNU extensions are handwritten so even empty and short overrides are present.
func gnuMetadataArchive(t *testing.T, flag byte, format tar.Format, value string, directory, dangling bool) []byte {
	t.Helper()
	var member bytes.Buffer
	writer := tar.NewWriter(&member)
	typ := byte(tar.TypeReg)
	if directory {
		typ = tar.TypeDir
	}
	if err := writer.WriteHeader(&tar.Header{Name: archivePrefix + "original", Typeflag: typ, Mode: 0644, Format: format}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	payload := []byte(value)
	if value != "" {
		payload = append(payload, 0)
	}
	header := append([]byte(nil), member.Bytes()[:512]...)
	clear(header[:100])
	copy(header[:100], "././@LongLink")
	header[156] = flag
	copy(header[124:136], []byte(fmt.Sprintf("%011o\x00", len(payload))))
	copy(header[148:156], []byte("        "))
	sum := 0
	for _, b := range header {
		sum += int(b)
	}
	copy(header[148:156], []byte(fmt.Sprintf("%06o\x00 ", sum)))
	tail := member.Bytes()
	if dangling {
		tail = tail[512:]
	}
	var out bytes.Buffer
	zip := gzip.NewWriter(&out)
	for _, part := range [][]byte{header, payload, make([]byte, (512-len(payload)%512)%512), tail} {
		if _, err := zip.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestGNUArchiveMetadataRefusals(t *testing.T) {
	for _, tc := range []struct {
		name      string
		flag      byte
		format    tar.Format
		value     string
		directory bool
		dangling  bool
	}{
		{"safe long name", tar.TypeGNULongName, tar.FormatGNU, archivePrefix + "safe", false, false},
		{"safe long link", tar.TypeGNULongLink, tar.FormatGNU, "safe", false, false},
		{"empty long name", tar.TypeGNULongName, tar.FormatGNU, "", false, false},
		{"empty long link", tar.TypeGNULongLink, tar.FormatGNU, "", false, false},
		{"mixed format long name", tar.TypeGNULongName, tar.FormatUSTAR, archivePrefix + "safe", false, false},
		{"mixed format long link", tar.TypeGNULongLink, tar.FormatUSTAR, "safe", false, false},
		{"dangling long name", tar.TypeGNULongName, tar.FormatGNU, archivePrefix + "safe", false, true},
		{"dangling empty long link", tar.TypeGNULongLink, tar.FormatGNU, "", false, true},
		{"directory long name", tar.TypeGNULongName, tar.FormatGNU, archivePrefix + "safe", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := gnuMetadataArchive(t, tc.flag, tc.format, tc.value, tc.directory, tc.dangling)
			zip, err := gzip.NewReader(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			header, err := tar.NewReader(zip).Next()
			if tc.dangling {
				if err != io.EOF {
					t.Fatal("fixture did not reach transparent metadata EOF", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if header.Typeflag == tar.TypeGNULongName || header.Typeflag == tar.TypeGNULongLink || header.Format == tar.FormatUSTAR {
					t.Fatal("fixture did not reach effective GNU or mixed-format member")
				}
				if tc.value != "" && tc.flag == tar.TypeGNULongName && header.Name != tc.value {
					t.Fatal("fixture lost safe name override")
				}
				if tc.value != "" && tc.flag == tar.TypeGNULongLink && header.Linkname != tc.value {
					t.Fatal("fixture lost link metadata")
				}
			}
			_ = zip.Close()
			if _, err := readArchive(raw, retainedcolumn.Digest(raw), strings.Repeat("1", 40)); err == nil || !strings.Contains(err.Error(), "unsupported per-file metadata") {
				t.Fatal("unsupported GNU metadata admitted", err)
			}
		})
	}
}

func TestDanglingPAXMetadataRefusals(t *testing.T) {
	for _, key := range []string{"", "unknown", "path"} {
		t.Run("record="+key, func(t *testing.T) {
			raw := perFilePAXArchive(t, key, archivePrefix+"safe", false)
			zip, err := gzip.NewReader(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			plain, err := io.ReadAll(zip)
			if err != nil {
				t.Fatal(err)
			}
			if err := zip.Close(); err != nil {
				t.Fatal(err)
			}
			// Remove the zero-length member header, preserving the manually
			// written extension and the two zero end-marker blocks.
			plain = append(plain[:len(plain)-1536], plain[len(plain)-1024:]...)
			if _, err := tar.NewReader(bytes.NewReader(plain)).Next(); err != io.EOF {
				t.Fatal("fixture did not reach transparent metadata EOF", err)
			}
			var out bytes.Buffer
			writer := gzip.NewWriter(&out)
			if _, err := writer.Write(plain); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			raw = out.Bytes()
			if _, err := readArchive(raw, retainedcolumn.Digest(raw), strings.Repeat("1", 40)); err == nil || !strings.Contains(err.Error(), "unsupported per-file metadata") {
				t.Fatal("dangling per-file metadata admitted", err)
			}
		})
	}
}

func TestArchiveTrailingPadding(t *testing.T) {
	for _, name := range []string{"zero padding", "nonzero byte", "second archive", "hidden metadata"} {
		t.Run(name, func(t *testing.T) {
			var plain bytes.Buffer
			writer := tar.NewWriter(&plain)
			if err := writer.WriteHeader(&tar.Header{Name: archivePrefix + "safe", Mode: 0644, Format: tar.FormatUSTAR}); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			var trailing []byte
			switch name {
			case "zero padding":
				trailing = make([]byte, 1536)
			case "nonzero byte":
				trailing = []byte{0, 0, 1}
			case "second archive":
				trailing = plain.Bytes()
			case "hidden metadata":
				trailing = append([]byte(nil), plain.Bytes()[:512]...)
				trailing[156] = tar.TypeGNULongName
			}
			var out bytes.Buffer
			zip := gzip.NewWriter(&out)
			for _, part := range [][]byte{plain.Bytes(), trailing} {
				if _, err := zip.Write(part); err != nil {
					t.Fatal(err)
				}
			}
			if err := zip.Close(); err != nil {
				t.Fatal(err)
			}
			raw := out.Bytes()
			files, err := readArchive(raw, retainedcolumn.Digest(raw), strings.Repeat("1", 40))
			if name == "zero padding" {
				if err != nil || len(files) != 1 || files["safe"] == nil {
					t.Fatal("zero padding refused", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "nonzero trailing data") {
				t.Fatal("nonzero trailing data admitted", err)
			}
		})
	}
}

func TestUnsupportedMemberFormats(t *testing.T) {
	for _, name := range []string{"GNU", "V7"} {
		t.Run(name, func(t *testing.T) {
			var plain bytes.Buffer
			writer := tar.NewWriter(&plain)
			if err := writer.WriteHeader(&tar.Header{Name: archivePrefix + "safe", Mode: 0644, Format: tar.FormatGNU}); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			data := plain.Bytes()
			if name == "V7" {
				clear(data[257:512])
				copy(data[148:156], []byte("        "))
				sum := 0
				for _, b := range data[:512] {
					sum += int(b)
				}
				copy(data[148:156], []byte(fmt.Sprintf("%06o\x00 ", sum)))
			}
			header, err := tar.NewReader(bytes.NewReader(data)).Next()
			if err != nil || header.Format == tar.FormatUSTAR {
				t.Fatal("invalid unsupported format fixture", err)
			}
			var out bytes.Buffer
			zip := gzip.NewWriter(&out)
			if _, err := zip.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := zip.Close(); err != nil {
				t.Fatal(err)
			}
			raw := out.Bytes()
			if _, err := readArchive(raw, retainedcolumn.Digest(raw), strings.Repeat("1", 40)); err == nil || !strings.Contains(err.Error(), "unsupported per-file metadata") {
				t.Fatal("unsupported member format admitted", err)
			}
		})
	}
}
