package fssecure

import (
	"errors"
	"os"
)

// OpenSnapshot opens a trail and its companions and captures their sizes under
// the trail's shared lock. Each opener must resolve the same name on every call;
// an optional absent companion returns nil, nil. The caller owns the returned
// files. Companions are opened only after taking the lock, so a writer's first
// creation cannot be mistaken for absence before that writer's transaction.
// Names are opened again under the lock to check identity, including absence;
// a changed identity retries the entire snapshot, up to a bounded count.
// Without a lock, sizes are still read companions first, but are not a
// between-writes snapshot. Independently locked files have their own semantics.
func OpenSnapshot(openTrail func() (*os.File, error), companions ...func() (*os.File, error)) ([]*os.File, []int64, bool, error) {
	openers := append([]func() (*os.File, error){openTrail}, companions...)
	for range appendOpenAttempts {
		files, sizes, locked, retry, err := openSnapshot(openers)
		if err != nil || !retry {
			return files, sizes, locked, err
		}
	}
	return nil, nil, false, errors.New("snapshot paths kept changing while taking the lock")
}

func openSnapshot(openers []func() (*os.File, error)) (files []*os.File, sizes []int64, locked, retry bool, err error) {
	trail, err := openers[0]()
	if err != nil {
		return nil, nil, false, false, err
	}
	files = []*os.File{trail}
	defer func() {
		if err != nil || retry {
			for _, file := range files {
				if file != nil {
					file.Close()
				}
			}
			files, sizes = nil, nil
		}
	}()
	locked = true
	if err = lockShared(trail); errors.Is(err, errNoLock) {
		locked, err = false, nil
	} else if err != nil {
		return
	}
	if locked {
		defer unlockFile(trail)
	}
	for _, open := range openers[1:] {
		var file *os.File
		file, err = open()
		if err != nil {
			return
		}
		files = append(files, file)
	}
	sizes = make([]int64, len(files))
	// Companions first, as in SizesBetweenWrites when no lock is available.
	for index := 1; index <= len(files); index++ {
		i := index % len(files)
		var current *os.File
		current, err = openers[i]()
		if err != nil {
			return
		}
		file := files[i]
		if current == nil || file == nil {
			retry = current != file
			if current != nil {
				current.Close()
			}
		} else {
			var held, named os.FileInfo
			held, err = file.Stat()
			if err == nil {
				named, err = current.Stat()
			}
			current.Close()
			if err != nil {
				return
			}
			retry = !os.SameFile(held, named)
			sizes[i] = held.Size()
		}
		if retry {
			return
		}
	}
	return
}
