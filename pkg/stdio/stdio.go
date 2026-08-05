// Package stdio provides context-scoped stdin/stdout/stderr for applets.
// Shell builtins use this so redirects apply; multicall defaults to os.Std*.
package stdio

import (
	"context"
	"io"
	"os"
)

type key struct{}

// Streams is the process stdio triple for an applet invocation.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// With attaches streams to ctx.
func With(ctx context.Context, s Streams) context.Context {
	return context.WithValue(ctx, key{}, s)
}

// From returns streams from ctx, or os.Stdin/Stdout/Stderr.
func From(ctx context.Context) Streams {
	if s, ok := ctx.Value(key{}).(Streams); ok {
		if s.In == nil {
			s.In = os.Stdin
		}
		if s.Out == nil {
			s.Out = os.Stdout
		}
		if s.Err == nil {
			s.Err = os.Stderr
		}
		return s
	}
	return Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
}
