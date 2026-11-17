package emit

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// A staging file a stopped run left in a directory the host writes to, and
// that no owns glob matches, is reported in both modes as
// EMIT-STAGING-LEFTOVER at its path, and never deleted. One in a directory
// the host does not write to is not looked for, and an owned one is a stale
// owned file, which write mode deletes, as before.
func TestEmitReportsStagingLeftoversItDoesNotOwn(t *testing.T) {
	leftovers := map[string]string{
		"gen/.jpack-emit-111": "left\n", "gen/.jpack-emit-222": "left\n",
		".jpack-emit-333": "beside, not written to\n", "other/.jpack-emit-444": "not written to\n",
	}
	response := responseJSON(t, map[string]any{"path": "gen/a.go", "text": "package gen\n"})
	want := []string{"EMIT-STAGING-LEFTOVER@gen/.jpack-emit-111", "EMIT-STAGING-LEFTOVER@gen/.jpack-emit-222"}

	t.Run("unowned", func(t *testing.T) {
		root := testRepo(t)
		writeFiles(t, root, leftovers)
		narrow := map[string]any{"owns": []any{"gen/*.go"}}
		for _, check := range []bool{false, true} {
			report := echoHost(t, root, response, check, narrow)
			if report.Exit() != 0 || !slices.Equal(codes(report.Diagnostics), want) || len(report.Deleted) != 0 {
				t.Fatalf("check %v: %s", check, reportJSON(t, report))
			}
			for _, d := range report.Diagnostics {
				if d.Severity != SeverityWarning || d.Location.Document != DocumentRepository || d.Message != Codes["EMIT-STAGING-LEFTOVER"].message {
					t.Fatalf("check %v: %+v", check, d)
				}
			}
			for path, text := range leftovers {
				if got, _, ok := onDisk(t, root, path); !ok || got != text {
					t.Fatalf("check %v: %s was deleted or changed", check, path)
				}
			}
		}
	})

	t.Run("owned", func(t *testing.T) {
		root := testRepo(t)
		writeFiles(t, root, leftovers)
		report := echoHost(t, root, response, false, nil) // gen/**
		if report.Outcome != OutcomeWritten || len(report.Diagnostics) != 0 ||
			!slices.Equal(report.Deleted, []string{"gen/.jpack-emit-111", "gen/.jpack-emit-222"}) {
			t.Fatalf("owned: %s", reportJSON(t, report))
		}
	})

	// A response path held by a stale file is staged beside that file: its
	// directory is the one looked in.
	t.Run("beside a stale file", func(t *testing.T) {
		root := testRepo(t)
		writeFiles(t, root, map[string]string{"gen/sub": "stale\n", "gen/.jpack-emit-555": "left\n"})
		owns := map[string]any{"owns": []any{"gen/sub", "gen/sub/*.go"}}
		report := echoHost(t, root, responseJSON(t, map[string]any{"path": "gen/sub/x.go", "text": "x\n"}), false, owns)
		if report.Outcome != OutcomeWritten || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-STAGING-LEFTOVER@gen/.jpack-emit-555"}) {
			t.Fatalf("beside: %s", reportJSON(t, report))
		}
		if _, _, ok := onDisk(t, root, "gen/.jpack-emit-555"); !ok {
			t.Fatal("the leftover was deleted")
		}
	})
}

// ListPaths lists the first MaxListedPaths paths in sorted order, one
// diagnostic each, and past them one more at the document alone whose
// message counts the rest.
func TestListPathsCapsTheListingAndCountsTheRest(t *testing.T) {
	if got := ListPaths("EMIT-STAGING-LEFTOVER", DocumentRepository, nil); len(got) != 0 {
		t.Fatalf("no paths: %+v", got)
	}
	paths := make([]string, 0, MaxListedPaths+3)
	for i := MaxListedPaths + 2; i >= 0; i-- {
		paths = append(paths, fmt.Sprintf("gen/.jpack-emit-%03d", i))
	}
	given := slices.Clone(paths)
	for _, n := range []int{MaxListedPaths, MaxListedPaths + 3} {
		got := ListPaths("EMIT-STAGING-LEFTOVER", DocumentRepository, paths[len(paths)-n:])
		if !slices.Equal(paths, given) {
			t.Fatal("the paths given were reordered")
		}
		listed := min(n, MaxListedPaths)
		if len(got) != listed+min(1, n-listed) {
			t.Fatalf("%d paths: %d diagnostics", n, len(got))
		}
		for i, d := range got[:listed] {
			if want := fmt.Sprintf("gen/.jpack-emit-%03d", i); d.Code != "EMIT-STAGING-LEFTOVER" || d.Location.Path != want || d.Message != Codes[d.Code].message {
				t.Fatalf("%d paths: diagnostic %d is %+v, want %s", n, i, d, want)
			}
		}
		if n == listed {
			continue
		}
		last := got[listed]
		if last.Code != "EMIT-STAGING-LEFTOVER" || last.Severity != SeverityWarning || last.Location != (Location{Document: DocumentRepository}) ||
			!strings.HasPrefix(last.Message, Codes[last.Code].message) || !strings.HasSuffix(last.Message, fmt.Sprintf(" first %d by path; 3 more are not listed.", MaxListedPaths)) {
			t.Fatalf("the count: %+v", last)
		}
	}
}
