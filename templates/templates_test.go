// The embed must not be empty. A silently-empty FS turns every `dlc new` into a
// confusing "file does not exist" at run time, on whichever tier was built
// wrong — and it has already happened once, in CI only, where the wasm build
// shipped no templates while the native build had them.
package templates

import (
	"io/fs"
	"testing"
)

func TestEmbedIsPopulated(t *testing.T) {
	var files int
	var sawDotfile bool
	err := fs.WalkDir(FS, Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		files++
		if d.Name() == ".gitignore.tmpl" {
			sawDotfile = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded templates: %v", err)
	}
	if files < 20 {
		t.Errorf("only %d template files embedded — expected the full skeleton", files)
	}
	// The `all:` prefix is what includes dotfiles; without it .gitignore.tmpl
	// vanishes and a scaffolded project silently commits gen/.
	if !sawDotfile {
		t.Error(".gitignore.tmpl missing — the go:embed `all:` prefix is not taking effect")
	}
}
