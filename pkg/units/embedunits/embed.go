// Package embedunits holds factory-default unit files.
package embedunits

import "embed"

// FS is the embedded unit tree (*.service, *.socket, *.target at root of this FS).
//
//go:embed *.service *.socket *.target
var FS embed.FS
