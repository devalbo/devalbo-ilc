// Spike 3 — OPFS filesystem persistence (T-B1.3).
//
// Engine handlers exercise Go os.WriteFile / os.ReadFile under wasip2. The
// browser host maps the WASI root to an in-memory preview2-shim tree that is
// hydrated from / flushed to OPFS (see opfs-bridge.js). Modes:
//
//	["write", path, content] → write content to path (relative to "/")
//	["read", path]           → return file bytes
//
// See spikes/README.md (Spike 3).
package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/engine"
	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/types"
	"go.bytecodealliance.org/cm"
)

func init() {
	engine.Exports.ExecuteCli = executeCli
}

func executeCli(args cm.List[string]) engine.CommandResult {
	a := args.Slice()
	if len(a) == 0 {
		return fail("usage: write <path> <content> | read <path>")
	}
	switch a[0] {
	case "write":
		if len(a) < 3 {
			return fail("usage: write <path> <content>")
		}
		path := cleanPath(a[1])
		content := strings.Join(a[2:], " ")
		if dir := filepath.Dir(path); dir != "/" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fail("mkdir: " + err.Error())
			}
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fail("write: " + err.Error())
		}
		return ok([]byte("wrote:" + path))
	case "read":
		if len(a) < 2 {
			return fail("usage: read <path>")
		}
		b, err := os.ReadFile(cleanPath(a[1]))
		if err != nil {
			return fail("read: " + err.Error())
		}
		return ok(b)
	default:
		return fail("unknown mode: " + a[0])
	}
}

func cleanPath(p string) string {
	p = filepath.Clean(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func ok(out []byte) engine.CommandResult {
	return types.CommandResult{
		Success: true,
		Output:  cm.ToList(out),
		Error:   cm.None[string](),
	}
}

func fail(msg string) engine.CommandResult {
	return types.CommandResult{
		Success: false,
		Output:  cm.ToList([]byte(nil)),
		Error:   cm.Some(msg),
	}
}

func main() {}
