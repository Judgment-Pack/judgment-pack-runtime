//go:build windows

package emit

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unicode/utf16"
	"unsafe"
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

// A repository root that is a junction is never walked as an empty root:
// RealRoot either refuses it, or resolves it to a real directory whose owned
// files the scan finds.
func TestAJunctionRootIsNotScannedAsEmpty(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "gen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "gen", "stale.txt"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "root")
	junction(t, link, target)
	real, err := RealRoot(link)
	if err != nil {
		return // refused: the scan never starts
	}
	if info, err := os.Lstat(real); err != nil || !rootIsDirectory(info.Mode()) {
		t.Fatalf("RealRoot gave %q, which reads as %v: %v", real, info, err)
	}
	glob, err := parseGlob("gen/**")
	if err != nil {
		t.Fatal(err)
	}
	files, diagnostics := scanOwned(real, []Glob{glob})
	if _, ok := files["gen/stale.txt"]; !ok || len(diagnostics) != 0 {
		t.Fatalf("the scan from %q found %v, %v; want gen/stale.txt", real, files, diagnostics)
	}
}

// A junction below the root where a glob can match refuses the run with
// EMIT-LINK at its path, as a symbolic link does.
func TestAJunctionBelowTheRootIsALink(t *testing.T) {
	root, err := RealRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "gen"), 0o755); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(root, "gen", "out"), t.TempDir())
	glob, err := parseGlob("gen/**")
	if err != nil {
		t.Fatal(err)
	}
	_, diagnostics := scanOwned(root, []Glob{glob})
	if len(diagnostics) != 1 || diagnostics[0].Code != "EMIT-LINK" || diagnostics[0].Location.Path != "gen/out" {
		t.Fatalf("diagnostics: %+v; want one EMIT-LINK at gen/out", diagnostics)
	}
}

// A junction at or above a response path refuses the run with EMIT-LINK at
// it, though Go may read the junction as a directory as well as irregular.
func TestAJunctionAboveAResponsePathIsALink(t *testing.T) {
	root, err := RealRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "gen"), 0o755); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(root, "gen", "out"), t.TempDir())
	_, _, diagnostics := preflight(root, []File{{Path: "gen/out/a.txt", Data: []byte("a\n"), Mode: ModeRegular}})
	if len(diagnostics) != 1 || diagnostics[0].Code != "EMIT-LINK" || diagnostics[0].Location.Path != "gen/out" {
		t.Fatalf("diagnostics: %+v; want one EMIT-LINK at gen/out", diagnostics)
	}
}
