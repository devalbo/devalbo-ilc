package engine

// Filesystem writes. There is no custom filesystem capability (§5.2): the engine
// uses plain Go `os`, TinyGo lowers it to WASI, and each host binds the root —
// native process cwd, browser OPFS preopen, embedded littlefs. So this file is
// ordinary Go and behaves the same on every tier.
//
// Paths are **relative to the process/WASI root** and never absolute: the host
// decides what the root is, and the engine must not be able to reach outside it.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// writeTree writes files under root, creating parent directories as needed.
// Order follows the slice, which keeps native and wasm runs byte-comparable.
func writeTree(root string, files []templateFile) error {
	for _, f := range files {
		dest, err := safeJoin(root, f.path)
		if err != nil {
			return err
		}
		if dir := filepath.Dir(dest); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		if err := os.WriteFile(dest, []byte(f.content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// safeJoin resolves a template-relative path under root, rejecting anything that
// would escape it. Template paths are ours today, but `import-fs` will accept
// bundles from elsewhere and go through the same door — so the check belongs
// here, not at the call site.
func safeJoin(root, path string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return "", errors.New("absolute path not allowed: " + path)
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes root: " + path)
	}
	if root == "" {
		return clean, nil
	}
	return filepath.Join(root, clean), nil
}

// dirIsOccupied reports whether root already exists with anything in it. Refusing
// to scaffold over an existing tree is deliberate: `new` must never silently
// overwrite a user's work.
//
// Any read failure counts as "not occupied" rather than being classified. This
// is deliberate and portability-driven: TinyGo under wasip2 returns a raw WASI
// errno that `os.IsNotExist` / `errors.Is(err, fs.ErrNotExist)` do not match, so
// classifying here made the engine behave differently native vs wasm (caught by
// the parity check). Nothing is lost — a genuine permission or I/O problem still
// surfaces from writeTree a moment later, with a better message.
func dirIsOccupied(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	return len(entries) > 0
}
