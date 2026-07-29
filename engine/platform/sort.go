package platform

import "sort"

// sortStrings keeps sorting in one place: several platform outputs are ordered
// so that native and wasm runs produce identical bytes (the parity contract).
func sortStrings(s []string) { sort.Strings(s) }

// sortUint32 orders method ids for the same reason. Go's map iteration order is
// deliberately unspecified, so an unsorted surface would differ between two runs
// of the SAME binary — a parity flake that reads exactly like a real divergence.
func sortUint32(s []uint32) { sort.Slice(s, func(i, j int) bool { return s[i] < s[j] }) }
