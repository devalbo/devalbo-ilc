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

	// Root is this tier's SLOT: the directory holding this app's host code for
	// that tier (Decision 34) — `hosts/web`, `hosts/native`. Required for every
	// tier, and checked to exist. On the web tier it doubles as the Vite root,
	// which is why the assets below must sit inside it.
	Root string

	// web only
	Assets string // jco output; must be inside Root

	Component string // the wasm component (not a web asset) — every wasm tier's input
	// embedded only: where the AOT'd Pulley bytecode lands. Defaults to
	// build/engine.<target>.cwasm, which names the TARGET rather than the tier
	// because two boards sharing a target share the file (`rp2350` and `esp32p4`
	// both consume build/engine.pulley32.cwasm — one build, not two).
	Cwasm string
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
	if err := checkSlots(m); err != nil {
		return nil, fmt.Errorf("%s: %w", manifestFile, err)
	}
	return m, nil
}

// checkSlots enforces the one thing dlc.toml actually gates (Decision 34): every
// declared tier names a slot directory, and that directory is there.
//
// Until now nothing in this file was load-bearing — `capabilities` had one
// writer and zero readers, and `root` was parsed and never read, so a typo or an
// omission cost nothing until something downstream did the wrong thing quietly.
// A tier with no slot is a tier with nowhere to put host code; saying so here is
// the difference between a named error and a web build that succeeds and serves
// an empty directory.
//
// Checked against the filesystem rather than trusted, because the failure this
// prevents is a MOVED or renamed directory — exactly what a stale `root` looks
// like, and exactly what happened when the web slot moved out of `frontend/`.
func checkSlots(m *Manifest) error {
	names := make([]string, 0, len(m.Tiers))
	for name := range m.Tiers {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic: report the FIRST bad tier, not a random one

	for _, name := range names {
		tier := m.Tiers[name]
		if tier.Root == "" {
			return fmt.Errorf("[tiers.%s] has no root — every tier names the directory holding its host code, e.g. root = \"hosts/%s\"", name, name)
		}
		info, err := os.Stat(tier.Root)
		if err != nil {
			return fmt.Errorf("[tiers.%s] root %q does not exist", name, tier.Root)
		}
		if !info.IsDir() {
			return fmt.Errorf("[tiers.%s] root %q is not a directory", name, tier.Root)
		}
	}
	return nil
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
		case "cwasm":
			tier.Cwasm = unquote(value)
		default:
			return unknownKey(section, key, "capabilities", "root", "assets", "component", "cwasm")
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
