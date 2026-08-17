# Getting a payload onto a badge — implementation plan

**Status: PROPOSED 2026-08-16.** Nothing here is built. Written in the shape of
[`EMBEDDED-PLAN.md`](./EMBEDDED-PLAN.md): design decisions first, phases that each leave the tree green,
and nothing claimed until it has been broken on purpose.

How a person puts an app on a badge **without reflashing the world** — and why that is two transports
sharing one ingest path rather than two features.

---

## 0. Where this starts

The payload region already works. `make badge-payload` produces a `data`-family UF2 targeting
`0x10400000`; the firmware lives at `0x10000000` and the two never overlap, so flashing one leaves the
other untouched. That was verified on hardware on 2026-08-16: firmware and payload were loaded
separately, in one BOOTSEL session, and the badge ran the new app.

So the *storage* half is done. The catalog (`dlc-platform/embedded/src/catalog.rs`) records name, length,
entry method, checksum, flags and — as of the same day — an engine tag, and terminates on a bad magic so
images append without rewriting live sectors.

**What is missing is a way in that does not involve BOOTSEL.** Today's route costs: hold BOOT, tap RESET,
`picotool load`, reboot. That is fine for firmware development and wrong for the thing this tier is
supposed to enable — handing someone a badge and a file.

Two routes are wanted, and they arrived as separate requests:

| | what the user does | what the badge sees |
| --- | --- | --- |
| **USB mass storage** | drags a `.cwasm` onto a mounted volume | SCSI sector writes |
| **WebSerial** | picks a file in a browser page | framed bytes on the CDC port already there |

---

## 1. Design decisions

### D1 — INGEST IS TRANSPORT-INDEPENDENT, and that is the whole architecture

Both routes end in the same place: *here are N bytes that claim to be a named payload; decide whether to
commit them to the catalog.* Validation (magic, length, checksum, engine tag), slot allocation, erase and
write are identical either way.

So the core is written once and the transports are adapters:

```
MSC ─────┐
         ├──▶ ingest::stage(name, bytes) ──▶ validate ──▶ catalog::append
WebSerial┘
```

**This is the decision that makes the second transport cheap.** Written the other way round — MSC growing
its own flash-writing code, WebSerial growing another — the two would diverge in exactly the ways that
matter, and only one of them would have been tested against a half-finished transfer.

It also means the ingest core is testable on a laptop, with no USB and no board, which is the only way
its tests run at all (`check-embedded.sh` cross-compiles firmware; it cannot execute it).

### D2 — WEBSERIAL FIRST, mass storage second

WebSerial is dramatically the smaller of the two and exercises the same ingest core. It needs a framing
protocol over a CDC port that already exists, already enumerates, and is already interrupt-driven. There
is no filesystem, no SCSI, no cache coherence, and no host writing sectors in an order of its choosing.

Mass storage is the better *user experience* and the far larger build. Doing it second means the ingest
path is already proven when the hard transport lands, so a failure is unambiguously in the transport.

**The risk of the reverse order is concrete:** debugging a FAT write parser against a flash-commit path
that has itself never worked, with the only diagnostic being a badge that says nothing.

### D3 — STAGE IN PSRAM, COMMIT ON A BOUNDARY. Never write flash incrementally

There is 8 MB of PSRAM and a payload is under one. Accept the whole transfer into RAM, and touch flash
only when the transfer is known complete.

This dissolves three problems at once rather than solving them:

- **Arrival order stops mattering.** A host may write sectors in any order and rewrite them freely.
- **Erase granularity stops mattering.** One contiguous commit, sector-aligned, instead of
  erase-modify-write against live data.
- **A half-finished transfer never commits.** Strictly better than today, where an interrupted UF2 lands
  in flash and shows up as `Corrupt`.

The commit boundary differs by transport: SCSI `SYNCHRONIZE CACHE` or eject for MSC, an explicit
end-of-transfer frame for WebSerial. Both mean "the sender says it is done", and neither is trusted
further than the checksum that follows it.

### D4 — THE BADGE VALIDATES; IT DOES NOT TRUST THE SENDER

Every payload is checked before it reaches the catalog: length against what was declared, FNV-1a
checksum, and engine tag against `catalog::ENGINE_TAG`. A payload that fails is refused with a reason,
not committed and left for `deserialize_raw` to reject later.

**Rejecting late is what this is avoiding.** A payload compiled for another Wasmtime is byte-perfect, so
a checksum verifies it and the region reports healthy — the failure then surfaces at instantiate, wearing
a firmware fault's clothes. That was diagnosed the slow way on 2026-08-16 and is the reason the engine
tag exists.

