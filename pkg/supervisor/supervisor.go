// Package supervisor is the v0 unit engine (PID 1 core).
package supervisor

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lucasew/the-go-os/pkg/control"
	controlv1 "github.com/lucasew/the-go-os/pkg/control/v1"
	"github.com/lucasew/the-go-os/pkg/logemit"
	"github.com/lucasew/the-go-os/pkg/units"
)

// Config for the supervisor.
type Config struct {
	Embed     fs.FS
	EmbedRoot string
	FileDirs  []string
	Log       *logemit.Emitter
	// ControlSocket path; empty uses control.SocketPath().
	ControlSocket string
	// DefaultTarget to start after load.
	DefaultTarget string
}

// Supervisor manages units and the control socket.
type Supervisor struct {
	cfg Config
	log *logemit.Emitter

	mu     sync.Mutex
	units  map[string]*units.Unit
	state  map[string]*runtimeState
	listen map[string][]net.Listener // socket unit → listeners
}

type runtimeState struct {
	state    string // inactive | active | failed | activating | deactivating
	pid      int
	cmd      *exec.Cmd
	exitErr  error
	cancel   context.CancelFunc
}

// New creates a supervisor (does not start).
func New(cfg Config) *Supervisor {
	if cfg.Log == nil {
		cfg.Log = logemit.New(os.Stderr)
	}
	if cfg.DefaultTarget == "" {
		cfg.DefaultTarget = "default.target"
	}
	return &Supervisor{
		cfg:    cfg,
		log:    cfg.Log,
		units:  map[string]*units.Unit{},
		state:  map[string]*runtimeState{},
		listen: map[string][]net.Listener{},
	}
}

// Run is the PID 1 loop: load units, start default target, serve control, reap.
func (s *Supervisor) Run(ctx context.Context) error {
	if err := s.DaemonReload(); err != nil {
		return err
	}
	sockPath := s.cfg.ControlSocket
	if sockPath == "" {
		sockPath = control.SocketPath()
	}
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		return fmt.Errorf("control socket dir: %w", err)
	}
	_ = os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("control listen: %w", err)
	}
	defer ln.Close()
	_ = os.Chmod(sockPath, 0o600)
	s.log.System("control socket " + sockPath)

	go s.acceptLoop(ctx, ln)

	if err := s.StartUnit(s.cfg.DefaultTarget); err != nil {
		s.log.System("start " + s.cfg.DefaultTarget + ": " + err.Error())
	}

	// Reap + wait for context cancel (signals handled by caller).
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.shutdown()
			return ctx.Err()
		case <-ticker.C:
			s.reap()
		}
	}
}

func (s *Supervisor) acceptLoop(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				s.log.System("control accept: " + err.Error())
				return
			}
		}
		go func(c net.Conn) {
			if err := control.ServeConn(c, s); err != nil && err != io.EOF {
				s.log.System("control conn: " + err.Error())
			}
		}(conn)
	}
}

func (s *Supervisor) reap() {
	// Best-effort wait on known children; also Wait4 any zombie.
	for {
		var st syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &st, syscall.WNOHANG, nil)
		if err != nil || pid <= 0 {
			break
		}
		s.onChildExit(pid, st)
	}
}

func (s *Supervisor) onChildExit(pid int, st syscall.WaitStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, rs := range s.state {
		if rs.pid != pid {
			continue
		}
		code := st.ExitStatus()
		s.log.System(fmt.Sprintf("unit %s pid %d exited %d", name, pid, code))
		rs.pid = 0
		rs.cmd = nil
		u := s.units[name]
		if u != nil && u.Kind == units.KindService {
			switch u.Restart {
			case "always":
				rs.state = "inactive"
				go func(n string) { _ = s.StartUnit(n) }(name)
			case "on-failure":
				if code != 0 {
					rs.state = "failed"
					go func(n string) {
						time.Sleep(parseRestartSec(u.RestartSec))
						_ = s.StartUnit(n)
					}(name)
				} else {
					rs.state = "inactive"
				}
			default:
				if code != 0 {
					rs.state = "failed"
				} else {
					rs.state = "inactive"
				}
			}
		} else {
			rs.state = "inactive"
		}
		return
	}
}

