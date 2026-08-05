package registry_test

import (
	"context"
	"testing"

	"github.com/lucasew/the-go-os/pkg/registry"
)

func TestRegisterLookupDispatch(t *testing.T) {
	// Use a unique name so we do not clash with real applets if tests share package init.
	// registry panics on duplicate — register only in this test package via a helper name
	// that core profile might not use. "test-applet-xyz" is fine.
	const name = "test-applet-xyz"
	if _, ok := registry.Lookup(name); ok {
		t.Fatal("precondition: name already registered")
	}
	var saw []string
	registry.Register(registry.Applet{
		Name:  name,
		Short: "test",
		Run: func(ctx context.Context, args []string) error {
			saw = append([]string{}, args...)
			return nil
		},
	})
	a, ok := registry.Lookup(name)
	if !ok || a.Name != name {
		t.Fatalf("lookup: ok=%v a=%+v", ok, a)
	}
	if err := registry.Dispatch(context.Background(), name, []string{name, "a"}); err != nil {
		t.Fatal(err)
	}
	if len(saw) < 2 || saw[0] != name || saw[1] != "a" {
		t.Fatalf("args=%v", saw)
	}
	found := false
	for _, n := range registry.Symlinks() {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Fatal("expected symlink list to include test applet")
	}
}
