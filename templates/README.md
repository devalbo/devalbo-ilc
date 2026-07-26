# templates/ — what `dlc new` emits (its own concern)

## Every file declares its intent with a suffix

| Suffix | Renderer does | For |
| --- | --- | --- |
| `.tmpl` | substitutes tokens in the content (must be UTF-8) | source, config, docs |
| `.raw` | copies the bytes verbatim | binary assets, or text that must keep literal `{{braces}}` |

The suffix is stripped on the way out, so `go.mod.tmpl` lands as `go.mod`. **Paths are substituted
either way**, so a `.raw` asset can still live under `proto/{{.PkgName}}/`.

Declared, not sniffed: guessing text-vs-binary from content works until it doesn't, and the failure —
a silently corrupted asset in someone's new project — is invisible and hard to trace. A `.tmpl` file
that isn't valid UTF-8 is a render-time error telling you to mark it `.raw`.

The suffix also keeps the Go tool out, which it must, because template files are **not** the files they
produce — they carry `{{.Tokens}}`, and without a suffix each kind breaks a different tool in a way that
never points back at the template:

| Without `.tmpl` | What breaks |
| --- | --- |
| `go.mod` | this directory becomes a nested **module**; `go:embed` refuses it (*"in different module"*) |
| `*.go` | not valid Go, so `go build ./...`, `go vet`, and gopls all fail |
| anything | `gofmt -l .` walks in and reports parse errors |

An `_`-prefixed directory was tried and is **not** sufficient: it hides files from `go build` but not
from `gofmt`, and does nothing about the module boundary. Suffixing everything means no Go tool ever
looks inside. `TestTemplateFilesAreSuffixed` enforces the rule.

## Tokens come from the command

The dictionary is built from the `dlc new` **request** — `scaffoldVars()` in `engine/commands.go` is the
single table mapping command input to template input. Adding an option is three steps:

1. add the field to `NewRequest` in `proto/devalbo/dlc/v1/commands.proto`
2. add one line to `scaffoldVars()`
3. use `{{YourToken}}` in any template

No second schema for the scaffolder to drift from: what the command accepts *is* what templates can see.

| Token | From |
| --- | --- |
| `{{AppName}}` | `NewRequest.name`, verbatim |
| `{{Module}}` | `NewRequest.module`, defaulted to `github.com/you/<app>` |
| `{{PkgName}}` | derived — identifier-safe (`my-app` → `my_app`); proto packages and Go imports cannot contain dashes |
| `{{PlatformReplace}}` | derived from `--platform-path` (bootstrap `replace` line) |

`{{Name}}`, `{{.Name}}` and `{{ .Name }}` are the same token — a formatting slip is not a silent miss.

**An unknown token is a render-time error**, not something that ships. `{{.AppNam}}` would otherwise land
in a user's project and fail much later, far from the template that caused it; instead rendering refuses
and names both the bad token and the available ones. Derivations live in Go, not in templates, so the
templates stay logic-free and the derivation stays testable.

## Layout

(§16.6, Decision 25) — directory names = **substrate**; track prose = Rich/CM vs Portable/WAMR:

| Path | Kind | Role |
| --- | --- | --- |
| `templates/component-model/` | **in-tree first** (submodule later) | Rich/CM skeleton. **Terminal path works today; the web host is not written yet.** |
| `templates/wamr/` | 📋 not created | Portable/WAMR — only after embedded verify exists |
| `templates/fragments/` | 📋 not created | `--caps` / `--tiers` / `--ui` / `--storage` overlays |

**Bootstrap sequencing (locked):**
1. Author skeletons **in-tree**; lift to per-skeleton git submodules later.
2. **Defer** versioned `ilc-platform` `go.mod` depends until that submodule graduation. Until then
   `dlc new --platform-path` writes a `replace` directive, clearly marked in the generated `go.mod`.
3. `component-model/` is a **full `dlc`-shaped app**, not a thin hello-world.
4. Template trees are **`go:embed`'d into the engine** so `dlc new` works offline *and* in the
   browser. Never runtime-`git clone`.

**Note:** templates are compiled into the binary, so **editing a template requires rebuilding `dlc`**
before the change takes effect.

**Rules:**
- **Depend-on, never inline.** The scaffold requires `ilc-platform` as a module; it never copies
  platform code, so upstream fixes arrive on a version bump instead of being frozen at scaffold time.
  The one deliberate exception is `proto/devalbo/options/v1/options.proto`, vendored because a
  generated project has no other way to resolve `method_id` until the platform is published — kept a
  byte-copy and guarded by `TestTemplateOptionsProtoInSync`.
- **Validated by** `make verify-scaffold`: scaffold → generate → test → build → run (test-steps
  Scaffolder row). A template that does not compile fails there, not on a user's laptop.

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §5.4, §16.6 and [`WASI-UPGRADES.md`](../docs/WASI-UPGRADES.md).
