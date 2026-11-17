//go:build unix

package emit

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// A descriptor the host holds without close-on-exec, as one jpack inherited
// would be, is not passed on to the emitter.
func TestTheEmitterInheritsNoDescriptorOfTheHost(t *testing.T) {
	// dup makes a descriptor without the close-on-exec flag.
	fd, err := syscall.Dup(2)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	saved := markInherited
	defer func() { markInherited = saved }()
	markInherited = sync.OnceFunc(closeInherited)
	root := testRepo(t)
	report := host(t, root, []string{"fds", "gen/fds.txt"}, map[string]any{"environments": "all"}, Options{})
	if report.Outcome != OutcomeWritten {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	text, _, _ := onDisk(t, root, "gen/fds.txt")
	if fds := strings.Fields(text); len(fds) == 0 || slices.Contains(fds, strconv.Itoa(fd)) {
		t.Fatalf("the emitter holds the host's descriptor %d: %v", fd, fds)
	}
}
