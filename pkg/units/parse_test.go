package units_test

import (
	"strings"
	"testing"

	"github.com/lucasew/the-go-os/pkg/units"
)

func TestParseServiceAllowlist(t *testing.T) {
	body := `
[Unit]
Description=Hello
After=network.target

[Service]
Type=simple
ExecStart=/bin/echo hi
Restart=on-failure

[Install]
WantedBy=default.target
`
	u, err := units.Parse("hello.service", "test", units.KindService, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if u.Description != "Hello" || u.ExecStart != "/bin/echo hi" {
		t.Fatalf("%+v", u)
	}
	if len(u.After) != 1 || u.After[0] != "network.target" {
		t.Fatalf("After=%v", u.After)
	}
}

func TestParseRejectsUnknownKey(t *testing.T) {
	body := `
[Service]
Type=simple
ExecStart=/bin/true
PrivateTmp=yes
`
	_, err := units.Parse("x.service", "test", units.KindService, strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for PrivateTmp")
	}
}
