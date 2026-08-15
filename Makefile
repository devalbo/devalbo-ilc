# DEVALBO-DLC — build & verify targets.
# Run inside `devbox shell` (see docs/DEVALBO-DLC-PREREQUISITES.md).

.PHONY: doctor
doctor: ## assess prerequisites (system + provisioned toolchain)
	@./scripts/preflight.sh

COMPONENT   := engine.component.wasm
DLC         := dlc
ENGINE_SRC  := ./cmd/engine-component   # wasip2 component entrypoint (shim over engine.Execute)
HOST_SRC    := ./hosts/native           # native dlc host: engine linked in-process (Decision 26)

.PHONY: gen
gen: ## generate WIT + proto bindings (requires devbox shell)
	# The WIT world and the ILC schema belong to the PLATFORM module (§16.4), so
	# their bindings are generated into it — and, unlike everything else under
	# gen/, they are COMMITTED: a consumer of dlc-platform cannot run buf or
	# wit-bindgen (AGENTS.md §6).
	wit-bindgen-go generate --world engine --out dlc-platform/gen/go ./dlc-platform/wit
	wit-bindgen-go generate --world async-engine --out dlc-platform/gen/go ./dlc-platform/wit
	buf lint
	buf generate --template buf.gen.platform.yaml dlc-platform/proto
	buf generate --template buf.gen.yaml proto

