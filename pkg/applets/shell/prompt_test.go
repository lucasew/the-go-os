package shell

import (
	"context"
	"os"
	"strings"
	"testing"

	_ "github.com/lucasew/the-go-os/pkg/applets/coreutils"
	"mvdan.cc/sh/v3/expand"
)

func TestExpandPS1PathAndUser(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	out := expandPS1(`\u@\h:\w\$ `)
	if !strings.Contains(out, ":") {
		t.Fatalf("expected user@host:path, got %q", out)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "$") && !strings.HasSuffix(strings.TrimSpace(out), "#") {
		t.Fatalf("expected $ or # suffix: %q", out)
	}
	// \w should reflect cwd (possibly with ~)
	if !strings.Contains(out, wd) && !strings.Contains(out, "~") {
		// home may rewrite entirely to ~something; still must be non-empty path part
		parts := strings.SplitN(out, ":", 2)
		if len(parts) < 2 || parts[1] == "" {
			t.Fatalf("path missing in %q (wd=%s)", out, wd)
		}
	}
}

func TestExpandPS1StripsReadlineMarkers(t *testing.T) {
	out := expandPS1(`\[\e[1;32m\]hi\[\e[0m\]\$ `)
	if strings.Contains(out, "[") && strings.Contains(out, `\[`) {
		t.Fatalf("markers not stripped: %q", out)
	}
	if !strings.Contains(out, "hi") {
		t.Fatalf("got %q", out)
	}
	// escape became ESC
	if !strings.Contains(out, "\x1b[1;32m") {
		t.Fatalf("expected ANSI in %q", out)
	}
}

func TestCurrentPS1InheritsEnv(t *testing.T) {
	t.Setenv("PS1", "CUSTOM> ")
	got := currentPS1(nil, nil)
	if got != "CUSTOM> " {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureDefaultPS1(t *testing.T) {
	env := newShellEnv(nil)
	ensureDefaultPS1(env)
	if env.Get("PS1").String() != defaultPS1 {
		t.Fatalf("got %q", env.Get("PS1").String())
	}
	// do not clobber
	_ = env.Set("PS1", expandStringVar("X> "))
	ensureDefaultPS1(env)
	if env.Get("PS1").String() != "X> " {
		t.Fatalf("clobbered: %q", env.Get("PS1").String())
	}
}

func TestExpandPromptExitStatus(t *testing.T) {
	ctx := context.Background()
	out := expandPromptDynamics(ctx, "st=$? done", 42, nil, nil)
	if out != "st=42 done" {
		t.Fatalf("got %q", out)
	}
}

func TestExpandPromptCmdSubst(t *testing.T) {
	ctx := context.Background()
	// Use printf/echo via PATH or our applets — register coreutils in this package test via import.
	out := expandPromptDynamics(ctx, "X$(echo hi)Y", 0, nil, newShellEnv(os.Environ()))
	if out != "XhiY" {
		t.Fatalf("got %q", out)
	}
}

func TestPromptForLinerStripsANSI(t *testing.T) {
	in := "hi\x1b[1;32mcolor\x1b[0m there\x07"
	out := promptForLiner(in)
	if strings.Contains(out, "\x1b") || strings.ContainsRune(out, '\a') {
		t.Fatalf("controls remain: %q", out)
	}
	if !strings.Contains(out, "hi") || !strings.Contains(out, "color") || !strings.Contains(out, "there") {
		t.Fatalf("text lost: %q", out)
	}
}

func TestPromptForLinerTputLike(t *testing.T) {
	// tput sgr0 often emits ESC [ m or similar
	in := "user@host:~/path 1$ \x1b[m"
	out := promptForLiner(in)
	if out != "user@host:~/path 1$ " {
		t.Fatalf("got %q", out)
	}
}

func expandStringVar(s string) expand.Variable {
	return expand.Variable{Set: true, Kind: expand.String, Str: s, Exported: true}
}
