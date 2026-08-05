// Package coreutils registers basic utility applets.
package coreutils

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lucasew/the-go-os/pkg/registry"
)

func init() {
	registry.Register(registry.Applet{
		Name:  "true",
		Short: "do nothing, successfully",
		Run:   runTrue,
	})
	registry.Register(registry.Applet{
		Name:  "false",
		Short: "do nothing, unsuccessfully",
		Run:   runFalse,
	})
	registry.Register(registry.Applet{
		Name:  "echo",
		Short: "display a line of text",
		Run:   runEcho,
	})
	registry.Register(registry.Applet{
		Name:  "cat",
		Short: "concatenate files to stdout",
		Run:   runCat,
	})
	registry.Register(registry.Applet{
		Name:  "ls",
		Short: "list directory contents",
		Run:   runLS,
	})
	registry.Register(registry.Applet{
		Name:  "pwd",
		Short: "print working directory",
		Run:   runPwd,
	})
	registry.Register(registry.Applet{
		Name:  "env",
		Short: "print environment",
		Run:   runEnv,
	})
	registry.Register(registry.Applet{
		Name:  "sleep",
		Short: "delay for a specified amount of time",
		Run:   runSleep,
	})
}

func runTrue(context.Context, []string) error { return nil }

type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit %d", int(e)) }
func (e exitCode) ExitCode() int { return int(e) }

func runFalse(context.Context, []string) error { return exitCode(1) }

func runEcho(_ context.Context, args []string) error {
	fmt.Println(strings.Join(args[1:], " "))
	return nil
}

func runCat(_ context.Context, args []string) error {
	if len(args) < 2 {
		_, err := io.Copy(os.Stdout, os.Stdin)
		return err
	}
	for _, path := range args[1:] {
		if path == "-" {
			if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
				return err
			}
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(os.Stdout, f)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func runLS(_ context.Context, args []string) error {
	dir := "."
	if len(args) > 1 {
		dir = args[1]
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		fmt.Println(name)
	}
	return nil
}

func runPwd(context.Context, []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	fmt.Println(filepath.Clean(wd))
	return nil
}

func runEnv(context.Context, []string) error {
	for _, e := range os.Environ() {
		fmt.Println(e)
	}
	return nil
}

func runSleep(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: sleep SECONDS")
	}
	sec, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		return fmt.Errorf("sleep: invalid duration %q", args[1])
	}
	d := time.Duration(sec * float64(time.Second))
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
