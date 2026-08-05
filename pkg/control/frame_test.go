package control_test

import (
	"bytes"
	"testing"

	"github.com/lucasew/the-go-os/pkg/control"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("hello control")
	if err := control.WriteFrame(&buf, payload); err != nil {
		t.Fatal(err)
	}
	got, err := control.ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
}
