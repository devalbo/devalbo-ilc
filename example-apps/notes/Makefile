# notes — build & verify.
# Run inside `devbox shell`.

.PHONY: gen
gen: ## generate proto bindings, command dispatch, and dlcconfig from dlc.toml
	cd proto && buf lint
	dlc gen

.PHONY: build
build: gen ## build the native CLI (engine linked in-process)
	go build -o notes ./hosts/native

.PHONY: run
run: build ## build, then show the generated command surface
	./notes

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

.PHONY: build-web
build-web: gen-web ## build the wasm component + jco transpile (needs dlc on PATH)
	# Puts the component in build/ and the web assets in hosts/web/src/wasm —
	# they have different audiences, so dlc writes each where it belongs.
	dlc build web

.PHONY: dev-web
dev-web: build-web ## serve the web tier
	cd hosts/web && npm install && npm run dev

.PHONY: verify
verify: build ## the smoke test: the binary exists and answers
	# Exercised in a temp dir because the native filesystem root IS the working
	# directory — `reset-fs` here would delete the checkout's own files.
	@./notes version
	@tmp=$$(mktemp -d) && cd $$tmp && \
		"$(CURDIR)/notes" create --title "Smoke test" --body "written by make verify" >/dev/null && \
		"$(CURDIR)/notes" list && \
		"$(CURDIR)/notes" delete --id smoke-test >/dev/null && \
		rm -rf $$tmp
