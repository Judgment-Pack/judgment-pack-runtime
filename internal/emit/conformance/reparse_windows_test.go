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

// A junction in the repository that leads out, and that no path the run
// writes or reads could name, is left out of the copy and reported as
// EMIT-LINK-OMITTED at its path: a write at its path is a stray write of the
// copy and never reaches where the junction leads. Go reads it as irregular,
// not as a link; os.Readlink reads where it leads.
func TestAJunctionOutOfTheRepositoryIsLeftOut(t *testing.T) {
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
	if report.Outcome != OutcomeFail || len(report.Diagnostics) != 1 {
		t.Fatalf("report: %s", dump(report))
	}
	if d := report.Diagnostics[0]; d.Code != "EMIT-LINK-OMITTED" || d.Location != (emit.Location{Document: emit.DocumentRepository, Path: "docs/out"}) {
		t.Fatalf("diagnostic: %+v", d)
	}
	if row := rowOf(t, report, "a", checkStrayWrites); row.Status != StatusFail || len(row.StrayWrites) != 1 || row.StrayWrites[0].Path != "docs" {
		t.Fatalf("check 6: %+v", row)
	}
	if _, err := os.Lstat(filepath.Join(outside, "written.txt")); !os.IsNotExist(err) {
		t.Fatalf("the emitter wrote through the junction: %v", err)
	}
}

// A junction that an owned path could name refuses the run with
// EMIT-HARNESS-BOUND at its path, before the emitter runs, as a symbolic link
// there does; so does a junction that stays in the repository, which the copy
// does not make.
func TestAJunctionTheCopyCannotLeaveOutIsRefused(t *testing.T) {
	cases := map[string]struct {
		link   string
		inside bool
	}{
		"at an owned path":          {link: "gen/out"},
		"staying in the repository": {link: "docs/in", inside: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			target := t.TempDir()
			write(t, f.root, map[string]string{"docs/a.md": "a\n", "gen/a.txt": "a\n"})
			if c.inside {
				target = filepath.Join(f.root, "docs")
			}
			junction(t, filepath.Join(f.root, filepath.FromSlash(c.link)), target)
			m := f.manifest([]string{"stray", "create", "ran.txt"}, map[string]any{"environments": "all"})
			f.registryVector("a", nil)
			report := f.run(m, true)
			if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 {
				t.Fatalf("report: %s", dump(report))
			}
			if d := report.Diagnostics[0]; d.Code != "EMIT-HARNESS-BOUND" || d.Location != (emit.Location{Document: emit.DocumentRepository, Path: c.link}) {
				t.Fatalf("diagnostic: %+v", d)
			}
			if _, err := os.Lstat(filepath.Join(f.root, "ran.txt")); !os.IsNotExist(err) {
				t.Fatalf("the emitter ran: %v", err)
			}
		})
	}
}
