//go:build windows

package conformance

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unicode/utf16"
	"unsafe"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

const (
	fsctlSetReparsePoint    = 0x000900A4
	ioReparseTagMountPoint  = 0xA0000003
	mountPointHeaderLength  = 8
	reparseDataHeaderLength = 8
)

// junction makes link a junction to the absolute directory target, as
// `mklink /J` does, without running a program: the mount point reparse
// data is set on an empty directory. No privilege is needed.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Mkdir(link, 0o755); err != nil {
		t.Fatal(err)
	}
	substitute := utf16.Encode([]rune(`\??\` + target))
	printed := utf16.Encode([]rune(target))
	paths := make([]uint16, 0, len(substitute)+len(printed)+2)
	paths = append(append(append(append(paths, substitute...), 0), printed...), 0)
	dataLength := mountPointHeaderLength + 2*len(paths)
	buffer := make([]byte, reparseDataHeaderLength+dataLength)
	binary.LittleEndian.PutUint32(buffer[0:], ioReparseTagMountPoint)
	binary.LittleEndian.PutUint16(buffer[4:], uint16(dataLength))
	binary.LittleEndian.PutUint16(buffer[8:], 0)
	binary.LittleEndian.PutUint16(buffer[10:], uint16(2*len(substitute)))
	binary.LittleEndian.PutUint16(buffer[12:], uint16(2*(len(substitute)+1)))
	binary.LittleEndian.PutUint16(buffer[14:], uint16(2*len(printed)))
	for i, unit := range paths {
		binary.LittleEndian.PutUint16(buffer[16+2*i:], unit)
	}
	name, err := syscall.UTF16PtrFromString(link)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	var returned uint32
	if err := syscall.DeviceIoControl(handle, fsctlSetReparsePoint, (*byte)(unsafe.Pointer(&buffer[0])), uint32(len(buffer)), nil, 0, &returned, nil); err != nil {
		t.Fatal(err)
	}
}

// A junction in the repository that leads out refuses the run with
// EMIT-HARNESS-BOUND at its path, before the emitter runs, fallback or not:
// Go reads it as irregular, not as a link, so the link check cannot follow
// it.
func TestAJunctionInTheRepositoryIsRefused(t *testing.T) {
	f := newFixture(t)
	outside := t.TempDir()
	write(t, f.root, map[string]string{"docs/a.md": "a\n"})
	junction(t, filepath.Join(f.root, "docs", "out"), outside)
	if info, err := os.Lstat(filepath.Join(f.root, "docs", "out")); err != nil || info.Mode()&os.ModeIrregular == 0 {
		t.Fatalf("the junction reads as %v: %v", info, err)
	}
	m := f.manifest([]string{"stray", "create", "docs/out/written.txt"}, map[string]any{"environments": "all"})
	f.registryVector("a", nil)
	report := f.run(m, true)
	if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 {
		t.Fatalf("report: %s", dump(report))
	}
	if d := report.Diagnostics[0]; d.Code != "EMIT-HARNESS-BOUND" || d.Location.Document != emit.DocumentRepository || d.Location.Path != "docs/out" {
		t.Fatalf("diagnostic: %+v", d)
	}
	if _, err := os.Lstat(filepath.Join(outside, "written.txt")); !os.IsNotExist(err) {
		t.Fatalf("the emitter wrote through the junction: %v", err)
	}
}
