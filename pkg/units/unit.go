// Package units loads and represents a v0 subset of systemd unit files.
package units

import (
	"fmt"
	"strings"
)

// Kind is the unit type.
type Kind string

const (
	KindService Kind = "service"
	KindSocket  Kind = "socket"
	KindTarget  Kind = "target"
)

// Unit is a parsed unit (allowlisted keys only).
type Unit struct {
	Name        string
	Kind        Kind
	Path        string // source path or "embed:..."
	Description string

	// [Unit]
	After    []string
	Before   []string
	Requires []string
	Wants    []string
	Conflicts []string
	BindsTo  []string
	PartOf   []string

	// [Install]
	WantedBy []string

	// [Service]
	Type            string // simple | oneshot
	ExecStart       string
	ExecStartPre    []string
	ExecStop        string
	Restart         string // no | on-failure | always
	RestartSec      string
	Environment     []string
	EnvironmentFile []string
	WorkingDirectory string
	SyslogIdentifier string

	// [Socket]
	ListenStream   []string
	ListenDatagram []string
	Accept         string // no
	Service        string // bound service name
}

// NormalizeName ensures name has the correct suffix for kind when possible.
func NormalizeName(name string, kind Kind) string {
	if name == "" {
		return name
	}
	if strings.Contains(name, ".") {
		return name
	}
	return name + "." + string(kind)
}

// Validate checks v0 constraints.
func (u *Unit) Validate() error {
	if u.Name == "" {
		return fmt.Errorf("unit missing name")
	}
	switch u.Kind {
	case KindService, KindSocket, KindTarget:
	default:
		return fmt.Errorf("%s: unsupported kind %q", u.Name, u.Kind)
	}
	if u.Kind == KindService {
		switch u.Type {
		case "", "simple", "oneshot":
		default:
			return fmt.Errorf("%s: unsupported Type=%q", u.Name, u.Type)
		}
		if u.Type == "" {
			u.Type = "simple"
		}
		switch u.Restart {
		case "", "no", "on-failure", "always":
		default:
			return fmt.Errorf("%s: unsupported Restart=%q", u.Name, u.Restart)
		}
		if u.Restart == "" {
			u.Restart = "no"
		}
	}
	if u.Kind == KindSocket {
		if u.Accept != "" && u.Accept != "no" {
			return fmt.Errorf("%s: only Accept=no supported in v0", u.Name)
		}
		u.Accept = "no"
	}
	return nil
}
