package main

// The CLI surface, derived from the .proto (Decision 29).
//
// A native host's command surface is already described by the schema: rpcs are
// subcommands, request fields are flags, and `help`/`required`/`default`/`short`
// have been declarable field options since the options spike. Nothing read them
// until now — every host hand-wrote a `switch args[0]`, which is a second place
// for the command surface to live and a second place for it to be wrong.
//
// So this emits the surface as DATA, and `platform/cli` turns that data into a
// parser. What it deliberately does NOT emit is how to print a response: that is
// a presentation decision, which belongs to the tier slot (Decision 34 — a slot
// renders, it never decides).

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	helpExt      protoreflect.FullName = "devalbo.options.v1.help"
	requiredExt  protoreflect.FullName = "devalbo.options.v1.required"
	defaultExt   protoreflect.FullName = "devalbo.options.v1.default"
	shortExt     protoreflect.FullName = "devalbo.options.v1.short"
	cliNameExt   protoreflect.FullName = "devalbo.options.v1.cli_name"       // MethodOptions
	cliHiddenExt protoreflect.FullName = "devalbo.options.v1.cli_hidden"     // MethodOptions
	hostLocalExt protoreflect.FullName = "devalbo.options.v1.host_local"     // MethodOptions
	cliFlagExt   protoreflect.FullName = "devalbo.options.v1.cli_flag"       // FieldOptions
	cliSourceExt protoreflect.FullName = "devalbo.options.v1.cli_source"     // FieldOptions
	cliPosExt    protoreflect.FullName = "devalbo.options.v1.cli_positional" // FieldOptions
)

// checkPositionals enforces the rules that would otherwise fail at run time in
// ways nobody could diagnose from the .proto.
func checkPositionals(request string, flags []cliFlag) error {
	var pos []cliFlag
	for _, f := range flags {
		if f.positional > 0 {
			pos = append(pos, f)
		}
	}
	if len(pos) == 0 {
		return nil
	}
	sort.Slice(pos, func(i, j int) bool { return pos[i].positional < pos[j].positional })

	for i, f := range pos {
		want := uint32(i + 1)
		if f.positional != want {
			// Contiguous from 1: a gap means an argument position nothing can
			// ever fill, so every later positional silently shifts.
			return fmt.Errorf("%s: positional %d is missing (--%s claims %d) — positions must run 1..n with no gaps",
				request, want, f.name, f.positional)
		}
		if f.repeated && i != len(pos)-1 {
			// A repeated positional swallows the rest of argv, so anything after
			// it could never be reached.
			return fmt.Errorf("%s: --%s is repeated and positional, so it must be LAST — nothing after it could ever be parsed", request, f.name)
		}
	}
	return nil
}

// extEnumNumber reads an enum-valued custom option as its number.
func extEnumNumber(m proto.Message, name protoreflect.FullName) (int32, bool) {
	var out int32
	var found bool
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.FullName() == name {
			out, found = int32(v.Enum()), true
			return false
		}
		return true
	})
	return out, found
}

// checkCLIToken rejects a cli_name that could not be typed as one argument.
//
// Checked at BUILD time rather than discovered at run time: a name with a space
// in it produces a subcommand nobody can invoke, and the .proto is where someone
// can still fix it cheaply.
func checkCLIToken(name string) error {
	if name == "" {
		return nil // absent means "derive from the declared name"
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("%q must not start with '-'", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return fmt.Errorf("%q must be lower-case letters, digits, '-' or '_'", name)
		}
	}
	return nil
}

type cliFlag struct {
	name       string
	short      string
	field      uint32
	kind       string // a clispec.Kind constant name
	source     string // a clispec.Source constant name
	repeated   bool
	positional uint32
	help       string
	required   bool
	def        string
	enumValues []string
}

type cliCommand struct {
	name        string
	method      uint32
	request     string
	summary     string
	flags       []cliFlag
	unsupported []string
}

// leadingComments maps a descriptor path to the comment written above it.
//
// The rpc's own doc comment becomes its subcommand help, rather than a third
// place to write the same sentence. protoc ships these in SourceCodeInfo; the
// path for a method is [6 (service), serviceIndex, 2 (method), methodIndex],
// which is the only obscure part.
func leadingComments(file *descriptorpb.FileDescriptorProto) map[string]string {
	out := map[string]string{}
	for _, loc := range file.GetSourceCodeInfo().GetLocation() {
		if loc.LeadingComments == nil {
			continue
		}
		key := make([]string, 0, len(loc.Path))
		for _, p := range loc.Path {
			key = append(key, fmt.Sprint(p))
		}
		out[strings.Join(key, ".")] = firstSentence(loc.GetLeadingComments())
	}
	return out
}

