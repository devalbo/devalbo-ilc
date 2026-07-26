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
	wit-bindgen-go generate --world engine --out gen/go ./wit
	wit-bindgen-go generate --world async-engine --out gen/go ./wit
	cd proto && buf lint && buf generate

.PHONY: sync-template-proto
sync-template-proto: ## copy the shared options.proto into the scaffold template
	# A scaffolded app imports devalbo/options/v1/options.proto for `method_id`,
	# but that file lives HERE — so it travels with the scaffold until it is
	# published to a schema registry. Kept a pure byte-copy (provenance lives in
	# the README beside it) so TestTemplateOptionsProtoInSync is exact equality
	# and the eventual swap to a registry dep is a clean delete.
	@cp proto/devalbo/options/v1/options.proto \
		templates/component-model/proto/devalbo/options/v1/options.proto.tmpl

.PHONY: build-host
build-host: sync-template-proto ## native dlc binary — engine linked in-process (Decision 26)
	go build -o $(DLC) $(HOST_SRC)

.PHONY: build-engine
build-engine: gen ## TinyGo -target=wasip2 -> engine.component.wasm (wasip2-direct, Spike 1)
	tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
		-o $(COMPONENT) $(ENGINE_SRC)

.PHONY: component
component: build-engine ## wasip2-direct emits the component in one shot (no wasip1 adapter)

.PHONY: verify-parity
verify-parity: ## Decision 26: native dlc and the wasip2 component agree byte-for-byte (argv + method boundaries)
	@./scripts/verify-parity.sh

.PHONY: verify-scaffold
verify-scaffold: build-host ## §11 Scaffolder: `dlc new` output generates, builds, and runs
	@./scripts/verify-scaffold.sh

.PHONY: parity-vectors
parity-vectors: ## regenerate verify/parity/method-vectors.json from the typed fixtures
	go run ./cmd/parity-runner -gen verify/parity/method-vectors.json

.PHONY: build-wasm
build-wasm: component gen-web ## transpile the component for the web (jco)
	jco transpile $(COMPONENT) -o frontend/src/wasm

.PHONY: gen-web
gen-web: gen ## copy generated es-lite messages into the Vite root
	# Bundlers resolve @aptre/* from the IMPORTING file's directory tree, and
	# gen/ts sits outside frontend/ — so the generated messages are copied in
	# rather than aliased (same workaround as spike-proto; Spike 2 finding).
	rm -rf frontend/src/gen
	mkdir -p frontend/src/gen
	cp -R gen/ts/devalbo frontend/src/gen/

.PHONY: dev-web
dev-web: build-wasm ## run the web tier dev server (Vite)
	# `npm run vite` (not `npm run dev`) — the dev script delegates back here, so
	# calling it would recurse. This target is the one that owns the build order.
	cd frontend && npm install --silent --no-audit --no-fund && npm run vite

.PHONY: verify-web
verify-web: build-wasm ## B3: dlc new in the browser, persisted in OPFS (headless)
	cd frontend && npm install --silent --no-audit --no-fund
	cd frontend && npx playwright install chromium
	cd frontend && npx playwright test

.PHONY: verify-web-watch
verify-web-watch: build-wasm ## verify-web headed + slowMo, so you can watch it
	cd frontend && npm install --silent --no-audit --no-fund
	cd frontend && npx playwright install chromium
	cd frontend && DLC_WEB_WATCH=1 npx playwright test --headed

.PHONY: test-b0
test-b0: ## Phase B0 repo-integrity checks (no toolchain needed)
	@./scripts/test-b0.sh

# ---- B1 spikes -----------------------------------------------------------
# Spike 1 (T-B1.1): TinyGo -target=wasip2 (native Component Model) -> component
# -> jco transpile -> run under Node. Proves one engine builds + runs via jco.
# wasip2 lets TinyGo supply cabi_realloc + wire _initialize (vs the fragile
# wasip1+wasm-tools adapter path — see docs/DEVALBO-DLC-TEST-STEPS.md T-B1.1).
SPIKE_COMPONENT := spikes/component

.PHONY: spike-component
spike-component: gen ## Spike 1 (T-B1.1): component round-trip → prints ok:hi
	cd $(SPIKE_COMPONENT) && npm install --silent --no-audit --no-fund
	tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
		-o $(SPIKE_COMPONENT)/engine.component.wasm ./$(SPIKE_COMPONENT)
	cd $(SPIKE_COMPONENT) && npx jco transpile engine.component.wasm -o out
	cd $(SPIKE_COMPONENT) && node harness.mjs

# Spike 2 (T-B1.2): protobuf-go-lite under TinyGo + es-lite cross-decode.
SPIKE_PROTO := spikes/proto

.PHONY: spike-proto-goldens
spike-proto-goldens: gen ## regenerate Spike 2 golden.hex / golden.json from the fixture
	@go run ./spikes/proto/cmd/goldens

