// The embed must not be empty. A silently-empty FS turns every `dlc new` into a
// confusing "file does not exist" at run time, on whichever tier was built
// wrong — and it has already happened once, in CI only, where the wasm build
// shipped no templates while the native build had them.
package templates

import (
	"io/fs"
	"strings"
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

// The scaffolded environment must be able to run the scaffolded codegen.
//
// WHY THIS EXISTS. `dlc new` prints "cd <app> && devbox shell / make gen" as its
// very first instruction, and that failed: the template's buf.gen.yaml calls
// protoc-gen-es-lite for the web tier, and the template's devbox.json installed
// protoc-gen-go-lite and nothing else. A brand-new project could not generate.
//
// It survived because every existing check runs a scaffolded project's `make
// gen` from inside the REPO's devbox shell, where the plugin is already on PATH
// — so the scaffold's own declared environment, which is the one a real user is
// in, was never exercised. Same blind spot verify-platform-gen.sh exists for:
// only broken from outside, therefore invisible from inside.
//
// A STATIC invariant rather than actually running devbox, deliberately: it is
// instant, it needs no nix inside a test, and it encodes the exact failure —
// a plugin the codegen names that the environment does not supply. A full
// "boot the scaffold's own environment" check would catch more (a wrong
// version, a broken hook) and costs minutes; worth adding the day that class of
// failure actually happens.
func TestTemplateEnvironmentSuppliesItsCodegenPlugins(t *testing.T) {
	gen, err := fs.ReadFile(FS, Root+"/proto/buf.gen.yaml.tmpl")
	if err != nil {
		t.Fatalf("reading the template buf.gen.yaml: %v", err)
	}
	env, err := fs.ReadFile(FS, Root+"/devbox.json.tmpl")
	if err != nil {
		t.Fatalf("reading the template devbox.json: %v", err)
	}

	for _, line := range strings.Split(string(gen), "\n") {
		line = strings.TrimSpace(line)
		name, ok := strings.CutPrefix(line, "- local: ")
		if !ok {
			continue
		}
		// The list form is `["go", "run", …]` — a plugin built from source, which
		// needs Go and nothing else. Only the scalar form names a binary that has
		// to already exist on PATH.
		if strings.HasPrefix(name, "[") {
			continue
		}
		if !strings.Contains(string(env), name) {
			t.Errorf("buf.gen.yaml runs %q but devbox.json never installs it — "+
				"a scaffolded project cannot run `make gen`", name)
		}
	}
}
