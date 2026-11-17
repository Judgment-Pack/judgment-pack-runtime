//go:build windows

package fssecure

import (
	"io/fs"
	"syscall"
	"testing"
)

type attributeInfo struct {
	modeInfo
	attributes uint32
}

func (a attributeInfo) Sys() any {
	return &syscall.Win32FileAttributeData{FileAttributes: a.attributes}
}

// What Go reports for a junction: irregular, ModeDir cleared, and
// FILE_ATTRIBUTE_DIRECTORY in its attributes. It is refused; a file reparse
// point, without the directory bit, is not.
func TestLinkLikeReadsTheDirectoryAttribute(t *testing.T) {
	saved := linkGOOS
	defer func() { linkGOOS = saved }()
	linkGOOS = "windows"
	directory := attributeInfo{modeInfo(fs.ModeIrregular), syscall.FILE_ATTRIBUTE_DIRECTORY | syscall.FILE_ATTRIBUTE_REPARSE_POINT}
	if !LinkLike(directory) {
		t.Error("a directory reparse point was not link-like")
	}
	file := attributeInfo{modeInfo(fs.ModeIrregular), syscall.FILE_ATTRIBUTE_REPARSE_POINT}
	if LinkLike(file) {
		t.Error("a file reparse point was link-like")
	}
}
