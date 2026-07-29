package platform

// Filesystem errors, worded by the ENGINE rather than by whichever runtime it
// happens to be on.
//
// THE PROBLEM THIS SOLVES. The parity check diffs the error STRING as well as
// the result, which is what makes "native and wasm agree on envelope errors" a
// checked claim. But a raw OS error is not portable text:
//
//	native     export-fs: open app-default: no such file or directory
//	component  export-fs: open /app-default: file does not exist
//
// Two divergences in one line. The PATH differs because `Root()` is a relative
// directory natively and `/` under WASI, and the joined path lands in the
// message. The WORDING differs because Go's `os` and TinyGo's WASI runtime
// phrase the same condition differently. So any command whose error wrapped an
// OS error was un-parity-able, and the vectors only stayed green by never
// touching such a path — a trip-wire rather than a guarded boundary.
//
// THE FIX HAS TWO HALVES, and both are needed:
//
//  1. Report the path the CALLER named — relative to the root — not the joined
//     absolute one. A caller asked for "app-default"; that is the only spelling
//     that means anything to them, and it is the same on every tier.
//  2. Word the reason here, from a portable probe, instead of forwarding the
//     runtime's text.

import (
	"errors"
	"os"
	"path/filepath"
)

// FSError describes a filesystem failure in tier-independent words.
//
// `op` is the command ("export-fs"), `rel` the path as the caller named it,
// relative to the root.
//
// A CAVEAT WORTH STATING: a permission failure is reported as "does not exist",
// because the two cannot be told apart portably. `AGENTS.md` §2 records why —
// `os.IsNotExist` and `errors.Is(err, fs.ErrNotExist)` do not match TinyGo's
// WASI errno, so classifying by inspecting the error is exactly the trap this
// package has already paid for once. Probing with Stat and not parsing anything
// is the portable option; being occasionally imprecise beats being
// tier-dependent, because a message that differs per tier is a parity failure
// and a message that is merely coarse is not.
func FSError(op, rel string, err error) error {
	if err == nil {
		return nil
	}
	if rel == "" {
		rel = "." // the root itself
	}
	if !pathExists(rel) {
		return errors.New(op + ": " + rel + ": does not exist")
	}
	return errors.New(op + ": " + rel + ": cannot be read")
}

// pathExists probes without classifying.
//
// Deliberately does not look at WHY Stat failed: that is the errno trap above.
// The question asked is only "is something there", which both tiers answer the
// same way.
func pathExists(rel string) bool {
	clean, err := SafeJoin("", rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(Root(), clean))
	return err == nil
}
