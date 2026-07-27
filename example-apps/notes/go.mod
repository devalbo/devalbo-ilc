module github.com/devalbo/devalbo-ilc/example-apps/notes

go 1.23.0

// The ILC platform: command dispatch, the filesystem root seam, path
// containment, BFT bundles, and the inherited verbs (version, export-fs,
// import-fs, reset-fs). Depend on it — never copy it in — so a fix upstream
// reaches this app on a version bump.
require github.com/devalbo/devalbo-ilc v0.0.0

require github.com/aperturerobotics/protobuf-go-lite v0.15.0

require (
	github.com/aperturerobotics/json-iterator-lite v1.1.0 // indirect
	go.bytecodealliance.org/cm v0.3.0 // indirect
)

// BOOTSTRAP: ilc-platform is not published yet. This `replace` points at a
// local checkout of devalbo-ilc; delete it once the module is tagged and
// `go get github.com/devalbo/devalbo-ilc` resolves on its own.
replace github.com/devalbo/devalbo-ilc => ../..