.PHONY: spike-proto
spike-proto: gen ## Spike 2 (T-B1.2): go-lite ↔ es-lite round-trip under wasip2
	go test ./$(SPIKE_PROTO)/
	cd $(SPIKE_PROTO) && npm install --silent --no-audit --no-fund
	# Node resolves @aptre/* from the importing file's directory tree; copy the
	# generated binding into the spike so spikes/proto/node_modules is found.
	cp gen/ts/devalbo/spike/v1/spike.pb.ts $(SPIKE_PROTO)/spike.pb.ts
	tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
		-o $(SPIKE_PROTO)/engine.component.wasm ./$(SPIKE_PROTO)
	cd $(SPIKE_PROTO) && npx jco transpile engine.component.wasm -o out
	cd $(SPIKE_PROTO) && npx tsx harness.ts

# Spike 3 (T-B1.3): engine os.WriteFile → WASI preopen → OPFS; survives reload.
# Default headless. Watch the browser: `make spike-opfs-watch` (or SPIKE_OPFS_HEADED=1).
SPIKE_OPFS := spikes/opfs

.PHONY: spike-opfs
spike-opfs: gen ## Spike 3 (T-B1.3): OPFS persistence (headless Playwright)
	cd $(SPIKE_OPFS) && npm install --silent --no-audit --no-fund
	cd $(SPIKE_OPFS) && npx playwright install chromium
	tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
		-o $(SPIKE_OPFS)/engine.component.wasm ./$(SPIKE_OPFS)
	cd $(SPIKE_OPFS) && npx jco transpile engine.component.wasm -o out
	cd $(SPIKE_OPFS) && npm test

.PHONY: spike-opfs-watch
spike-opfs-watch: gen ## Spike 3 headed+slowMo; pauses so you can inspect OPFS in DevTools
	cd $(SPIKE_OPFS) && npm install --silent --no-audit --no-fund
	cd $(SPIKE_OPFS) && npx playwright install chromium
	tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
		-o $(SPIKE_OPFS)/engine.component.wasm ./$(SPIKE_OPFS)
	cd $(SPIKE_OPFS) && npx jco transpile engine.component.wasm -o out
	cd $(SPIKE_OPFS) && npm run test:watch

# Spike 4 (T-B1.4): in-engine CLI bake-off (flag / ffcli / hand / sub / cobra /
# kong / go-arg). B1 gate = ≥1 lean green; full table printed by the script.
SPIKE_CLI := spikes/cli

.PHONY: spike-cli
spike-cli: gen ## Spike 4 (T-B1.4): in-engine CLI interpreter bake-off
	@./scripts/spike-cli.sh

# Spike 5 (T-B1.5): dual-track async probe — Rich/CM (jco JSPI on Node ≥24) +
# Portable/WAMR-shaped (wasip1 + blocking host). No ILC async shims.
SPIKE_ASYNC := spikes/async

.PHONY: spike-async
spike-async: gen ## Spike 5 (T-B1.5): async probe Rich/CM + Portable/WAMR-shaped
	@./scripts/spike-async.sh

# Registry de-risk: go-lite `oneof` command envelope + map dispatch (no switch)
# under TinyGo wasip2 — the assumption Decision 29 rides on (Spike 2 was flat).
SPIKE_ONEOF := spikes/oneof

.PHONY: spike-oneof
spike-oneof: gen ## go-lite oneof under TinyGo (registry de-risk, Decision 29)
	cd $(SPIKE_ONEOF) && npm install --silent --no-audit --no-fund
	tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
		-o $(SPIKE_ONEOF)/engine.component.wasm ./$(SPIKE_ONEOF)
	cd $(SPIKE_ONEOF) && npx jco transpile engine.component.wasm -o out
	cd $(SPIKE_ONEOF) && node harness.mjs

# Registry de-risk: go-lite tolerates descriptor.proto + custom options
# (method_id / field metadata). Host reads them via FileDescriptorSet.
SPIKE_OPTIONS := spikes/options

.PHONY: spike-options
spike-options: gen ## go-lite custom options gate (Decision 29)
	@./scripts/spike-options.sh

.PHONY: test-b1
test-b1: ## Phase B1 spikes (requires the devbox toolchain)
	@./scripts/test-b1.sh

.PHONY: test-b2
test-b2: ## Phase B2 engine boundary: unit + parity + parity self-test
	@./scripts/test-b2.sh

.PHONY: verify-parity-selftest
verify-parity-selftest: ## prove verify-parity can FAIL (inject tinygo-only drift)
	@./scripts/verify-parity-selftest.sh

.PHONY: verify-bundle-xtier
verify-bundle-xtier: build-wasm ## §7.3: a BFT bundle exported in the browser imports in the terminal
	@./scripts/verify-bundle-xtier.sh

.PHONY: test-b3
test-b3: verify-web verify-bundle-xtier ## Phase B3 web tier: dlc new in the browser, OPFS persistence, BFT interchange

.PHONY: test
test: test-b0 test-b1 test-b2 test-b3 ## run all regression suites from first principles (B0..B3)

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'
