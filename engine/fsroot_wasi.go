//go:build tinygo

package engine

// The wasm filesystem root: the WASI preopen. There is no working directory
// under WASI — TinyGo resolves a path by matching it against the preopens the
// host installed, so a bare relative path finds no preopen and fails ENOENT.
// Anchoring at "/" makes it match the host's root preopen: OPFS in the browser,
// a granted directory under FSA, littlefs on embedded, a temp dir in the parity
// harness (§5.2).
//
// This is the whole filesystem "capability seam" — the logic above it is
// identical across tiers, which is the point.
func fsRoot() string { return "/" }
