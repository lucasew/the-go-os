// Package shell registers the POSIX sh applet (mvdan.cc/sh).
// Registered applets are preferred over PATH and override shell builtins
// of the same name (echo, true, pwd, …) via CallHandler + ExecHandlers.
package shell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lucasew/the-go-os/pkg/registry"
	"github.com/lucasew/the-go-os/pkg/stdio"
	"golang.org/x/term"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// appletMarker rewrites bare applet names so mvdan does not take its own
// builtins (echo, true, …) and ExecHandlers runs our registry instead.
const appletMarker = "__the_go_os__"

func init() {
	registry.Register(registry.Applet{
		Name:  "sh",
		Short: "POSIX shell (mvdan.cc/sh; applets as builtins)",
		Run:   runSh,
	})
}

func runSh(ctx context.Context, args []string) error {
	runner, err := newRunner(ctx)
	if err != nil {
		return err
	}

	rest := args[1:]
	if len(rest) >= 2 && rest[0] == "-c" {
		return runReader(ctx, runner, strings.NewReader(rest[1]), "args")
	}
	if len(rest) >= 1 && rest[0] != "-" {
		f, err := os.Open(rest[0])
		if err != nil {
			return err
		}
		defer f.Close()
		return runReader(ctx, runner, f, rest[0])
	}

	if term.IsTerminal(int(os.Stdin.Fd())) {
		return runInteractive(ctx, runner)
	}
	return runReader(ctx, runner, os.Stdin, "")
}

func newRunner(ctx context.Context) (*interp.Runner, error) {
	s := stdio.From(ctx)
	in := s.In
	// interp.StdIO expects stdin usable as file for interactive; use os.Stdin when possible.
	if in == nil {
		in = os.Stdin
	}
	return interp.New(
		interp.StdIO(asFileOrNil(in), s.Out, s.Err),
		interp.CallHandler(callAppletsFirst),
		interp.ExecHandlers(execAppletsFirst),
	)
}

func asFileOrNil(r io.Reader) io.Reader {
	if r == nil {
		return os.Stdin
	}
	return r
}

// callAppletsFirst rewrites bare registered applet names so they are not
// handled as mvdan shell builtins and instead reach execAppletsFirst.
func callAppletsFirst(_ context.Context, args []string) ([]string, error) {
	if len(args) == 0 {
		return args, nil
	}
	name := args[0]
	if strings.Contains(name, "/") || strings.HasPrefix(name, appletMarker) {
		return args, nil
	}
	if _, ok := registry.Lookup(name); !ok {
		return args, nil
	}
	out := make([]string, len(args))
	copy(out, args)
	out[0] = appletMarker + name
	return out, nil
}

func execAppletsFirst(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		if len(args) == 0 {
			return next(ctx, args)
		}
		name := args[0]
		switch {
		case strings.HasPrefix(name, appletMarker):
			name = strings.TrimPrefix(name, appletMarker)
			args = append([]string{name}, args[1:]...)
		case strings.Contains(name, "/"):
			return next(ctx, args)
		default:
			if _, ok := registry.Lookup(name); !ok {
				return next(ctx, args)
			}
		}

		a, ok := registry.Lookup(name)
		if !ok {
			return next(ctx, args)
		}

		hc := interp.HandlerCtx(ctx)
		actx := stdio.With(ctx, stdio.Streams{
			In:  hc.Stdin,
			Out: hc.Stdout,
			Err: hc.Stderr,
		})

		oldWd, err := os.Getwd()
		if err == nil && hc.Dir != "" && hc.Dir != oldWd {
			if err := os.Chdir(hc.Dir); err != nil {
				return err
			}
			defer func() { _ = os.Chdir(oldWd) }()
		}

		runArgs := args
		if len(runArgs) == 0 || runArgs[0] != name {
			runArgs = append([]string{name}, args[1:]...)
		}
		err = a.Run(actx, runArgs)
		if err == nil {
			return nil
		}
		if code, ok := err.(interface{ ExitCode() int }); ok {
			return interp.NewExitStatus(uint8(code.ExitCode()))
		}
		if status, ok := interp.IsExitStatus(err); ok {
			return interp.NewExitStatus(status)
		}
		fmt.Fprintln(hc.Stderr, err)
		return interp.NewExitStatus(1)
	}
}

func runReader(ctx context.Context, runner *interp.Runner, r io.Reader, name string) error {
	prog, err := syntax.NewParser().Parse(r, name)
	if err != nil {
		return err
	}
	err = runner.Run(ctx, prog)
	if status, ok := interp.IsExitStatus(err); ok {
		return exitStatus(status)
	}
	return err
}

func runInteractive(ctx context.Context, runner *interp.Runner) error {
	parser := syntax.NewParser()
	fmt.Fprintln(os.Stderr, "the-go-os sh (applets first; mvdan; not bash)")
	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stdout, "$ ")
		if !in.Scan() {
			return in.Err()
		}
		line := in.Text()
		if line == "" {
			continue
		}
		if line == "exit" {
			return nil
		}
		prog, err := parser.Parse(strings.NewReader(line), "")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		err = runner.Run(ctx, prog)
		if _, ok := interp.IsExitStatus(err); ok {
			continue
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, err)
		}
	}
}

type exitStatus uint8

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", e) }
func (e exitStatus) ExitCode() int { return int(e) }
