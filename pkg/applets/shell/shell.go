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
	"github.com/peterh/liner"
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
	fmt.Fprintln(os.Stderr, "the-go-os sh (applets first; line edit; no history file)")

	// liner puts the TTY in raw mode and restores on Close.
	// No ReadHistory/WriteHistory — optional in-memory recall only.
	if !liner.TerminalSupported() {
		return runInteractivePlain(ctx, runner, parser)
	}

	line := liner.NewLiner()
	defer line.Close()
	line.SetCtrlCAborts(true)
	line.SetCompleter(appletCompleter)

	for {
		input, err := line.Prompt("$ ")
		if err != nil {
			if errors.Is(err, liner.ErrPromptAborted) {
				fmt.Fprintln(os.Stderr)
				continue
			}
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(os.Stderr)
				return nil
			}
			return err
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if input == "exit" {
			return nil
		}
		line.AppendHistory(input) // session only
		if err := runLine(ctx, runner, parser, input); err != nil {
			return err
		}
	}
}

func runInteractivePlain(ctx context.Context, runner *interp.Runner, parser *syntax.Parser) error {
	fmt.Fprintln(os.Stderr, "(plain input; no line editing)")
	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stdout, "$ ")
		if !in.Scan() {
			return in.Err()
		}
		input := strings.TrimSpace(in.Text())
		if input == "" {
			continue
		}
		if input == "exit" {
			return nil
		}
		if err := runLine(ctx, runner, parser, input); err != nil {
			return err
		}
	}
}

func runLine(ctx context.Context, runner *interp.Runner, parser *syntax.Parser, input string) error {
	prog, err := parser.Parse(strings.NewReader(input), "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil
	}
	err = runner.Run(ctx, prog)
	if _, ok := interp.IsExitStatus(err); ok {
		return nil
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
	}
	return nil
}

// appletCompleter suggests registered applet names for the first word.
func appletCompleter(line string) (c []string) {
	fields := strings.Fields(line)
	prefix := ""
	if len(fields) > 0 && !strings.HasSuffix(line, " ") {
		prefix = fields[len(fields)-1]
	}
	// Only complete first token as command name.
	if len(fields) > 1 || (len(fields) == 1 && strings.HasSuffix(line, " ")) {
		return nil
	}
	for _, name := range registry.Names() {
		if strings.HasPrefix(name, prefix) {
			c = append(c, name)
		}
	}
	return c
}

type exitStatus uint8

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", e) }
func (e exitStatus) ExitCode() int { return int(e) }
