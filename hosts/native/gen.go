package main

// `dlc gen` — a TOOLCHAIN verb (Decision 30): it runs buf and writes generated
// code, so it lives host-side and never enters the engine.
//
// Its one job beyond wrapping buf is turning `dlc.toml` into Go the engine can
// import. That is how the manifest reaches the engine WITHOUT the engine ever
// reading a config file: the values are resolved at build time, exactly as
// method ids are. The engine keeps no parser, no file dependency, and no way to
// disagree with the manifest.

import (
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"

	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func runGen(request []byte) error {
	// The generated surface already refused any argument — `GenRequest` has no
	// fields, so a stray flag fails at parse time with the schema's own message.
	// Decoding is still done rather than skipped: it is the same contract an
	// engine handler honours, and a silently-ignored request is how a field added
	// later would go unnoticed.
	var req dlcv1.GenRequest
	if err := req.UnmarshalVT(request); err != nil {
		return fmt.Errorf("gen: %w", err)
	}
	m, err := loadManifest()
	if err != nil {
		return err
	}

	// buf first: the config package is small, but a failed codegen should not
	// leave a half-updated tree behind it.
	if _, err := exec.LookPath("buf"); err != nil {
		return fmt.Errorf("gen: buf not found on PATH — run inside `devbox shell`")
	}
	cmd := exec.Command("buf", "generate")
	cmd.Dir = "proto"
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gen: buf generate: %w", err)
	}

	if err := copyPlatformTS(m); err != nil {
		return err
	}
	return writeConfigPackage(m)
}

// copyPlatformTS puts the platform's generated TypeScript into the app's gen
// tree, so a browser front end can reach the INHERITED commands.
//
// WHY THIS IS NEEDED AT ALL. On the Go side an app imports the platform's
// generated code straight from the platform module, so `version`, `export-fs`,
// `import-fs` and `reset-fs` are one import away. TypeScript has no equivalent:
// an app's `buf generate` sees only the app's own protos, so before this the
// inherited verbs were simply unreachable from any browser front end — the web
// terminal could not run `version` at all.
//
// WHY NOT IN THE TEMPLATE. `AGENTS.md` §3: templates depend on the platform,
// they never inline it, because code copied into a scaffold is frozen there
// forever. A platform message vendored at scaffold time could never be fixed
// upstream. Copying at GENERATE time is the same relationship the Go module
// dependency has — the app tracks whatever platform it is built against.
//
// This mirrors `dlc build web` supplying the WIT world: the app does not own it,
// carry it, or go stale on it.
func copyPlatformTS(m *Manifest) error {
	if m.PlatformPath == "" {
		// No local platform checkout: nothing to copy, and nothing to say. When
		// dlc-platform is published this becomes a package dependency instead.
		return nil
	}
	// From the PLATFORM MODULE's own generated tree (§16.4), not dlc's. Since the
	// extraction those are two directories: `dlc-platform/gen/ts` holds the
	// inherited surface and `gen/ts` holds dlc's own commands. An app inherits
	// the first and has no business carrying the second — dlc is an app like any
	// other, and its command surface is not part of what other apps inherit.
	// THE WHOLE `devalbo` TREE, not a named subdirectory.
	//
	// This said `"devalbo", "ilc"`, and the omission cost three separate
	// failures as the platform grew a second namespace: `platform.proto`
	// imports `SchemaInfo`, which imports `StdVersion` from
	// `devalbo/dlc/std/v1`, so an app given only `ilc` had TypeScript importing
	// a file that was never copied. Every time it surfaced as a browser test
	// timing out on a missing module, naming nothing.
	//
	// The distinction that matters is already made by the path above: this is
	// the PLATFORM's generated tree (`dlc-platform/gen/ts`), as opposed to
	// dlc's own (`gen/ts`), and an app inherits all of the first and none of the
	// second. Naming subdirectories inside it re-litigates a decision the path
	// already made, and goes stale the next time a namespace is added.
	src := filepath.Join(m.PlatformPath, "dlc-platform", "gen", "ts", "devalbo")
	if _, err := os.Stat(src); err != nil {
		// The platform has not generated its own TS yet. Not fatal: an app that
		// never touches an inherited command still builds.
		return nil
	}
	dst := filepath.Join("gen", "ts", "devalbo")
	return copyTree(src, dst)
}

// copyTree copies a directory recursively, skipping anything the app generates
// for itself.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// writeConfigPackage emits gen/go/dlcconfig from the manifest.
func writeConfigPackage(m *Manifest) error {
	dir := filepath.Join("gen", "go", "dlcconfig")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("// Code generated by `dlc gen` from dlc.toml. DO NOT EDIT.\n//\n")
	b.WriteString("// The engine imports this instead of reading dlc.toml, because it also runs\n")
	b.WriteString("// in a browser tab and on devices where that file does not exist. Editing\n")
	b.WriteString("// these values means editing dlc.toml and re-running `dlc gen`.\n")
	b.WriteString("package dlcconfig\n\n")
	b.WriteString("const (\n")
	fmt.Fprintf(&b, "\t// Name is the project name from [project].\n\tName = %s\n", strconv.Quote(m.Name))
	fmt.Fprintf(&b, "\t// Version is the project version from [project].\n\tVersion = %s\n", strconv.Quote(m.Version))
	b.WriteString(")\n\n")
	b.WriteString("// Version string apps hand to platform.SetVersion.\n")
	b.WriteString("func Display() string { return Name + \" \" + Version }\n")

	out := filepath.Join(dir, "config.go")
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "gen: wrote "+out)
	return nil
}
