package shell_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	// Register applets used by the shell.
	_ "github.com/lucasew/the-go-os/pkg/applets/coreutils"
	_ "github.com/lucasew/the-go-os/pkg/applets/shell"
	"github.com/lucasew/the-go-os/pkg/registry"
	"github.com/lucasew/the-go-os/pkg/stdio"
)

func TestShellUsesAppletsFirst(t *testing.T) {
	var out bytes.Buffer
	ctx := stdio.With(context.Background(), stdio.Streams{
		In:  strings.NewReader(""),
		Out: &out,
		Err: &out,
	})
	// echo is both a mvdan builtin and our applet — rewrite forces registry path.
	err := registry.Dispatch(ctx, "sh", []string{"sh", "-c", "echo hello-from-applet-shell"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "hello-from-applet-shell") {
		t.Fatalf("out=%q", out.String())
	}
}

func TestShellFalseExit(t *testing.T) {
	var out bytes.Buffer
	ctx := stdio.With(context.Background(), stdio.Streams{Out: &out, Err: &out})
	err := registry.Dispatch(ctx, "sh", []string{"sh", "-c", "false"})
	if err == nil {
		t.Fatal("expected non-zero exit from false")
	}
	if code, ok := err.(interface{ ExitCode() int }); !ok || code.ExitCode() != 1 {
		t.Fatalf("err=%v", err)
	}
}

func TestShellLsBuiltin(t *testing.T) {
	var out bytes.Buffer
	ctx := stdio.With(context.Background(), stdio.Streams{Out: &out, Err: &out})
	// ls is not a mvdan builtin; must hit registry without relying on PATH.
	err := registry.Dispatch(ctx, "sh", []string{"sh", "-c", "ls ."})
	if err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		t.Fatal("expected ls output")
	}
}
