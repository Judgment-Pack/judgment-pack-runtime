package emit

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// On Windows the emitter's home, application data and temporary directory
// are the fresh HOME (TEMP and TMP a directory in it), never the host's, and
// a name the manifest's env lists wins, as one setting. The
// environment is formed as on Windows wherever the test runs.
func TestTheEmitterGetsAFreshWindowsHome(t *testing.T) {
	saved := hostGOOS
	defer func() { hostGOOS = saved }()
	hostGOOS = "windows"
	for _, name := range []string{"USERPROFILE", "APPDATA", "TEMP", "TMP"} {
		// Directories that exist: jpack's own temporary directory is made
		// under TEMP before the emitter starts.
		t.Setenv(name, t.TempDir())
	}
	t.Setenv("LOCALAPPDATA", "declared-local")
	root := testRepo(t)
	report := host(t, root, []string{"env", "gen/env.txt"}, map[string]any{"environments": "all", "env": []any{"LOCALAPPDATA"}}, Options{})
	if report.Outcome != OutcomeWritten || len(report.Diagnostics) != 0 {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	text, _, _ := onDisk(t, root, "gen/env.txt")
	got := map[string]string{}
	var names []string
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		name, value, _ := strings.Cut(line, "=")
		if strings.EqualFold(name, "SYSTEMROOT") {
			continue
		}
		got[strings.ToUpper(name)] = value
		names = append(names, strings.ToUpper(name))
	}
	slices.Sort(names)
	if want := []string{"APPDATA", "HOME", "LANG", "LOCALAPPDATA", "PATH", "TEMP", "TMP", "USERPROFILE"}; !slices.Equal(names, want) {
		t.Fatalf("the emitter's environment has %v, want %v", names, want)
	}
	home := got["HOME"]
	if home == "" || got["USERPROFILE"] != home || got["APPDATA"] != home {
		t.Fatalf("the Windows homes are not the fresh HOME: %v", got)
	}
	if tmp := filepath.Join(home, "tmp"); got["TEMP"] != tmp || got["TMP"] != tmp {
		t.Fatalf("TEMP and TMP are not HOME's tmp: %v", got)
	}
	if got["LOCALAPPDATA"] != "declared-local" {
		t.Fatalf("the manifest's LOCALAPPDATA did not win: %v", got)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("the emitter's HOME was not removed: %v", err)
	}
}
