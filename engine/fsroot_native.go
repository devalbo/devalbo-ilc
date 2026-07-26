//go:build !tinygo

package engine

// The native filesystem root: the process working directory. The host chooses
// what that is before invoking the engine (cwd for the CLI, the app's config
// dir for desktop) — §5.2/§5.5, two-phase launch.
func fsRoot() string { return "." }