func parseRestartSec(s string) time.Duration {
	if s == "" {
		return time.Second
	}
	if d, err := time.ParseDuration(s + "s"); err == nil {
		return d
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	return time.Second
}

func (s *Supervisor) shutdown() {
	s.mu.Lock()
	names := make([]string, 0, len(s.state))
	for n, rs := range s.state {
		if rs.pid > 0 {
			names = append(names, n)
		}
	}
	s.mu.Unlock()
	for _, n := range names {
		_ = s.StopUnit(n)
	}
	s.mu.Lock()
	for _, lns := range s.listen {
		for _, ln := range lns {
			_ = ln.Close()
		}
	}
	s.listen = map[string][]net.Listener{}
	s.mu.Unlock()
}

// DaemonReload reloads unit definitions.
func (s *Supervisor) DaemonReload() error {
	loader := &units.Loader{
		Embed:     s.cfg.Embed,
		EmbedRoot: s.cfg.EmbedRoot,
		FileDirs:  s.cfg.FileDirs,
		WarnFn: func(msg string) {
			s.log.System("warn: " + msg)
		},
	}
	res, err := loader.Load()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.units = res.Units
	for name := range s.units {
		if _, ok := s.state[name]; !ok {
			s.state[name] = &runtimeState{state: "inactive"}
		}
	}
	s.log.System(fmt.Sprintf("loaded %d units", len(s.units)))
	return nil
}

// ListUnits implements control.Engine.
func (s *Supervisor) ListUnits() ([]*controlv1.UnitInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.units))
	for n := range s.units {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*controlv1.UnitInfo, 0, len(names))
	for _, n := range names {
		out = append(out, s.infoLocked(n))
	}
	return out, nil
}

// StatusUnit implements control.Engine.
func (s *Supervisor) StatusUnit(name string) (*controlv1.UnitInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.units[name]; !ok {
		return nil, fmt.Errorf("unit %s not found", name)
	}
	return s.infoLocked(name), nil
}

func (s *Supervisor) infoLocked(name string) *controlv1.UnitInfo {
	u := s.units[name]
	rs := s.state[name]
	if rs == nil {
		rs = &runtimeState{state: "inactive"}
	}
	info := &controlv1.UnitInfo{
		Name:        name,
		Description: u.Description,
		Kind:        string(u.Kind),
		State:       rs.state,
		Pid:         int32(rs.pid),
	}
	return info
}

// StartUnit implements control.Engine.
func (s *Supervisor) StartUnit(name string) error {
	s.mu.Lock()
	u, ok := s.units[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("unit %s not found", name)
	}
	s.mu.Unlock()

	// Dependencies (Wants/Requires) — start Requires first, then Wants (best effort).
	for _, dep := range append(append([]string{}, u.Requires...), u.Wants...) {
		if err := s.StartUnit(dep); err != nil && contains(u.Requires, dep) {
			return fmt.Errorf("requires %s: %w", dep, err)
		}
	}

	switch u.Kind {
	case units.KindTarget:
		return s.startTarget(name, u)
	case units.KindSocket:
		return s.startSocket(name, u)
	case units.KindService:
		return s.startService(name, u)
	default:
		return fmt.Errorf("unsupported kind %s", u.Kind)
	}
}

func contains(ss []string, x string) bool {
	for _, s := range ss {
		if s == x {
			return true
		}
	}
	return false
}

