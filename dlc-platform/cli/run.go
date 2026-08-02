package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/peterbourgon/ff/v3/ffcli"

	"github.com/devalbo/devalbo-ilc/dlc-platform"
	"github.com/devalbo/devalbo-ilc/dlc-platform/clispec"
	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// Renderer prints one response. Nil means the command prints nothing.
//
// This is the ONLY hand-written part of a command, and deliberately so: how a
// `ListRecordsResponse` should look is a presentation decision, which belongs to
// the tier slot (Decision 34). Everything above it — which subcommands exist,
// which flags they take, what is required — comes from the schema.
type Renderer func(out io.Writer, response []byte) error

// App is a native tier slot's command line, built from a generated surface.
type App struct {
	// Name is the program name, used in usage.
	Name  string
	Short string

	// Commands is the generated surface, usually one or more `…ServiceCLI`
	// slices concatenated — an app's own plus the platform's inherited verbs.
	Commands []clispec.Command

	// Port is the engine. Injected rather than reached for, so a test can run
	// the whole command line against a fake (Decision 34).
	Port platform.EnginePort

	// Local supplies handlers for commands the HOST serves rather than the engine
	// (Decision 30, `host_local` in the .proto): `dlc gen`, `dlc build`.
	//
	// WHY THEY GO THROUGH THE SAME SURFACE. They used to be a hand-written map in
	// the host's main(), checked before the CLI ran — which meant they never
	// appeared in `--help`. The two commands every tutorial tells you to run were
	// invisible in the tool's own command list. Declaring them in the .proto puts
	// their name and summary in the one place every other command's comes from;
	// this map is where their behaviour attaches.
	//
	// The handler receives the SAME encoded request an engine handler would —
	// flags, positionals, defaults and required-ness all parsed from the schema.
	// The only difference is that nothing crosses a boundary: the bytes are
	// handed straight over instead of going through `Port.Execute`.
	//
	// Bytes rather than a typed message because this package cannot know an app's
	// types; the handler decodes with the generated `UnmarshalVT`, exactly as the
	// engine side does.
	Local map[uint32]func(request []byte) error

	// Render maps a method id to its printer. A command with no entry is an
	// ERROR at run time, not a silent no-op: forgetting to print a response is
	// a bug, and it should not look like a command that succeeded quietly.
	Render map[uint32]Renderer

	// Fill supplies values the user should not have to type. The standing case
	// is the clock: the engine has no clock capability, because a browser tab
	// and an MCU disagree about what one is, so "now" is native input exactly
	// like argv.
	Fill func(cmd clispec.Command, values map[string][]string)

	Stdout io.Writer
	Stderr io.Writer
	// Stdin backs fields declared CLI_SOURCE_STDIN (and `--flag -`). Optional:
	// a host that supplies none turns such a flag into a named error rather
	// than a hang on a stream nobody is writing to.
	Stdin io.Reader
}

// Run parses args, dispatches, and returns a process exit code.
func (a App) Run(args []string) int {
	root, err := a.build()
	if err != nil {
		fmt.Fprintf(a.Stderr, "%s: %v\n", a.Name, err)
		return 2
	}
	if err := root.ParseAndRun(context.Background(), a.permute(args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0 // -h is what the user asked for, not a failure
		}
		fmt.Fprintf(a.Stderr, "%s: %v\n", a.Name, err)
		return 1
	}
	return 0
}

func (a App) build() (*ffcli.Command, error) {
	commands := append([]clispec.Command(nil), a.Commands...)
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })

	live := a.liveSurface()

	subs := make([]*ffcli.Command, 0, len(commands))
	for _, cmd := range commands {
		sub, err := a.subcommand(cmd)
		if err != nil {
			return nil, err
		}
		// A HOST-LOCAL verb is never in the engine's registry, so the live surface
		// has nothing to say about it — asking would mark `gen` and `build`
		// permanently unavailable, which is exactly what happened the first time
		// this ran.
		if live != nil && !cmd.Local && !live[cmd.Method] {
			sub = unavailable(cmd, sub)
		}
		subs = append(subs, sub)
	}

	// Help goes to the App's OWN stderr, not the process's. App takes its
	// writers as arguments so a slot can be driven by a test (Decision 34) —
	// but ffcli prints usage through the FlagSet, whose default output is
	// os.Stderr, so until now `-h` escaped the seam entirely and no test could
	// see what a user reads.
	rootFlags := flag.NewFlagSet(a.Name, flag.ContinueOnError)
	if a.Stderr != nil {
		rootFlags.SetOutput(a.Stderr)
	}

	return &ffcli.Command{
		Name:        a.Name,
		ShortHelp:   a.Short,
		ShortUsage:  a.Name + " <command> [flags]",
		Subcommands: subs,
		FlagSet:     rootFlags,
		Exec: func(_ context.Context, args []string) error {
			// ffcli routes anything it does not recognise to the ROOT Exec, so
			// without this a typo'd subcommand printed the help and exited 0 —
			// success, as far as any script calling it could tell.
			if len(args) > 0 {
				return fmt.Errorf("unknown command %q (try `%s -h`)", args[0], a.Name)
			}
			return flag.ErrHelp // bare invocation: show the command list
		},
	}, nil
}

