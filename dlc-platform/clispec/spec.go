// Package clispec describes a command service as a command-line surface.
//
// This is the shape `protoc-gen-dlc-registry` emits from the .proto (Decision
// 29): one Command per rpc, one Flag per request field, with the help/required/
// default/short metadata that already lives in the schema as field options and
// that until now nothing read.
//
// WHY GENERATED RATHER THAN REFLECTED. Decision 29 first said the host would
// embed a FileDescriptorSet and walk it with protoreflect. The plugin already
// reads that descriptor set and already reads custom options — it solved that
// problem at build time — and go-lite messages carry no protoreflect support, so
// runtime walking would additionally need dynamicpb and the unknown-field dance
// the options spike found unreliable. Generating plain data instead means a
// schema change is a compile error rather than a runtime surprise.
//
// WHY A LEAF PACKAGE WITH NO IMPORTS. Generated code lives in the message
// package, and platform imports that package — so anything the generated file
// references must not reach back. Data only, no behaviour: the runner that
// consumes this lives in `platform/cli`, which is host-side and never linked
// into the wasm engine.
package clispec

// Kind is a flag's wire type — what the runner needs to parse a string argument
// and encode it as a protobuf field.
type Kind uint8

const (
	KindUnsupported Kind = iota
	KindString
	KindBool
	KindInt32
	KindInt64
	KindUint32
	KindUint64
	KindEnum
	KindBytes
)

// Source is where a flag's value comes from, as opposed to what type it is.
//
// Kept separate from Kind because they vary independently: a `bytes` field is
// almost always a file, but a long `string` may equally well be piped in, and a
// host must not have to guess which. Declared in the .proto (`cli_source`), so
// every host resolves a value the same way rather than each inventing its own
// `@file` convention.
type Source uint8

const (
	// SourceLiteral — the argument is the value. The default for scalars.
	SourceLiteral Source = iota
	// SourceFile — the argument is a path; the runner reads it. The default
	// for bytes, which is what makes an inherited `import-fs` usable from a
	// command line without the app hand-writing a file read.
	SourceFile
	// SourceStdin — read standard input. At most one flag per command.
	SourceStdin
)

// Flag is one field of a request message, as a command-line flag.
type Flag struct {
	// Name is the kebab-cased field name: `created_at` → `--created-at`.
	Name string
	// Short is an optional single letter from the `short` field option.
	Short string
	// Field is the proto field NUMBER. The runner encodes by number, never by
	// name, for the same reason dispatch keys on method_id: the name is
	// cosmetic and the number is the wire.
	Field uint32
	Kind  Kind
	// Source is where the value comes from. Zero value is SourceLiteral, which
	// is right for every scalar; the generator sets SourceFile for bytes.
	Source Source
	// Repeated fields accept the flag more than once.
	Repeated bool
	// Positional is a 1-based argument position, or 0 for flag-only. A
	// positional field also keeps its flag spelling, so `dlc new myapp` and
	// `dlc new --name myapp` are the same command.
	Positional uint32
	Help       string
	Required   bool
	// Default in string form, parsed by the runner to the field's wire type.
	Default string
	// EnumValues are the permitted names for a KindEnum flag — also the menu a
	// richer host would show.
	EnumValues []string
}

// Command is one rpc, as a subcommand.
type Command struct {
	// Name is the kebab-cased rpc name: `CreateRecord` → `create-record`.
	Name    string
	Method  uint32
	Request string // request message name, for diagnostics
	// Summary is the rpc's doc comment, first line — so `-h` is written where
	// the command is defined rather than in a third place that goes stale.
	Summary string
	Flags   []Flag
	// Local marks a verb the HOST handles rather than the engine (Decision 30,
	// `host_local` in the .proto). The runner refuses to build a surface with a
	// local command and no local handler, the same way it refuses a missing
	// renderer — a declared command that silently does nothing is worse than a
	// build error.
	Local bool
	// Unsupported names request fields the CLI cannot express yet (nested
	// messages, maps, repeated non-scalars). Listed rather than dropped: a
	// command that silently ignores a field is worse than one that says it
	// cannot set it.
	Unsupported []string
}

// Positionals returns a command's positional fields, in position order.
func (c Command) Positionals() []Flag {
	var out []Flag
	for _, f := range c.Flags {
		if f.Positional > 0 {
			out = append(out, f)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Positional < out[j-1].Positional; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Find returns the command with the given name.
func Find(commands []Command, name string) (Command, bool) {
	for _, c := range commands {
		if c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}
