package units

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultFilesystemDir is the systemd-shaped unit path.
const DefaultFilesystemDir = "/etc/systemd/system"

// LoadResult is the merged unit set.
type LoadResult struct {
	Units map[string]*Unit
	// Warnings includes replace-by-name messages (embed overridden by FS).
	Warnings []string
}

// Loader loads embed FS then filesystem overrides.
type Loader struct {
	Embed     fs.FS  // may be nil
	EmbedRoot string // directory inside Embed, e.g. "units"
	FileDirs  []string
	WarnFn    func(string) // optional; also collected in Warnings
}

// Load merges embed then filesystem (FS wins with warning).
func (l *Loader) Load() (*LoadResult, error) {
	res := &LoadResult{Units: map[string]*Unit{}}
	warn := func(msg string) {
		res.Warnings = append(res.Warnings, msg)
		if l.WarnFn != nil {
			l.WarnFn(msg)
		}
	}

	if l.Embed != nil {
		root := l.EmbedRoot
		if root == "" {
			root = "."
		}
		err := fs.WalkDir(l.Embed, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			base := path.Base(p)
			kind, err := KindFromName(base)
			if err != nil {
				return nil // skip unknown extensions in embed
			}
			b, err := fs.ReadFile(l.Embed, p)
			if err != nil {
				return err
			}
			u, err := Parse(base, "embed:"+p, kind, strings.NewReader(string(b)))
			if err != nil {
				return fmt.Errorf("embed %s: %w", p, err)
			}
			res.Units[base] = u
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// FileDirs empty means embed-only (no host /etc scan). Callers opt in explicitly.
	for _, dir := range l.FileDirs {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			base := e.Name()
			kind, err := KindFromName(base)
			if err != nil {
				continue
			}
			fp := filepath.Join(dir, base)
			b, err := os.ReadFile(fp)
			if err != nil {
				return nil, err
			}
			u, err := Parse(base, fp, kind, strings.NewReader(string(b)))
			if err != nil {
				// Host /etc/systemd/system often has full systemd units outside our allowlist.
				// Skip with warning so embed defaults still boot; our units must still parse.
				warn(fmt.Sprintf("skip %s: %v", fp, err))
				continue
			}
			if prev, ok := res.Units[base]; ok {
				warn(fmt.Sprintf("unit %s: filesystem %s replaces %s", base, fp, prev.Path))
			}
			res.Units[base] = u
		}
	}
	return res, nil
}
