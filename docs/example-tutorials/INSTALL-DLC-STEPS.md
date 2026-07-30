# Install `dlc`

**Do this once.** After it, you build ILC apps without thinking about the framework again — with two
exceptions this document names honestly at the end, because pretending otherwise would waste your afternoon.

`dlc` is the ILC command-line tool: it scaffolds projects, runs codegen, and builds the web tier. It is also
an ILC app itself, which is why installing it looks like building one.

---

## 1. System prerequisites

Two things you install yourself:

| Tool | Why |
| --- | --- |
| **git** | to clone the repository |
| **[devbox](https://www.jetify.com/docs/devbox/installing-devbox)** | provisions everything else at pinned versions |

Optional: **direnv**, which activates the environment when you `cd` into the directory instead of you running
`devbox shell`.

**Everything else is provisioned for you** — Go, TinyGo, buf, Node, wasmtime, `wit-bindgen-go`, `wasm-tools`,
`jco`, and the two protobuf codegen plugins. You never install those by hand, and you should not try: the
versions are pinned together and a system Go of the wrong vintage is a confusing afternoon.

---

## 2. Clone and enter the environment

```bash
git clone <this repository> devalbo-ilc
cd devalbo-ilc
devbox shell
```

**The first `devbox shell` is slow** — it installs Nix if you do not have it, then downloads the toolchain.
Later runs are fast.

### Check it

```bash
devbox run doctor
```

That prints three groups: system prerequisites you own, the Nix layer devbox manages, and the pinned toolchain.
Inside a devbox shell every tool in the third group must show a version; outside one, `✗` is expected and the
script says so. Fix anything in the first group before continuing — the rest cascades from it.

---

## 3. Build and install the tool

Pick a directory on your `PATH` that survives reboots. `~/.local/bin` is a common choice:

```bash
mkdir -p ~/.local/bin
go build -buildvcs=false -o ~/.local/bin/dlc ./hosts/native
```

**Use `-o …/dlc` and not `go install ./hosts/native`.** Go names an installed binary after its package
*directory*, so `go install` would give you a command called `native`. The `-o` is not stylistic.

Make sure the directory is on your `PATH` — add this to your shell profile if it is not:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Check it

```bash
dlc --help
```

You should see the command list:

```
SUBCOMMANDS
  echo       Echo the words back, through the engine
  export-fs  Bundle a subtree into one portable blob
  import-fs  Write a bundle back into the filesystem
  new        Scaffold a new ILC project
  reset-fs   Delete everything under a prefix
  version    Every app wants a version command; the string itself is app-supplied
```

Those are not hand-written subcommands. `dlc`'s command line is **generated from its own `.proto`**, which is
the same mechanism your apps will use — the tool eats its own cooking, which is why a rough edge in your app's
CLI is usually a rough edge in `dlc`'s.

---

## 4. Confirm it can actually make something

Scaffold a throwaway project outside the repository:

```bash
cd /tmp
dlc new hello --tiers native --tiers web \
  --module github.com/you/hello \
  --platform-path /path/to/devalbo-ilc
cd hello
devbox shell                       # the project ships its own, and it is small
make gen && go mod tidy && make verify
```

You should end with:

```
hello 0.1.0
hello, ILC — from hello
```

**That is the installation working**, and it exercised more than you might think: the generated command
surface, the protobuf toolchain, the project's own devbox environment, and a Go build of an engine plus a
native host.

Then clean up: `cd /tmp && rm -rf hello`.

---

## 5. What "installed" means here

Worth being precise, because `dlc` is not only a scaffolder:

- **Every app needs `dlc` on `PATH` at build time**, not just when created. An app's `make gen` runs
  `dlc gen`, and `make build-web` runs `dlc build web`. It is a build dependency, like `buf`.
- **Apps get their own devbox environment.** `dlc new` writes a `devbox.json` into the project, so an app can
  be built without the framework's shell. You still need `dlc` itself on `PATH`.
- **Your app does not depend on the `dlc` Go module.** It depends on `dlc-platform`, which is a separate
  module. That separation is checked: the scaffold verification asserts an app's module graph contains the
  platform and not `dlc`, and builds it with the module proxy disabled so a dependency that only resolves by
  fetching cannot hide.

---

## 6. Two things that are not yet install-once

The goal is that you install `dlc` and stop thinking about the framework. Two gaps stand between here and
there, and neither is hidden: **both are the subject of
[`DLC-PACKAGING-PLAN.md`](../DLC-PACKAGING-PLAN.md)**, which is where the packaging and publishing story
lives — the phases, what each one changes about this page, and what is deliberately out of scope.

The short version of where that plan is going, so you know what this section will look like when it lands:

| | today | after the plan |
| --- | --- | --- |
| getting `dlc` | `go build` from this repo into `~/.local/bin` (§3) | a project's own `devbox shell` provisions it (plan Phase 2) |
| creating a project | `--platform-path /path/to/devalbo-ilc` | nothing — the modules are published (plan Phase 3) |
| keeping the checkout | required, permanently | not required |

Until then, the two gaps in full:

**1. Nothing is published, so new projects need `--platform-path`.**

`dlc-platform` (Go) and `@devalbo/dlc-web` (npm) are not released yet, so a scaffolded project resolves them
from your local checkout: a `replace` directive in `go.mod` and a `file:` dependency in `package.json`. That is
what `--platform-path` writes, and it is why you pass a path to the repository when creating a project. When
the packages are published, the flag stops being necessary and existing projects change one line.

**Practical consequence:** do not delete or move the repository after installing. `dlc` is a self-contained
binary, but your *projects* point at that directory.

**2. If you change the framework, rebuild `dlc`.**

`dlc` embeds its project templates in the binary. Editing anything under `templates/` has no effect until you
re-run the `go build` from §3. This only matters if you are working *on* ILC rather than *with* it — but when
it matters, the symptom is baffling: a scaffold that ignores your change.

```bash
go build -buildvcs=false -o ~/.local/bin/dlc ./hosts/native   # after any framework change
```

**This one does not go away with packaging, and the plan says so.** Even once a project's shell provisions
`dlc` from a pinned release, a framework change you made locally is not in that release — so working *on*
ILC still means building the binary yourself. Packaging fixes the story for people using ILC, not for people
changing it.

---

## 7. Troubleshooting

| Symptom | Cause |
| --- | --- |
| `dlc: command not found` | the install directory is not on `PATH` (§3) |
| a command called `native` appeared | you ran `go install` instead of `go build -o …/dlc` (§3) |
| `protoc-gen-es-lite: executable file not found` | you are outside a devbox shell — either the framework's or the project's |
| `devbox run doctor` shows `✗` for the pinned tools | expected outside `devbox shell`; run it inside |
| `tier "native,web" is not supported yet` | `--tiers` is repeatable, not comma-separated: `--tiers native --tiers web` |
| `go.mod` `replace` points at a missing directory | the repository moved after you scaffolded; fix the path in `dlc.toml` and `go.mod` |
| the first `devbox shell` seems to hang | it is installing Nix and a toolchain. Give it a few minutes, once |

---

## Next

**[Build tic-tac-toe](./TIC-TAC-TOE-TUTORIAL.md)** — one engine, a terminal board and a browser board, in
about an hour. It assumes `dlc` is installed and never mentions it again.

**[`DLC-PACKAGING-PLAN.md`](../DLC-PACKAGING-PLAN.md)** — if the two gaps in §6 are what brought you here,
that is where they are being fixed.
