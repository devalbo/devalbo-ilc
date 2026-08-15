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
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/devalbo/devalbo-ilc/dlc-platform"
	dlcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/dlc/v1"
	"github.com/devalbo/devalbo-ilc/templates"
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
	// DISCOVERED rather than eager (Decision 32, plan D7): dlc's filesystem
	// verbs register when a host says there is a filesystem, and not before.
	//
	// Registration policy is an engine-side choice but the reason to discover is
	// a host-side fact, and the engine is ONE artifact across every tier
	// (Decision 26) — so an app cannot be eager natively and discovering in a
	// browser. dlc picks discovery because its web tier can genuinely lose OPFS,
	// and the price is that every dlc host must now send a manifest before
	// anything else: miss it and `export-fs` does not exist. platform.Boot is
	// what makes that safe to depend on.
	//
	// notes and tictactoe deliberately stay on RegisterAll, which keeps one app
	// on each policy and proves both work.
	platform.RegisterDiscovered()
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
			strings.Join(supportedTiers(), ", "))
	}
	for _, tier := range req.Tiers {
		if supportedTier(tier) {
			continue
		}
		// A DECLARED tier gets a different error from an unknown one, because the
		// two are different mistakes. "rp2350 has no skeleton yet" tells you
		// the name was right and the work is pending; "hologram is not a tier"
		// tells you to check your spelling. Collapsing them into one message
		// makes the roadmap look like a typo.
		if spec, declared := tierSpec(tier); declared {
			return errors.New("new: tier " + strconv.Quote(tier) +
				" is declared but has no template slot yet — " + spec.What +
				"; buildable today: " + strings.Join(supportedTiers(), ", "))
		}
		return errors.New("new: tier " + strconv.Quote(tier) +
			" is not a tier (buildable today: " + strings.Join(supportedTiers(), ", ") + ")")
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
	webSlot = "hosts/web"
)

// supportedTiers are the tiers `dlc new` will actually emit: the DECLARED
// landscape (tiers.go) intersected with what the template has a slot for.
//
// Both halves are load-bearing. The landscape names the tiers and claims their
// names; the template decides which of them can be produced today. A row with no
// slot directory stays unavailable no matter what it claims, and a slot directory
// with no row is a template that has outgrown its vocabulary — the tests in
// execute_test.go assert both directions.
//
// Canonical order comes from the landscape, not from the filesystem: the order
// reaches users as help text and as the order of `[tiers.*]` sections.
func supportedTiers() []string {
	emittable := templateSlots()
	out := make([]string, 0, len(TierLandscape))
	for _, t := range TierLandscape {
		if t.Status == TierAvailable && emittable[t.Name] {
			out = append(out, t.Name)
		}
	}
	return out
}

// SupportedTiers is the exported view, for tests that check the DERIVATION rather
// than a hard-coded expectation.
func SupportedTiers() []string { return supportedTiers() }

// templateSlots is the set of tiers the embedded template can emit, which is
// exactly its `hosts/*` directories (Decision 34, and tierOf says the same).
func templateSlots() map[string]bool {
	slots := map[string]bool{}
	entries, err := fs.ReadDir(templates.FS, templates.Root+"/hosts")
	if err != nil {
		return slots
	}
	for _, e := range entries {
		if e.IsDir() {
			slots[e.Name()] = true
		}
	}
	return slots
}