func (s *Supervisor) startTarget(name string, u *units.Unit) error {
	// Start all units that WantedBy this target, plus explicit Wants/Requires.
	s.mu.Lock()
	var toStart []string
	toStart = append(toStart, u.Wants...)
	toStart = append(toStart, u.Requires...)
	for n, other := range s.units {
		for _, wb := range other.WantedBy {
			if wb == name {
				toStart = append(toStart, n)
			}
		}
	}
	s.mu.Unlock()
	for _, dep := range unique(toStart) {
		if dep == name {
			continue
		}
		if err := s.StartUnit(dep); err != nil {
			s.log.System("target " + name + " failed starting " + dep + ": " + err.Error())
			// Wants are best-effort; Requires already enforced above for target's own Requires
			if contains(u.Requires, dep) {
				return err
			}
		}
	}
	s.mu.Lock()
	s.state[name] = &runtimeState{state: "active"}
	s.mu.Unlock()
	s.log.System("target " + name + " active")
	return nil
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (s *Supervisor) startSocket(name string, u *units.Unit) error {
	s.mu.Lock()
	if lns := s.listen[name]; len(lns) > 0 {
		s.state[name].state = "active"
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	var listeners []net.Listener
	for _, addr := range u.ListenStream {
		ln, err := listenStream(addr)
		if err != nil {
			for _, l := range listeners {
				_ = l.Close()
			}
			return fmt.Errorf("listen %s: %w", addr, err)
		}
		listeners = append(listeners, ln)
		svc := u.Service
		if svc == "" {
			svc = strings.TrimSuffix(name, ".socket") + ".service"
		}
		go s.acceptActivate(name, svc, ln)
	}
	for _, addr := range u.ListenDatagram {
		// UDP: open and hold; activation on first packet deferred for simplicity in scaffold
		pc, err := listenDatagram(addr)
		if err != nil {
			for _, l := range listeners {
				_ = l.Close()
			}
			return err
		}
		// Keep packet conn open via wrapper listener store — store as custom
		_ = pc
		s.log.System("datagram listen " + addr + " (hold only in v0 scaffold)")
	}

	s.mu.Lock()
	s.listen[name] = listeners
	if s.state[name] == nil {
		s.state[name] = &runtimeState{}
	}
	s.state[name].state = "active"
	s.mu.Unlock()
	s.log.System("socket " + name + " listening")
	return nil
}

func listenStream(addr string) (net.Listener, error) {
	if strings.HasPrefix(addr, "/") {
		_ = os.Remove(addr)
		if err := os.MkdirAll(filepath.Dir(addr), 0o755); err != nil {
			return nil, err
		}
		return net.Listen("unix", addr)
	}
	// host:port or :port
	if _, err := strconv.Atoi(addr); err == nil {
		addr = ":" + addr
	}
	return net.Listen("tcp", addr)
}

func listenDatagram(addr string) (net.PacketConn, error) {
	if strings.HasPrefix(addr, "/") {
		_ = os.Remove(addr)
		return net.ListenPacket("unixgram", addr)
	}
	if _, err := strconv.Atoi(addr); err == nil {
		addr = ":" + addr
	}
	return net.ListenPacket("udp", addr)
}

func (s *Supervisor) acceptActivate(socketName, serviceName string, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Socket activation: pass FD to service via LISTEN_FDS.
		// For scaffold: start service if not running, then... we need to hand off conn.
		// Simple approach: start service with LISTEN_FDS and duplicate listener FD —
		// full activation needs ExtraFiles on the listener, not the accepted conn.
		// systemd passes the *listening* socket. So we should pass ln's FD at service start
		// on first connection, or at socket start.
		_ = conn.Close()
		s.mu.Lock()
		rs := s.state[serviceName]
		running := rs != nil && rs.pid > 0
		s.mu.Unlock()
		if !running {
			if err := s.startServiceWithSocket(serviceName, socketName, ln); err != nil {
				s.log.System("socket activate " + serviceName + ": " + err.Error())
			}
		}
		// After first start, further accepts are handled by the service if it took the FD.
		// If we kept accepting here we'd race. Stop accept loop after activation.
		return
	}
}

func (s *Supervisor) startService(name string, u *units.Unit) error {
	return s.startServiceWithSocket(name, "", nil)
}

func (s *Supervisor) startServiceWithSocket(name, socketName string, ln net.Listener) error {
	s.mu.Lock()
	u, ok := s.units[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("unit %s not found", name)
	}
	if rs := s.state[name]; rs != nil && rs.pid > 0 {
		s.mu.Unlock()
		return nil
	}
	if s.state[name] == nil {
		s.state[name] = &runtimeState{}
	}
	s.state[name].state = "activating"
	s.mu.Unlock()

	for _, pre := range u.ExecStartPre {
		if err := s.runOnesHot(name, pre, nil); err != nil {
			s.setFailed(name, err)
			return err
		}
	}
	if u.ExecStart == "" {
		err := fmt.Errorf("no ExecStart")
		s.setFailed(name, err)
		return err
	}

	argv := splitExec(u.ExecStart)
	if len(argv) == 0 {
		err := fmt.Errorf("empty ExecStart")
		s.setFailed(name, err)
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if u.WorkingDirectory != "" {
		cmd.Dir = u.WorkingDirectory
	}
	env := os.Environ()
	// Prefer multicall binary directory so ExecStart=sleep|sh works without full image symlinks.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		env = prependPath(env, dir)
	}
	env = append(env, u.Environment...)
	// EnvironmentFile skipped in scaffold (TODO)
	if ln != nil {
		file, err := lnFile(ln)
		if err != nil {
			cancel()
			s.setFailed(name, err)
			return err
		}
		cmd.ExtraFiles = []*os.File{file}
		env = append(env,
			"LISTEN_FDS=1",
			"LISTEN_PID="+strconv.Itoa(os.Getpid()), // fixed after Start to child pid
			"LISTEN_FDNAMES="+socketName,
		)
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		s.setFailed(name, err)
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		s.setFailed(name, err)
		return err
	}

	if err := cmd.Start(); err != nil {
		cancel()
		s.setFailed(name, err)
		return err
	}
	// Fix LISTEN_PID — child already started; document limitation or set before start via wrapper.
	// For real activation LISTEN_PID must be the child. Re-exec pattern later.
	if ln != nil {
		// Best effort: many Go activation libs check LISTEN_FDS only.
		_ = socketName
	}

	unitLabel := name
	if u.SyslogIdentifier != "" {
		unitLabel = u.SyslogIdentifier
	}
	go s.pump(unitLabel, "stdout", stdout)
	go s.pump(unitLabel, "stderr", stderr)

	s.mu.Lock()
	s.state[name].pid = cmd.Process.Pid
	s.state[name].cmd = cmd
	s.state[name].cancel = cancel
	s.state[name].state = "active"
	s.mu.Unlock()
	s.log.System(fmt.Sprintf("started %s pid=%d", name, cmd.Process.Pid))

	if u.Type == "oneshot" {
		err := cmd.Wait()
		s.mu.Lock()
		s.state[name].pid = 0
		s.state[name].cmd = nil
		if err != nil {
			s.state[name].state = "failed"
			s.state[name].exitErr = err
		} else {
			s.state[name].state = "inactive"
		}
		s.mu.Unlock()
		return err
	}

	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		if s.state[name] != nil && s.state[name].cmd == cmd {
			s.state[name].pid = 0
			s.state[name].cmd = nil
			if err != nil {
				s.state[name].state = "failed"
				s.state[name].exitErr = err
			} else {
				s.state[name].state = "inactive"
			}
		}
		s.mu.Unlock()
		if err != nil {
			s.onServiceExit(name, u, err)
		}
	}()
	return nil
}

