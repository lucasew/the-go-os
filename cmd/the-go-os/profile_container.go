package main

// Container profile: blank-import applet roles so they register on init.
import (
	_ "github.com/lucasew/the-go-os/pkg/applets/coreutils"
	_ "github.com/lucasew/the-go-os/pkg/applets/shell"
	_ "github.com/lucasew/the-go-os/pkg/applets/systemd"
)
