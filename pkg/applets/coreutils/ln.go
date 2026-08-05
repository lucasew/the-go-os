package coreutils

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lucasew/the-go-os/pkg/registry"
	"github.com/lucasew/the-go-os/pkg/stdio"
)

func init() {
	registry.Register(registry.Applet{
		Name:  "ln",
		Short: "make links between files",
		Run:   runLn,
	})
}

// runLn implements a busybox-ish ln: ln [-sfv] TARGET LINK_NAME | ln [-sfv] TARGET... DIR
func runLn(ctx context.Context, args []string) error {
	out := stdio.From(ctx).Out
	errOut := stdio.From(ctx).Err

	symlink, force, verbose := false, false, false
	operands := make([]string, 0, len(args))
	for _, a := range args[1:] {
		if a == "--" {
			// rest are operands; we don't track remainder specially beyond append
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && !strings.HasPrefix(a, "--") {
			for _, c := range a[1:] {
				switch c {
				case 's':
					symlink = true
				case 'f':
					force = true
				case 'v':
					verbose = true
				case 'n':
					// ignore: "no-dereference" for link name that is symlink-to-dir
				default:
					return fmt.Errorf("ln: unknown option -%c", c)
				}
			}
			continue
		}
		if a == "--help" {
			fmt.Fprintln(out, "usage: ln [-sfv] TARGET LINK_NAME\n       ln [-sfv] TARGET... DIRECTORY")
			return nil
		}
		operands = append(operands, a)
	}

	if len(operands) < 2 {
		return fmt.Errorf("usage: ln [-sfv] TARGET LINK_NAME")
	}

	// Many targets → last is directory
	if len(operands) > 2 {
		dir := operands[len(operands)-1]
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			return fmt.Errorf("ln: target %q is not a directory", dir)
		}
		for _, target := range operands[:len(operands)-1] {
			link := filepath.Join(dir, filepath.Base(target))
			if err := doLink(target, link, symlink, force, verbose, out); err != nil {
				fmt.Fprintln(errOut, err)
				return exitCode(1)
			}
		}
		return nil
	}

	target, link := operands[0], operands[1]
	if err := doLink(target, link, symlink, force, verbose, out); err != nil {
		return err
	}
	return nil
}

func doLink(target, link string, symlink, force, verbose bool, out io.Writer) error {
	if force {
		_ = os.Remove(link)
	} else if _, err := os.Lstat(link); err == nil {
		return fmt.Errorf("ln: failed to create %q: file exists", link)
	}

	var err error
	if symlink {
		err = os.Symlink(target, link)
	} else {
		err = os.Link(target, link)
	}
	if err != nil {
		return fmt.Errorf("ln: %w", err)
	}
	if verbose {
		kind := ""
		if symlink {
			kind = "symbolic "
		}
		fmt.Fprintf(out, "%s -> %s (%slink)\n", link, target, kind)
	}
	return nil
}
