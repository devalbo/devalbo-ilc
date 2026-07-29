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

	"github.com/devalbo/devalbo-ilc/engine/platform"
	"github.com/devalbo/devalbo-ilc/engine/platform/clispec"
	ilcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/v1"
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
	if err := root.ParseAndRun(context.Background(), args); err != nil {
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
		if live != nil && !live[cmd.Method] {
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
	renderer, ok := a.Render[cmd.Method]
	if !ok {
		return nil, fmt.Errorf("command %q (method %d) has no renderer registered", cmd.Name, cmd.Method)
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
		fs.Func(f.Name, usageFor(f), collect)
		if f.Short != "" {
			// Registered as a second name on the same target. stdlib flag has no
			// separate short/long concept — which is the point: it accepts
			// `-title` as well as `--title`, where pflag would read that as a
			// cluster of shorthands (Spike 4, case 3).
			fs.Func(f.Short, usageFor(f)+" (short for --"+f.Name+")", collect)
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
			return a.exec(cmd, values, renderer)
		},
	}, nil
}

func (a App) exec(cmd clispec.Command, values map[string][]string, render Renderer) error {
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
