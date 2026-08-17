# countdown — build & verify.
# Run inside `devbox shell`.

.PHONY: gen
gen: ## generate proto bindings, command dispatch, and dlcconfig from dlc.toml
	cd proto && buf lint
	dlc gen

.PHONY: build
build: gen ## build the native CLI (engine linked in-process)
	go build -o countdown ./hosts/native

.PHONY: run
run: build ## build then say hello
	./countdown greet --name ILC

.PHONY: test
test: ## unit tests
	go test ./...

.PHONY: gen-web
gen-web: gen ## copy generated es-lite messages into the Vite root
	# Bundlers resolve @aptre/* from the IMPORTING file's tree, and gen/ sits
	# outside hosts/web/ — so the messages are copied in rather than aliased.
	rm -rf hosts/web/src/gen
	mkdir -p hosts/web/src/gen
	cp -R gen/ts/countdown hosts/web/src/gen/
	# The INHERITED platform messages, put here by `dlc gen`. Without them a
	# browser front end cannot invoke version/export-fs/import-fs/reset-fs at
	# all — the Go host gets them from the platform module; TypeScript has no
	# equivalent, so dlc supplies them the same way it supplies the WIT world.
	@test -d gen/ts/devalbo/ilc && mkdir -p hosts/web/src/gen/devalbo && cp -R gen/ts/devalbo/ilc hosts/web/src/gen/devalbo/ || true

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
	@./countdown version
	@./countdown greet --name ILC