.PHONY: sync-template-proto
sync-template-proto: ## copy the shared options.proto into the template AND the example apps
	# A scaffolded app imports devalbo/options/v1/options.proto for `method_id`,
	# but that file lives HERE — so it travels with the scaffold until it is
	# published to a schema registry. Kept a pure byte-copy (provenance lives in
	# the README beside it) so TestTemplateOptionsProtoInSync is exact equality
	# and the eventual swap to a registry dep is a clean delete.
	@cp dlc-platform/proto/devalbo/options/v1/options.proto \
		templates/component-model/proto/devalbo/options/v1/options.proto.tmpl
	# The example apps vendor it too, and they went stale the first time this
	# file gained an option: notes' codegen failed with an error naming neither
	# the file nor the app, because buf cancels every plugin when one fails.
	# An example app demonstrates current practice or it teaches the wrong thing.
	@for app in example-apps/*/proto/devalbo/options/v1/options.proto; do \
		[ -f "$$app" ] && cp dlc-platform/proto/devalbo/options/v1/options.proto "$$app"; \
	done

.PHONY: build-host
build-host: sync-template-proto ## native dlc binary — engine linked in-process (Decision 26)
	go build -o $(DLC) $(HOST_SRC)

.PHONY: build-engine
build-engine: gen ## TinyGo -target=wasip2 -> engine.component.wasm (wasip2-direct, Spike 1)
	tinygo build -target=wasip2 --wit-package ./dlc-platform/wit --wit-world engine \
		-o $(COMPONENT) $(ENGINE_SRC)

.PHONY: component
component: build-engine ## wasip2-direct emits the component in one shot (no wasip1 adapter)

.PHONY: verify-parity
verify-parity: ## Decision 26: native and wasip2 engines agree byte-for-byte (results + filesystem)
	@./scripts/verify-parity.sh

.PHONY: scaffold-golden
scaffold-golden: ## re-bless the §11 golden FS snapshot after a deliberate template change
	go run ./cmd/scaffold-golden > verify/scaffold/golden.txt
	@echo "re-blessed verify/scaffold/golden.txt — review the diff"

.PHONY: gen-names
gen-names: ## regenerate the name-rule tables into Go and Rust from names/RULES.json
	# ONE SPEC, TWO LANGUAGES. Apps are Go and the badge host is Rust, so the rules
	# used to be written twice and VECTORS.tsv existed to DETECT the drift between
	# them. Generating both removes the drift instead. Edit names/RULES.json (or
	# WORLDS.tsv), run this, and commit the generated files.
	@go run ./cmd/gen-names

.PHONY: verify-names
verify-names: ## fail if the generated name tables are stale, or the two implementations disagree
	# THE GUARD THAT MAKES CODEGEN WORTH ANYTHING. Without it, someone edits a
	# generated file by hand and the single source quietly becomes a third copy.
	@go run ./cmd/gen-names -check
	# ...and the vectors still run, because codegen makes the two implementations
	# IDENTICAL and cannot make them CORRECT. Different question, still worth asking.
	@cd dlc-platform && go test . -run 'TestName|TestCollision|TestWorld|TestUndefined' >/dev/null && echo "  go name rules agree with the shared vectors"
	@cd dlc-platform/embedded && cargo test -q --lib names 2>/dev/null | tail -1

.PHONY: verify-scaffold-golden
verify-scaffold-golden: ## §11: `dlc new` emits exactly the tree we meant
	@go run ./cmd/scaffold-golden -check

.PHONY: verify-scaffold
verify-scaffold: build-host ## §11 Scaffolder: `dlc new` output generates, builds, and runs
	@./scripts/verify-scaffold.sh

.PHONY: qemu-payload
qemu-payload: ## AOT-compile hello for the QEMU firmware to embed (gitignored; regenerate freely)
	# The qemu crate does include_bytes! on this, so a fresh clone must run this
	# target before `cargo build` there. Not committed: it is 1.6 MB, derived
	# twice over, and version-locked to the exact Wasmtime that made it.
	@test -f example-apps/hello/build/engine.component.wasm \
		|| { echo "  first: cd example-apps/hello && make build-web"; exit 1; }
	$(MAKE) embedded-cwasm COMPONENT_IN=example-apps/hello/build/engine.component.wasm \
		CWASM_OUT=dlc-platform/embedded/qemu-armv7m/hello.pulley32.cwasm

.PHONY: qemu
qemu: ## run the embedded tier on an emulated 32-bit ARM core and print its RAM cost
	# THE MEASUREMENT THE PSRAM DECISION RESTS ON, so it is a target rather than a
	# command someone has to remember. It loads a flash-resident .cwasm with
	# `deserialize_raw`, instantiates it through `MinimalHost` — the badge's own
	# host, not a stand-in — runs one command, and prints the peak heap against the
	# RP2350's 520 KB of SRAM.
	#
	# NOT the badge (EMBEDDED-PLAN D7): a Cortex-M3, no RP2350 peripherals, and
	# QEMU's own 16 MB window standing in for a heap. What it does share is the
	# thing under test — `pulley32` on a 32-bit core, through the real host.
	#
	# `mps2-an385` rather than a real M33 machine: QEMU's mps2-an505 boots secure
	# at an address cortex-m-rt's default layout does not match, and the claim
	# under test does not depend on ARMv8-M.
	$(MAKE) qemu-payload
	cd dlc-platform/embedded/qemu-armv7m && cargo build --release
	qemu-system-arm -machine mps2-an385 -cpu cortex-m3 -nographic \
		-semihosting-config enable=on,target=native \
		-kernel dlc-platform/embedded/qemu-armv7m/target/thumbv7m-none-eabi/release/dlc-qemu-armv7m

.PHONY: badge-cwasm
badge-cwasm: ## AOT-compile hello for the badge (gitignored; regenerate freely)
	# The SAME artifact the QEMU harness runs — one payload, two places that load
	# it — so a hardware failure cannot be the bytes.
	@test -f example-apps/hello/build/engine.component.wasm \
		|| { echo "  first: cd example-apps/hello && make build-web"; exit 1; }
	$(MAKE) embedded-cwasm COMPONENT_IN=example-apps/hello/build/engine.component.wasm \
		CWASM_OUT=build/hello.pulley32.cwasm

.PHONY: hello-component
hello-component: ## hello's wasm component — what the badge payload is AOT-compiled from
	# `dlc build web` is what produces it, so this needs the dlc BINARY on PATH —
	# the same idiom verify-example-apps-web.sh uses, in a temp dir so it cannot
	# leave an executable at the repo root (AGENTS.md §6).
	#
	# ITS OWN TARGET because two callers want it and only one of them is the web
	# tier: `ci.sh full` gets this component as a side effect of B3, while an
	# embedded-only CI slice runs no browser at all and would otherwise have
	# nothing to AOT. A side effect that is load-bearing should be nameable.
	@BIN="$$(mktemp -d)"; trap 'rm -rf "$$BIN"' EXIT; \
		go build -buildvcs=false -o "$$BIN/dlc" $(HOST_SRC) \
		&& PATH="$$BIN:$$PATH" $(MAKE) -C example-apps/hello build-web

.PHONY: badge-uf2
badge-uf2: ## build the badge firmware (an empty loader by default) as a flashable .uf2
	# THE DEFAULT IS AN EMPTY LOADER: app-agnostic firmware that runs whatever has
	# been dragged onto the payload region and shows what comes back. Nothing is
	# baked in unless asked for, so adding an app does not mean rebuilding
	# firmware — which is the whole point of the region.
	#
	# WHAT IT CAN RUN is a FLASH-TIME choice (rp2350/build.rs):
	#
	#   make badge-uf2                                     empty loader        (default)
	#   make badge-uf2 BADGE_PAYLOAD=$$PWD/build/x.cwasm   that app baked in, region still scanned
	#   make badge-uf2 BADGE_PAYLOAD=... BADGE_REGION=off  that app and nothing else
	#
	# WHICH WORLD is the other flash-time choice (rp2350/src/world.rs). Same
	# component, same imports, same .cwasm — they differ in what reaches a human,
	# and minimal is a strict subset of normal:
	#
	#   make badge-uf2                      normal  — shows the app's text  (default)
	#   make badge-uf2 BADGE_WORLD=minimal  minimal — one status colour, no text
	#
	# HOW FAST IT BOOTS is the third. The bring-up narrates itself on the screen;
	# BADGE_BEAT_MS is how long each stage lingers. Zero (the default) is normal
	# boot — the stages still appear, at machine speed. Use a beat for a FIRST
	# bring-up, where being able to read which check failed is the whole point:
	#
	#   make badge-uf2                      boot as fast as the board can  (default)
	#   make badge-uf2 BADGE_BEAT_MS=700    watchable — ~10s, readable per stage
	#
	# picotool, NOT elf2uf2-rs — because it tags the UF2 family and elf2uf2-rs got
	# it wrong: it emitted family `rp2040` from RP2350 firmware, which would have
	# been flashed at a Tufty 2350 with nothing in the build to explain the result.
	cd dlc-platform/embedded/rp2350 && \
		BADGE_PAYLOAD="$(BADGE_PAYLOAD)" \
		BADGE_REGION="$(BADGE_REGION)" \
		BADGE_WORLD="$(BADGE_WORLD)" \
		BADGE_BEAT_MS="$(BADGE_BEAT_MS)" \
		cargo build --release
	@mkdir -p build
	@cp dlc-platform/embedded/rp2350/target/thumbv8m.main-none-eabihf/release/dlc-rp2350-bringup build/badge-bringup.elf
	picotool uf2 convert build/badge-bringup.elf build/badge-bringup.uf2
	# THE GATE. A UF2 whose family is not rp2350 will not boot this board, and the
	# only symptom is a badge that does nothing at all. Fail here instead — see
	# dlc-platform/embedded/rp2350/memory.x for the open placement bug.
	@picotool info build/badge-bringup.uf2 | grep -q rp2350 || { \
		echo "  x wrong UF2 family — this will not boot; see rp2350/memory.x"; \
		picotool info build/badge-bringup.uf2 | head -3; exit 1; }
	@ls -l build/badge-bringup.uf2 | awk '{print "  flashable: "$$5" bytes"}'


.PHONY: badge-payload
badge-payload: ## pack payloads into a UF2 you can DRAG onto the badge's BOOTSEL drive
	# THE POINT: adding an app to a badge should not need a toolchain. Hold BOOT,
	# tap RESET, drag the .uf2 onto the RP2350 drive, reset. The bootloader writes
	# each block to the address that block names — 0x10100000 and up, which is the
	# payload region — so the FIRMWARE IS UNTOUCHED. Different sectors entirely.
	#
	# IT MUST BE A UF2, and this is the one thing that surprises people: the
	# BOOTSEL drive is a synthetic FAT12 volume, not storage. Its only real
	# operation is parsing UF2 blocks. A .cwasm dragged onto it is accepted by the
	# Finder and discarded by the bootloader, silently, with no error anywhere.
	#
	#   make badge-payload                                    just hello
	#   make badge-payload PAYLOADS="hello=build/hello.pulley32.cwasm ttt=build/ttt.pulley32.cwasm"
	#
	# ONE IMAGE, ALL PAYLOADS. The catalog terminates on a bad magic rather than
	# carrying an index, so images append cleanly — but a SECOND drag has to know
	# the offset the first one ended at, and nothing on the badge tells you that.
	# Packing them together sidesteps the bookkeeping; OFFSET is there for when you
	# genuinely want to add one without rewriting the rest.
	@$(if $(PAYLOADS),,$(MAKE) badge-cwasm)
	# `--manifest-path` rather than `cd`, so the tool runs with the REPO ROOT as
	# its working directory and a relative path in PAYLOADS means what the person
	# typing it meant. Previously this cd'd into dlc-platform/embedded first, so
	# the README's own documented command —
	#   make badge-payload PAYLOADS="hello=build/hello.pulley32.cwasm"
	# — resolved against the wrong directory and died with a bare ENOENT naming
	# no file. Absolute paths worked, which is exactly why it survived: every
	# invocation in this Makefile passes $(PWD).
	cargo run -q --manifest-path dlc-platform/embedded/Cargo.toml --bin payload-image -- \
		build/badge-payload.bin \
		$(or $(PAYLOADS),hello=build/hello.pulley32.cwasm)
	# THE ADDRESS COMES FROM board.rs, and it is read rather than repeated for a
	# reason this target already got wrong once: the base moved from 0x10100000 to
	# 0x10400000 when a built-in payload turned out to live in FLASH, this line did
	# not, and a payload written 3 MB below where the firmware scans produces a
	# badge that reports "no payloads" with the file sitting right there. Nothing
	# fails; it just does not work.
	#
	# `--family data` because this is NOT executable code — it is bytes at an
	# address. Tagging it as an RP2350 image would invite the bootloader to treat
	# a payload as firmware.
	@base=$$(sed -n 's/^pub const PAYLOAD_BASE: usize = \(0x[0-9A-Fa-f_]*\);/\1/p' \
		dlc-platform/embedded/rp2350/src/board.rs | tr -d _); \
	test -n "$$base" || { echo "  x could not read PAYLOAD_BASE from board.rs"; exit 1; }; \
	offset=$(or $(OFFSET),$$base); \
	echo "picotool uf2 convert build/badge-payload.bin -t bin -o $$offset --family data build/badge-payload.uf2"; \
	picotool uf2 convert build/badge-payload.bin -t bin \
		-o $$offset --family data build/badge-payload.uf2; \
	ls -l build/badge-payload.uf2 | awk -v o=$$offset '{print "  draggable: "$$5" bytes -> "o}'

.PHONY: embedded-cwasm
embedded-cwasm: ## AOT a loose component — the escape hatch under `dlc build <tier>`
	#
	# PREFER `dlc build rp2350`, which does tinygo + this in one verb and takes the
	# output path from dlc.toml. This target stays for components that belong to no
	# project — the hello example, a hand-built probe — where there is no manifest
	# to read a tier out of.
	# `dlc-precompile`, NOT `wasmtime compile`. A .cwasm records the feature set
	# of the compiler that produced it, so the stock CLI's artifacts are
	# unloadable by our default-features=false runtime — five features' worth of
	# mismatch before the cause was clear. The crate is built like the runtime,
	# which makes the mismatch impossible rather than merely fixed.
	#
	# TARGET is pulley32/pulley64 — pointer width, and nothing else. Pulley
	# bytecode is ISA-independent, so one pulley32 artifact serves the RP2350's
	# Cortex-M33, its Hazard3 RISC-V cores, and an ESP32-P4 alike.
	@test -n "$(COMPONENT_IN)" || { echo "usage: make embedded-cwasm COMPONENT_IN=<file.wasm> [CWASM_OUT=<f>] [TARGET=pulley32]"; exit 2; }
	cd dlc-platform/embedded/precompile && cargo run -q -- \
		$(PWD)/$(COMPONENT_IN) \
		$(PWD)/$(or $(CWASM_OUT),build/engine.pulley32.cwasm) \
		$(or $(TARGET),pulley32)


.PHONY: verify-npm-package
verify-npm-package: ## @devalbo/dlc-web ships what its `exports` advertise
	@./scripts/verify-npm-package.sh

.PHONY: verify-platform-ref
verify-platform-ref: build-host ## the ref `dlc new` pins actually resolves (nightly; needs network)
	@./scripts/verify-platform-ref.sh

.PHONY: verify-scaffold-env
verify-scaffold-env: build-host ## the scaffold generates in ITS OWN devbox, not ours (slow; needs network)
	@./scripts/verify-scaffold-env.sh

.PHONY: verify-scaffold-web
verify-scaffold-web: build-host ## a scaffolded app runs in a browser, via its own shipped test
	@./scripts/verify-scaffold-web.sh

.PHONY: parity-vectors
parity-vectors: ## regenerate verify/parity/method-vectors.json from the typed fixtures
	go run ./cmd/parity-runner -gen verify/parity/method-vectors.json

.PHONY: build-wasm
build-wasm: component gen-web ## transpile the component for the web (jco)
	# --map: jco emits `import { emit } from 'devalbo:ilc/events'` for the custom
	# capability import, which no bundler resolves on its own. Same mapping that
	# `dlc build web` applies for scaffolded apps.
	jco transpile $(COMPONENT) -o hosts/web/src/wasm \
		--map 'devalbo:ilc/events=@devalbo/dlc-web/events'

.PHONY: gen-web
gen-web: gen ## copy generated es-lite messages into the Vite root
	# Bundlers resolve @aptre/* from the IMPORTING file's directory tree, and
	# gen/ts sits outside hosts/web/ — so the generated messages are copied in
	# rather than aliased (same workaround as spike-proto; Spike 2 finding).
	rm -rf hosts/web/src/gen
	mkdir -p hosts/web/src/gen
	# TWO sources now: the platform's schema generates into the platform module
	# (§16.4) and dlc's own into gen/. Both land under one @gen root because an
	# importing file should not have to know which module a message came from —
	# that is exactly the detail the extraction is meant to hide.
	cp -R dlc-platform/gen/ts/devalbo hosts/web/src/gen/
	cp -R gen/ts/devalbo/* hosts/web/src/gen/devalbo/

.PHONY: dev-web
dev-web: build-wasm ## run the web tier dev server (Vite)
	# `npm run vite` (not `npm run dev`) — the dev script delegates back here, so
	# calling it would recurse. This target is the one that owns the build order.
	cd hosts/web && npm install --silent --no-audit --no-fund && npm run vite

.PHONY: verify-web
verify-web: build-wasm ## B3: dlc new in the browser, persisted in OPFS (headless)
	cd hosts/web && npm install --silent --no-audit --no-fund
	cd hosts/web && npx playwright install chromium
	cd hosts/web && npx playwright test

.PHONY: verify-web-watch
verify-web-watch: build-wasm ## verify-web headed + slowMo, so you can watch it
	cd hosts/web && npm install --silent --no-audit --no-fund
	cd hosts/web && npx playwright install chromium
	cd hosts/web && DLC_WEB_WATCH=1 npx playwright test --headed

.PHONY: test-b0
test-b0: ## Phase B0 repo-integrity checks (no toolchain needed)
	@./scripts/test-b0.sh

# ---- B1 spikes -----------------------------------------------------------





.PHONY: spike-async
spike-async: gen ## Spike 5 (T-B1.5): async probe — jco JSPI vs sync transpile
	@./scripts/spike-async.sh

.PHONY: spike-sqlite-sync
spike-sqlite-sync: ## SQLite index Phase 0 GATE: sync query over OPFS in a worker
	@./scripts/spike-sqlite-sync.sh



.PHONY: test-b1
test-b1: ## Phase B1 spikes (requires the devbox toolchain)
	@./scripts/test-b1.sh

.PHONY: lint-scripts
lint-scripts: ## shell patterns that only fail once output grows (each one reached CI once)
	@./scripts/lint-scripts.sh

.PHONY: verify-example-apps
verify-example-apps: ## the example apps (which CONSUME the platform) still build and pass
	@./scripts/verify-example-apps.sh

.PHONY: test-b2
test-b2: ## Phase B2 engine boundary: unit + parity + parity self-test
	@./scripts/test-b2.sh

.PHONY: verify-platform-gen
verify-platform-gen: ## §16.4: dlc-platform's committed generated code is current
	@./scripts/verify-platform-gen.sh

.PHONY: verify-parity-selftest
verify-parity-selftest: ## prove verify-parity can FAIL (inject tinygo-only drift)
	@./scripts/verify-parity-selftest.sh

.PHONY: verify-bundle-xtier
verify-bundle-xtier: build-wasm ## §7.3: a BFT bundle exported in the browser imports in the terminal
	@./scripts/verify-bundle-xtier.sh

.PHONY: verify-example-apps-web
verify-example-apps-web: build-host ## example apps run in a browser (their own shipped tests)
	@./scripts/verify-example-apps-web.sh

.PHONY: test-b3
test-b3: verify-web verify-bundle-xtier verify-scaffold-web verify-example-apps-web ## Phase B3 web tier: dlc in the browser, OPFS, BFT interchange, and a SCAFFOLDED app in the browser

.PHONY: ci
ci: ## what CI runs — identical locally (fast | full | all via scripts/ci.sh)
	@./scripts/ci.sh full

.PHONY: ci-fast
ci-fast: ## structure + unit + fmt/vet, no wasm or browser
	@./scripts/ci.sh fast

.PHONY: check-fmt
check-fmt: ## gofmt over the tree (skips templates/, which is not valid Go)
	@./scripts/check-fmt.sh

.PHONY: test
test: test-b0 test-b1 test-b2 test-b3 ## run all regression suites from first principles (B0..B3)

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'
