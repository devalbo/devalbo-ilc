package platform

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

// File is one file to write: a root-relative path and its bytes. The neutral
// shape apps hand to WriteTree — the platform must not know about an app's
// template types.
type File struct {
	Path    string
	Content []byte
}

// WriteTree writes files under root, creating parent directories as needed.
// Order follows the slice, which keeps native and wasm runs byte-comparable.
// Every path goes through SafeJoin: apps get containment by using this rather
// than by remembering to check.
func WriteTree(root string, files []File) error {
	for _, f := range files {
		dest, err := SafeJoin(root, f.Path)
		if err != nil {
			return err
		}
		if dir := filepath.Dir(dest); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		if err := os.WriteFile(dest, f.Content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// SafeJoin resolves a template-relative path under root, rejecting anything that
// would escape it. Template paths are ours today, but `import-fs` will accept
// bundles from elsewhere and go through the same door — so the check belongs
// here, not at the call site.
func SafeJoin(root, path string) (string, error) {
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

// ReadTree loads everything under root into a BFT tree. Directory entries are
// read in whatever order the filesystem reports; encodeBFT sorts, so the bundle
// is deterministic regardless.
func ReadTree(root string) (*bftNode, error) {
	node := newDir()
	if err := readInto(root, "", node); err != nil {
		return nil, err
	}
	return node, nil
}

func readInto(dir, prefix string, node *bftNode) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(dir, name)
		if entry.IsDir() {
			child := newDir()
			node.entries[name] = child
			if err := readInto(path, filepath.Join(prefix, name), child); err != nil {
				return err
			}
			continue
		}
		// BFT models exactly three things: directories, text, and binary. There
		// is no node type for a symlink, socket, or device — and reading one can
		// fail outright (a symlink to a directory yields "is a directory"), which
		// would make export fragile on any real tree. Skip them.
		//
		// This is lossy, and knowingly so: it is the difference between "export
		// works" and "export dies on the first symlink". Revisit if BFT grows a
		// symlink node; until then a bundle is a tree of plain files.
		if entry.Type() != 0 {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		node.entries[name] = &bftNode{content: content}
	}
	return nil
}

// writeBFTTree writes every file in a bundle under root. Each path goes through
// safeJoin, because a bundle is untrusted input — it may have been edited by
// hand or arrived from another machine, and `../` in a path is the obvious
// attack on "import this into my project".
func writeBFTTree(root string, node *bftNode) ([]string, error) {
	var written []string
	err := node.walk("", func(path string, content []byte) error {
		dest, err := SafeJoin(root, path)
		if err != nil {
			return err
		}
		if dir := filepath.Dir(dest); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		if err := os.WriteFile(dest, content, 0o644); err != nil {
			return err
		}
		written = append(written, path)
		return nil
	})
	return written, err
}

// DirIsOccupied reports whether root already exists with anything in it. Refusing
// to scaffold over an existing tree is deliberate: `new` must never silently
// overwrite a user's work.
//
// Any read failure counts as "not occupied" rather than being classified. This
// is deliberate and portability-driven: TinyGo under wasip2 returns a raw WASI
// errno that `os.IsNotExist` / `errors.Is(err, fs.ErrNotExist)` do not match, so
// classifying here made the engine behave differently native vs wasm (caught by
// the parity check). Nothing is lost — a genuine permission or I/O problem still
// surfaces from writeTree a moment later, with a better message.
func DirIsOccupied(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// ---- per-file access for app handlers ------------------------------------
//
// Added because App #2 needed them: an app storing one file per record has to
// read, list, and delete individual files, and WriteTree/ReadTree only speak in
// whole trees. These live here rather than in each app for the same reason
// SafeJoin does — every one of them is a path-containment decision, and an app
// that reached for `os` directly would be one `../` away from writing outside
// its root.
//
// All paths are relative to the host-bound root (Root()), so an app never
// composes an absolute path and never learns which tier it is on.

// ReadFile reads one file under the root.
func ReadFile(path string) ([]byte, error) {
	full, err := SafeJoin(Root(), path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

// WriteFile writes one file under the root, creating parents.
func WriteFile(path string, content []byte) error {
	return WriteTree(Root(), []File{{Path: path, Content: content}})
}

// ListDir returns the entry names directly under path, sorted.
//
// Sorted because the caller's output crosses the parity check: unsorted
// directory order differs between filesystems, and native-vs-wasm would diverge
// on nothing more than that.
func ListDir(path string) ([]string, error) {
	full, err := SafeJoin(Root(), path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sortStrings(names)
	return names, nil
}

// RemoveFile deletes one file, reporting whether it was there.
//
// "Not there" is not an error — deleting is idempotent. Note what this does NOT
// do: classify the error with os.IsNotExist, which does not match TinyGo's WASI
// errno. It re-stats instead, which is portable.
func RemoveFile(path string) (bool, error) {
	full, err := SafeJoin(Root(), path)
	if err != nil {
		return false, err
	}
	if err := os.Remove(full); err != nil {
		if _, statErr := os.Stat(full); statErr != nil {
			return false, nil // gone already
		}
		return false, err
	}
	return true, nil
}
