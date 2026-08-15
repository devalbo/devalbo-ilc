package cli

// The generated CLI surface is only worth having if the runner turns it into the
// right request bytes. These tests decode what was built with protowire rather
// than comparing against a golden blob: a byte-for-byte fixture would say
// "different" without saying which field, and the field number is the whole
// contract.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/devalbo/devalbo-ilc/dlc-platform"
	"github.com/devalbo/devalbo-ilc/dlc-platform/clispec"
	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// fakePort records the request it was given and answers with canned bytes.
// Local and unexported: one test package needs it, so it does not yet earn a
// shared helper.
type fakePort struct {
	method  uint32
	request []byte
	result  platform.Result
}

func (f *fakePort) Execute(method uint32, request []byte) platform.Result {
	f.method = method
	f.request = append([]byte(nil), request...)
	if f.result.Success || f.result.Err != "" {
		return f.result
	}
	return platform.Result{Success: true}
}

const methodTest uint32 = 4242

// field pulls one field out of an encoded request, so an assertion can name the
// field NUMBER — which is what the wire actually carries.
func field(t *testing.T, encoded []byte, num protowire.Number) (string, uint64, bool) {
	t.Helper()
	for len(encoded) > 0 {
		n, typ, taglen := protowire.ConsumeTag(encoded)
		if taglen < 0 {
			t.Fatalf("bad tag: %v", protowire.ParseError(taglen))
		}
		encoded = encoded[taglen:]
		switch typ {
		case protowire.BytesType:
			v, l := protowire.ConsumeBytes(encoded)
			if l < 0 {
				t.Fatal("bad bytes field")
			}
			if n == num {
				return string(v), 0, true
			}
			encoded = encoded[l:]
		case protowire.VarintType:
			v, l := protowire.ConsumeVarint(encoded)
			if l < 0 {
				t.Fatal("bad varint field")
			}
			if n == num {
				return "", v, true
			}
			encoded = encoded[l:]
		default:
			t.Fatalf("unexpected wire type %v", typ)
		}
	}
	return "", 0, false
}

func testApp(t *testing.T, cmd clispec.Command, port *fakePort, stdin string) (App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	return App{
		Name:     "t",
		Commands: []clispec.Command{cmd},
		Port:     port,
		Stdout:   &out,
		Stderr:   &errOut,
		Stdin:    strings.NewReader(stdin),
		Render:   map[uint32]Renderer{cmd.Method: nil},
	}, &out, &errOut
}