// liveSurface asks the engine which commands are actually registered, or
// returns nil when it cannot tell.
//
// NIL IS A THIRD STATE, not a default. "Available", "not available" and "this
// engine cannot say" are genuinely different, and only the first two justify
// changing what the user sees. A port that does not answer is either a test
// fake with scripted replies or an engine older than GetCommandSurface;
// neither should make the CLI refuse to run, and neither licenses claiming a
// command is missing when nobody said so.
func (a App) liveSurface() map[uint32]bool {
	if a.Port == nil {
		return nil
	}
	req, err := (&ilcv1.GetCommandSurfaceRequest{}).MarshalVT()
	if err != nil {
		return nil
	}
	res := a.Port.Execute(platform.MethodGetCommandSurface, req)
	if !res.Success {
		return nil
	}
	var resp ilcv1.GetCommandSurfaceResponse
	if err := resp.UnmarshalVT(res.Output); err != nil {
		return nil
	}
	live := make(map[uint32]bool, len(resp.MethodIds))
	for _, id := range resp.MethodIds {
		live[id] = true
	}
	// A TRUTHFUL SURFACE CONTAINS THE ID THAT ANSWERED IT. Anything else did not
	// really answer: a scripted fake returns success for every method and decodes
	// to an empty list, which would otherwise read as "this engine has no
	// commands at all" and mark every one of them unavailable. Self-consistency
	// is the cheapest way to tell a real answer from a plausible-looking one.
	if !live[platform.MethodGetCommandSurface] {
		return nil
	}
	return live
}

// unavailable keeps a command VISIBLE but refuses to run it.
//
// Hiding it would be worse: a user who read the docs would find the command
// silently missing, with the generated surface and the live registry disagreeing
// and nothing reconciling them. And letting it dispatch would surface an
// internal "unknown method_id 100" — an error that reads like the user mistyped
// something, when in fact this host simply cannot do it.
func unavailable(cmd clispec.Command, sub *ffcli.Command) *ffcli.Command {
	sub.ShortHelp = strings.TrimSpace(sub.ShortHelp + " (unavailable on this host)")
	sub.Exec = func(context.Context, []string) error {
		return fmt.Errorf("this host does not provide the capability %q needs", cmd.Name)
	}
	return sub
}

func (a App) subcommand(cmd clispec.Command) (*ffcli.Command, error) {
	// A host-local verb never reaches the engine, so it owes no renderer and
	// takes no request — but it does owe a handler, and a declared command that
	// silently does nothing is worse than a build error (the same stance the
	// missing-renderer check takes).
	var local func([]byte) error
	if cmd.Local {
		var ok bool
		local, ok = a.Local[cmd.Method]
		if !ok {
			return nil, fmt.Errorf("command %q (method %d) is host-local but no handler is registered", cmd.Name, cmd.Method)
		}
	}

	// A host-local verb owes no renderer: it prints its own progress as it drives
	// the toolchain, and there is no response message to format.
	var renderer Renderer
	if !cmd.Local {
		var ok bool
		renderer, ok = a.Render[cmd.Method]
		if !ok {
			return nil, fmt.Errorf("command %q (method %d) has no renderer registered", cmd.Name, cmd.Method)
		}
	}

	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	if a.Stderr != nil {
		fs.SetOutput(a.Stderr)
	}
	values := map[string][]string{}

	for _, f := range cmd.Flags {
		// flag.Func appends rather than assigns, which gives repeated fields
		// their natural command-line form (--tier native --tier web) with no
		// extra machinery.
		collect := func(s string) error {
			values[f.Name] = append(values[f.Name], s)
			return nil
		}
		// A BOOLEAN IS A SWITCH, not a field you have to answer. `--no-open`
		// means true; requiring `--no-open true` is the kind of ceremony nobody
		// should have to type, and it is what this did before. `--no-open=false`
		// still works, because BoolFunc keeps the explicit form.
		register := fs.Func
		if f.Kind == clispec.KindBool {
			register = fs.BoolFunc
		}
		register(f.Name, usageFor(f), collect)
		if f.Short != "" {
			// Registered as a second name on the same target. stdlib flag has no
			// separate short/long concept — which is the point: it accepts
			// `-title` as well as `--title`, where pflag would read that as a
			// cluster of shorthands (Spike 4, case 3).
			register(f.Short, usageFor(f)+" (short for --"+f.Name+")", collect)
		}
	}

	return &ffcli.Command{
		Name:       cmd.Name,
		ShortHelp:  shortHelp(cmd),
		ShortUsage: a.Name + " " + cmd.Name + usageSuffix(cmd),
		FlagSet:    fs,
		Exec: func(_ context.Context, rest []string) error {
			if err := assignPositionals(cmd, rest, values); err != nil {
				return err
			}
			return a.exec(cmd, values, renderer, local)
		},
	}, nil
}

