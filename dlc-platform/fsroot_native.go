//go:build !tinygo

package platform

import (
	"errors"
	"os"
)

// The native filesystem fsRoot — GRANTED by the host, the way WASI grants a
// preopen (§5.2/§5.5, two-phase launch).
//
// It used to be the constant ".", meaning "wherever the user happened to be
// standing", and that was not a fsRoot at all. Three problems, the first severe:
//
//  1. `reset-fs` is an INHERITED verb — every ILC app has it, no author writes
//     it — and with the cwd as fsRoot it recursively clears whatever directory you
//     ran the app in. During development it deleted an exported bundle. In a
//     user's home directory that is data loss from a command the app never
//     opted into.
//  2. No confinement. `SafeJoin` blocks `../` escapes RELATIVE TO the fsRoot, but
//     the fsRoot itself moved with the shell — so the WASI guarantee ("this is
//     your filesystem, there is nothing else") had no native equivalent.
//  3. Paths in errors differed per tier, because one side was cwd-relative and
//     the other `/`-rooted.
//
// Now the host must grant one before the engine touches a file. Wasm is
// unchanged: its fsRoot is the preopen, and there is nothing to grant.
var fsRoot string

// SetFSRoot grants the engine its filesystem, creating it if absent.
//
// Called by the HOST during startup, never by app code — the same relationship
// the browser has with its OPFS preopen. Creating it here rather than on first
// write means `export-fs` on a fresh install sees an empty directory instead of
// an error about a path that was never made.
func SetFSRoot(dir string) error {
	if dir == "" {
		return errors.New("platform: empty filesystem fsRoot — a host must grant one")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fsRoot = dir
	return nil
}

// AppFSRoot is the conventional native fsRoot for an app: `./.<name>/`.
//
// Project-local, like git — an app run in two projects keeps two stores — but
// CONFINED, so `reset-fs` can only ever clear the app's own subtree. That
// combination is the reason for the leading dot rather than a bare `./<name>/`:
// it stays out of the way of the project's real files.
//
// `dlc` deliberately overrides this with "." — its data IS the user's working
// directory, since `dlc new myapp` must scaffold where you are standing. An app
// whose output belongs to the user rather than to itself should do the same.
func AppFSRoot(name string) string {
	return "." + name
}

// Root is where the engine's filesystem begins. Panics if no host granted one:
// silently falling back to the working directory is exactly the behaviour this
// replaced, and it fails as data loss rather than as an error.
func FSRoot() string {
	if fsRoot == "" {
		panic("platform: no filesystem fsRoot granted — the host must call SetFSRoot before running commands")
	}
	return fsRoot
}

// FSRootGranted reports whether a host has granted a fsRoot. For tests and hosts
// that need to check before dispatching.
func FSRootGranted() bool { return fsRoot != "" }
