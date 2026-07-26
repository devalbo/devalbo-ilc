package engine

// dlc's OWN commands (Decision 30) — App #1's app-specific verbs. The verbs
// every app inherits (version, export-fs, import-fs, reset-fs) come from
// engine/platform; dlc depends on them exactly the way a scaffolded app does,
// which is what keeps the platform boundary honest.
//
// Each handler is `func(*FooRequest) (*FooResponse, error)` — typed messages, no
// envelope. platform.TypedHandler wraps them into the byte-level registry shape.

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/devalbo/devalbo-ilc/engine/platform"
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
)

const version = "dlc 0.0.0-bootstrap"

// dlc's own method ids, GENERATED from commands.proto and re-exported for
// callers (tests, the parity runner). They live in the **app range** (1000+);
// 1–999 belongs to the platform, in per-capability blocks.
const (
	MethodNew  = dlcv1.MethodNew
	MethodEcho = dlcv1.MethodEcho
)

// This init is the shape every scaffolded app's init will have: inherit the
// platform's verbs, tell it who you are, then register your own — and never
// write an id down, because the generated Handlers map carries them.
func init() {
	platform.RegisterAll()
	platform.SetVersion(version)

	platform.RegisterRaw(dlcv1.DlcServiceHandlers(handleNew, handleEcho))
}

func handleEcho(req *dlcv1.EchoRequest) (*dlcv1.EchoResponse, error) {
	return &dlcv1.EchoResponse{Text: strings.Join(req.Args, " ")}, nil
}

func handleNew(req *dlcv1.NewRequest) (*dlcv1.NewResponse, error) {
	root, _, files, err := scaffold(req.Name, req.Module)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	// Path is the scaffold root relative to the host-bound filesystem root (§5.2).
	return &dlcv1.NewResponse{Path: root, Files: paths}, nil
}

// scaffold is the one implementation behind both `new` entry points (the proto
// handler and the argv shim), so the two can never drift. It writes the tree to
// the host-bound filesystem root and returns what it wrote.
//
// Note what it does NOT do: path containment, directory creation, and the root
// seam all come from the platform. An app writes template rendering; the platform
// writes the dangerous parts once.
func scaffold(name, module string) (root, resolvedModule string, files []platform.File, err error) {
	if name == "" {
		return "", "", nil, errors.New("new: missing <app> name")
	}
	if module == "" {
		module = defaultModule(name)
	}
	// `root` is what we report — always relative, so a native run and a wasm run
	// describe the same tree. `dest` is where it actually lands, anchored at the
	// tier's filesystem root.
	root, err = platform.SafeJoin("", name)
	if err != nil {
		return "", "", nil, errors.New("new: " + err.Error())
	}
	dest := filepath.Join(platform.Root(), root)
	if platform.DirIsOccupied(dest) {
		return "", "", nil, errors.New("new: " + name + " already exists and is not empty")
	}
	files = scaffoldFiles(name, module)
	if err := platform.WriteTree(dest, files); err != nil {
		return "", "", nil, errors.New("new: " + err.Error())
	}
	return root, module, files, nil
}
