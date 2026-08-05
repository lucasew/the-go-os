package shell

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// defaultPS1 used when PS1 is unset in the environment.
// Classic user@host:cwd$ with a trailing space.
const defaultPS1 = `\u@\h:\w\$ `

// shellEnv is a small writable environ so export PS1=… is visible to the prompt.
type shellEnv struct {
	parent expand.Environ
	local  map[string]expand.Variable
}

func newShellEnv(pairs []string) *shellEnv {
	return &shellEnv{
		parent: expand.ListEnviron(pairs...),
		local:  map[string]expand.Variable{},
	}
}

func (e *shellEnv) Get(name string) expand.Variable {
	if e == nil {
		return expand.Variable{}
	}
	if v, ok := e.local[name]; ok {
		return v
	}
	if e.parent != nil {
		return e.parent.Get(name)
	}
	return expand.Variable{}
}

func (e *shellEnv) Each(fn func(name string, vr expand.Variable) bool) {
	seen := map[string]bool{}
	for name, vr := range e.local {
		seen[name] = true
		if !vr.IsSet() && !vr.Declared() {
			continue
		}
		if !fn(name, vr) {
			return
		}
	}
	if e.parent == nil {
		return
	}
	e.parent.Each(func(name string, vr expand.Variable) bool {
		if seen[name] {
			return true
		}
		return fn(name, vr)
	})
}

func (e *shellEnv) Set(name string, vr expand.Variable) error {
	if name == "" {
		return nil
	}
	if !vr.IsSet() && !vr.Declared() {
		e.local[name] = expand.Variable{}
		return nil
	}
	if vr.Kind == expand.KeepValue {
		prev := e.Get(name)
		prev.Local = vr.Local || prev.Local
		prev.Exported = vr.Exported || prev.Exported
		prev.ReadOnly = vr.ReadOnly || prev.ReadOnly
		e.local[name] = prev
		return nil
	}
	e.local[name] = vr
	return nil
}

func ensureDefaultPS1(env *shellEnv) {
	if env.Get("PS1").String() != "" {
		return
	}
	_ = env.Set("PS1", expand.Variable{
		Set:      true,
		Kind:     expand.String,
		Str:      defaultPS1,
		Exported: true,
	})
}

// currentPS1 returns the prompt template: shell var, else process env, else default.
func currentPS1(r *interp.Runner, env *shellEnv) string {
	if r != nil {
		if v, ok := r.Vars["PS1"]; ok && v.IsSet() {
			if s := v.String(); s != "" {
				return s
			}
		}
	}
	if env != nil {
		if s := env.Get("PS1").String(); s != "" {
			return s
		}
	}
	if s := os.Getenv("PS1"); s != "" {
		return s
	}
	return defaultPS1
}

// expandPS1 expands a subset of bash-style PS1 backslash escapes.
// Also strips \[ and \] (readline invisible markers; liner has no width API).
// After this, bash still runs parameter expansion and command substitution.
func expandPS1(ps1 string) string {
	home := ""
	if u, err := user.Current(); err == nil {
		home = u.HomeDir
	}
	if home == "" {
		home = os.Getenv("HOME")
	}
	username := os.Getenv("USER")
	if username == "" {
		if u, err := user.Current(); err == nil {
			username = u.Username
		}
	}
	host, _ := os.Hostname()
	shortHost := host
	if i := strings.IndexByte(host, '.'); i > 0 {
		shortHost = host[:i]
	}
	wd, err := os.Getwd()
	if err != nil {
		wd = "?"
	}
	wdTilde := wd
	if home != "" && (wd == home || strings.HasPrefix(wd, home+string(os.PathSeparator))) {
		wdTilde = "~" + wd[len(home):]
	}
	base := filepath.Base(wd)
	if wdTilde == "~" {
		base = "~"
	}
	promptChar := "$"
	if os.Geteuid() == 0 {
		promptChar = "#"
	}

	var b strings.Builder
	b.Grow(len(ps1) + 16)
	for i := 0; i < len(ps1); i++ {
		if ps1[i] != '\\' || i+1 >= len(ps1) {
			b.WriteByte(ps1[i])
			continue
		}
		i++
		switch ps1[i] {
		case 'u':
			b.WriteString(username)
		case 'h':
			b.WriteString(shortHost)
		case 'H':
			b.WriteString(host)
		case 'w':
			b.WriteString(wdTilde)
		case 'W':
			b.WriteString(base)
		case '$':
			b.WriteString(promptChar)
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteString(time.Now().Format("15:04:05"))
		case 'T':
			b.WriteString(time.Now().Format("15:04"))
		case 'a':
			b.WriteByte('\a')
		case 'e', 'E':
			b.WriteByte('\x1b')
		case '\\':
			b.WriteByte('\\')
		case '[', ']':
			// non-printing markers — ignore for liner
		case '0', '1', '2', '3', '4', '5', '6', '7':
			val := int(ps1[i] - '0')
			n := 1
			for n < 3 && i+1 < len(ps1) && ps1[i+1] >= '0' && ps1[i+1] <= '7' {
				i++
				n++
				val = val*8 + int(ps1[i]-'0')
			}
			b.WriteByte(byte(val))
		default:
			b.WriteByte('\\')
			b.WriteByte(ps1[i])
		}
	}
	return b.String()
}

// promptEnv supplies expand.Config specials (?, $) plus shell variables.
type promptEnv struct {
	exit uint8
	r    *interp.Runner
	env  *shellEnv
}

