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

.PHONY: build-host
build-host: ## native dlc binary — engine linked in-process (Decision 26)
	go build -o $(DLC) $(HOST_SRC)

.PHONY: build-engine
build-engine: gen ## TinyGo -target=wasip2 -> engine.component.wasm (wasip2-direct, Spike 1)
	tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
		-o $(COMPONENT) $(ENGINE_SRC)

.PHONY: component
component: build-engine ## wasip2-direct emits the component in one shot (no wasip1 adapter)

.PHONY: verify-parity
verify-parity: ## Decision 26: native dlc and the wasip2 component agree byte-for-byte
	@./scripts/verify-parity.sh

.PHONY: build-wasm
build-wasm: component ## transpile the component for the web (jco)
	jco transpile $(COMPONENT) -o frontend/src/wasm

.PHONY: dev-web
dev-web: build-wasm ## run the web tier dev server (Vite)
	cd frontend && npm install && npm run dev

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

.PHONY: test
test: test-b0 test-b1 ## run all regression suites from first principles (B0 then B1)

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'
