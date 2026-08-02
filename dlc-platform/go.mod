// dlc-platform — what every ILC app INHERITS (§16.3, §16.4).
//
// A SEPARATE MODULE, in this repo ON PURPOSE — the path names the directory it
// lives in, which is what makes it fetchable.
//
// It was `github.com/devalbo/dlc-platform` for a while, named for a repo it was
// expected to move to. That name could never be fetched: Go resolves a module
// path to its OWN repo, so a module in a subdirectory of devalbo-ilc cannot
// answer to a path that claims to be a repo of its own. Renamed 2026-08-02.
//
// Fetch it like any other module in a monorepo — the tag carries the
// subdirectory prefix, which is Go's rule and not a convention we picked:
//
//	go get github.com/devalbo/devalbo-ilc/dlc-platform@v0.1.0   ← tag: dlc-platform/v0.1.0
//
// Splitting it out later is still possible and still cheap; it is a directory
// move plus a path change, exactly as it is today. What it is NOT is a
// prerequisite for anyone outside this repo using the platform.
module github.com/devalbo/devalbo-ilc/dlc-platform

go 1.23.0

require (
	github.com/aperturerobotics/protobuf-go-lite v0.15.0
	github.com/peterbourgon/ff/v3 v3.4.0
	go.bytecodealliance.org/cm v0.3.0
	google.golang.org/protobuf v1.36.11
)

require github.com/aperturerobotics/json-iterator-lite v1.1.0 // indirect
