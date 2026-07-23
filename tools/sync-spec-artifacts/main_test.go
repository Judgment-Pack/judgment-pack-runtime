package main

import "testing"

func TestImmutableRevisionSpec(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{name: "tag", ref: "v0.1.0-draft", want: "refs/tags/v0.1.0-draft^{commit}"},
		{name: "full tag ref", ref: "refs/tags/v0.1.0-draft", want: "refs/tags/v0.1.0-draft^{commit}"},
		{name: "commit", ref: "0123456789abcdef0123456789abcdef01234567", want: "0123456789abcdef0123456789abcdef01234567^{commit}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := immutableRevisionSpec(test.ref)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestImmutableRevisionSpecRejectsNonExactRefs(t *testing.T) {
	for _, ref := range []string{"", "refs/heads/main", "v0.1.0\nmain"} {
		t.Run(ref, func(t *testing.T) {
			if _, err := immutableRevisionSpec(ref); err == nil {
				t.Fatal("expected ref to be rejected")
			}
		})
	}
}