// firstSentence takes the first real line of a doc comment.
//
// One line, because this becomes a column in a subcommand list — the rest of the
// comment stays in the .proto where there is room for it.
//
// SECTION DIVIDERS ARE SKIPPED. `// --- filesystem block (100–199) ---` sits
// directly above an rpc, so protoc hands it over as that rpc's leading comment
// and `version` ended up documented as "--- core lifecycle block (1–99) ---".
// Filtering here rather than requiring a blank line in every .proto: the
// formatting rule would be invisible, and breaking it would silently mislabel a
// command rather than fail.
func firstSentence(comment string) string {
	for _, line := range strings.Split(comment, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}
		return strings.TrimSuffix(line, ".")
	}
	return ""
}

// cliCommandsOf turns each rpc into a subcommand by looking up its REQUEST
// message and mapping its fields to flags.
func cliCommandsOf(file *descriptorpb.FileDescriptorProto, services []service, resolver *protoregistry.Types) ([]cliCommand, error) {
	messages := map[string]*descriptorpb.DescriptorProto{}
	for _, m := range file.MessageType {
		messages[m.GetName()] = m
	}
	enums := map[string]*descriptorpb.EnumDescriptorProto{}
	for _, e := range file.EnumType {
		enums[e.GetName()] = e
	}

	// An override can COLLIDE, which a derived name never could — two rpcs
	// renamed to the same thing would leave one of them unreachable, and the
	// symptom is "that command runs the wrong thing", nowhere near the .proto.
	seen := map[string]string{}

	comments := leadingComments(file)
	// Service and method indexes come from the FILE, not from `services`, which
	// is sorted by id — a mismatch here would attach one command's help to
	// another, which is exactly the sort of thing nobody notices.
	methodPath := map[string]string{}
	for si, svc := range file.Service {
		for mi, m := range svc.Method {
			methodPath[svc.GetName()+"."+m.GetName()] = fmt.Sprintf("6.%d.2.%d", si, mi)
		}
	}

	var out []cliCommand
	for _, svc := range services {
		for _, m := range svc.methods {
			// A host lifecycle verb is dispatchable but not typeable: it never
			// enters the surface, so no name is claimed and no renderer is owed.
			if m.cliHidden {
				continue
			}
			// The declared name is the DEFAULT, not the rule: kebab(rpc name)
			// unless the .proto says otherwise.
			name := m.cliName
			if name == "" {
				name = kebab(m.name)
			}
			if prev, dup := seen[name]; dup {
				return nil, fmt.Errorf("cli command %q is claimed by both %s and %s — an override must not collide", name, prev, m.name)
			}
			seen[name] = m.name

			cmd := cliCommand{
				name:    name,
				method:  m.id,
				request: m.input,
				summary: comments[methodPath[svc.name+"."+m.name]],
			}
			msg, ok := messages[m.input]
			if !ok {
				// A request from another file. Not an error: the command still
				// dispatches, it just has no flags this generator can see.
				out = append(out, cmd)
				continue
			}
			flagNames := map[string]string{}
			stdinBy := ""
			for _, f := range msg.Field {
				flag, ok, err := cliFlagOf(f, enums, resolver)
				if err != nil {
					return nil, fmt.Errorf("%s.%s: %w", m.input, f.GetName(), err)
				}
				if !ok {
					cmd.unsupported = append(cmd.unsupported, f.GetName())
					continue
				}
				// Same collision hazard as command names, one level down: two
				// flags with one name means the second silently wins, and the
				// request carries a field the user never set.
				if prev, dup := flagNames[flag.name]; dup {
					return nil, fmt.Errorf("%s: flag --%s is claimed by both %s and %s", m.input, flag.name, prev, f.GetName())
				}
				flagNames[flag.name] = f.GetName()
				if flag.source == "SourceStdin" {
					if stdinBy != "" {
						return nil, fmt.Errorf("%s: --%s and --%s both read stdin; only one field per command may", m.input, stdinBy, flag.name)
					}
					stdinBy = flag.name
				}
				cmd.flags = append(cmd.flags, flag)
			}
			if err := checkPositionals(m.input, cmd.flags); err != nil {
				return nil, err
			}
			out = append(out, cmd)
		}
	}
	return out, nil
}

