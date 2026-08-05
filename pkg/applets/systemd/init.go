package systemd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/lucasew/the-go-os/pkg/logemit"
	"github.com/lucasew/the-go-os/pkg/registry"
	"github.com/lucasew/the-go-os/pkg/supervisor"
	"github.com/lucasew/the-go-os/pkg/units"
	"github.com/lucasew/the-go-os/pkg/units/embedunits"
)

func init() {
	registry.Register(registry.Applet{
		Name:  "init",
		Short: "PID 1 supervisor (unit engine + control socket)",
		Run:   runInit,
	})
}

func runInit(ctx context.Context, args []string) error {
	log := logemit.New(os.Stderr)
	log.System("the-go-os init starting")

	cfg := supervisor.Config{
		Embed:         embedunits.FS,
		EmbedRoot:     ".",
		FileDirs:      nil, // embed only unless THE_GO_OS_UNIT_PATH / LOAD_SYSTEM_UNITS
		Log:           log,
		DefaultTarget: "default.target",
	}
	// Optional filesystem units (container: COPY into /etc/systemd/system and set LOAD_SYSTEM_UNITS=1,
	// or point THE_GO_OS_UNIT_PATH at a directory of units).
	if d := os.Getenv("THE_GO_OS_UNIT_PATH"); d != "" {
		cfg.FileDirs = append(cfg.FileDirs, d)
	}
	if os.Getenv("THE_GO_OS_LOAD_SYSTEM_UNITS") == "1" {
		cfg.FileDirs = append(cfg.FileDirs, units.DefaultFilesystemDir)
	}
	if p := os.Getenv("THE_GO_OS_CONTROL_SOCKET"); p != "" {
		cfg.ControlSocket = p
	}

	sup := supervisor.New(cfg)

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()

	// Also handle SIGCHLD by relying on supervisor reap ticker + Wait in service goroutines.
	err := sup.Run(ctx)
	if err != nil && err != context.Canceled {
		return fmt.Errorf("init: %w", err)
	}
	log.System("init exiting")
	return nil
}
