# badge-selftest — build & verify.
# Run inside `devbox shell`.

.PHONY: gen
gen: ## generate proto bindings, command dispatch, and dlcconfig from dlc.toml
	cd proto && buf lint
	dlc gen

.PHONY: build
build: gen ## build the native CLI (engine linked in-process)
	go build -o badge-selftest ./hosts/native

.PHONY: run
run: build ## build then say hello
	./badge-selftest greet --name ILC

.PHONY: test
test: ## unit tests
	go test ./...

.PHONY: gen-web
gen-web: gen ## copy generated es-lite messages into the Vite root
	# Bundlers resolve @aptre/* from the IMPORTING file's tree, and gen/ sits
	# outside hosts/web/ — so the messages are copied in rather than aliased.
	rm -rf hosts/web/src/gen
	mkdir -p hosts/web/src/gen
	# EVERYTHING THAT WAS GENERATED, rather than a list of subtrees.
	#
	# This used to name them one by one — the app's own package, then
	# `devalbo/ilc`, then `devalbo/dlc` — and each addition arrived as a bug: the
	# platform messages import `SchemaInfo`, which imports `StdVersion`, and a
	# copy missing that last tree left the bundler resolving
	# `../../dlc/std/v1/version.pb.js` to nothing. It failed twice that way, both
	# times surfacing as a browser test timing out on a missing module rather
	# than anything naming the cause.
	#
	# A list of what to copy is a list that goes stale. `gen/ts` holds exactly
	# what `dlc gen` produced for this tier, so copying all of it is both simpler
	# and correct by construction.
	cp -R gen/ts/. hosts/web/src/gen/

.PHONY: build-badge
build-badge: gen ## build the wasm component for the BADGE (needs dlc on PATH)
	# THE TARGET THIS APP ACTUALLY WANTS. `dlc.toml` declares `native` and
	# `rp2350` and no web tier, so `build-web` below cannot work here — and
	# reaching for it is the obvious mistake, because every other example app in
	# this repo builds its component that way.
	#
	# What comes out is `build/engine.component.wasm`, which is the input to
	# `make badge-cwasm APP=badge-selftest` at the repo root. That AOT-compiles it
	# to pulley32 for the badge, and `make badge-payload` packs it.
	#
	# DLC_PRECOMPILE POINTS AT THE COMPILER, so this completes rather than
	# stopping half-done. Left unset, `dlc build rp2350` writes the component and
	# then exits non-zero — which is honest but leaves you with an artifact and a
	# failed build, and no hint that the two are compatible states.
	#
	# It is a path rather than a bundled binary because the AOT compiler is a RUST
	# crate: unlike the WIT world, it cannot ship inside a Go binary. The crate is
	# the same one the repo-root Makefile drives, so the .cwasm produced here and
	# there are byte-identical — one engine config, one producer (engine_config.rs).
	DLC_PRECOMPILE=$(CURDIR)/../../dlc-platform/embedded/precompile dlc build rp2350

.PHONY: build-web
build-web: gen-web ## NOT AVAILABLE — this app declares no web tier
	# KEPT AS A SIGNPOST rather than deleted. The target is inherited from the
	# scaffold, `hosts/web/` still exists on disk, and `dlc.toml` declares no
	# `[tiers.web]` — so this fails with "this project declares no [tiers.web]",
	# which is accurate and does not tell you what to run instead.
	@echo "  badge-selftest has no web tier — it targets native and rp2350."
	@echo "  For the badge:  make build-badge"
	@echo "  Then, at the repo root:  make badge-cwasm APP=badge-selftest"
	@exit 2

.PHONY: dev-web
dev-web: build-web ## serve the web tier
	cd hosts/web && npm install && npm run dev

.PHONY: verify
verify: build ## the smoke test: the binary exists and answers
	@./badge-selftest version
	@./badge-selftest greet --name ILC
