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

// dlc's version, HARDCODED — and the one dogfood gap that is a real constraint
// rather than neglect.
//
// Every scaffolded app does `platform.SetVersion(dlcconfig.Display())`, reading
// the value from its dlc.toml. dlc now has a dlc.toml too, and `dlc gen` writes
// the same `gen/go/dlcconfig` for it. But dlc cannot IMPORT it: dlcconfig is
// written by the dlc binary, and the dlc binary is built from this package — so
// depending on it makes the build depend on its own output.
//
// Breaking the cycle needs a standalone generator (the manifest parser lives in
// `hosts/native`, package main, so it would have to move first). Recorded in the
// dogfood checklist rather than papered over; until then this string and
// dlc.toml's `version` are two places, and the dogfood review is what catches
// them disagreeing.
const version = "dlc 0.0.0-bootstrap"

// initialVersion is what a freshly scaffolded project starts at. It lands in
// dlc.toml, and every other copy (the engine's version string, the web
// package) is generated from there rather than typed again.
const initialVersion = "0.1.0"

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
	// NO DEFAULT TIER SET. An empty list is a caller that did not say, and
	// choosing for them means scaffolding a slot layout nobody picked — a tier
	// is a directory of host code plus a `dlc.toml` entry that is then checked
	// to exist, so the wrong guess is something a user has to undo by hand.
	//
	// Refused HERE, in the engine, so it holds on every tier: the CLI marks
	// `--tiers` required and the browser has checkboxes, but neither of those is
	// what makes the rule true. A front end that forgot to ask would otherwise
	// quietly get the old default back.
	if len(req.Tiers) == 0 {
		return errors.New("new: no tiers requested — pass at least one of: " +
			strings.Join(supportedTiers, ", "))
	}
	for _, tier := range req.Tiers {
		if !supportedTier(tier) {
			return errors.New("new: tier " + strconv.Quote(tier) +
				" is not supported yet (have: " + strings.Join(supportedTiers, ", ") + ")")
		}
	}
	for _, cap := range req.Caps {
		if cap != "console" && cap != "filesystem" {
			return errors.New("new: capability " + strconv.Quote(cap) +
				" is not supported yet; only console and filesystem exist")
		}
	}
	// --ui still has one shape per tier: the web tier ships a vanilla-TS UI, and
	// there is no second UI to choose between yet. Selecting one is a template
	// variant question, not a tier question.
	if req.Ui == dlcv1.UiKind_UI_KIND_REACT {
		return errors.New("new: --ui REACT is not supported yet; the web tier scaffolds a vanilla-TS UI")
	}
	if req.Storage != dlcv1.StorageKind_STORAGE_KIND_UNSPECIFIED &&
		req.Storage != dlcv1.StorageKind_STORAGE_KIND_NONE {
		return errors.New("new: --storage " + req.Storage.String() +
			" is not supported yet; the template emits no storage layer")
	}
	return nil
}

// Tier slots — where each tier's HOST code lives (Decision 34). One directory
// per tier, and `root` in dlc.toml names it. Constants rather than literals
// because the scaffold, the manifest defaults, and the npm re-base all have to
// agree on the same path, and they are in three different files.
const (
	nativeSlot = "hosts/native"
	webSlot    = "hosts/web"
)

// tierSections renders the [tiers.*] blocks for dlc.toml.
//
// Built here rather than in the template because the set is dynamic: the
// template renders one string, and this decides what that string contains.
func tierSections(tiers []string) string {
	var b strings.Builder
	for _, t := range tiers {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[tiers." + t + "]\n")
		b.WriteString(`capabilities = ["console", "filesystem"]` + "\n")
		switch t {
		case "native":
			b.WriteString("# This tier's HOST code — argv in, a proto request out.\n")
			b.WriteString(`root      = "` + nativeSlot + `"` + "\n")
		case "web":
			b.WriteString("# This tier's HOST code, and where `dlc build web` writes into it.\n")
			b.WriteString("# The assets MUST sit inside the slot: jco's loader fetches the core\n")
			b.WriteString("# .wasm at run time, and a dev server will not serve a path outside\n")
			b.WriteString("# its root.\n")
			b.WriteString(`root      = "` + webSlot + `"` + "\n")
			b.WriteString(`assets    = "` + webSlot + `/src/wasm"` + "\n")
			b.WriteString(`component = "build/engine.component.wasm"` + "\n")
		}
	}
	return b.String()
}

