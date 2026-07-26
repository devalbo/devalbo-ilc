package engine

// Scaffold rendering — turning the embedded template tree into an app's files.
// Rendering is pure; writing is platform.WriteTree, and `scaffold` in commands.go
// composes the two.
//
// Substitution is plain string replacement, not text/template: the engine must
// stay reflection-free for TinyGo (text/template is the canonical offender), and
// two tokens do not justify a template engine. It applies to PATHS as well as
// contents, so `proto/{{.PkgName}}/v1/` lands under the app's own package name.

import (
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/devalbo/devalbo-ilc/engine/platform"
	"github.com/devalbo/devalbo-ilc/templates"
)

// Template file suffixes. Every file under templates/ carries exactly one, and
// it declares what the renderer should DO with the bytes:
//
//	.tmpl   substitute tokens in the content (must be UTF-8 text)
//	.raw    copy verbatim — binary assets, or text that must keep its {{braces}}
//
// Declared rather than sniffed on purpose. Guessing text-vs-binary from content
// works until the day it doesn't, and the failure — a silently corrupted asset
// in someone's new project — is both invisible and hard to trace. Paths are
// substituted either way, so a `.raw` asset can still live under
// `proto/{{.PkgName}}/`.
const (
	suffixTemplated = ".tmpl"
	suffixVerbatim  = ".raw"
)

// scaffoldFiles renders the whole embedded template tree for an app.
func scaffoldFiles(vars map[string]string) ([]platform.File, error) {
	var files []platform.File

	err := fs.WalkDir(templates.FS, templates.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		content, err := fs.ReadFile(templates.FS, path)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, templates.Root+"/")

		var out []byte
		switch {
		case strings.HasSuffix(rel, suffixTemplated):
			rel = strings.TrimSuffix(rel, suffixTemplated)
			if !utf8.Valid(content) {
				// A binary file marked .tmpl would be mangled by substitution.
				// Refuse instead of shipping corruption.
				return errors.New(rel + ": not UTF-8 text; mark binary assets " + suffixVerbatim)
			}
			rendered, err := render(string(content), vars)
			if err != nil {
				return errors.New(rel + ": " + err.Error())
			}
			out = []byte(rendered)
		case strings.HasSuffix(rel, suffixVerbatim):
			rel = strings.TrimSuffix(rel, suffixVerbatim)
			out = content
		default:
			return errors.New(rel + ": template files must end in " + suffixTemplated + " or " + suffixVerbatim)
		}

		// Paths go through the same renderer — that is how `proto/{{.PkgName}}/v1/`
		// lands under the app's own package name, and it means a bad token in a
		// FILENAME fails just as loudly as one in a file.
		renderedPath, err := render(rel, vars)
		if err != nil {
			return errors.New(rel + ": in path: " + err.Error())
		}
		files = append(files, platform.File{Path: renderedPath, Content: out})
		return nil
	})
	if err != nil {
		return nil, err
	}
	// WalkDir is lexical, so this is already deterministic — but the parity
	// check compares written trees byte-for-byte across native and wasm, and
	// relying on an implementation detail for that is not worth the risk.
	sortFiles(files)
	return files, nil
}

// render substitutes `{{Name}}` from the dictionary — a mustache subset: no
// logic, no loops, no reflection.
//
// An UNKNOWN token is an error, never a passthrough. That is the whole point of
// the dictionary: a typo like {{.AppNam}} would otherwise ship literally into a
// user's project and surface as a baffling compile error in a file they did not
// write. Failing here names the file, the token, and what was available.
//
// `{{Name}}`, `{{.Name}}` and `{{ .Name }}` are the same token — normalizing
// whitespace and an optional leading dot means a formatting slip is not a silent
// miss. Everything else is strict.
func render(src string, vars map[string]string) (string, error) {
	var b strings.Builder
	rest := src
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			b.WriteString(rest)
			return b.String(), nil
		}
		b.WriteString(rest[:open])
		rest = rest[open+2:]

		close := strings.Index(rest, "}}")
		if close < 0 {
			return "", errors.New("unterminated {{ near " + snippet(rest))
		}
		key := strings.TrimPrefix(strings.TrimSpace(rest[:close]), ".")
		rest = rest[close+2:]

		value, ok := vars[key]
		if !ok {
			return "", errors.New("unknown token {{" + key + "}} (known: " + knownKeys(vars) + ")")
		}
		b.WriteString(value)
	}
}

// knownKeys lists the dictionary, sorted, for the error message — the fix for a
// bad token is almost always visible in the list of good ones.
func knownKeys(vars map[string]string) string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return strings.Join(keys, ", ")
}

func snippet(s string) string {
	if len(s) > 30 {
		s = s[:30]
	}
	return strconv.Quote(s)
}

func sortFiles(files []platform.File) {
	for i := 1; i < len(files); i++ {
		for j := i; j > 0 && files[j].Path < files[j-1].Path; j-- {
			files[j], files[j-1] = files[j-1], files[j]
		}
	}
}

// defaultModule is the module path `dlc new` assumes when --module is omitted.
func defaultModule(app string) string { return "github.com/you/" + app }