func supportedTier(name string) bool {
	for _, t := range supportedTiers() {
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
// validateNew. A tier is a directory of host code plus a `dlc.toml` entry that is
// checked to exist, so choosing one on a caller's behalf means scaffolding a
// layout nobody asked for. Every front end asks the question its own way: the CLI
// marks `--tiers` required, the browser has checkboxes.
func requestedTiers(req *dlcv1.NewRequest) []string {
	out := make([]string, 0, len(req.Tiers))
	for _, t := range supportedTiers() { // canonical order, not request order
		for _, r := range req.Tiers {
			if r == t {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// tierSections renders the [tiers.*] blocks for dlc.toml, from the landscape.
//
// Built here rather than in the template because the set is dynamic: the template
// renders one string, and this decides what that string contains. The per-tier
// prose and the extra web keys live in tiers.go, so adding a tier is a row rather
// than a case in a switch.
func tierSections(tiers []string) string {
	var b strings.Builder
	for _, name := range tiers {
		spec, ok := tierSpec(name)
		if !ok {
			continue // unreachable: validateNew refuses undeclared tiers first
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[tiers." + spec.Name + "]\n")
		b.WriteString(`capabilities = ["console", "filesystem"]` + "\n")
		if spec.Comment != "" {
			b.WriteString("# " + spec.Comment + "\n")
		}
		b.WriteString(`root      = "` + spec.slotRoot() + `"` + "\n")
		if spec.Assets != "" {
			b.WriteString(`assets    = "` + spec.Assets + `"` + "\n")
		}
		if spec.Component != "" {
			b.WriteString(`component = "` + spec.Component + `"` + "\n")
		}
	}
	return b.String()
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
		// What the web tier's package.json actually depends on. A LOCAL CHECKOUT
		// when one was named, a GIT REF otherwise — npm reads both, and the second
		// is what lets a scaffolded app build with no copy of this repo on disk.
		"PlatformWebDep": platformWebDep(webSlot, req.PlatformPath),
		// The version in the require line. A local checkout pins v0.0.0 and lets
		// the `replace` decide what that means; without one, the app requires a
		// real released version and fetches it.
		"PlatformGoVersion": platformGoVersion(req.PlatformPath),
		"PlatformReplace":   platformReplace(req.PlatformPath),
		// The manifest must describe what was actually emitted — a [tiers.web]
		// section in a project with no frontend/ would be a lie the build would
		// later trip over.
		"TierSections": tierSections(requestedTiers(req)),
	}
}

// PlatformWebRef is the git ref a scaffolded app depends on when no local
// checkout was named.
//
// A REF, NOT A REGISTRY VERSION, and the mechanism is worth stating because it
// constrains the layout: npm cannot install a package from a SUBDIRECTORY of a
// git repo — the `#fragment` is a committish, and there is no equivalent of
// pip's `#subdirectory=`. So `dlc-platform/web` is published as a branch whose
// ROOT is the package, produced by `scripts/release-dlc-web.sh` (a git subtree
// split) and tagged. That tag is this constant.
//
// Bump it in lockstep with the package's version when a release is cut; a ref
// that does not exist yet fails at `npm install` with a git error, which is
// loud but names the wrong problem.
const PlatformWebRef = "github:devalbo/devalbo-ilc#dlc-web-v0.1.0"

// PlatformGoVersion is the platform module version a scaffolded app requires
// when no local checkout was named — the Go counterpart of PlatformWebRef.
//
// Fetched by Go's SUBDIRECTORY RULE: the module lives at `dlc-platform/` inside
// this repo, so its tag carries that prefix (`dlc-platform/v0.1.0`) while the
// version an app records is plain `v0.1.0`. That rule is why the module could be
// renamed into the repo rather than needing one of its own.
//
// Bump it in the same commit as the tag. A version with no tag behind it fails
// at `go mod download` for someone who has never seen this repo, and passes
// every check inside it — verify-platform-ref.sh exists for exactly that gap.
const PlatformGoVersion = "v0.1.0"

// platformWebDep chooses between a local checkout and the published ref.
//
// The local path wins when given, because that is what development against an
// unreleased platform needs — and it is what this repo's own checks use, so CI
// exercises the file: path while a real user gets the git one.
func platformWebDep(subdir, path string) string {
	if path == "" {
		return PlatformWebRef
	}
	return "file:" + platformPathFrom(subdir, path) + "/dlc-platform/web"
}

// platformGoVersion picks the version the scaffold's go.mod requires.
//
// v0.0.0 with a local checkout, because the `replace` is what resolves it and a
// real version there would be a number nothing reads — the classic way a go.mod
// ends up claiming a dependency it does not have.
func platformGoVersion(path string) string {
	if path == "" {
		return PlatformGoVersion
	}
	return "v0.0.0"
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
// BOOTSTRAP ONLY: dlc-platform is not published yet, so a scaffolded project
// cannot `go get` it. A `replace` pointing at a local checkout is the smallest
// thing that builds, and it is deliberately loud about being temporary — delete
// it the day the module is tagged. Without a path we emit the instructions
// rather than a broken build with no explanation.
func platformReplace(path string) string {
	// NO REPLACE when no local checkout was named: the module is fetched from its
	// tag like any other dependency, which is the whole point of the rename. A
	// `replace` here would override the pinned version with a path that does not
	// exist on the user's machine.
	if path == "" {
		return ""
	}
	const why = "// DEVELOPMENT: this `replace` points at the platform inside a local\n" +
		"// devalbo-ilc checkout, because --platform-path was given. Delete it to use\n" +
		"// the released version instead — `go get github.com/devalbo/devalbo-ilc/dlc-platform`\n" +
		"// resolves on its own.\n"
	// PlatformPath names the devalbo-ilc checkout; the module is the platform
	// directory inside it (§16.4). A scaffolded app depends on the PLATFORM and
	// not on dlc — dlc is the tool that generated it, not something it links.
	return why + "replace github.com/devalbo/devalbo-ilc/dlc-platform => " + path + "/dlc-platform"
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
	dest := filepath.Join(platform.FSRoot(), root)
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