### D5 — THE BOARD NEVER COMPILES; THE BROWSER DOES

The board links a `no_std` Wasmtime with no Cranelift, and that is the tier's defining property rather
than a gap to close. A payload arriving at the badge is always a `.cwasm`.

**But a person should be able to hand over a `.wasm`,** and the browser is where that gets solved.
`precompile.rs` already cross-compiles — `config.target("pulley32")` produces Pulley bytecode for the
badge from any host — so the missing piece is running that same code somewhere a user already is.

Two distribution paths, and they want different things:

| | who compiles | what travels |
| --- | --- | --- |
| **author-hosted** (primary) | the author, once, with `dlc` | a `.cwasm` |
| **browser** | the user's own tab, on demand | a `.wasm` in, a `.cwasm` out |

The first is expected to carry most traffic and needs nothing new. The second is what makes a `.wasm`
draggable, and it is an *enablement*: no toolchain, no install, works on a machine that has only a
browser.

**Why this is more plausible than it sounds.** Compiling to Pulley emits BYTECODE, not machine code —
there is no JIT, no executable memory, and no signal handling. It is a pure function from `.wasm` bytes
to `.cwasm` bytes, which is exactly the shape of thing that can run inside a sandbox. The obstacle is not
architectural; it is whether `wasmtime` + `cranelift` builds with a **wasm32 host**, which is an
empirical question (§4).

A `.wasm` reaching a badge directly is still an error, and the commonest one — the mental model is right
and the extension is one letter away.

### D5a — ENGINE VERSION BECOMES A DISTRIBUTION PROBLEM

`catalog::ENGINE_TAG` currently answers "will this payload load on this board", on the board, at scan
time. Author-hosted content moves that question upstream: a `.cwasm` published today outlives the
firmware it was built against, and the person downloading it has no way to know before flashing.

So the tag has to be visible **before** the bytes reach the badge — in the filename, a sidecar, or an
index a host publishes. Undecided which (§4), but not optional: without it, author-hosted distribution
degrades to "download, transfer, watch it say WRONG ENGINE, guess which build you needed".

This also makes the browser compiler a **producer of tagged artifacts**, so it must be pinned to the same
Wasmtime the firmware links and must stamp `ENGINE_TAG` itself. Two producers of the same format is
exactly the situation `engine_tag_matches_the_pin` was written for, and that test currently guards only
one of them.

### D5b — THE `.cwasm` EXTENSION IS PART OF THE CONTRACT, and it is a cheap sanity check

Requiring it costs a user nothing and catches the single likeliest mistake before any bytes are
committed. It is also checkable at the very start of a transfer, where refusing is free — as opposed to
the magic bytes, which are better evidence but arrive interleaved with everything else.

**Both are checked, in that order.** The extension is a fast, friendly gate and a `.wasm` begins `\0asm`,
so the content check catches a file that was merely renamed. A misnamed payload is refused with the
reason it was refused, never committed and left for `deserialize_raw` to reject three stages later (D4).

**FAT SHORTENS IT, and the badge already shows this.** The boot log reads `HELLO.CWA`, because 8.3
allows three characters of extension and `names.rs` truncates accordingly. So the same file is
`hello.cwasm` to the person dragging it and `HELLO.CWA` to a host reading the volume back.

Ingest therefore accepts both spellings, and the check runs on the LONG name wherever one exists —
VFAT carries it, and the MSC transport should read it rather than matching only the 8.3 stub. Matching
`.CWA` alone would also accept `.cwa`, `.cwad` and anything else that truncates the same way, which is a
wider gate than intended.

This is a case where the shared name rules earn their keep: `names.rs` and `names.go` already define the
truncation, and both the FAT renderer and the validator call `short_stem_83`, so the two views cannot
disagree about what a payload is called.

### D5c — THE WORLD ADVERTISES WHICH ENGINES IT SUPPORTS, and it is a LIST

`ENGINE_TAG` lets a badge reject a payload it cannot load. That is necessary and it is the wrong end of
the exchange: the bytes have already crossed, the user has already waited, and the answer is a refusal.

**A world should say what it accepts, so a sender never transmits the wrong artifact.** This is the same
move the environment manifest makes for apps (ENVIRONMENT-PLAN D12) — the tier states what it can do
rather than letting the other side discover it by failing — applied one layer down, to the loader.

**A LIST, not a value.** Today a world supports exactly one engine, and writing the singular into the
protocol would make supporting two a breaking change. The cases are already visible: a firmware that
embeds two interpreters during a migration, a `pulley64` variant alongside `pulley32`, or a Wasmtime bump
where the previous artifact still loads. The advertisement is therefore an ordered list, most-preferred
first, and a sender picks the best it can produce.

