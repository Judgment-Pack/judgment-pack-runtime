package terraformfile

import (
	"bytes"
	"runtime"
	"strconv"
	"testing"
)

// allocated returns the bytes allocated while read runs. It measures
// allocation, not time, so it holds on any machine.
func allocated(t *testing.T, data []byte, src Source) (uint64, *Refusal) {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, refusal, err := Read(bytes.NewReader(data), src)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	return after.TotalAlloc - before.TotalAlloc, refusal
}

// The arena's worst case is one node per two input bytes, `[0,0,...]`: 12
// bytes of node for 2 of input. With the input's own copy and the removal
// bits, the read of such a file allocates about 7.1 times its size. The
// budget is 7.5 times, measured at 4 MiB so it fits well under the test's
// memory cap; at the 64 MiB default that is 480 MiB, and at the 256 MiB cap
// 1.9 GiB.
func TestTheArenaWorstCaseStaysWithinItsAllocationBudget(t *testing.T) {
	const size = 4 << 20
	worst := make([]byte, 0, size+1)
	worst = append(worst, '[')
	for len(worst) < size-1 {
		worst = append(worst, '0', ',')
	}
	worst = append(worst, '0', ']')

	// Object members: two nodes per member, and a key per member while
	// duplicates are checked.
	members := []byte(`{"format_version":"1.2","planned_values":{},"configuration":{`)
	for i := 0; len(members) < size-2; i++ {
		if i > 0 {
			members = append(members, ',')
		}
		members = append(members, '"')
		members = strconv.AppendInt(members, int64(i), 36)
		members = append(members, `":0`...)
	}
	members = append(members, "}}"...)

	for _, c := range []struct {
		name   string
		data   []byte
		code   string
		budget float64
	}{
		{"one node per two bytes", worst, CodeFormat, 7.5},
		{"one member per few bytes", members, "", 7.5},
	} {
		t.Run(c.name, func(t *testing.T) {
			total, refusal := allocated(t, c.data, planSource("synthetic_db"))
			if (refusal == nil) != (c.code == "") || (refusal != nil && refusal.Code != c.code) {
				t.Fatalf("refusal %v, want %q", refusal, c.code)
			}
			ratio := float64(total) / float64(len(c.data))
			t.Logf("%d bytes allocated for %d bytes of input: %.2f per byte", total, len(c.data), ratio)
			if ratio > c.budget {
				t.Fatalf("allocated %.2f bytes per input byte, over the budget of %.2f", ratio, c.budget)
			}
		})
	}
}

// A selected resource whose value is all of the file: the arena (6 bytes per
// input byte), the input (1), and the value written as canonical JSON, where
// each `0,` becomes `"0",` (2). The value is counted first and written into
// one buffer of its exact size, so the read allocates about 9 times the input.
func TestAFullReadStaysWithinItsAllocationBudget(t *testing.T) {
	const size = 4 << 20
	items := make([]byte, 0, size)
	items = append(items, '[')
	for len(items) < size-1 {
		items = append(items, '0', ',')
	}
	items = append(items, '0', ']')
	data := plan(t, change{address: "synthetic_db.main", after: rawJSON(items)})
	total, refusal := allocated(t, data, planSource("synthetic_db"))
	if refusal != nil {
		t.Fatalf("refused %s", refusal.Code)
	}
	ratio := float64(total) / float64(len(data))
	t.Logf("%d bytes allocated for %d bytes of input: %.2f per byte", total, len(data), ratio)
	if ratio > 9.5 {
		t.Fatalf("allocated %.2f bytes per input byte, over the budget of 9.5", ratio)
	}
}

type rawJSON []byte

func (r rawJSON) MarshalJSON() ([]byte, error) { return r, nil }
