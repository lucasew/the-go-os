// Package shell registers the POSIX sh applet (mvdan.cc/sh).
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
	"golang.org/x/term"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

func init() {
	registry.Register(registry.Applet{
		Name:  "sh",
		Short: "POSIX shell (mvdan.cc/sh; not bash)",
		Run:   runSh,
	})
}

func runSh(ctx context.Context, args []string) error {
	// sh [-c cmd] [script]
	runner, err := interp.New(interp.StdIO(os.Stdin, os.Stdout, os.Stderr))
	if err != nil {
		return err
	}

	// Parse flags lightly
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

	// Interactive if terminal
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return runInteractive(ctx, runner)
	}
	return runReader(ctx, runner, os.Stdin, "")
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
	fmt.Fprintln(os.Stderr, "the-go-os sh (mvdan; not bash)")
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

func (e exitStatus) Error() string   { return fmt.Sprintf("exit status %d", e) }
func (e exitStatus) ExitCode() int   { return int(e) }
