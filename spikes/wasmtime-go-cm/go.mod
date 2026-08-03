// Its own module ON PURPOSE: wasmtime-go ships prebuilt native libraries and is
// a heavy dependency. It stays out of the repo's go.mod until it earns a place.
module spike/wasmtimecm

go 1.23.0

require github.com/bytecodealliance/wasmtime-go/v47 v47.0.0
