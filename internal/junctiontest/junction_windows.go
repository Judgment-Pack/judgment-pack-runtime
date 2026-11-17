//go:build windows

package junctiontest

import (
	"encoding/binary"
	"os"
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

// Make makes link a junction to the absolute directory target, as
// `mklink /J` does, without running a program: the mount point reparse
// data is set on an empty directory. No privilege is needed.
func Make(t testing.TB, link, target string) {
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
