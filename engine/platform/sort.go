package platform

import "sort"

// sortStrings keeps sorting in one place: several platform outputs are ordered
// so that native and wasm runs produce identical bytes (the parity contract).
func sortStrings(s []string) { sort.Strings(s) }