Each entry carries the tag AND the human string it hashes from (`ENGINE_TAG_SOURCE`,
`"wasmtime=46.0.1;pulley32"`). The tag is what machines compare; the string is what a person reads when
nothing matches, and without it a mismatch is two opaque `u32`s and no way to tell which way to move.

**Three places it must appear, because three different things ask:**

| asker | mechanism |
| --- | --- |
| a person, before anything | the bring-up log and the payload-region stage |
| a loading host (WebSerial) | a handshake frame, sent before any transfer is accepted |
| a PC with no special software | a synthetic read-only file on the mounted volume |

The third falls out of D6 almost free. `fatview` already synthesises a volume from the catalog; adding an
`ENGINE.TXT` alongside the payloads costs one more directory entry and makes the badge **self-describing
to anything that can mount a drive** — no driver, no tool, no protocol. That is worth more than it costs:
the commonest question a hosting site will ask is "which build does this badge need", and the answer
becomes copy-and-paste.

### D5d — THE BROWSER ASKS, AND THE ANSWER PICKS THE COMPILATION TARGET

The advertisement is most useful as a **query**, not an announcement. A browser opens the port, asks what
the world supports, and compiles to that — so a `.wasm` becomes exactly the `.cwasm` the badge in front
of it needs, with no version guessed anywhere in the chain.

That is a better position than author-hosted content can reach. An author publishes for the engines they
knew about; a browser holding a live connection knows the answer for THIS board.

**It is an INTERSECTION, not a lookup, and this is the part worth getting right.** The browser can only
produce artifacts for the Wasmtime whose compiler it ships. So there are two lists:

```
world accepts:    [wasmtime=46.0.1;pulley32]
browser produces: [wasmtime=46.0.1;pulley32]   -> compile, transfer
```

and when they do not meet, the failure must be legible on both axes:

```
world accepts:    [wasmtime=47.0.0;pulley32]
browser produces: [wasmtime=46.0.1;pulley32]   -> "this badge needs 47.0.0; this page can build 46.0.1"
```

Naming both sides is what makes it actionable — one of them has to move, and the message should say which
options exist rather than reporting a mismatch and stopping. A bare "incompatible" sends someone
comparing hashes by hand, which is the state D5c exists to end.

**Query, not just connect-time announcement**, because the two are not equivalent: a page may attach to
read the log and decide to load something later, and a world may be asked again after a firmware change
without the port being reopened. Announcing on connect is a nice extra; answering when asked is the
contract.

**This does not remove the badge-side check.** A sender may be old, wrong, or lying, and D4 stands: the
world validates what it is given regardless of what it advertised. Advertising is what stops the honest
case from failing slowly; validation is what stops the dishonest one from failing dangerously.

### D6 — THE MSC VOLUME IS A VIEW, NOT A FILESYSTEM

`fatview.rs` already synthesises a FAT12 volume from the catalog, validated against `fsck_msdos` and a
real macOS mount with byte-identical SHA-256. It has no consumer yet.

The volume stays a *projection*: files appear because payloads exist, and a payload appears because a
file was written and committed. It is not a general filesystem and will not support arbitrary rewriting,
because the catalog underneath is append-only by design — a directory at the front would have to be
rewritten on every add, which on flash means erasing a sector holding live data.

**Deletion is the open question** (§4), not an oversight.

### D7 — WEBSERIAL SHARES THE PORT WITH THE LOG, and framing is what makes that safe

The CDC port already streams the bring-up log and already discards anything the host types. Adding an
inbound protocol means the two directions stop being independent.

Framing must therefore be unambiguous in both directions: a host that opens the port to *read the log*
must not be able to trip the loader, and a loader transfer must not interleave with log output in a way
that corrupts either. A length-prefixed frame with a magic and a checksum is the whole requirement.

**The alternative — a second CDC interface — was considered and deferred.** It is cleaner in principle
and doubles the endpoint budget for a problem framing already solves.

---

## 2. Phases

Each leaves the tree green and CI passing.

**Phase 1 — the ingest core.** `ingest::stage()` and `ingest::commit()` in
`dlc-platform-embedded`, with no transport and no USB. Validation, slot allocation, the refusal paths
(short, corrupt, wrong engine, `.wasm` mistaken for `.cwasm`). Host tests, run by `check-embedded.sh`.
Flash writing behind a trait so the tests use RAM.

