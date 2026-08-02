module github.com/devalbo/devalbo-ilc

go 1.23.0

require (
	github.com/aperturerobotics/protobuf-go-lite v0.15.0
	github.com/tetratelabs/wazero v1.9.0
	go.bytecodealliance.org/cm v0.3.0
)

require (
	github.com/aperturerobotics/json-iterator-lite v1.1.0 // indirect
	github.com/peterbourgon/ff/v3 v3.4.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

// The platform is a separate module (§16.4) that has not been published: it is
// named for where it is going, and resolved from the tree until it gets there.
require github.com/devalbo/devalbo-ilc/dlc-platform v0.0.0

replace github.com/devalbo/devalbo-ilc/dlc-platform => ./dlc-platform
