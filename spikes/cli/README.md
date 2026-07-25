# Spike 4 — in-engine CLI interpreter (T-B1.4) · ✅ GREEN

Findings (table + Decision 22/25 picks) live in [`../README.md`](../README.md) Spike 4.

**Question answered:** a real subcommand + flag parser runs **inside** the TinyGo engine; the host is
just an argv forwarder. Scaffolder default is **ffcli**; measured per-ABI fall-backs are hand-rolled
(portable size) and go-arg (rich struct tags).

## Layout

| File | Role |
| --- | --- |
| `main.go` | WIT wiring: `execute-cli(args)` → `dispatch` → `command-result` |
| `cmds.go` | Shared `formatGreet` / `formatCount` / `formatHostAdd` |
| `parse_flag.go` | stdlib `flag` (default) |
| `parse_ffcli.go` | `-tags cliffcli` — `ff/v3/ffcli` |
| `parse_hand.go` | `-tags clihand` — hand-rolled |
| `parse_sub.go` | `-tags clisub` — `google/subcommands` |
| `parse_cobra.go` | `-tags clicobra` — cobra |
| `parse_kong.go` | `-tags clikong` — kong |
| `parse_goarg.go` | `-tags cligoarg` — go-arg |
| `harness.mjs` | 17-case matrix (same expects every variant) |

## Run

```bash
devbox run make spike-cli    # full bake-off table; B1 gate = ≥1 lean green
# or: make test-b1
```

## Decision output

- **Scaffolder default: `ff/v3/ffcli`** — until Decision 25 forces a split.
- **Portable fall-back:** hand-rolled (~497 KiB wasip1).
- **Rich fall-back:** go-arg (struct tags, matrix-green).

See parent README for the full bake-off table (wasip2 + wasip1 sizes) and gotchas (kong `MethodByName`,
cobra `-name`, subcommands `os.Args`).
