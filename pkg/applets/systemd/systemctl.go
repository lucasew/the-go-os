package systemd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/lucasew/the-go-os/pkg/control"
	controlv1 "github.com/lucasew/the-go-os/pkg/control/v1"
	"github.com/lucasew/the-go-os/pkg/registry"
)

func init() {
	registry.Register(registry.Applet{
		Name:  "systemctl",
		Short: "control the system and service manager",
		Run:   runSystemctl,
	})
}

func runSystemctl(ctx context.Context, args []string) error {
	_ = ctx
	if len(args) < 2 {
		return fmt.Errorf("usage: systemctl <status|start|stop|restart|reload|list-units|daemon-reload> [unit]")
	}
	cmd := args[1]
	conn, err := control.Dial()
	if err != nil {
		return fmt.Errorf("dial control socket: %w (is init running?)", err)
	}
	defer conn.Close()

	switch cmd {
	case "daemon-reload", "reload":
		// reload alone often means daemon-reload for unit files in our lite CLI
		if cmd == "reload" && len(args) > 2 {
			// per-unit reload not in v0
			return fmt.Errorf("per-unit reload not supported in v0; use daemon-reload")
		}
		resp, err := control.Call(conn, &controlv1.Request{
			Op: &controlv1.Request_DaemonReload{DaemonReload: &controlv1.DaemonReloadRequest{}},
		})
		return rpcResult(resp, err)

	case "list-units", "list":
		resp, err := control.Call(conn, &controlv1.Request{
			Op: &controlv1.Request_ListUnits{ListUnits: &controlv1.ListUnitsRequest{}},
		})
		if err != nil {
			return err
		}
		if resp.Error != "" {
			return fmt.Errorf("%s", resp.Error)
		}
		list := resp.GetListUnits()
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "UNIT\tKIND\tSTATE\tPID\tDESCRIPTION")
		if list != nil {
			for _, u := range list.Units {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", u.Name, u.Kind, u.State, u.Pid, u.Description)
			}
		}
		return tw.Flush()

	case "status":
		if len(args) < 3 {
			return fmt.Errorf("usage: systemctl status UNIT")
		}
		resp, err := control.Call(conn, &controlv1.Request{
			Op: &controlv1.Request_StatusUnit{StatusUnit: &controlv1.StatusUnitRequest{Name: args[2]}},
		})
		if err != nil {
			return err
		}
		if resp.Error != "" {
			return fmt.Errorf("%s", resp.Error)
		}
		u := resp.GetStatusUnit().GetUnit()
		if u == nil {
			return fmt.Errorf("empty status")
		}
		fmt.Printf("● %s - %s\n", u.Name, u.Description)
		fmt.Printf("     Loaded: loaded\n")
		fmt.Printf("     Active: %s\n", u.State)
		fmt.Printf("       Kind: %s\n", u.Kind)
		if u.Pid > 0 {
			fmt.Printf("        PID: %d\n", u.Pid)
		}
		return nil

	case "start":
		if len(args) < 3 {
			return fmt.Errorf("usage: systemctl start UNIT")
		}
		resp, err := control.Call(conn, &controlv1.Request{
			Op: &controlv1.Request_StartUnit{StartUnit: &controlv1.StartUnitRequest{Name: args[2]}},
		})
		return rpcResult(resp, err)

	case "stop":
		if len(args) < 3 {
			return fmt.Errorf("usage: systemctl stop UNIT")
		}
		resp, err := control.Call(conn, &controlv1.Request{
			Op: &controlv1.Request_StopUnit{StopUnit: &controlv1.StopUnitRequest{Name: args[2]}},
		})
		return rpcResult(resp, err)

	case "restart":
		if len(args) < 3 {
			return fmt.Errorf("usage: systemctl restart UNIT")
		}
		resp, err := control.Call(conn, &controlv1.Request{
			Op: &controlv1.Request_RestartUnit{RestartUnit: &controlv1.RestartUnitRequest{Name: args[2]}},
		})
		return rpcResult(resp, err)

	default:
		return fmt.Errorf("unknown systemctl command %q", cmd)
	}
}

func rpcResult(resp *controlv1.Response, err error) error {
	if err != nil {
		return err
	}
	if resp != nil && resp.Error != "" {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}
