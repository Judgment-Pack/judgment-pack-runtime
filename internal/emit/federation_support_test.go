package emit

import (
	"strings"
	"testing"
)

// ValidPath is checkPathForm, as it is.
func TestValidPathIsThePathForm(t *testing.T) {
	for _, path := range []string{"registry.json", "kinds/db-role.json", strings.Repeat("a", MaxPathBytes)} {
		if !ValidPath(path) || checkPathForm(path) != nil {
			t.Errorf("%q refused", path)
		}
	}
	for _, path := range []string{"", "/a", "a//b", "a/../b", ".git/config", "a\\b", "c:x", "a\u0085b", "NUL.json", strings.Repeat("a", MaxPathBytes+1)} {
		if ValidPath(path) || checkPathForm(path) == nil {
			t.Errorf("%q admitted", path)
		}
	}
}
