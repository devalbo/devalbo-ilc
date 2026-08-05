// Package templates carries what `dlc new` emits, compiled into the binary.
//
// Embedded rather than read from disk for two reasons: scaffolding must work
// offline (no runtime clone — §16.6), and it must work in the BROWSER, where
// there is no source tree to copy from. The same embedded bytes serve both, so
// the terminal and the web tier cannot scaffold different projects.
//
// `all:` includes dotfiles — without it, embed silently skips `.gitignore.tmpl`,
// and the scaffold would be missing a file nobody notices until git adds `gen/`.
//
// EVERY file under component-model/ ends in `.tmpl`, and the renderer strips it.
// One uniform rule, no exceptions, because the alternatives all leak:
//   - a template file named `go.mod` makes this directory a nested MODULE, and
//     embed refuses it outright ("in different module")
//   - template `.go` files are not valid Go (they carry {{.Tokens}}), so
//     `go build ./...`, `go vet`, and gopls all choke on them
//   - an `_`-prefixed directory hides them from `go build` but NOT from
//     `gofmt -l .`, and does nothing about the module boundary
//
// Suffixing everything means no Go tool ever looks inside, and nobody has to
// remember which files are special. TestTemplateFilesAreSuffixed enforces it.
package templates

import "embed"

//go:embed all:component-model
var FS embed.FS

// Root is the directory inside FS holding the skeleton. There is one: every
// tier runs the same wasip2 component, so a tier adds a host, never a second
// project shape (Decision 25).
const Root = "component-model"
