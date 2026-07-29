// dlc-platform — what every ILC app INHERITS (§16.3, §16.4).
//
// A SEPARATE MODULE, in this repo for now. The module path is already the one
// it will have when it moves out, so that split is a directory move plus a tag
// rather than a second rewrite of every import in every app.
//
// Named for its destination, resolved by `replace` until then: `go get` on this
// path will not work yet, and is not meant to.
module github.com/devalbo/dlc-platform

go 1.23.0

require (
	github.com/aperturerobotics/protobuf-go-lite v0.15.0
	github.com/peterbourgon/ff/v3 v3.4.0
	go.bytecodealliance.org/cm v0.3.0
	google.golang.org/protobuf v1.36.11
)

require github.com/aperturerobotics/json-iterator-lite v1.1.0 // indirect
