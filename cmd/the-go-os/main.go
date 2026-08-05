package main

import (
	"context"
	"os"

	"github.com/lucasew/the-go-os/pkg/registry"
)

func main() {
	ctx := context.Background()
	argv0 := os.Args[0]
	args := os.Args[1:]
	if err := registry.MainEntry(ctx, argv0, args); err != nil {
		registry.Die(err)
	}
}
