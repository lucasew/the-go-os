package coreutils_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/lucasew/the-go-os/pkg/applets/coreutils"
	"github.com/lucasew/the-go-os/pkg/registry"
)

func TestLnSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.Dispatch(context.Background(), "ln", []string{"ln", "-s", target, link}); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Fatalf("readlink=%q want %q", got, target)
	}
}

func TestLnForceSymlink(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(a, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.Dispatch(context.Background(), "ln", []string{"ln", "-s", a, link}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Dispatch(context.Background(), "ln", []string{"ln", "-s", b, link}); err == nil {
		t.Fatal("expected error without -f")
	}
	if err := registry.Dispatch(context.Background(), "ln", []string{"ln", "-sf", b, link}); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if got != b {
		t.Fatalf("readlink=%q want %q", got, b)
	}
}