func cliFlagOf(f *descriptorpb.FieldDescriptorProto, enums map[string]*descriptorpb.EnumDescriptorProto, resolver *protoregistry.Types) (cliFlag, bool, error) {
	repeated := f.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED

	var kind string
	var enumValues []string
	switch f.GetType() {
	case descriptorpb.FieldDescriptorProto_TYPE_STRING:
		kind = "KindString"
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		kind = "KindBool"
	case descriptorpb.FieldDescriptorProto_TYPE_INT32, descriptorpb.FieldDescriptorProto_TYPE_SINT32:
		kind = "KindInt32"
	case descriptorpb.FieldDescriptorProto_TYPE_INT64, descriptorpb.FieldDescriptorProto_TYPE_SINT64:
		kind = "KindInt64"
	case descriptorpb.FieldDescriptorProto_TYPE_UINT32:
		kind = "KindUint32"
	case descriptorpb.FieldDescriptorProto_TYPE_UINT64:
		kind = "KindUint64"
	case descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		kind = "KindBytes"
	case descriptorpb.FieldDescriptorProto_TYPE_ENUM:
		kind = "KindEnum"
		// Enum values double as the choices a menu-driven host would show, so
		// they travel with the flag rather than being looked up again later.
		if e, ok := enums[shortName(f.GetTypeName())]; ok {
			for _, v := range e.Value {
				enumValues = append(enumValues, v.GetName())
			}
		}
	default:
		// Nested messages, maps, floats. Not an error — reported by name on the
		// command so a user is told the field exists and cannot be set here,
		// rather than having it silently ignored.
		return cliFlag{}, false, nil
	}

	opts, err := resolveFieldOpts(f.GetOptions(), resolver)
	if err != nil {
		return cliFlag{}, false, err
	}
	short, _ := extString(opts, shortExt)
	if len(short) > 1 {
		return cliFlag{}, false, fmt.Errorf("short option %q must be a single letter", short)
	}
	help, _ := extString(opts, helpExt)
	def, _ := extString(opts, defaultExt)
	required, _ := extBool(opts, requiredExt)

	name, _ := extString(opts, cliFlagExt)
	if err := checkCLIToken(name); err != nil {
		return cliFlag{}, false, fmt.Errorf("cli_flag: %w", err)
	}
	if name == "" {
		name = kebab(f.GetName())
	}

	// Source: declared if present, otherwise the sensible default for the type.
	// Bytes default to a file because a literal bundle on argv is not something
	// anyone types; everything else is the argument itself.
	source := "SourceLiteral"
	if kind == "KindBytes" {
		source = "SourceFile"
	}
	if n, ok := extEnumNumber(opts, cliSourceExt); ok {
		switch n {
		case 1:
			source = "SourceLiteral"
		case 2:
			source = "SourceFile"
		case 3:
			source = "SourceStdin"
		}
	}

	positional := uint32(0)
	if n, ok := extUint32(opts, cliPosExt); ok {
		positional = n
	}

	return cliFlag{
		source:     source,
		name:       name,
		positional: positional,
		short:      short,
		field:      uint32(f.GetNumber()),
		kind:       kind,
		repeated:   repeated,
		help:       help,
		required:   required,
		def:        def,
		enumValues: enumValues,
	}, true, nil
}

func resolveFieldOpts(src *descriptorpb.FieldOptions, resolver *protoregistry.Types) (*descriptorpb.FieldOptions, error) {
	if src == nil {
		return &descriptorpb.FieldOptions{}, nil
	}
	b, err := proto.Marshal(src)
	if err != nil {
		return nil, err
	}
	var out descriptorpb.FieldOptions
	// Re-unmarshal with the resolver: custom options arrive as UNKNOWN fields on
	// the options message, so they are invisible until parsed with the extension
	// types registered. Same dance as the method options above.
	if err := (proto.UnmarshalOptions{Resolver: resolver}).Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func extString(m proto.Message, name protoreflect.FullName) (string, bool) {
	var out string
	var found bool
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.FullName() == name {
			out, found = v.String(), true
			return false
		}
		return true
	})
	return out, found
}

func extBool(m proto.Message, name protoreflect.FullName) (bool, bool) {
	var out, found bool
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.FullName() == name {
			out, found = v.Bool(), true
			return false
		}
		return true
	})
	return out, found
}

