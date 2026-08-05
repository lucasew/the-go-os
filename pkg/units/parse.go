package units

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// allowed keys per section (v0 allowlist). Unknown keys → error.
var allowed = map[string]map[string]bool{
	"Unit": {
		"Description": true,
		"After":       true,
		"Before":      true,
		"Requires":    true,
		"Wants":       true,
		"Conflicts":   true,
		"BindsTo":     true,
		"PartOf":      true,
	},
	"Install": {
		"WantedBy": true,
	},
	"Service": {
		"Type":             true,
		"ExecStart":        true,
		"ExecStartPre":     true,
		"ExecStop":         true,
		"Restart":          true,
		"RestartSec":       true,
		"Environment":      true,
		"EnvironmentFile":  true,
		"WorkingDirectory": true,
		"SyslogIdentifier": true,
	},
	"Socket": {
		"ListenStream":   true,
		"ListenDatagram": true,
		"Accept":         true,
		"Service":        true,
	},
}

// Parse reads a unit file body for the given name/kind/path.
func Parse(name, path string, kind Kind, r io.Reader) (*Unit, error) {
	u := &Unit{Name: name, Kind: kind, Path: path}
	sc := bufio.NewScanner(r)
	// Allow long ExecStart lines
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	section := ""
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line[1 : len(line)-1]
			switch section {
			case "Unit", "Install", "Service", "Socket":
			default:
				return nil, fmt.Errorf("%s:%d: unsupported section [%s]", path, lineNo, section)
			}
			// Kind-specific sections
			if section == "Service" && kind != KindService {
				return nil, fmt.Errorf("%s:%d: [Service] only valid in .service", path, lineNo)
			}
			if section == "Socket" && kind != KindSocket {
				return nil, fmt.Errorf("%s:%d: [Socket] only valid in .socket", path, lineNo)
			}
			continue
		}
		if section == "" {
			return nil, fmt.Errorf("%s:%d: key outside section: %s", path, lineNo, line)
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected Key=Value", path, lineNo)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if !allowed[section][key] {
			return nil, fmt.Errorf("%s:%d: unsupported key %s.%s (v0 allowlist)", path, lineNo, section, key)
		}
		if err := apply(u, section, key, val); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := u.Validate(); err != nil {
		return nil, err
	}
	return u, nil
}

func apply(u *Unit, section, key, val string) error {
	switch section {
	case "Unit":
		switch key {
		case "Description":
			u.Description = val
		case "After":
			u.After = append(u.After, splitWS(val)...)
		case "Before":
			u.Before = append(u.Before, splitWS(val)...)
		case "Requires":
			u.Requires = append(u.Requires, splitWS(val)...)
		case "Wants":
			u.Wants = append(u.Wants, splitWS(val)...)
		case "Conflicts":
			u.Conflicts = append(u.Conflicts, splitWS(val)...)
		case "BindsTo":
			u.BindsTo = append(u.BindsTo, splitWS(val)...)
		case "PartOf":
			u.PartOf = append(u.PartOf, splitWS(val)...)
		}
	case "Install":
		if key == "WantedBy" {
			u.WantedBy = append(u.WantedBy, splitWS(val)...)
		}
	case "Service":
		switch key {
		case "Type":
			u.Type = val
		case "ExecStart":
			if u.ExecStart != "" {
				return fmt.Errorf("multiple ExecStart not supported in v0")
			}
			u.ExecStart = val
		case "ExecStartPre":
			u.ExecStartPre = append(u.ExecStartPre, val)
		case "ExecStop":
			u.ExecStop = val
		case "Restart":
			u.Restart = val
		case "RestartSec":
			u.RestartSec = val
		case "Environment":
			u.Environment = append(u.Environment, val)
		case "EnvironmentFile":
			u.EnvironmentFile = append(u.EnvironmentFile, val)
		case "WorkingDirectory":
			u.WorkingDirectory = val
		case "SyslogIdentifier":
			u.SyslogIdentifier = val
		}
	case "Socket":
		switch key {
		case "ListenStream":
			u.ListenStream = append(u.ListenStream, val)
		case "ListenDatagram":
			u.ListenDatagram = append(u.ListenDatagram, val)
		case "Accept":
			u.Accept = val
		case "Service":
			u.Service = val
		}
	}
	return nil
}

func splitWS(s string) []string {
	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// KindFromName derives kind from unit file name.
func KindFromName(name string) (Kind, error) {
	ext := strings.TrimPrefix(filepath.Ext(name), ".")
	switch ext {
	case "service":
		return KindService, nil
	case "socket":
		return KindSocket, nil
	case "target":
		return KindTarget, nil
	default:
		return "", fmt.Errorf("unsupported unit type for %q", name)
	}
}
