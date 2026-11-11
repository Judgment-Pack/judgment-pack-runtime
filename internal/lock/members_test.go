package lock

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The member names the decoder admits are exactly the json names of the types
// packs lock encodes, so a field added to one of them without its name here
// fails this test rather than every lock packs lock then writes.
func TestTheKnownMembersAreTheMembersPacksLockWrites(t *testing.T) {
	for _, c := range []struct {
		known map[string]bool
		value any
	}{{documentMembers, Document{}}, {configMembers, Config{}}, {entryMembers, Entry{}}} {
		kind := reflect.TypeOf(c.value)
		written := []string{}
		for index := 0; index < kind.NumField(); index++ {
			written = append(written, strings.Split(kind.Field(index).Tag.Get("json"), ",")[0])
		}
		known := []string{}
		for name := range c.known {
			known = append(known, name)
		}
		sort.Strings(written)
		sort.Strings(known)
		if !reflect.DeepEqual(written, known) {
			t.Fatalf("%s writes %v, the decoder admits %v", kind.Name(), written, known)
		}
	}
}

// A lock is read by its exact member names. A name packs lock does not write
// is refused wherever it appears, including another spelling of a known name
// that encoding/json would otherwise match without regard to case or under
// Unicode case folding, and the refusal names it, sanitized and bounded.
func TestDecodeRefusesAMemberPacksLockDoesNotWrite(t *testing.T) {
	// LATIN SMALL LETTER LONG S, which Unicode case folding takes to "s".
	const longS = "\u017f"
	digest := "sha256:" + strings.Repeat("a", 64)
	other := "sha256:" + strings.Repeat("b", 64)
	entry := `{"path":"packs/intake.json","digest":"` + digest + `"}`
	head := `{"lockVersion":"1","config":{"digest":"` + digest + `"}`
	const tail = ". It is generated: run jpack packs lock to write it again."
	for _, c := range []struct {
		name, lock, detail string
	}{
		{"the root, in another case", head + `,"packs":{"intake":` + entry + `},"Packs":{"intake":{"path":"packs/intake.json","digest":"` + other + `"}}}`,
			`has a member "Packs" that packs lock does not write`},
		{"the root, under Unicode case folding", head + `,"packs":{"intake":` + entry + `},"pack` + longS + `":{"intake":{"path":"packs/intake.json","digest":"` + other + `"}}}`,
			"has a member \"pack" + longS + "\" that packs lock does not write"},
		{"the version, in another case", head + `,"LOCKVERSION":"7"}`,
			`has a member "LOCKVERSION" that packs lock does not write`},
		{"the configuration, in another case", `{"lockVersion":"1","config":{"digest":"` + digest + `","DIGEST":"` + other + `"}}`,
			`declares the configuration with a member "DIGEST" that packs lock does not write`},
		{"the configuration, under Unicode case folding", `{"lockVersion":"1","config":{"digest":"` + digest + `","dige` + longS + `t":"` + other + `"}}`,
			"declares the configuration with a member \"dige" + longS + "t\" that packs lock does not write"},
		{"a pack entry, in another case", head + `,"packs":{"intake":{"path":"packs/intake.json","digest":"` + digest + `","Digest":"` + other + `"}}}`,
			`declares the pack "intake" with a member "Digest" that packs lock does not write`},
		{"a graph entry, under Unicode case folding", head + `,"graphs":{"flow":{"path":"graphs/flow.json","digest":"` + digest + `","dige` + longS + `t":"` + other + `"}}}`,
			"declares the graph \"flow\" with a member \"dige" + longS + "t\" that packs lock does not write"},
		{"the first of several, in sorted order", head + `,"zeta":1,"Alpha":2,"beta":3,"gamma":4,"delta":5,"epsilon":6}`,
			`has a member "Alpha" that packs lock does not write`},
		{"a name past the bound", head + `,"` + strings.Repeat("x", 70) + `\u0007":1}`,
			`has a member "` + strings.Repeat("x", 64) + `…" that packs lock does not write`},
	} {
		t.Run(c.name, func(t *testing.T) {
			want := &Failure{Code: "JPS-LOCK-SHAPE", Message: "The reviewed-set lock project/jpack.lock.json " + c.detail + tail, ExitCode: 1}
			// Repeated, because a member set is a map: the name refused must
			// not depend on the order it is walked in.
			for range 16 {
				set, failure := Decode("project/jpack.lock.json", []byte(c.lock))
				if set != nil || !reflect.DeepEqual(failure, want) {
					t.Fatalf("Decode = (%+v, %+v)\nwant %+v", set, failure, want)
				}
			}
		})
	}
	// A name the bound does not cut is shown whole, and a control in it is
	// shown as a question mark.
	_, failure := Decode("jpack.lock.json", []byte(head+`,"a\u0007b":1}`))
	if failure == nil || failure.Message != `The reviewed-set lock jpack.lock.json has a member "a?b" that packs lock does not write`+tail {
		t.Fatalf("failure = %+v", failure)
	}
	// The version is read first: a later lock is told it is one, whatever
	// members it carries.
	_, failure = Decode("jpack.lock.json", []byte(`{"lockVersion":"2","reviewers":[]}`))
	if failure == nil || failure.Code != "JPS-LOCK-VERSION" || failure.ExitCode != 2 {
		t.Fatalf("failure = %+v", failure)
	}
	// Every member packs lock writes is still read.
	written := head + `,"packs":{"intake":` + entry + `},"graphs":{"flow":{"path":"graphs/flow.json","digest":"` + other + `"}}}`
	set, failure := Decode("jpack.lock.json", []byte(written))
	if failure != nil || set.Document.Packs["intake"].Digest != digest || set.Document.Graphs["flow"].Digest != other {
		t.Fatalf("Decode = (%+v, %+v)", set, failure)
	}
}
