package control

import "os"

// DefaultSocketPath is the control plane UDS path (SPEC).
const DefaultSocketPath = "/run/the-go-os/control.sock"

// SocketPath returns THE_GO_OS_CONTROL_SOCKET or the default.
func SocketPath() string {
	if p := os.Getenv("THE_GO_OS_CONTROL_SOCKET"); p != "" {
		return p
	}
	return DefaultSocketPath
}
