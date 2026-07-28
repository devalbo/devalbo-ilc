//go:build !tinygo

package platform

import (
	"errors"
	"os"
)

// The native filesystem root — GRANTED by the host, the way WASI grants a
// preopen (§5.2/§5.5, two-phase launch).
//
// It used to be the constant ".", meaning "wherever the user happened to be
// standing", and that was not a root at all. Three problems, the first severe:
//
//  1. `reset-fs` is an INHERITED verb — every ILC app has it, no author writes
//     it — and with the cwd as root it recursively clears whatever directory you
//     ran the app in. During development it deleted an exported bundle. In a
//     user's home directory that is data loss from a command the app never
//     opted into.
//  2. No confinement. `SafeJoin` blocks `../` escapes RELATIVE TO the root, but
//     the root itself moved with the shell — so the WASI guarantee ("this is
//     your filesystem, there is nothing else") had no native equivalent.
//  3. Paths in errors differed per tier, because one side was cwd-relative and
//     the other `/`-rooted.
//
// Now the host must grant one before the engine touches a file. Wasm is
// unchanged: its root is the preopen, and there is nothing to grant.
var root string

// SetRoot grants the engine its filesystem, creating it if absent.
//
// Called by the HOST during startup, never by app code — the same relationship
// the browser has with its OPFS preopen. Creating it here rather than on first
// write means `export-fs` on a fresh install sees an empty directory instead of
// an error about a path that was never made.
func SetRoot(dir string) error {
	if dir == "" {
		return errors.New("platform: empty filesystem root — a host must grant one")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root = dir
	return nil
}

// AppRoot is the conventional native root for an app: `./.<name>/`.
//
// Project-local, like git — an app run in two projects keeps two stores — but
// CONFINED, so `reset-fs` can only ever clear the app's own subtree. That
// combination is the reason for the leading dot rather than a bare `./<name>/`:
// it stays out of the way of the project's real files.
//
// `dlc` deliberately overrides this with "." — its data IS the user's working
// directory, since `dlc new myapp` must scaffold where you are standing. An app
// whose output belongs to the user rather than to itself should do the same.
func AppRoot(name string) string {
	return "." + name
}

// Root is where the engine's filesystem begins. Panics if no host granted one:
// silently falling back to the working directory is exactly the behaviour this
// replaced, and it fails as data loss rather than as an error.
func Root() string {
	if root == "" {
		panic("platform: no filesystem root granted — the host must call SetRoot before running commands")
	}
	return root
}

// RootGranted reports whether a host has granted a root. For tests and hosts
// that need to check before dispatching.
func RootGranted() bool { return root != "" }