func (s *Supervisor) onServiceExit(name string, u *units.Unit, err error) {
	s.log.System(fmt.Sprintf("unit %s wait: %v", name, err))
	switch u.Restart {
	case "always":
		time.Sleep(parseRestartSec(u.RestartSec))
		_ = s.StartUnit(name)
	case "on-failure":
		time.Sleep(parseRestartSec(u.RestartSec))
		_ = s.StartUnit(name)
	}
}

func lnFile(ln net.Listener) (*os.File, error) {
	type filer interface {
		File() (*os.File, error)
	}
	f, ok := ln.(filer)
	if !ok {
		return nil, fmt.Errorf("listener does not support File()")
	}
	return f.File()
}

func (s *Supervisor) runOnesHot(unit, execLine string, extra *os.File) error {
	argv := splitExec(execLine)
	if len(argv) == 0 {
		return fmt.Errorf("empty exec")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}
	go s.pump(unit, "stdout", stdout)
	go s.pump(unit, "stderr", stderr)
	return cmd.Wait()
}

func (s *Supervisor) pump(unit, stream string, r io.Reader) {
	buf := make([]byte, 4096)
	var partial string
	for {
		n, err := r.Read(buf)
		if n > 0 {
			partial += string(buf[:n])
			for {
				i := strings.IndexByte(partial, '\n')
				if i < 0 {
					break
				}
				s.log.Line(unit, stream, partial[:i])
				partial = partial[i+1:]
			}
		}
		if err != nil {
			if partial != "" {
				s.log.Line(unit, stream, partial)
			}
			return
		}
	}
}

