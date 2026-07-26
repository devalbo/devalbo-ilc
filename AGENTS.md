# AGENTS.md — rules for working on `dlc` / ILC

For anyone (human or agent) changing this repo. These are the rules that are **not** discoverable by
reading the code — each one exists because breaking it fails somewhere far from the cause.

Architecture and rationale live in [`docs/DEVALBO-ILC-GO-PLAN.md`](docs/DEVALBO-ILC-GO-PLAN.md); current
state lives in [`README.md`](README.md). This file is only the rules.

---

## 1. Method ids

**Reserve ids in the `.proto`, never in a comment.** A comment is a hope; a declaration is checked.

```proto
service PlatformService {
  option (devalbo.options.v1.reserved_method_id) = 2;   // held for SetEnvironment
}
```

`protoc-gen-dlc-registry` fails the build if an rpc claims a reserved id, and records reservations in the
committed `proto/method-ids.lock`. Proto's own `reserved` keyword covers field numbers only — it does not
reach custom-option values, which is why this option exists.

**Never write an id in Go or TypeScript.** They are generated (`*.registry.pb.go`, `*.registry.pb.ts`).
Hand-mirroring is how an id ends up living in two places that silently disagree.

**Bands are permanent** (Decision 32 / `engine/platform/registry.go`):

| Range | Owner |
| --- | --- |
| 1 – 9999 | ILC, subdivided by capability (600–9999 held for capabilities not yet shipped) |
| 10000 + | the app — including `dlc`, which claims no privileged block |

**Changing an id is a breaking change.** `buf breaking` cannot see it (it validates message wire compat,
not an option's *value*), so the lock is the only guard. Re-bless deliberately:
`DLC_ID_LOCK_UPDATE=1 make gen`.

## 2. The engine

**One entry point:** `execute(method, request)`. Do not add a second boundary. Capability facts arrive as
*data* on this boundary (the environment manifest, §6.4a) rather than as new WIT imports.

**Reflection-free and TinyGo-safe.** No `encoding/json`, no `text/template`, no `reflect`. The engine
compiles to wasm for the browser and eventually to embedded targets; a reflection-heavy dependency does
not merely bloat, it fails to build. Generics are fine and are how the typed-handler adapter works.

**Never `os.WriteFile` a caller-supplied path.** Use `platform.SafeJoin` / `platform.WriteTree`, so path
containment is inherited rather than re-implemented per command.

**Portability traps already paid for** — do not re-introduce:
- `os.IsNotExist` / `errors.Is(err, fs.ErrNotExist)` do **not** match TinyGo's WASI errno
- `os.RemoveAll` fails under wasip2 (errno 52) even on a plain file — walk with `ReadDir` + `os.Remove`
- WASI has no working directory; anchor paths at `platform.Root()`

## 3. The platform boundary

`engine/platform/` is what every app **inherits**; `engine/` is `dlc`'s own app code. It becomes the
`ilc-platform` module, so:

**Templates depend on the platform; they never inline it.** Code copied into a scaffold is frozen there
forever — a path-containment fix inlined into a template could never reach an already-generated app. The
one deliberate exception is `options.proto`, vendored because a generated project cannot otherwise resolve
`method_id`; it is a byte-copy guarded by a test.

**`dlc` is an app like any other.** If `dlc` needs special treatment, the template is teaching something
`dlc` does not do.

## 4. Templates

**Every file under `templates/` ends in `.tmpl` (substituted) or `.raw` (verbatim).** Without a suffix:
a file named `go.mod` makes the directory a nested module and `go:embed` refuses it; `.go` files are not
valid Go and break `go build ./...`, `go vet`, and gopls; and `gofmt -l .` reports parse errors. An
`_`-prefixed directory is **not** sufficient — it hides files from the build but not from gofmt.

**Tokens come from the command.** `scaffoldVars()` maps `NewRequest` fields to template variables; an
unknown token is a render-time error, not a passthrough.

**Templates are embedded in the binary** — editing one requires rebuilding `dlc` before it takes effect.

## 5. Verification

**Every claim needs a check, and every check needs to be falsifiable.** `verify-parity-selftest.sh`
exists because a parity check nobody has seen fail is indistinguishable from a broken one. When you add a
check, break something on purpose and confirm it goes red.

**Prefer invariants to exhaustive lists.** Pinning every scaffold path made three consecutive template
additions look like regressions. Assert what must be true.

**`./scripts/ci.sh full` is what CI runs and what you run.** Nothing in the suites may know about a CI
provider; `.github/workflows/ci.yml` is a thin adapter.

## 6. Working style

**Run the toolchain through devbox** — `devbox run -- make …`, or work inside `devbox shell`.

**Do not run `git` in this repo.** The maintainer runs all git commands. Suggest them instead.

**Never `go build ./cmd/x` bare.** Go writes the executable into the current directory, named after the
package folder — that is how `engine.test`, `protoc-gen-dlc-registry`, and `native` each turned up at the
repo root, two of them inside a commit. Use `go build -o "$(mktemp -d)/x" ./cmd/x`, or `go vet` when you
only want to know it compiles. The `.gitignore` has an entry per main package as a backstop, but the
backstop only works for names someone remembered to add.

**Generated code is not committed** (`/gen/` is ignored) — but the **id lock is**. Note that publishing
`ilc-platform` will require committing its generated proto code, since consumers cannot run `buf`.
