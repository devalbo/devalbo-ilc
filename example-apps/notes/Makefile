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
	cp -R gen/ts/notes hosts/web/src/gen/

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