**Phase 2 — WebSerial transport.** Framing, an inbound state machine on the existing CDC port, and the
browser side in the web tier. **The query comes first** (D5d): a page that can ask a badge what it
supports is useful on its own, before anything can be transferred, and it is the smallest possible
exercise of the framing in both directions. Ends with a `.cwasm` loaded onto a badge from a browser page.

**Phase 3 — flash commit on hardware.** The real write path against `PAYLOAD_BASE`, erase and program,
with the catalog appended and re-scanned. This is where Phase 1's trait meets a real sector.

**Phase 4 — MSC transport.** SCSI class, PSRAM volume buffer, FAT *parser* to complement the
synthesiser, commit on `SYNCHRONIZE CACHE`. Ends with drag-and-drop while the world is running.

**Phase 5 — deletion and space.** Only once payloads accumulate and the question is real.

**Phase 6 — browser precompilation.** Spike is GREEN (§4). Ships the precompiler as wasm in the web
tier, stamping `ENGINE_TAG`, so a user can drag a `.wasm` and get a `.cwasm` without a toolchain. The
config must come from ONE shared place — the spike showed three ways to build a working compiler that
emits an artifact the badge refuses.

---

## 3. What this plan does NOT do

- **No compilation on the badge.** See D5. The tier's defining property is having no compiler, and
  browser precompilation does not change that — it moves the step to a machine that has one.
- **No replacement for BOOTSEL.** Firmware updates still go that way; this is about payloads.
- **No network transport.** Adding one is a capability, not a transport, and belongs in its own plan.
- **No security model.** Payloads are trusted by construction — they come from the person holding the
  board. Anything else needs signing, and signing needs a key story this tier does not have.

---

## 4. Open questions

**Deletion.** A FAT volume promises it and an append-only catalog cannot cheaply provide it. Options: a
tombstone flag (cheap, never reclaims space), compaction on demand (reclaims, needs somewhere to stand
while rewriting — PSRAM again), or refusing deletion and exposing the volume read-only except for new
files. Undecided.

**What a host writes that is not a payload.** macOS writes `.DS_Store` and `.Spotlight-V100`. The volume
must absorb these without committing garbage to the catalog, and without appearing broken to the host.
Probably: accept every write into the staging buffer, commit only files whose contents validate.

**Whether the log survives a transfer.** Both share the CDC port under D7. A long transfer that stalls
the log would remove the diagnostic exactly when it is needed.

**Engine-tag mismatch on ingest.** Refuse, or accept and mark? Refusing is honest; accepting would let
someone stage a payload for a firmware they are about to flash. Leaning refuse, with the reason named.

**~~SPIKE: does `wasmtime` + `cranelift` build for a wasm32 HOST?~~ ANSWERED 2026-08-16 — 🟢 GREEN.**
See [`spikes/browser-precompile/`](../spikes/browser-precompile/README.md). It builds for
`wasm32-wasip1`, runs, and produces a `.cwasm` **byte-for-byte identical** to the native toolchain's —
the same artifact now flashed on the badge. **9.9 MB** unoptimised, unstripped, uncompressed.

The finding that outlived the question: three earlier attempts compiled cleanly and produced artifacts
the badge would have REJECTED at load, because the config — `no_virtual_memory`, and
`generate_address_map(false)` — is what defines the artifact. D5a's second producer makes that a standing
hazard, not a one-off: `no_vm.rs` is already shared by `#[path]`, and `generate_address_map(false)` is
still a loose line in one crate. **Phase 6 must share the whole config, not copy it.**

Still open: size after `wasm-opt`/strip/brotli, and compile speed on a phone.

**How a hosted `.cwasm` advertises its engine.** Filename convention, sidecar file, or a published index
(D5a). Whichever it is, it has to be readable without downloading the payload — and it should use the
same vocabulary as the world's own advertisement (D5c), so "what this badge accepts" and "what this
download is" are comparable without a translation step.

**Two producers, one format.** If the browser stamps `ENGINE_TAG`, both it and `payload-image` must agree
forever. `engine_tag_matches_the_pin` guards the Rust side against its own `Cargo.toml`; a second
producer needs the same guard or the pair drifts silently.

---

## 5. Definition of done

1. `./scripts/ci.sh full` green.
2. A `.cwasm` transferred by WebSerial appears in the catalog and runs, verified on hardware.
3. A `.cwasm` dragged onto the mounted volume does the same.
4. **Every refusal has been provoked on purpose** — truncated transfer, wrong checksum, wrong engine
   tag, a `.wasm` sent by mistake — and each names its own cause on the badge and over the log.
5. An interrupted transfer leaves the previously working payload intact and runnable.
6. The ingest core's tests run on a laptop, in CI, without a board.
