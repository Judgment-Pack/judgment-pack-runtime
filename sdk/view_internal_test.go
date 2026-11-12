package sdk

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/viewhook"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// A construct the view builder cannot represent reaches the caller of View as
// a fault of this runtime: an error wrapping ErrInternal with the view's code
// and the builder's message, never a *Refusal and never a partial view. The
// real builder is handed a copy of the admitted root with one member it does
// not know.
func TestAViewFaultIsAnInternalFault(t *testing.T) {
	loaded, err := Load(context.Background(), []byte(reusePack), LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	previous := viewhook.Build
	viewhook.Build = func(root map[string]any, digest string) (any, string) {
		edited := make(map[string]any, len(root)+1)
		for name, value := range root {
			edited[name] = value
		}
		edited["owner"] = "x"
		return previous(edited, digest)
	}
	t.Cleanup(func() { viewhook.Build = previous })
	described, err := loaded.View()
	var refusal *Refusal
	if err == nil || !errors.Is(err, ErrInternal) || errors.As(err, &refusal) {
		t.Fatalf("View = %v (%T)", err, err)
	}
	const want = "sdk: internal fault: JPS-VIEW-UNREPRESENTABLE: The admitted pack holds a construct at /owner that this view cannot represent"
	if !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("Error() = %q, want the prefix %q", err.Error(), want)
	}
	if described.Rules != nil || described.Identity != (view.Identity{}) || described.JSON() != nil || described.Markdown() != nil {
		t.Fatalf("a partial view came back: %+v", described)
	}
	viewhook.Build = previous
	if _, err := loaded.View(); err != nil {
		t.Fatalf("the unedited root: %v", err)
	}
}

// View and Identity on a Pack Load did not return say so and never panic.
func TestViewOfAPackLoadDidNotReturn(t *testing.T) {
	var none *Pack
	if _, err := none.View(); err == nil || errors.Is(err, ErrInternal) {
		t.Fatalf("View on a nil Pack: %v", err)
	}
	if _, err := (&Pack{}).View(); err == nil || errors.Is(err, ErrInternal) {
		t.Fatalf("View on a zero Pack: %v", err)
	}
	if none.Identity() != (Identity{}) || (&Pack{}).Identity() != (Identity{}) {
		t.Fatal("Identity of a Pack Load did not return is not zero")
	}
}