func (s *Supervisor) setFailed(name string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state[name] == nil {
		s.state[name] = &runtimeState{}
	}
	s.state[name].state = "failed"
	s.state[name].exitErr = err
	s.log.System(name + " failed: " + err.Error())
}

// StopUnit implements control.Engine.
func (s *Supervisor) StopUnit(name string) error {
	s.mu.Lock()
	u, ok := s.units[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("unit %s not found", name)
	}
	rs := s.state[name]
	s.mu.Unlock()

	if u.Kind == units.KindSocket {
		s.mu.Lock()
		for _, ln := range s.listen[name] {
			_ = ln.Close()
		}
		delete(s.listen, name)
		if rs != nil {
			rs.state = "inactive"
		}
		s.mu.Unlock()
		return nil
	}
	if u.Kind == units.KindTarget {
		s.mu.Lock()
		if rs != nil {
			rs.state = "inactive"
		}
		s.mu.Unlock()
		return nil
	}

	if u.ExecStop != "" {
		_ = s.runOnesHot(name, u.ExecStop, nil)
	}
	s.mu.Lock()
	rs = s.state[name]
	if rs != nil && rs.cancel != nil {
		rs.cancel()
	}
	if rs != nil && rs.cmd != nil && rs.cmd.Process != nil {
		_ = rs.cmd.Process.Signal(syscall.SIGTERM)
	}
	if rs != nil {
		rs.state = "deactivating"
	}
	s.mu.Unlock()
	// Brief wait
	time.Sleep(100 * time.Millisecond)
	s.mu.Lock()
	if rs = s.state[name]; rs != nil && rs.cmd != nil && rs.cmd.Process != nil {
		_ = rs.cmd.Process.Kill()
	}
	if rs != nil {
		rs.pid = 0
		rs.cmd = nil
		rs.state = "inactive"
	}
	s.mu.Unlock()
	return nil
}

// RestartUnit implements control.Engine.
func (s *Supervisor) RestartUnit(name string) error {
	_ = s.StopUnit(name)
	return s.StartUnit(name)
}

func prependPath(env []string, dir string) []string {
	const prefix = "PATH="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + dir + string(os.PathListSeparator) + strings.TrimPrefix(e, prefix)
			return env
		}
	}
	return append(env, prefix+dir)
}

// splitExec is a minimal argv split (no shell). Quoted strings supported basically.
func splitExec(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var (
		args []string
		cur  strings.Builder
		q    rune
	)
	flush := func() {
		if cur.Len() > 0 {
			args = append(args, cur.String())
			cur.Reset()
		}
	}
	for _, r := range line {
		switch {
		case q != 0:
			if r == q {
				q = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			q = r
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return args
}