func (p *promptEnv) Get(name string) expand.Variable {
	switch name {
	case "?":
		return strVar(strconv.FormatUint(uint64(p.exit), 10))
	case "$":
		return strVar(strconv.Itoa(os.Getpid()))
	case "PPID":
		return strVar(strconv.Itoa(os.Getppid()))
	}
	if p.r != nil {
		if v, ok := p.r.Vars[name]; ok && (v.IsSet() || v.Declared()) {
			return v
		}
	}
	if p.env != nil {
		return p.env.Get(name)
	}
	return expand.Variable{}
}

func (p *promptEnv) Each(fn func(name string, vr expand.Variable) bool) {
	// Specials first
	for _, pair := range []struct {
		n string
		v expand.Variable
	}{
		{"?", p.Get("?")},
		{"$", p.Get("$")},
		{"PPID", p.Get("PPID")},
	} {
		if !fn(pair.n, pair.v) {
			return
		}
	}
	seen := map[string]bool{"?": true, "$": true, "PPID": true}
	if p.r != nil {
		for name, vr := range p.r.Vars {
			if seen[name] {
				continue
			}
			seen[name] = true
			if !fn(name, vr) {
				return
			}
		}
	}
	if p.env != nil {
		p.env.Each(func(name string, vr expand.Variable) bool {
			if seen[name] {
				return true
			}
			return fn(name, vr)
		})
	}
}

func strVar(s string) expand.Variable {
	return expand.Variable{Set: true, Kind: expand.String, Str: s}
}

// expandPromptDynamics runs bash-style parameter expansion and command
// substitution on the already-decoded PS1 string ($?, $(…), $VAR, …).
func expandPromptDynamics(ctx context.Context, decoded string, lastExit uint8, r *interp.Runner, env *shellEnv) string {
	if decoded == "" {
		return decoded
	}
	// Skip expand if nothing looks dynamic — fast path.
	if !strings.ContainsAny(decoded, "$`") {
		return decoded
	}

	penv := &promptEnv{exit: lastExit, r: r, env: env}
	cfg := &expand.Config{
		Env: penv,
		CmdSubst: func(w io.Writer, cs *syntax.CmdSubst) error {
			return runPromptCmdSubst(ctx, w, cs, penv)
		},
	}

	word, err := syntax.NewParser().Document(strings.NewReader(decoded))
	if err != nil {
		// Leave unexpanded rather than break the prompt.
		return decoded
	}
	out, err := expand.Literal(cfg, word)
	if err != nil {
		return decoded
	}
	return out
}

func runPromptCmdSubst(ctx context.Context, w io.Writer, cs *syntax.CmdSubst, penv *promptEnv) error {
	// Separate runner so prompt side-effects stay isolated; share applet dispatch + env.
	var stdout bytes.Buffer
	opts := []interp.RunnerOption{
		interp.StdIO(nil, &stdout, io.Discard),
		interp.CallHandler(callAppletsFirst),
		interp.ExecHandlers(execAppletsFirst),
	}
	if penv.env != nil {
		opts = append(opts, interp.Env(penv.env))
	}
	sub, err := interp.New(opts...)
	if err != nil {
		return err
	}
	// Seed specials via a one-shot: run stmts from the CmdSubst node.
	file := &syntax.File{Stmts: cs.Stmts}
	err = sub.Run(ctx, file)
	if err != nil {
		if _, ok := interp.IsExitStatus(err); !ok {
			// Failed external (e.g. missing tput): empty substitution, not a hard error.
			return nil
		}
	}
	// Bash strips all trailing newlines from command substitution.
	s := stdout.String()
	s = strings.TrimRight(s, "\n")
	_, err = io.WriteString(w, s)
	return err
}

func formatPrompt(ctx context.Context, r *interp.Runner, env *shellEnv, lastExit uint8) string {
	raw := currentPS1(r, env)
	decoded := expandPS1(raw)
	return expandPromptDynamics(ctx, decoded, lastExit, r, env)
}

// promptForLiner returns a prompt safe for peterh/liner.
// liner rejects any unicode.C rune (including ESC from colors / tput), with ErrInvalidPrompt.
// We strip ANSI CSI and other controls so path/user/$? still show; colors are dropped in the
// line-editor prompt (limitation of liner, not of PS1 expansion).
func promptForLiner(s string) string {
	s = stripANSI(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == utf8.RuneError {
			continue
		}
		// liner: unicode.Is(unicode.C, r) → ErrInvalidPrompt
		if unicode.Is(unicode.C, r) {
			if r == '\n' || r == '\r' {
				b.WriteByte(' ')
			}
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if strings.TrimSpace(out) == "" {
		return "$ "
	}
	return out
}

// stripANSI removes ECMA-48 CSI / OSC-ish escape sequences (ESC [ … final).
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '\x1b' {
			b.WriteByte(s[i])
			i++
			continue
		}
		i++ // skip ESC
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '[': // CSI: ESC [ params finalbyte
			i++
			for i < len(s) {
				c := s[i]
				i++
				// final byte of CSI is 0x40–0x7E
				if c >= 0x40 && c <= 0x7e {
					break
				}
			}
		case ']': // OSC: ESC ] … BEL or ESC \
			i++
			for i < len(s) {
				if s[i] == '\a' {
					i++
					break
				}
				if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		case '(':
			// charset designate ESC ( X
			i++
			if i < len(s) {
				i++
			}
		default:
			// skip single-char escape
			i++
		}
	}
	return b.String()
}
