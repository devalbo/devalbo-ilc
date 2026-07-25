# DEVALBO-DLC — build & verify targets.
# Run inside `devbox shell` (see docs/DEVALBO-DLC-PREREQUISITES.md).

.PHONY: doctor
doctor: ## assess prerequisites (system + provisioned toolchain)
	@./scripts/preflight.sh

ENGINE_WASM := engine.core.wasm
COMPONENT   := engine.component.wasm
# WASI preview1 -> component adapter (path resolved from the wasm-tools/wasmtime install).
ADAPTER     ?= wasi_snapshot_preview1.wasm

.PHONY: gen
gen: ## generate WIT + proto bindings (requires devbox shell)
	wit-bindgen-go generate --world engine --out gen/go ./wit
	cd proto && buf lint && buf generate

.PHONY: build-engine
build-engine: ## TinyGo -> engine.core.wasm (the shared unit)
	tinygo build -target=wasip1 -o $(ENGINE_WASM) ./engine

.PHONY: component
component: build-engine ## adapt the core module into a wasip2 component
	wasm-tools component new $(ENGINE_WASM) --adapt $(ADAPTER) -o $(COMPONENT)

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

.PHONY: test-b1
test-b1: ## Phase B1 spikes (requires the devbox toolchain)
	@./scripts/test-b1.sh

.PHONY: test
test: test-b0 test-b1 ## run all regression suites from first principles (B0 then B1)

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'
