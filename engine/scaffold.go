package engine

// Scaffold rendering — the engine-logic half of `dlc new`. Writing the tree to
// disk lands with the filesystem capability seam (import-fs → write tree, §7.3);
// everything here is pure, so it behaves identically native and in wasm.

import (
	"bytes"
	"strconv"
	"strings"
)

// templateFile is one file in the scaffold. A plain slice (not a map) keeps the
// output order deterministic — important for the native↔wasm parity check.
type templateFile struct {
	path    string
	content string
}

// scaffoldTemplates is the minimal self-shaped skeleton dlc emits. Tokens
// {{.AppName}} / {{.Module}} are substituted by scaffoldFiles. This is the
// bootstrap in-tree template; it grows into templates/component-model/ (§16.6).
var scaffoldTemplates = []templateFile{
	{"go.mod", "module {{.Module}}\n\ngo 1.23.0\n"},
	{"engine/execute.go", "package engine\n\n// {{.AppName}} engine, scaffolded by dlc.\nfunc Execute(args []string) string {\n\treturn \"hello from {{.AppName}}\"\n}\n"},
	{"README.md", "# {{.AppName}}\n\nScaffolded by `dlc new`. Module `{{.Module}}`.\n"},
}

// defaultModule is the module path `dlc new` assumes when --module is omitted.
func defaultModule(app string) string { return "github.com/you/" + app }

// scaffoldFiles renders every template file for the app. Both the proto `new`
// handler and the argv shim go through it, so the two paths cannot drift.
func scaffoldFiles(app, module string) []templateFile {
	files := make([]templateFile, 0, len(scaffoldTemplates))
	for _, t := range scaffoldTemplates {
		files = append(files, templateFile{t.path, substitute(t.content, app, module)})
	}
	return files
}

// renderScaffold formats the human-readable manifest the argv shim prints.
func renderScaffold(app, module string) []byte {
	var b bytes.Buffer
	b.WriteString("scaffold " + app + " (module " + module + ")\n")
	for _, f := range scaffoldFiles(app, module) {
		b.WriteString("  + " + f.path + " (" + strconv.Itoa(len(f.content)) + " bytes)\n")
	}
	b.WriteString("note: rendered in-memory; file writing lands with the filesystem capability\n")
	return b.Bytes()
}

// substitute is the reflection-free stand-in for text/template — safe under
// TinyGo, and enough for the two scaffold tokens.
func substitute(s, app, module string) string {
	s = strings.ReplaceAll(s, "{{.AppName}}", app)
	s = strings.ReplaceAll(s, "{{.Module}}", module)
	return s
}