// supportedTiers are the tiers the template can actually emit. Requesting one
// outside this list is refused by name rather than silently ignored.
var supportedTiers = []string{"native", "web"}

func supportedTier(name string) bool {
	for _, t := range supportedTiers {
		if t == name {
			return true
		}
	}
	return false
}

// requestedTiers is what the project will declare in dlc.toml, in canonical
// order rather than request order.
//
// NO DEFAULT. An empty list is an error, not "give them everything" — see
// validateNew. A tier is a directory of host code plus a `dlc.toml` entry that
// is checked to exist, so choosing one on a caller's behalf means scaffolding a
// layout nobody asked for. Every front end asks the question its own way: the
// CLI marks `--tiers` required, the browser has checkboxes.
func requestedTiers(req *dlcv1.NewRequest) []string {
	out := make([]string, 0, len(req.Tiers))
	for _, t := range supportedTiers { // canonical order, not request order
		for _, r := range req.Tiers {
			if r == t {
				out = append(out, t)
				break
			}
		}
	}
	return out
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
		// The vocabulary is PROJECT, matching dlc.toml — `dlc new` creates a
		// project, and one word for one thing beats two.
		"ProjectName":    req.Name,
		"ProjectVersion": initialVersion,
		"Module":         module,
		"PkgName":        identifier(req.Name),
		// The raw path AND the composed go.mod line. The web tier needs the bare
		// path for an npm `file:` dependency, which is the TypeScript half of the
		// same bootstrap: depend on the local checkout until the packages are
		// published. Empty is legal — the templates then emit instructions
		// instead of a silently broken dependency.
		"PlatformPath": req.PlatformPath,
		// Same location, one directory deeper. `frontend/package.json` needs an
		// npm `file:` path relative to ITSELF, not to the project root — using
		// the root-relative one silently resolves to a sibling directory that
		// does not exist, and npm reports only "package not found".
		"PlatformPathFrontend": platformPathFrom(webSlot, req.PlatformPath),
		"PlatformReplace":      platformReplace(req.PlatformPath),
		// The manifest must describe what was actually emitted — a [tiers.web]
		// section in a project with no frontend/ would be a lie the build would
		// later trip over.
		"TierSections": tierSections(requestedTiers(req)),
	}
}

// platformPathFrom re-bases a project-root-relative platform path for a file in
// a subdirectory. Absolute paths are already location-independent and pass
// through unchanged.
//
// The depth comes from `subdir`, which it did NOT before the web slot moved to
// `hosts/web`: this hard-coded a single "..", ignored its own argument, and was
// correct only by coincidence while every caller was one level down. A silently
// wrong `file:` dependency is an npm install resolving to nothing, reported
// nowhere near the manifest that caused it.
func platformPathFrom(subdir, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	parts := []string{}
	for _, seg := range strings.Split(filepath.ToSlash(subdir), "/") {
		if seg != "" && seg != "." {
			parts = append(parts, "..")
		}
	}
	return filepath.Join(append(parts, path)...)
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
	files, err = scaffoldFiles(scaffoldVars(req), requestedTiers(req))
	if err != nil {
		return "", nil, errors.New("new: " + err.Error())
	}
	if err := platform.WriteTree(dest, files); err != nil {
		return "", nil, errors.New("new: " + err.Error())
	}
	return root, files, nil
}