func TestEncodesByFieldNumber(t *testing.T) {
	cmd := clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{
			{Name: "title", Field: 1, Kind: clispec.KindString},
			{Name: "count", Field: 7, Kind: clispec.KindInt64},
			{Name: "on", Field: 3, Kind: clispec.KindBool},
		},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")

	// `-on` is a SWITCH: no value. This test used to pass `-on true`, which was the
	// ceremony the bool handling removed — the assertion below is unchanged, so it
	// is the interface that improved rather than the expectation that moved.
	if code := app.Run([]string{"go", "-title", "hello", "-count", "42", "-on"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if port.method != methodTest {
		t.Errorf("dispatched method %d, want %d", port.method, methodTest)
	}
	if s, _, ok := field(t, port.request, 1); !ok || s != "hello" {
		t.Errorf("field 1: %q (present=%v), want hello", s, ok)
	}
	if _, n, ok := field(t, port.request, 7); !ok || n != 42 {
		t.Errorf("field 7: %d (present=%v), want 42", n, ok)
	}
	if _, n, ok := field(t, port.request, 3); !ok || n != 1 {
		t.Errorf("field 3: %d, want 1", n)
	}
}

// A field the user did not set must be ABSENT, not zero: proto3 cannot tell the
// two apart on the wire, but sending an explicit zero for every unset flag would
// mean a partial update silently blanks fields.
func TestUnsetFlagsAreAbsent(t *testing.T) {
	cmd := clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{
			{Name: "title", Field: 1, Kind: clispec.KindString},
			{Name: "body", Field: 2, Kind: clispec.KindString},
		},
	}
	port := &fakePort{}
	app, _, _ := testApp(t, cmd, port, "")
	app.Run([]string{"go", "-title", "only"})

	if _, _, ok := field(t, port.request, 2); ok {
		t.Error("unset --body was encoded anyway")
	}
}

func TestRequiredFlagIsEnforced(t *testing.T) {
	cmd := clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{{Name: "title", Field: 1, Kind: clispec.KindString, Required: true}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")

	if code := app.Run([]string{"go"}); code == 0 {
		t.Fatal("a missing required flag must not succeed")
	}
	if !strings.Contains(errOut.String(), "--title is required") {
		t.Errorf("error should name the flag: %q", errOut.String())
	}
	if port.request != nil {
		t.Error("the engine must not be called when a required flag is missing")
	}
}

func TestDefaultApplies(t *testing.T) {
	cmd := clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{{Name: "mode", Field: 1, Kind: clispec.KindString, Default: "merge"}},
	}
	port := &fakePort{}
	app, _, _ := testApp(t, cmd, port, "")
	app.Run([]string{"go"})

	if s, _, _ := field(t, port.request, 1); s != "merge" {
		t.Errorf("default not applied: %q", s)
	}
}

// Fill runs BEFORE required is checked, so a host supplying the clock satisfies
// a required created_at and the user is not told to pass something the host is
// about to overwrite.
func TestFillSatisfiesRequired(t *testing.T) {
	cmd := clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{{Name: "at", Field: 4, Kind: clispec.KindInt64, Required: true}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")
	app.Fill = func(_ clispec.Command, values map[string][]string) {
		values["at"] = []string{"1700000000"}
	}

	if code := app.Run([]string{"go"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if _, n, _ := field(t, port.request, 4); n != 1700000000 {
		t.Errorf("filled value not encoded: %d", n)
	}
}

func TestEnumNameToNumber(t *testing.T) {
	cmd := clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{{
			Name: "mode", Field: 3, Kind: clispec.KindEnum,
			EnumValues: []string{"MODE_UNSPECIFIED", "MODE_MERGE", "MODE_REPLACE"},
		}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")

	if code := app.Run([]string{"go", "-mode", "MODE_REPLACE"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if _, n, _ := field(t, port.request, 3); n != 2 {
		t.Errorf("MODE_REPLACE encoded as %d, want 2", n)
	}

	// An unknown name must be refused by NAME, listing what is allowed — a
	// number silently out of range would reach the engine as a valid-looking
	// enum nobody defined.
	port2 := &fakePort{}
	app2, _, errOut2 := testApp(t, cmd, port2, "")
	if code := app2.Run([]string{"go", "-mode", "MODE_SIDEWAYS"}); code == 0 {
		t.Fatal("an unknown enum value must not succeed")
	}
	if !strings.Contains(errOut2.String(), "MODE_MERGE") {
		t.Errorf("error should list the permitted values: %q", errOut2.String())
	}
}

// The redesign that made `bytes` expressible: a value's SOURCE is declared, so
// the runner reads a file rather than each host inventing an @file convention.
func TestBytesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{{Name: "bundle", Field: 1, Kind: clispec.KindBytes, Source: clispec.SourceFile}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")

	if code := app.Run([]string{"go", "-bundle", path}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, _ := field(t, port.request, 1); s != `{"a":1}` {
		t.Errorf("file contents not encoded: %q", s)
	}

	// A missing file names the flag AND the path — either could be the mistake.
	port2 := &fakePort{}
	app2, _, errOut2 := testApp(t, cmd, port2, "")
	if code := app2.Run([]string{"go", "-bundle", filepath.Join(dir, "nope.json")}); code == 0 {
		t.Fatal("a missing file must not succeed")
	}
	if !strings.Contains(errOut2.String(), "--bundle") || !strings.Contains(errOut2.String(), "nope.json") {
		t.Errorf("error should name flag and path: %q", errOut2.String())
	}
}

func TestStdinSources(t *testing.T) {
	cmd := clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{{Name: "bundle", Field: 1, Kind: clispec.KindBytes, Source: clispec.SourceStdin}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "piped content")
	if code := app.Run([]string{"go", "-bundle", "ignored"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, _ := field(t, port.request, 1); s != "piped content" {
		t.Errorf("stdin not encoded: %q", s)
	}
}

// `-` means stdin even on a file-sourced flag, so a bundle can be piped without
// the schema having to predict it.
func TestDashMeansStdin(t *testing.T) {
	cmd := clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{{Name: "bundle", Field: 1, Kind: clispec.KindBytes, Source: clispec.SourceFile}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "from a pipe")
	if code := app.Run([]string{"go", "-bundle", "-"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, _ := field(t, port.request, 1); s != "from a pipe" {
		t.Errorf("piped bundle not encoded: %q", s)
	}
}

func TestRepeatedFlag(t *testing.T) {
	cmd := clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{{Name: "tier", Field: 2, Kind: clispec.KindString, Repeated: true}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")
	if code := app.Run([]string{"go", "-tier", "native", "-tier", "web"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	// Both values must survive: a repeated field encoded once is a silently
	// truncated list.
	count := 0
	rest := port.request
	for len(rest) > 0 {
		_, _, taglen := protowire.ConsumeTag(rest)
		rest = rest[taglen:]
		_, l := protowire.ConsumeBytes(rest)
		rest = rest[l:]
		count++
	}
	if count != 2 {
		t.Errorf("encoded %d values, want 2", count)
	}
}

// Giving a non-repeated flag twice is a mistake worth naming, not a last-one-wins
// surprise.
func TestNonRepeatedRejectsSecondValue(t *testing.T) {
	cmd := clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{{Name: "title", Field: 1, Kind: clispec.KindString}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")
	if code := app.Run([]string{"go", "-title", "a", "-title", "b"}); code == 0 {
		t.Fatal("a repeated value on a scalar flag must not succeed")
	}
	if !strings.Contains(errOut.String(), "not repeated") {
		t.Errorf("error should say why: %q", errOut.String())
	}
}

// Errors ride the result envelope on every tier — a failing command must exit
// non-zero and print what the engine said.
func TestEngineErrorSurfaces(t *testing.T) {
	cmd := clispec.Command{Name: "go", Method: methodTest}
	port := &fakePort{result: platform.Result{Err: "no such note"}}
	app, _, errOut := testApp(t, cmd, port, "")

	if code := app.Run([]string{"go"}); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "no such note") {
		t.Errorf("engine error not reported: %q", errOut.String())
	}
}

// A command with no renderer is a BUILD error, not a silent success: forgetting
// to print a response should not look like a command that worked quietly.
func TestMissingRendererIsRefused(t *testing.T) {
	var out, errOut bytes.Buffer
	app := App{
		Name:     "t",
		Commands: []clispec.Command{{Name: "go", Method: methodTest}},
		Port:     &fakePort{},
		Render:   map[uint32]Renderer{}, // deliberately empty
		Stdout:   &out,
		Stderr:   &errOut,
	}
	if code := app.Run([]string{"go"}); code == 0 {
		t.Fatal("a command with no renderer must not run")
	}
	if !strings.Contains(errOut.String(), "no renderer") {
		t.Errorf("error should say what is missing: %q", errOut.String())
	}
}

func TestRendererReceivesOutput(t *testing.T) {
	cmd := clispec.Command{Name: "go", Method: methodTest}
	port := &fakePort{result: platform.Result{Success: true, Output: []byte("payload")}}
	var out, errOut bytes.Buffer
	app := App{
		Name: "t", Commands: []clispec.Command{cmd}, Port: port,
		Stdout: &out, Stderr: &errOut,
		Render: map[uint32]Renderer{methodTest: func(w io.Writer, response []byte) error {
			_, err := w.Write(response)
			return err
		}},
	}
	if code := app.Run([]string{"go"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if out.String() != "payload" {
		t.Errorf("renderer output: %q", out.String())
	}
}

// A subcommand nobody declared must not be silently ignored.
func TestUnknownSubcommand(t *testing.T) {
	cmd := clispec.Command{Name: "go", Method: methodTest}
	port := &fakePort{}
	app, _, _ := testApp(t, cmd, port, "")
	if code := app.Run([]string{"nope"}); code == 0 {
		t.Error("an unknown subcommand must not succeed")
	}
	if port.request != nil {
		t.Error("an unknown subcommand must not reach the engine")
	}
}

// Positionals — without them every command is flags-only, which reads worse
// than the hand-written CLIs this replaces (`dlc new --name myapp` is a
// regression, not a migration).
func TestPositionalArguments(t *testing.T) {
	cmd := clispec.Command{
		Name: "new", Method: methodTest,
		Flags: []clispec.Flag{
			{Name: "name", Field: 1, Kind: clispec.KindString, Required: true, Positional: 1},
			{Name: "module", Field: 2, Kind: clispec.KindString},
		},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")
	if code := app.Run([]string{"new", "-module", "example.com/x", "myapp"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, _ := field(t, port.request, 1); s != "myapp" {
		t.Errorf("positional not encoded: %q", s)
	}
	if s, _, _ := field(t, port.request, 2); s != "example.com/x" {
		t.Errorf("flag alongside positional: %q", s)
	}
}

// The flag spelling keeps working, so scripts have both forms.
func TestPositionalAlsoAcceptsItsFlag(t *testing.T) {
	cmd := clispec.Command{
		Name: "new", Method: methodTest,
		Flags: []clispec.Flag{{Name: "name", Field: 1, Kind: clispec.KindString, Positional: 1}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")
	if code := app.Run([]string{"new", "-name", "myapp"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, _ := field(t, port.request, 1); s != "myapp" {
		t.Errorf("flag form not encoded: %q", s)
	}
}

// Giving both is a mistake worth naming rather than resolving silently in
// whichever order the parser happened to run.
func TestPositionalAndFlagTogetherIsRefused(t *testing.T) {
	cmd := clispec.Command{
		Name: "new", Method: methodTest,
		Flags: []clispec.Flag{{Name: "name", Field: 1, Kind: clispec.KindString, Positional: 1}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")
	if code := app.Run([]string{"new", "-name", "a", "b"}); code == 0 {
		t.Fatal("both spellings at once must not silently pick one")
	}
	if !strings.Contains(errOut.String(), "unexpected argument") {
		t.Errorf("error should name the surplus argument: %q", errOut.String())
	}
}

// A repeated positional takes the remainder — `dlc echo one two three`.
func TestRepeatedPositionalTakesTheRest(t *testing.T) {
	cmd := clispec.Command{
		Name: "echo", Method: methodTest,
		Flags: []clispec.Flag{{Name: "args", Field: 1, Kind: clispec.KindString, Repeated: true, Positional: 1}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")
	if code := app.Run([]string{"echo", "one", "two", "three"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	count := 0
	rest := port.request
	for len(rest) > 0 {
		_, _, taglen := protowire.ConsumeTag(rest)
		rest = rest[taglen:]
		_, l := protowire.ConsumeBytes(rest)
		rest = rest[l:]
		count++
	}
	if count != 3 {
		t.Errorf("encoded %d values, want 3", count)
	}
}

// A missing required positional reports the FLAG name, which is the only name a
// user can act on.
func TestMissingRequiredPositional(t *testing.T) {
	cmd := clispec.Command{
		Name: "new", Method: methodTest,
		Flags: []clispec.Flag{{Name: "name", Field: 1, Kind: clispec.KindString, Required: true, Positional: 1}},
	}
	port := &fakePort{}
	app, _, errOut := testApp(t, cmd, port, "")
	if code := app.Run([]string{"new"}); code == 0 {
		t.Fatal("a missing required positional must not succeed")
	}
	if !strings.Contains(errOut.String(), "--name is required") {
		t.Errorf("error should name it: %q", errOut.String())
	}
}

// A host lifecycle verb is dispatchable but not typeable.
//
// SetEnvironment is a command like any other — the host sends it by id — but
// generating a SUBCOMMAND for it would ask a person to hand-write a capability
// manifest on a command line, and would oblige every host to register a
// renderer for a command with nothing to render. That is what broke
// verify-bundle-xtier the moment the rpc was added, which is why `cli_hidden`
// exists and why this pins it.
func TestHiddenCommandsAreNotInTheSurface(t *testing.T) {
	for _, c := range ilcv1.PlatformServiceCLI {
		if c.Method == platform.MethodSetEnvironment {
			t.Fatalf("set-environment is in the CLI surface as %q; it is (cli_hidden)", c.Name)
		}
	}
	// Guard the guard: a surface that lost everything would also pass the
	// check above.
	for _, want := range []string{"version", "export-fs", "import-fs", "reset-fs"} {
		if _, ok := clispec.Find(ilcv1.PlatformServiceCLI, want); !ok {
			t.Fatalf("%q vanished from the CLI surface", want)
		}
	}
}

// --- the surface reflects the environment (ENVIRONMENT-PLAN phase 3) --------

// revision advances so each helper call states genuinely new facts.
var revision uint32

// runAgainstLiveEngine drives the CLI against the REAL registry rather than a
// scripted fake, because what is under test here is registration itself — and a
// fake by construction has none.
func runAgainstLiveEngine(t *testing.T, fsAvailability ilcv1.Availability, args ...string) (string, string, int) {
	t.Helper()
	// Registration is process-global and idempotent, so no reset is needed —
	// registerBlock skips what is already there and syncCapabilityVerbs removes
	// what should not be.
	platform.RegisterDiscovered()
	if err := platform.SetFSRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// A DISTINCT revision per call, and this is not bookkeeping: an unchanged
	// revision is a deliberate no-op (plan D11), so reusing 1 would leave the
	// third test asserting against the first test's facts and passing or failing
	// for reasons unconnected to what it claims to test.
	revision++
	body, err := (&ilcv1.SetEnvironmentRequest{Environment: &ilcv1.Environment{
		Revision:   revision,
		Filesystem: &ilcv1.Filesystem{Availability: fsAvailability, Kind: ilcv1.FilesystemKind_FILESYSTEM_KIND_APP_DIR},
	}}).MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	if res := platform.Execute(platform.MethodSetEnvironment, body); !res.Success {
		t.Fatalf("set-environment: %s", res.Err)
	}

	var stdout, stderr bytes.Buffer
	code := App{
		Name:     "t",
		Commands: ilcv1.PlatformServiceCLI,
		Port:     platform.Live,
		Render: map[uint32]Renderer{
			platform.MethodVersion:  func(io.Writer, []byte) error { return nil },
			platform.MethodExportFs: func(io.Writer, []byte) error { return nil },
			platform.MethodImportFs: func(io.Writer, []byte) error { return nil },
			platform.MethodResetFs:  func(io.Writer, []byte) error { return nil },
			// Every command in the generated surface owes a renderer, whether or
			// not this fixture's engine registers it — that is the missing-renderer
			// rule, and leaving one out here would fail every test in this helper
			// with a message about the wrong command.
			platform.MethodRebuildIndex: func(io.Writer, []byte) error { return nil },
		},
		Stdout: &stdout,
		Stderr: &stderr,
		Stdin:  strings.NewReader(""),
	}.Run(args)
	return stdout.String(), stderr.String(), code
}

// A command this host cannot provide fails by SAYING SO, not with the internal
// dispatch error. "unknown method_id 100" reads like the user mistyped
// something; the truth is that the host has no filesystem.
func TestUnavailableCommandExplainsItself(t *testing.T) {
	_, stderr, code := runAgainstLiveEngine(t, ilcv1.Availability_AVAILABILITY_ABSENT, "export-fs")
	if code == 0 {
		t.Fatal("export-fs succeeded on a host with no filesystem")
	}
	if strings.Contains(stderr, "unknown method_id") {
		t.Fatalf("leaked the internal dispatch error: %q", stderr)
	}
	if !strings.Contains(stderr, "does not provide") {
		t.Fatalf("error does not explain the cause: %q", stderr)
	}
}

// And it stays VISIBLE. Filtering it out would leave a user who read the docs
// hunting for a command that silently vanished, with the generated surface and
// the live registry disagreeing and nothing reconciling them.
func TestUnavailableCommandIsStillListed(t *testing.T) {
	stdout, stderr, _ := runAgainstLiveEngine(t, ilcv1.Availability_AVAILABILITY_ABSENT, "-h")
	help := stdout + stderr
	if !strings.Contains(help, "export-fs") {
		t.Fatalf("export-fs vanished from the help instead of being marked: %q", help)
	}
	if !strings.Contains(help, "unavailable on this host") {
		t.Fatalf("export-fs is listed but not marked: %q", help)
	}
}

// The mark must track the manifest, not the schema: with a filesystem, the same
// command is ordinary again and carries no annotation.
func TestAvailableCommandIsNotMarked(t *testing.T) {
	stdout, stderr, _ := runAgainstLiveEngine(t, ilcv1.Availability_AVAILABILITY_PRESENT, "-h")
	help := stdout + stderr
	if !strings.Contains(help, "export-fs") {
		t.Fatalf("export-fs missing from the help entirely: %q", help)
	}
	// Scoped to export-fs's own LINE, not the whole help text. It read the whole
	// text until rebuild-index arrived — a command this fixture's app genuinely
	// does not provide (no index rebuilder), and therefore genuinely marked. A
	// blanket "nothing anywhere is marked" was asserting something broader than
	// this test's own name, and the first honestly-unavailable command broke it.
	if strings.Contains(helpLine(help, "export-fs"), "unavailable on this host") {
		t.Fatalf("export-fs marked unavailable on a host that has a filesystem: %q", help)
	}
}

// helpLine returns the help line naming cmd, so an assertion about ONE command
// cannot be tripped by another.
func helpLine(help, cmd string) string {
	for _, line := range strings.Split(help, "\n") {
		if strings.Contains(line, cmd) {
			return line
		}
	}
	return ""
}

// --- flags after positionals (the permute step) -----------------------------

// positionalCmd is `t go <name> [--title x]` — one positional, one plain flag.
func positionalCmd() clispec.Command {
	return clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{
			{Name: "name", Field: 1, Kind: clispec.KindString, Positional: 1},
			{Name: "title", Field: 2, Kind: clispec.KindString, Short: "T"},
		},
	}
}

// THE BUG: `t go myapp --title x` failed with `unexpected argument "--title"`,
// because Go's flag stops at the first non-flag argument and hands the rest over
// as positionals. The generated usage line advertises exactly this order.
func TestFlagsMayFollowPositionals(t *testing.T) {
	port := &fakePort{}
	app, _, errOut := testApp(t, positionalCmd(), port, "")

	if code := app.Run([]string{"go", "myapp", "--title", "hello"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, ok := field(t, port.request, 1); !ok || s != "myapp" {
		t.Errorf("positional: %q (present=%v), want myapp", s, ok)
	}
	if s, _, ok := field(t, port.request, 2); !ok || s != "hello" {
		t.Errorf("flag after positional: %q (present=%v), want hello", s, ok)
	}
}

// The order that always worked must keep working — a permutation that fixed one
// spelling by breaking the other would be no improvement.
func TestFlagsMayStillPrecedePositionals(t *testing.T) {
	port := &fakePort{}
	app, _, errOut := testApp(t, positionalCmd(), port, "")

	if code := app.Run([]string{"go", "-T", "hello", "myapp"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, ok := field(t, port.request, 1); !ok || s != "myapp" {
		t.Errorf("positional: %q, want myapp", s)
	}
	if s, _, ok := field(t, port.request, 2); !ok || s != "hello" {
		t.Errorf("short flag: %q, want hello", s)
	}
}

// `--flag=value` carries its own value, so permuting must not drag the next
// token along with it — that token is the positional.
func TestJoinedFlagFormKeepsItsPositional(t *testing.T) {
	port := &fakePort{}
	app, _, errOut := testApp(t, positionalCmd(), port, "")

	if code := app.Run([]string{"go", "--title=hello", "myapp"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, ok := field(t, port.request, 1); !ok || s != "myapp" {
		t.Errorf("positional: %q, want myapp", s)
	}
	if s, _, ok := field(t, port.request, 2); !ok || s != "hello" {
		t.Errorf("joined flag: %q, want hello", s)
	}
}

// `--` is the escape hatch for a positional that begins with a dash. Without it
// there is no way to name a task "--weird", and permuting makes the need sharper
// rather than creating it.
func TestDoubleDashEndsFlags(t *testing.T) {
	port := &fakePort{}
	app, _, errOut := testApp(t, positionalCmd(), port, "")

	if code := app.Run([]string{"go", "--title", "x", "--", "--weird"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, ok := field(t, port.request, 1); !ok || s != "--weird" {
		t.Errorf("positional after --: %q, want --weird", s)
	}
}

// An unrecognised flag must still be reported as one. Permuting only moves flags
// the command actually declares, so an unknown token cannot be quietly
// reinterpreted as a positional — which would turn a typo into a wrong value.
func TestUnknownFlagStillFails(t *testing.T) {
	port := &fakePort{}
	app, _, errOut := testApp(t, positionalCmd(), port, "")

	if code := app.Run([]string{"go", "myapp", "--nope", "x"}); code == 0 {
		t.Fatalf("unknown flag accepted; stderr: %s", errOut.String())
	}
}

// --- booleans are switches, not questions ----------------------------------

func switchCmd() clispec.Command {
	return clispec.Command{
		Name: "go", Method: methodTest,
		Flags: []clispec.Flag{
			{Name: "name", Field: 1, Kind: clispec.KindString, Positional: 1},
			{Name: "force", Field: 2, Kind: clispec.KindBool},
			// A third flag AFTER the positional is what makes the permutation
			// observable — see TestSwitchBeforePositionalWithTrailingFlag.
			{Name: "title", Field: 3, Kind: clispec.KindString},
		},
	}
}

// `--force` means true. Requiring `--force true` was ceremony nobody should have
// to type, and it is what this did before.
func TestBoolFlagNeedsNoValue(t *testing.T) {
	port := &fakePort{}
	app, _, errOut := testApp(t, switchCmd(), port, "")

	if code := app.Run([]string{"go", "myapp", "--force"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, ok := field(t, port.request, 1); !ok || s != "myapp" {
		t.Errorf("positional: %q, want myapp", s)
	}
	if _, n, ok := field(t, port.request, 2); !ok || n != 1 {
		t.Errorf("bool field: %d (present=%v), want 1", n, ok)
	}
}

// A SWITCH BEFORE A POSITIONAL must not eat it. The permutation moves known flags
// ahead of positionals and used to assume every flag consumes the next token —
// which would have moved `myapp` into the flag list and left nothing behind.
func TestBoolFlagDoesNotSwallowThePositional(t *testing.T) {
	port := &fakePort{}
	app, _, errOut := testApp(t, switchCmd(), port, "")

	if code := app.Run([]string{"go", "--force", "myapp"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, ok := field(t, port.request, 1); !ok || s != "myapp" {
		t.Errorf("positional: %q, want myapp", s)
	}
	if _, n, ok := field(t, port.request, 2); !ok || n != 1 {
		t.Errorf("bool field: %d, want 1", n)
	}
}

// The explicit form survives, because `--force=false` is how you override a
// default that is true.
func TestBoolFlagAcceptsExplicitValue(t *testing.T) {
	port := &fakePort{}
	app, _, errOut := testApp(t, switchCmd(), port, "")

	if code := app.Run([]string{"go", "--force=false", "myapp"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	// Explicitly false is still ABSENT on the wire: proto3 cannot tell a false
	// from an unset field, and sending zeros for every unset flag is what makes a
	// partial update blank things it never mentioned.
	if _, n, ok := field(t, port.request, 2); ok && n != 0 {
		t.Errorf("bool field: %d, want absent or 0", n)
	}
}

// THE CASE THAT ACTUALLY EXERCISES THE PERMUTATION, and the one the first two
// tests missed.
//
// `go --force myapp --title x`: if permuting treats `--force` as taking a value it
// drags `myapp` into the flag section, and stdlib flag then stops at that
// non-flag token — leaving `myapp --title x` as three positionals for a command
// that declares one. The earlier tests stayed green with the guard removed,
// because a swallowed token still ended up trailing where the positional parser
// could find it. This one does not.
func TestSwitchBeforePositionalWithTrailingFlag(t *testing.T) {
	port := &fakePort{}
	app, _, errOut := testApp(t, switchCmd(), port, "")

	if code := app.Run([]string{"go", "--force", "myapp", "--title", "x"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s, _, ok := field(t, port.request, 1); !ok || s != "myapp" {
		t.Errorf("positional: %q, want myapp", s)
	}
	if _, n, ok := field(t, port.request, 2); !ok || n != 1 {
		t.Errorf("switch: %d, want 1", n)
	}
	if s, _, ok := field(t, port.request, 3); !ok || s != "x" {
		t.Errorf("trailing flag: %q, want x", s)
	}
}