func (a App) exec(cmd clispec.Command, values map[string][]string, render Renderer, local func([]byte) error) error {
	// Defaults first, so Fill and the user both override them.
	for _, f := range cmd.Flags {
		if f.Default != "" && len(values[f.Name]) == 0 {
			values[f.Name] = []string{f.Default}
		}
	}
	if a.Fill != nil {
		a.Fill(cmd, values)
	}
	// Required is checked AFTER Fill: a host that supplies the clock has
	// satisfied a required `created_at`, and the user should not be told to
	// pass something the host is going to overwrite.
	for _, f := range cmd.Flags {
		if f.Required && len(values[f.Name]) == 0 {
			// The help text comes along: a required flag whose error says only
			// its name leaves the reader to go and find out what it wants, and
			// the schema already knows.
			msg := fmt.Sprintf("%s: --%s is required", cmd.Name, f.Name)
			if f.Help != "" {
				msg += " — " + f.Help
			}
			if len(f.EnumValues) > 0 {
				msg += " (one of: " + strings.Join(f.EnumValues, ", ") + ")"
			}
			return errors.New(msg)
		}
	}

	request, err := encodeRequest(cmd, values, a.Stdin)
	if err != nil {
		return err
	}

	// A HOST-LOCAL verb stops here: same parsing, same encoding, no boundary
	// (Decision 30). Everything above this line is shared with engine commands,
	// which is the point — one grammar, one help text, one place defaults live.
	if local != nil {
		return local(request)
	}

	result := a.Port.Execute(cmd.Method, request)
	if !result.Success {
		// Errors ride the envelope, not exceptions — the same on every tier.
		return errors.New(result.Err)
	}
	if render == nil {
		return nil
	}
	return render(a.Stdout, result.Output)
}

// assignPositionals maps bare arguments onto the fields that declared a
// position, so `dlc new myapp` works as well as `dlc new --name myapp`.
//
// A value already given as a FLAG wins and does not consume a positional slot:
// `dlc new --name a b` is a mistake worth naming, not a silent overwrite in
// whichever order the parser happened to run.
func assignPositionals(cmd clispec.Command, rest []string, values map[string][]string) error {
	positionals := cmd.Positionals()
	if len(positionals) == 0 {
		if len(rest) > 0 {
			return fmt.Errorf("%s takes flags, not positional arguments (got %q)", cmd.Name, strings.Join(rest, " "))
		}
		return nil
	}

	i := 0
	for _, f := range positionals {
		if len(values[f.Name]) > 0 {
			continue // supplied as a flag
		}
		if i >= len(rest) {
			break // absent; `required` decides whether that is an error
		}
		if f.Repeated {
			// Last by construction (the generator enforces it), so it takes
			// everything remaining — `dlc echo one two three`.
			values[f.Name] = append(values[f.Name], rest[i:]...)
			i = len(rest)
			break
		}
		values[f.Name] = []string{rest[i]}
		i++
	}

	if i < len(rest) {
		return fmt.Errorf("%s: unexpected argument %q", cmd.Name, rest[i])
	}
	return nil
}

func usageFor(f clispec.Flag) string {
	parts := []string{}
	if f.Help != "" {
		parts = append(parts, f.Help)
	}
	if len(f.EnumValues) > 0 {
		parts = append(parts, "one of: "+strings.Join(f.EnumValues, ", "))
	}
	if f.Required {
		parts = append(parts, "(required)")
	}
	if f.Repeated {
		parts = append(parts, "(repeatable)")
	}
	if len(parts) == 0 {
		return "(no help declared — add (devalbo.options.v1.help) in the .proto)"
	}
	return strings.Join(parts, " ")
}

func shortHelp(cmd clispec.Command) string {
	if len(cmd.Unsupported) == 0 {
		return cmd.Summary
	}
	// Said out loud rather than dropped: a command that silently ignores part of
	// its request is worse than one that admits it cannot set it here.
	note := "(" + strings.Join(cmd.Unsupported, ", ") + " cannot be set from the command line)"
	if cmd.Summary == "" {
		return note
	}
	return cmd.Summary + " " + note
}

