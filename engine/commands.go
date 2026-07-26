package engine

// dlc's in-engine commands (Decision 30): the verbs that touch nothing but the
// filesystem / app-data, so the same handler runs in the terminal and in the
// browser. Toolchain verbs (build, run, verify, doctor, gen) are host-side
// orchestration and never appear here.
//
// Each handler is `func(*FooRequest) (*FooResponse, error)` — typed messages,
// no envelope. typedHandler wraps them into the byte-level registry shape.

import (
	"errors"
	"path/filepath"
	"strings"

	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
)

// Permanent method ids, mirroring the `(devalbo.options.v1.method_id)` options
// on DlcService in proto/devalbo/dlc/v1/commands.proto. These constants and the
// registration below are what protoc-gen-dlc-registry will generate (and lock)
// once the plugin lands; until then they are kept in sync by hand, and
// `buf breaking` guards the numbers themselves.
const (
	MethodVersion  uint32 = 1
	MethodEcho     uint32 = 2
	MethodNew      uint32 = 3
	MethodExportFs uint32 = 4
	MethodImportFs uint32 = 5
)

func init() {
	Register(MethodVersion, typedHandler(handleVersion))
	Register(MethodEcho, typedHandler(handleEcho))
	Register(MethodNew, typedHandler(handleNew))
	// MethodExportFs / MethodImportFs land with the filesystem capability seam
	// (§7.3); until then execute reports them as unregistered rather than
	// pretending to succeed.
}

func handleVersion(*dlcv1.VersionRequest) (*dlcv1.VersionResponse, error) {
	return &dlcv1.VersionResponse{Version: version}, nil
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
		paths = append(paths, f.path)
	}
	// Path is the scaffold root relative to the host-bound filesystem root (§5.2).
	return &dlcv1.NewResponse{Path: root, Files: paths}, nil
}

// scaffold is the one implementation behind both `new` entry points (the proto
// handler and the argv shim), so the two can never drift. It writes the tree to
// the host-bound filesystem root and returns what it wrote.
func scaffold(name, module string) (root, resolvedModule string, files []templateFile, err error) {
	if name == "" {
		return "", "", nil, errors.New("new: missing <app> name")
	}
	if module == "" {
		module = defaultModule(name)
	}
	// `root` is what we report — always relative, so a native run and a wasm run
	// describe the same tree. `dest` is where it actually lands, anchored at the
	// tier's filesystem root (fsRoot: cwd natively, the WASI preopen in wasm).
	root, err = safeJoin("", name)
	if err != nil {
		return "", "", nil, errors.New("new: " + err.Error())
	}
	dest := filepath.Join(fsRoot(), root)
	if dirIsOccupied(dest) {
		return "", "", nil, errors.New("new: " + name + " already exists and is not empty")
	}
	files = scaffoldFiles(name, module)
	if err := writeTree(dest, files); err != nil {
		return "", "", nil, errors.New("new: " + err.Error())
	}
	return root, module, files, nil
}
