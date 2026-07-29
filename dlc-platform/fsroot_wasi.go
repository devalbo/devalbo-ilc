//go:build tinygo

package platform

// Root is the wasm filesystem root: the WASI preopen. There is no working
// directory under WASI — TinyGo resolves a path by matching it against the
// preopens the host installed, so a bare relative path finds no preopen and
// fails ENOENT. Anchoring at "/" makes it match the host's root preopen: OPFS in
// the browser, a granted directory under FSA, littlefs on embedded, a temp dir
// in the parity harness (§5.2).
//
// This is the whole filesystem "capability seam" — the logic above it is
// identical across tiers, which is the point.
func Root() string { return "/" }

// SetRoot is a NO-OP here, and deliberately not an error.
//
// The grant already happened: the host installed a preopen before instantiating
// the component, and the guest cannot rebind it afterwards (see worker.ts — the
// engine snapshots its preopen, which is why `reset()` reloads the page). App
// and host code calls SetRoot on both tiers so the startup sequence reads the
// same everywhere; on this tier it is describing something already true.
func SetRoot(string) error { return nil }

// AppRoot is the conventional per-app root. On this tier storage is already
// app-private — one OPFS per origin — so the preopen IS the app's directory and
// there is nothing to nest inside it.
func AppRoot(string) string { return "/" }

// RootGranted is always true: the preopen exists or the component did not load.
func RootGranted() bool { return true }
