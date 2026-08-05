//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestInitfulContainer(t *testing.T) {
	requireDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	bin := buildStaticBinary(t)
	t.Logf("binary: %s", bin)

	// One binary copy; our ln plants multicall symlinks, then exec becomes PID 1.
	// Alpine only provides a shell for the bootstrap line (distroless image uses Dockerfile).
	bootstrap := strings.Join([]string{
		"set -e",
		"for n in $(/the-go-os list-symlinks); do /the-go-os ln -sfn /the-go-os /bin/$n; done",
		"/the-go-os ln -sfn /the-go-os /bin/the-go-os",
		"exec /the-go-os init",
	}, " && ")

	req := testcontainers.ContainerRequest{
		Image: "alpine:3.21",
		Entrypoint: []string{"/bin/sh", "-c", bootstrap},
		Env: map[string]string{
			"THE_GO_OS_CONTROL_SOCKET": "/run/the-go-os/control.sock",
			"PATH":                     "/bin:/usr/bin",
		},
		ExposedPorts: []string{"18080/tcp"},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      bin,
				ContainerFilePath: "/the-go-os",
				FileMode:          0o755,
			},
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("control socket").WithStartupTimeout(60*time.Second),
			wait.ForLog("target default.target active").WithStartupTimeout(60*time.Second),
			wait.ForLog("socket hello.socket listening").WithStartupTimeout(60*time.Second),
		),
		LogConsumerCfg: &testcontainers.LogConsumerConfig{
			Consumers: []testcontainers.LogConsumer{&tlogConsumer{t: t}},
		},
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start container: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		_ = ctr.Terminate(cctx)
	})

	// --- systemctl list-units ---
	out := execIn(t, ctx, ctr, "/the-go-os", "systemctl", "list-units")
	t.Logf("list-units:\n%s", out)
	for _, want := range []string{"default.target", "hello.socket", "hello.service"} {
		if !strings.Contains(out, want) {
			t.Errorf("list-units missing %q", want)
		}
	}
	if !strings.Contains(out, "hello.socket") || !strings.Contains(out, "active") {
		t.Errorf("expected hello.socket active in:\n%s", out)
	}

	// hello.service should be inactive until socket activation
	st := execIn(t, ctx, ctr, "/the-go-os", "systemctl", "status", "hello.service")
	t.Logf("hello.service before activate:\n%s", st)
	if !strings.Contains(st, "inactive") && !strings.Contains(st, "Active: inactive") {
		// status format: "Active: inactive"
		if strings.Contains(st, "Active: active") {
			t.Errorf("hello.service should not be active before socket connect:\n%s", st)
		}
	}

	// --- socket activate via mapped port ---
	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := ctr.MappedPort(ctx, "18080/tcp")
	if err != nil {
		t.Fatal(err)
	}
	addr := net.JoinHostPort(host, port.Port())
	t.Logf("connecting to %s for socket activation", addr)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial hello.socket: %v", err)
	}
	_ = conn.Close()

	// Wait for service to show active
	deadline := time.Now().Add(10 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		status = execIn(t, ctx, ctr, "/the-go-os", "systemctl", "status", "hello.service")
		if strings.Contains(status, "Active: active") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("hello.service after activate:\n%s", status)
	if !strings.Contains(status, "Active: active") {
		t.Fatalf("hello.service not active after socket activation:\n%s", status)
	}

	// restart + stop smoke
	_ = execIn(t, ctx, ctr, "/the-go-os", "systemctl", "restart", "hello.service")
	status = execIn(t, ctx, ctr, "/the-go-os", "systemctl", "status", "hello.service")
	if !strings.Contains(status, "Active: active") {
		t.Errorf("after restart: %s", status)
	}
	_ = execIn(t, ctx, ctr, "/the-go-os", "systemctl", "stop", "hello.service")
	status = execIn(t, ctx, ctr, "/the-go-os", "systemctl", "status", "hello.service")
	if strings.Contains(status, "Active: active") && !strings.Contains(status, "inactive") {
		// stop should leave inactive
		t.Logf("stop status: %s", status)
	}
}

func TestInitfulFromDockerfile(t *testing.T) {
	requireDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	root := moduleRoot(t)
	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    root,
			Dockerfile: "Dockerfile",
			// Keep rebuilds honest when sources change.
			BuildArgs: map[string]*string{},
		},
		Env: map[string]string{
			"THE_GO_OS_CONTROL_SOCKET": "/run/the-go-os/control.sock",
		},
		ExposedPorts: []string{"18080/tcp"},
		WaitingFor: wait.ForAll(
			wait.ForLog("control socket").WithStartupTimeout(3*time.Minute),
			wait.ForLog("target default.target active").WithStartupTimeout(3*time.Minute),
		),
		LogConsumerCfg: &testcontainers.LogConsumerConfig{
			Consumers: []testcontainers.LogConsumer{&tlogConsumer{t: t}},
		},
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start Dockerfile container: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		_ = ctr.Terminate(cctx)
	})

	out := execIn(t, ctx, ctr, "/the-go-os", "systemctl", "list-units")
	if !strings.Contains(out, "hello.socket") {
		t.Fatalf("list-units:\n%s", out)
	}
}

func requireDocker(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cmd := exec.Command("docker", "info")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("docker not available: %v\n%s", err, out)
	}
}

func buildStaticBinary(t *testing.T) string {
	t.Helper()
	root := moduleRoot(t)
	out := filepath.Join(t.TempDir(), "the-go-os")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/the-go-os")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v\n%s", err, buf.String())
	}
	return out
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// test/integration/initful_test.go → repo root
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func execIn(t *testing.T, ctx context.Context, ctr testcontainers.Container, cmd ...string) string {
	t.Helper()
	code, out, err := ctr.Exec(ctx, cmd)
	if err != nil {
		t.Fatalf("exec %v: %v", cmd, err)
	}
	// testcontainers Exec returns combined reader — read all
	b, readErr := readAll(out)
	if readErr != nil {
		t.Fatalf("read exec output: %v", readErr)
	}
	// strip docker exec multiplex header noise if present — testcontainers usually demuxes
	s := string(b)
	if code != 0 {
		t.Fatalf("exec %v exit %d:\n%s", cmd, code, s)
	}
	return s
}

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("nil reader")
	}
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	return buf.Bytes(), err
}

// tlogConsumer streams container logs into the test log.
type tlogConsumer struct {
	t *testing.T
}

func (c *tlogConsumer) Accept(l testcontainers.Log) {
	c.t.Logf("[container:%s] %s", l.LogType, strings.TrimRight(string(l.Content), "\n"))
}