func usageSuffix(cmd clispec.Command) string {
	var parts []string
	positional := map[string]bool{}
	for _, f := range cmd.Positionals() {
		positional[f.Name] = true
		token := "<" + f.Name + ">"
		if f.Repeated {
			token = "[" + f.Name + "...]"
		} else if !f.Required {
			token = "[" + f.Name + "]"
		}
		parts = append(parts, token)
	}
	for _, f := range cmd.Flags {
		if f.Required && !positional[f.Name] {
			parts = append(parts, "--"+f.Name+" <"+f.Name+">")
		}
	}
	if len(parts) == 0 {
		return " [flags]"
	}
	return " " + strings.Join(parts, " ") + " [flags]"
}

// permute moves a subcommand's flags ahead of its positionals.
//
// WHY THIS IS NEEDED. Go's `flag` stops parsing at the first non-flag argument
// and hands everything after it to the command as positionals. So the spelling
// the generated usage line advertises — `dlc new <name> --tiers native` — failed
// with `unexpected argument "--tiers"`, while the same flags placed BEFORE the
// name worked. A tool whose own `--help` prints a command that does not run is
// worse than one with a documented restriction, and "flags may only come first"
// is a restriction nobody expects from a modern CLI.
//
// ONLY THE TAIL, and only after a recognised subcommand: the root FlagSet has no
// flags of its own, and routing depends on the subcommand name being the first
// non-flag token. Reordering across that boundary would break dispatch.
//
// ONLY KNOWN FLAGS ARE MOVED. An unrecognised `-x` stays where it is so
// `flag` reports it as undefined, rather than being silently reinterpreted; and
// because every generated flag takes a value (they are all registered with
// `flag.Func`), a known flag carries the token after it along.
//
// `--` ENDS IT, which is the escape hatch for a positional that begins with a
// dash — `db add -- --weird-title`.
func (a App) permute(args []string) []string {
	// Find the subcommand: the first token that is not a flag.
	name := -1
	for i, t := range args {
		if !strings.HasPrefix(t, "-") {
			name = i
			break
		}
	}
	if name < 0 {
		return args
	}
	var cmd *clispec.Command
	for i := range a.Commands {
		if a.Commands[i].Name == args[name] {
			cmd = &a.Commands[i]
			break
		}
	}
	if cmd == nil {
		return args // unknown command: let the root Exec say so
	}
	// HOST-LOCAL VERBS ARE NOT EXEMPT. They were, briefly, on the grounds that
	// they parsed their own argv — which stopped being true when their flags moved
	// into the .proto. `dlc build web --entry x` failed with "unexpected argument"
	// until this exemption came out: the reason for it had gone, but the code had
	// not.

	// Known flags, and which of them take a value. A BOOLEAN TAKES NONE, so
	// permuting must not drag the following token along with it — `--no-open web`
	// would otherwise move the tier into the flag list and leave no positional.
	known := map[string]bool{}
	takesValue := map[string]bool{}
	for _, f := range cmd.Flags {
		known[f.Name] = true
		takesValue[f.Name] = f.Kind != clispec.KindBool
		if f.Short != "" {
			known[f.Short] = true
			takesValue[f.Short] = f.Kind != clispec.KindBool
		}
	}

	tail := args[name+1:]
	flags := make([]string, 0, len(tail))
	pos := make([]string, 0, len(tail))
	terminated := false
	for i := 0; i < len(tail); i++ {
		t := tail[i]
		if t == "--" {
			// Kept, not consumed: the tokens after it may look like flags, and
			// `flag` needs the terminator to know they are not. Dropping it here
			// re-exposed exactly what the escape hatch exists to prevent.
			terminated = true
			pos = append(pos, tail[i+1:]...)
			break
		}
		if len(t) > 1 && strings.HasPrefix(t, "-") {
			bare := strings.TrimLeft(t, "-")
			if base, _, split := strings.Cut(bare, "="); split {
				// `--flag=value` carries its own value.
				_ = base
				flags = append(flags, t)
				continue
			}
			if known[bare] && takesValue[bare] && i+1 < len(tail) {
				flags = append(flags, t, tail[i+1])
				i++
				continue
			}
			flags = append(flags, t) // -h, or an unknown flag `flag` will reject
			continue
		}
		pos = append(pos, t)
	}

	out := make([]string, 0, len(args)+1)
	out = append(out, args[:name+1]...)
	out = append(out, flags...)
	if terminated {
		out = append(out, "--")
	}
	return append(out, pos...)
}
