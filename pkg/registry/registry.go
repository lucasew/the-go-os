// Package registry is the multicall / Cobra command table.
// Applets register themselves via init(); profiles blank-import applet packages.
package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/spf13/cobra"
)

// Handler runs an applet. args[0] is the applet name (like argv0 base).
type Handler func(ctx context.Context, args []string) error

// Applet is one multicall personality.
type Applet struct {
	// Name is the command / symlink name (e.g. "ls", "systemctl", "init").
	Name string
	// Short is one-line help for Cobra.
	Short string
	// Run is the shared implementation for multicall and Cobra.
	Run Handler
	// NoSymlink skips image symlink generation (rare).
	NoSymlink bool
	// Hidden hides the command from Cobra help.
	Hidden bool
}

var (
	mu      sync.RWMutex
	applets = map[string]Applet{}
)

// Register adds an applet. Panics on duplicate name (fail fast at init).
func Register(a Applet) {
	if a.Name == "" {
		panic("registry: empty applet name")
	}
	if a.Run == nil {
		panic("registry: nil Run for " + a.Name)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := applets[a.Name]; ok {
		panic("registry: duplicate applet " + a.Name)
	}
	applets[a.Name] = a
}

// Lookup returns an applet by name.
func Lookup(name string) (Applet, bool) {
	mu.RLock()
	defer mu.RUnlock()
	a, ok := applets[name]
	return a, ok
}

// Names returns sorted applet names.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(applets))
	for n := range applets {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Symlinks returns names that should be installed as symlinks to the main binary.
func Symlinks() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(applets))
	for n, a := range applets {
		if a.NoSymlink {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// PrimaryName is the canonical binary name used to decide multicall vs Cobra root.
const PrimaryName = "the-go-os"

// IsPrimary reports whether basename is the main binary (not an applet symlink).
func IsPrimary(base string) bool {
	switch base {
	case PrimaryName, PrimaryName + ".exe":
		return true
	default:
		return false
	}
}

// Dispatch runs the applet named name with args (args[0] should be name).
func Dispatch(ctx context.Context, name string, args []string) error {
	a, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("unknown applet %q", name)
	}
	if len(args) == 0 {
		args = []string{name}
	} else if args[0] != name {
		args = append([]string{name}, args...)
	}
	return a.Run(ctx, args)
}

// RootCommand builds the Cobra tree from registered applets.
func RootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           PrimaryName,
		Short:         "Go multicall userspace (init, tools, systemctl)",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.AddCommand(&cobra.Command{
		Use:   "list-applets",
		Short: "List registered multicall applets",
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, n := range Names() {
				a, _ := Lookup(n)
				fmt.Fprintln(cmd.OutOrStdout(), n+"\t"+a.Short)
			}
			return nil
		},
	})
	root.AddCommand(&cobra.Command{
		Use:   "list-symlinks",
		Short: "Print symlink names for image builds",
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, n := range Symlinks() {
				fmt.Fprintln(cmd.OutOrStdout(), n)
			}
			return nil
		},
	})

	mu.RLock()
	names := make([]string, 0, len(applets))
	for n := range applets {
		names = append(names, n)
	}
	mu.RUnlock()
	sort.Strings(names)

	for _, n := range names {
		a, _ := Lookup(n)
		name := a.Name
		short := a.Short
		run := a.Run
		c := &cobra.Command{
			Use:    name,
			Short:  short,
			Hidden: a.Hidden,
			// Disable cobra's flag parsing for applets that use flag package / raw args.
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				full := append([]string{name}, args...)
				return run(cmd.Context(), full)
			},
		}
		root.AddCommand(c)
	}
	return root
}

// MainEntry is the process entry after imports have registered applets.
// If argv0 base is a registered applet, multicall; otherwise Cobra.
func MainEntry(ctx context.Context, argv0 string, args []string) error {
	base := filepath.Base(argv0)
	// Strip common path noise
	if base == "." || base == string(filepath.Separator) {
		base = PrimaryName
	}

	if !IsPrimary(base) {
		if _, ok := Lookup(base); ok {
			// Multicall: full argv as the child would see (argv0 base + rest).
			full := append([]string{base}, args...)
			return Dispatch(ctx, base, full)
		}
		// Unknown symlink name — fall through to cobra with a note? Prefer error.
		return fmt.Errorf("unknown multicall name %q (not a registered applet)", base)
	}

	root := RootCommand()
	root.SetArgs(args)
	return root.ExecuteContext(ctx)
}

// ExitCode maps errors to process exit codes.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if code, ok := err.(interface{ ExitCode() int }); ok {
		return code.ExitCode()
	}
	return 1
}

// Die prints err to stderr and exits. Used by main only.
func Die(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(ExitCode(err))
}
