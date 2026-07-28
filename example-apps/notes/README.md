# notes

Scaffolded by `dlc new`. An **ILC** app: your business logic lives once, in
`engine/`, and every tier drives it through the same command boundary.

```bash
devbox shell        # provision go + buf
make gen            # proto → Go messages + command dispatch
make verify         # build and smoke-test the CLI
./notes                              # the generated command list
./notes create --title "Buy milk"    # write a note
./notes list
```

## Layout

| Path | What |
| --- | --- |
| `engine/` | **all** business logic; portable (native + wasm). Reflection-free. |
| `hosts/native/` | this tier's SLOT: how a response prints. The command surface is generated. |
| `proto/` | your command surface — one rpc per command, permanent `method_id` |
| `gen/` | generated; never edit, never commit |

See [`AGENTS.md`](AGENTS.md) for the rules that are not visible in the code — where logic goes, method-id
bands, and the portability constraints the engine has to keep.

## Adding a command

1. Add an `rpc` to `proto/notes/v1/commands.proto` with the next free
   `method_id` **at or above 10000**. Everything below is reserved for ILC itself,
   including capabilities it has not shipped yet.
2. `make gen` — the id constant and dispatch entry are generated.
3. Write the handler in `engine/` and add it to `NotesServiceHandlers(...)`.
4. Add a renderer for it in `hosts/native/main.go` — the ONLY per-command code
   a host writes. The subcommand, its flags, which are required and the `-h`
   text all come from the `.proto` (Decision 29), so there is no `switch` to
   update and no usage string to forget.

The CLI shape is declarable in the schema too: `(cli_name)` renames a
subcommand, `(cli_flag)` renames a flag, `(cli_source)` says a value comes from a
file or stdin, and an rpc's doc comment becomes its `-h` summary. All cosmetic —
dispatch is on `method_id` and encoding is by field number.

You never write a `method_id` in Go. `proto/method-ids.lock` is committed and
fails the build if an id ever changes — the id *is* the wire.

## What you inherit

`version`, `export-fs`, `import-fs`, `reset-fs` come from the ILC platform, along
with command dispatch, the filesystem root seam, and path containment. They are a
**dependency, not a copy** — upstream fixes arrive on a version bump.
