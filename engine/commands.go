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
	"strconv"
	"strings"

	"github.com/devalbo/devalbo-ilc/engine/platform"
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
)

const version = "dlc 0.0.0-bootstrap"

// dlc's own method ids, GENERATED from commands.proto and re-exported for
// callers (tests, the parity runner). They live in the **app band** (10000+);
// 1–9999 is reserved for ILC itself. `dlc` is an app like any other.
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
	// Refuse before touching the filesystem — a rejected request must not leave
	// a half-written tree behind.
	if err := checkScaffoldOptions(req); err != nil {
		return nil, err
	}
	root, files, err := scaffold(req)
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

// checkScaffoldOptions refuses options the template cannot honor yet.
//
// These fields exist in the schema and the web UI sends all of them, but the
// single template emits one shape: native tier, no UI, no storage layer. Until
// the template can vary (which needs the web host and per-file conditions),
// ACCEPTING them and quietly emitting something else is the worst option — the
// user gets a project that contradicts what they asked for, with nothing to
// indicate why. Say no instead, and name what is supported.
func checkScaffoldOptions(req *dlcv1.NewRequest) error {
	for _, tier := range req.Tiers {
		if tier != "native" {
			return errors.New("new: tier " + strconv.Quote(tier) +
				" is not supported yet; the template emits the native tier only")
		}
	}
	for _, cap := range req.Caps {
		if cap != "console" && cap != "filesystem" {
			return errors.New("new: capability " + strconv.Quote(cap) +
				" is not supported yet; only console and filesystem exist")
		}
	}
	if req.Ui != dlcv1.UiKind_UI_KIND_UNSPECIFIED && req.Ui != dlcv1.UiKind_UI_KIND_NONE {
		return errors.New("new: --ui " + req.Ui.String() +
			" is not supported yet; the template has no web host, so only UI_KIND_NONE is emitted")
	}
	if req.Storage != dlcv1.StorageKind_STORAGE_KIND_UNSPECIFIED &&
		req.Storage != dlcv1.StorageKind_STORAGE_KIND_NONE {
		return errors.New("new: --storage " + req.Storage.String() +
			" is not supported yet; the template emits no storage layer")
	}
	return nil
}

// scaffoldVars maps the COMMAND to the template dictionary.
//
// This is the one table to edit when `dlc new` grows an option: add the field to
// NewRequest in commands.proto, add a line here, and `{{YourToken}}` is live in
// every template. Command input and template input are the same thing, written
// down once — no second schema for the scaffolder to drift from.
//
// Derived values (PkgName, the platform replace line) are computed here rather
// than in the template, so the derivation is testable and the templates stay
// logic-free.
func scaffoldVars(req *dlcv1.NewRequest) map[string]string {
	module := req.Module
	if module == "" {
		module = defaultModule(req.Name)
	}
	return map[string]string{
		"AppName": req.Name,
		"Module":  module,
		"PkgName": identifier(req.Name),
		// The raw path AND the composed go.mod line. The web tier needs the bare
		// path for an npm `file:` dependency, which is the TypeScript half of the
		// same bootstrap: depend on the local checkout until the packages are
		// published. Empty is legal — the templates then emit instructions
		// instead of a silently broken dependency.
		"PlatformPath":    req.PlatformPath,
		"PlatformReplace": platformReplace(req.PlatformPath),
	}
}

// identifier makes an app name safe for a proto package and a Go import alias:
// anything but a letter or digit becomes '_', and a leading digit gets a prefix.
// `dlc new my-app` is fine on the command line but `package my-app` is not.
func identifier(app string) string {
	var b strings.Builder
	for _, r := range app {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.ToLower(b.String())
	if out == "" {
		return "app"
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "app" + out
	}
	return out
}

// platformReplace emits the go.mod line that lets a generated project resolve
// the platform module.
//
// BOOTSTRAP ONLY: ilc-platform is not published yet, so a scaffolded project
// cannot `go get` it. A `replace` pointing at a local checkout is the smallest
// thing that builds, and it is deliberately loud about being temporary — delete
// it the day the module is tagged. Without a path we emit the instructions
// rather than a broken build with no explanation.
func platformReplace(path string) string {
	const why = "// BOOTSTRAP: ilc-platform is not published yet. This `replace` points at a\n" +
		"// local checkout of devalbo-ilc; delete it once the module is tagged and\n" +
		"// `go get github.com/devalbo/devalbo-ilc` resolves on its own.\n"
	if path == "" {
		return why + "// replace github.com/devalbo/devalbo-ilc => /path/to/devalbo-ilc\n" +
			"//\n// ^ uncomment and set the path, or re-run `dlc new` with --platform-path."
	}
	return why + "replace github.com/devalbo/devalbo-ilc => " + path
}

// scaffold renders the template tree and writes it. One implementation, reached
// only through handleNew — the argv shim builds a NewRequest and dispatches like
// any other caller, so there is no second scaffolding path to drift.
func scaffold(req *dlcv1.NewRequest) (root string, files []platform.File, err error) {
	if req.Name == "" {
		return "", nil, errors.New("new: missing <app> name")
	}
	// `root` is what we report — always relative, so a native run and a wasm run
	// describe the same tree. `dest` is where it actually lands, anchored at the
	// tier's filesystem root.
	root, err = platform.SafeJoin("", req.Name)
	if err != nil {
		return "", nil, errors.New("new: " + err.Error())
	}
	dest := filepath.Join(platform.Root(), root)
	if platform.DirIsOccupied(dest) {
		return "", nil, errors.New("new: " + req.Name + " already exists and is not empty")
	}
	files, err = scaffoldFiles(scaffoldVars(req))
	if err != nil {
		return "", nil, errors.New("new: " + err.Error())
	}
	if err := platform.WriteTree(dest, files); err != nil {
		return "", nil, errors.New("new: " + err.Error())
	}
	return root, files, nil
}