// kebab turns CreateRecord / created_at into create-record / created-at — the
// spelling a command line expects, derived rather than declared so an rpc rename
// cannot leave a stale subcommand behind.
func kebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '_' || r == ' ':
			b.WriteByte('-')
		case r >= 'A' && r <= 'Z':
			if i > 0 && s[i-1] != '_' {
				b.WriteByte('-')
			}
			b.WriteRune(r - 'A' + 'a')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// renderCLI emits the command surface as plain data.
func renderCLI(file *descriptorpb.FileDescriptorProto, services []service, cmds []cliCommand) (string, error) {
	pkg, err := goPackageName(file)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by protoc-gen-dlc-registry. DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// source: %s\n//\n", file.GetName())
	fmt.Fprintf(&b, "// The command-line surface, derived from the service and its request messages\n")
	fmt.Fprintf(&b, "// (Decision 29). Subcommand names come from rpc names and flags from request\n")
	fmt.Fprintf(&b, "// fields, so adding an rpc adds a subcommand and nothing has to be hand-mirrored.\n")
	fmt.Fprintf(&b, "//\n// How a response is PRINTED is not here: that is presentation, and it belongs\n")
	fmt.Fprintf(&b, "// to the tier slot (Decision 34).\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	fmt.Fprintf(&b, "import \"github.com/devalbo/dlc-platform/clispec\"\n\n")

	for _, svc := range services {
		fmt.Fprintf(&b, "// %sCLI is %s as a command-line surface.\n", svc.name, svc.name)
		fmt.Fprintf(&b, "var %sCLI = []clispec.Command{\n", svc.name)
		for _, m := range svc.methods {
			cmd, ok := findCmd(cmds, m.id)
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "\t{\n")
			fmt.Fprintf(&b, "\t\tName:    %q,\n", cmd.name)
			fmt.Fprintf(&b, "\t\tMethod:  Method%s,\n", m.name)
			fmt.Fprintf(&b, "\t\tRequest: %q,\n", cmd.request)
			if m.hostLocal {
				// Decision 30: the host serves this, so the runner must find a
				// local handler for it rather than sending it to the engine.
				fmt.Fprintf(&b, "\t\tLocal:   true,\n")
			}
			if cmd.summary != "" {
				fmt.Fprintf(&b, "\t\tSummary: %q,\n", cmd.summary)
			}
			if len(cmd.flags) > 0 {
				fmt.Fprintf(&b, "\t\tFlags: []clispec.Flag{\n")
				for _, f := range cmd.flags {
					fmt.Fprintf(&b, "\t\t\t{Name: %q, Field: %d, Kind: clispec.%s", f.name, f.field, f.kind)
					if f.source != "SourceLiteral" {
						fmt.Fprintf(&b, ", Source: clispec.%s", f.source)
					}
					if f.short != "" {
						fmt.Fprintf(&b, ", Short: %q", f.short)
					}
					if f.repeated {
						fmt.Fprintf(&b, ", Repeated: true")
					}
					if f.positional > 0 {
						fmt.Fprintf(&b, ", Positional: %d", f.positional)
					}
					if f.required {
						fmt.Fprintf(&b, ", Required: true")
					}
					if f.help != "" {
						fmt.Fprintf(&b, ", Help: %q", f.help)
					}
					if f.def != "" {
						fmt.Fprintf(&b, ", Default: %q", f.def)
					}
					if len(f.enumValues) > 0 {
						fmt.Fprintf(&b, ", EnumValues: []string{")
						for i, v := range f.enumValues {
							if i > 0 {
								fmt.Fprintf(&b, ", ")
							}
							fmt.Fprintf(&b, "%q", v)
						}
						fmt.Fprintf(&b, "}")
					}
					fmt.Fprintf(&b, "},\n")
				}
				fmt.Fprintf(&b, "\t\t},\n")
			}
			if len(cmd.unsupported) > 0 {
				fmt.Fprintf(&b, "\t\tUnsupported: []string{")
				for i, u := range cmd.unsupported {
					if i > 0 {
						fmt.Fprintf(&b, ", ")
					}
					fmt.Fprintf(&b, "%q", u)
				}
				fmt.Fprintf(&b, "},\n")
			}
			fmt.Fprintf(&b, "\t},\n")
		}
		fmt.Fprintf(&b, "}\n\n")
	}
	return b.String(), nil
}

