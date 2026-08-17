//go:build !darwin && !freebsd && !netbsd && !openbsd && !dragonfly && !linux

package main

import "fmt"

// makeRaw refuses on a platform with no termios.
//
// # Why this file exists
//
// `raw_bsd.go` and `raw_linux.go` were written as "the two platforms that
// matter" — a laptop and a CI runner. CI also cross-builds this tool for
// WINDOWS, so the package stopped compiling there entirely: not "the serial
// port will not work on Windows", which would be a fair limitation, but
// `undefined: makeRaw`, which is a broken build.
//
// The lesson is narrow and worth keeping: a build tag that names the platforms
// you thought of leaves every platform you did not think of with NO
// implementation. The fallback has to be written at the same time as the first
// specialisation, not when something notices.
//
// # Why it refuses rather than doing nothing
//
// Returning nil would leave the port in whatever mode it happened to be in, and
// this tool's whole framing depends on raw mode: a cooked terminal rewrites 0x0A
// on the way out and eats 0x11 and 0x13 on the way in, so frames would be
// corrupted SELECTIVELY, depending on their bytes. A tool that cannot guarantee
// the link says so at the point of connection instead of producing intermittent,
// payload-dependent failures that look like a fault in the badge.
//
// Making this work on Windows means the Win32 serial API (`SetCommState` and a
// `DCB`), which is a real port and not a stub — worth doing when somebody wants
// to drive a badge from Windows, and not worth guessing at before then.
func makeRaw(fd uintptr) error {
	return fmt.Errorf("raw serial mode is not implemented on this platform")
}
