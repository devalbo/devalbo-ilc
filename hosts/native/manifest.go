package main

// Reading `dlc.toml` — host-side only (Decision 30), because the engine has no
// config file on any tier it runs on.
//
// Hand-parsed rather than pulling in a TOML library. The subset we define is
// small and fixed (sections, `key = "string"`, `key = ["a", "b"]`), a dependency
// here would be one more thing pinned in a bootstrap that already pins a lot,
// and — the deciding reason — an unknown key should be an ERROR rather than
// silently ignored, which most decoders do the opposite of. A typo'd `capabilties`
// that silently means "no capabilities" is exactly the failure this file exists
// to prevent.

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Manifest is the project's declared shape. Only fields dlc actually uses are
// modelled; anything else in the file is an error, not decoration.
type Manifest struct {
	Name    string
	Version string
	Module  string

	PlatformPath string

	// Tiers, keyed by name ("native", "web"). A tier exists because its section
	// does — there is no separate enabled-list to disagree with the sections.
	Tiers map[string]Tier
}

// Tier is one deployment target's build-time composition.
type Tier struct {
	// Capabilities linked into THIS tier. Build-time selection, not a runtime
	// description: what the host actually has (a display's resolution) arrives
	// separately via the environment manifest.
	Capabilities []string

	// web only
	Root      string // the web root; assets must live inside it
	Assets    string // jco output
	Component string // the wasm component (not a web asset)
}

const manifestFile = "dlc.toml"

// loadManifest reads dlc.toml from the current directory.
func loadManifest() (*Manifest, error) {
	raw, err := os.ReadFile(manifestFile)
	if err != nil {
		return nil, fmt.Errorf("no %s here — run from a project root, or `dlc new` one", manifestFile)
	}
	m, err := parseManifest(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", manifestFile, err)
	}
	if m.Name == "" {
		return nil, fmt.Errorf("%s: [project] name is required", manifestFile)
	}
	return m, nil
}

func parseManifest(src string) (*Manifest, error) {
	m := &Manifest{Tiers: map[string]Tier{}}
	section := ""

	for i, line := range strings.Split(src, "\n") {
		lineNo := i + 1
		text := strings.TrimSpace(line)
		if idx := strings.Index(text, "#"); idx >= 0 {
			text = strings.TrimSpace(text[:idx])
		}
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
			section = strings.TrimSpace(text[1 : len(text)-1])
			if strings.HasPrefix(section, "tiers.") {
				name := strings.TrimPrefix(section, "tiers.")
				if _, ok := m.Tiers[name]; !ok {
					m.Tiers[name] = Tier{}
				}
			}
			continue
		}

		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected `key = value`", lineNo)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if err := assign(m, section, key, value); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
	}
	return m, nil
}

func assign(m *Manifest, section, key, value string) error {
	switch section {
	case "project":
		switch key {
		case "name":
			m.Name = unquote(value)
		case "version":
			m.Version = unquote(value)
		case "module":
			m.Module = unquote(value)
		default:
			return unknownKey(section, key, "name", "version", "module")
		}
	case "platform":
		switch key {
		case "path":
			m.PlatformPath = unquote(value)
		default:
			return unknownKey(section, key, "path")
		}
	default:
		if !strings.HasPrefix(section, "tiers.") {
			return fmt.Errorf("unknown section [%s]", section)
		}
		name := strings.TrimPrefix(section, "tiers.")
		tier := m.Tiers[name]
		switch key {
		case "capabilities":
			tier.Capabilities = unquoteList(value)
		case "root":
			tier.Root = unquote(value)
		case "assets":
			tier.Assets = unquote(value)
		case "component":
			tier.Component = unquote(value)
		default:
			return unknownKey(section, key, "capabilities", "root", "assets", "component")
		}
		m.Tiers[name] = tier
	}
	return nil
}

// unknownKey names what WAS allowed — a misspelling is the common case, and the
// fix is usually visible in the list.
func unknownKey(section, key string, allowed ...string) error {
	sort.Strings(allowed)
	return errors.New("unknown key " + key + " in [" + section + "] (allowed: " + strings.Join(allowed, ", ") + ")")
}

func unquote(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"`)
}

func unquoteList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	var out []string
	for _, part := range strings.Split(s, ",") {
		if v := unquote(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}