// renderCLITS emits the same surface for the web tier.
//
// STRING kinds and NO IMPORTS, unlike the Go side which references
// `clispec.KindString`. A generated TypeScript file that imported the web host
// package would make the generated output depend on which host an app uses;
// plain data with string literals is valid on its own, and `hosts/web/clispec.ts`
// describes the shape structurally.
func renderCLITS(file *descriptorpb.FileDescriptorProto, services []service, cmds []cliCommand) (string, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by protoc-gen-dlc-registry. DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// source: %s\n//\n", file.GetName())
	fmt.Fprintf(&b, "// The command-line surface for the web tier — the same data the native host\n")
	fmt.Fprintf(&b, "// reads (Decision 29), so an in-page terminal and the CLI cannot drift apart.\n")
	fmt.Fprintf(&b, "// Deliberately import-free: this is plain data, typed structurally by\n")
	fmt.Fprintf(&b, "// `@devalbo/dlc-web/clispec`.\n\n")

	for _, svc := range services {
		fmt.Fprintf(&b, "export const %sCLI = [\n", svc.name)
		for _, m := range svc.methods {
			cmd, ok := findCmd(cmds, m.id)
			if !ok {
				continue
			}
			// HOST-LOCAL VERBS ARE OMITTED FROM THE WEB SURFACE ENTIRELY, not
			// marked. A browser cannot spawn a toolchain, so `gen` and `build`
			// could never work there — and a command that is listed and cannot
			// run is worse than one that is absent. Omitting rather than flagging
			// also means no runtime check to forget: there is nothing to skip.
			if m.hostLocal {
				continue
			}
			fmt.Fprintf(&b, "  {\n")
			fmt.Fprintf(&b, "    name: %q,\n", cmd.name)
			fmt.Fprintf(&b, "    method: %d,\n", cmd.method)
			fmt.Fprintf(&b, "    request: %q,\n", cmd.request)
			if cmd.summary != "" {
				fmt.Fprintf(&b, "    summary: %q,\n", cmd.summary)
			}
			if len(cmd.flags) > 0 {
				fmt.Fprintf(&b, "    flags: [\n")
				for _, f := range cmd.flags {
					fmt.Fprintf(&b, "      { name: %q, field: %d, kind: %q", f.name, f.field, tsKind(f.kind))
					if f.short != "" {
						fmt.Fprintf(&b, ", short: %q", f.short)
					}
					if f.source != "SourceLiteral" {
						fmt.Fprintf(&b, ", source: %q", tsSource(f.source))
					}
					if f.repeated {
						fmt.Fprintf(&b, ", repeated: true")
					}
					if f.positional > 0 {
						fmt.Fprintf(&b, ", positional: %d", f.positional)
					}
					if f.required {
						fmt.Fprintf(&b, ", required: true")
					}
					if f.help != "" {
						fmt.Fprintf(&b, ", help: %q", f.help)
					}
					if f.def != "" {
						fmt.Fprintf(&b, ", default: %q", f.def)
					}
					if len(f.enumValues) > 0 {
						fmt.Fprintf(&b, ", enumValues: [")
						for i, v := range f.enumValues {
							if i > 0 {
								fmt.Fprintf(&b, ", ")
							}
							fmt.Fprintf(&b, "%q", v)
						}
						fmt.Fprintf(&b, "]")
					}
					fmt.Fprintf(&b, " },\n")
				}
				fmt.Fprintf(&b, "    ],\n")
			}
			if len(cmd.unsupported) > 0 {
				fmt.Fprintf(&b, "    unsupported: [")
				for i, u := range cmd.unsupported {
					if i > 0 {
						fmt.Fprintf(&b, ", ")
					}
					fmt.Fprintf(&b, "%q", u)
				}
				fmt.Fprintf(&b, "],\n")
			}
			fmt.Fprintf(&b, "  },\n")
		}
		fmt.Fprintf(&b, "] as const;\n\n")
	}
	return b.String(), nil
}

// tsKind maps the Go constant name to the string literal the TS surface uses.
func tsKind(goKind string) string {
	return strings.ToLower(strings.TrimPrefix(goKind, "Kind"))
}

func tsSource(goSource string) string {
	return strings.ToLower(strings.TrimPrefix(goSource, "Source"))
}

func findCmd(cmds []cliCommand, method uint32) (cliCommand, bool) {
	for _, c := range cmds {
		if c.method == method {
			return c, true
		}
	}
	return cliCommand{}, false
}
