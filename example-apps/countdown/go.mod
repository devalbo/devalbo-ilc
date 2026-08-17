module github.com/you/countdown

go 1.23.0

// The ILC platform: command dispatch, the filesystem root seam, path
// containment, BFT bundles, and the inherited verbs (version, export-fs,
// import-fs, reset-fs). Depend on it — never copy it in — so a fix upstream
// reaches this app on a version bump.
require github.com/devalbo/devalbo-ilc/dlc-platform v0.1.0

require github.com/aperturerobotics/protobuf-go-lite v0.15.0

require (
	github.com/aperturerobotics/json-iterator-lite v1.1.0 // indirect
	github.com/peterbourgon/ff/v3 v3.4.0 // indirect
	go.bytecodealliance.org/cm v0.3.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

// DEVELOPMENT: this `replace` points at the platform inside a local checkout, so
// the app builds against the code beside it rather than a published version.
// Same as every other example app here.
replace github.com/devalbo/devalbo-ilc/dlc-platform => ../../dlc-platform
