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

.PHONY: test
test: test-b0 ## run available regression suites (B1 spikes land with the toolchain)
	@echo "B1 spikes (make test-b1) land once the toolchain is installed + spikes written."

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'
