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

.PHONY: verify-scaffold-golden
verify-scaffold-golden: ## §11: `dlc new` emits exactly the tree we meant
	@go run ./cmd/scaffold-golden -check

.PHONY: verify-scaffold
verify-scaffold: build-host ## §11 Scaffolder: `dlc new` output generates, builds, and runs
	@./scripts/verify-scaffold.sh

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
spike-async: gen ## Spike 5 (T-B1.5): async probe Rich/CM + Portable/WAMR-shaped
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
