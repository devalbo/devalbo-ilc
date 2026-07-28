// protoc-gen-dlc-registry — generates the engine's command dispatch from the
// .proto, so `method_id` is written down exactly once (Decision 29).
//
// WHY THIS EXISTS: protoc-gen-go-lite emits messages only — it ignores services
// entirely — so `method_id` reaches the generated Go nowhere. Without this
// plugin the ids have to be hand-mirrored into Go constants, and nothing catches
// a mismatch: the parity check reads the Go constants on both sides, so it would
// compare a wrong id against itself and pass.
//
// WHAT IT EMITS, per .proto that declares a service, into that proto's own Go
// package (so there is no new import to wire up):
//
//	const MethodNew uint32 = 1000              // one per rpc
//	func DlcServiceHandlers(...) map[uint32]func([]byte) ([]byte, error)
//
// The handler map is built from typed functions the app supplies; the generated
// closures do the decode/encode. Note what is deliberately absent: any reference
// to engine/platform. Generated code lives in the *message* package, and platform
// imports that package — so importing platform here would be an import cycle.
// Handing back a plain map keeps the generated code dependency-free and lets the
// app feed it to platform.RegisterRaw.
//
// IT ALSO LOCKS THE IDS. `buf breaking` guards message wire compatibility but
// knows nothing about a custom option's *value*, so renumbering a command is a
// one-character change that silently breaks every deployed host. The plugin
// compares every id against a committed lock file and fails the build on any
// change. Re-bless deliberately with DLC_ID_LOCK_UPDATE=1.
//
// Custom options arrive as UNKNOWN FIELDS on MethodOptions (that is how they
// ship in a buf image), so they need dynamicpb + a re-unmarshal to read — see
// resolveMethodOpts. `HasExtension` is unreliable across dynamicpb type
// identities, hence Range. All spike-measured; see spikes/options/.
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/pluginpb"
)

const (
	methodIDExt       = "devalbo.options.v1.method_id"
	reservedMethodExt = "devalbo.options.v1.reserved_method_id"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "protoc-gen-dlc-registry:", err)
		os.Exit(1)
	}
}

func run() error {
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	var req pluginpb.CodeGeneratorRequest
	if err := proto.Unmarshal(in, &req); err != nil {
		return fmt.Errorf("unmarshal CodeGeneratorRequest: %w", err)
	}

	resolver, err := extensionResolver(&req)
	if err != nil {
		return err
	}

	// lang=go (default) or lang=ts. The ids are needed on BOTH sides — the engine
	// dispatches on them and the web UI sends them — and hand-mirroring into
	// TypeScript would reopen exactly the hole this plugin closed for Go. buf
	// invokes the plugin once per output directory, so the language is a
	// parameter rather than one invocation emitting both.
	lang := paramValue(req.GetParameter(), "lang")
	if lang == "" {
		lang = "go"
	}
	if lang != "go" && lang != "ts" {
		return fmt.Errorf("unknown lang=%q (want go or ts)", lang)
	}

	generate := map[string]bool{}
	for _, name := range req.FileToGenerate {
		generate[name] = true
	}

	var resp pluginpb.CodeGeneratorResponse
	locked := map[string]uint32{}
	lockedTopics := map[string]string{}

	for _, file := range req.ProtoFile {
		// EVENTS are independent of services: a file may declare event messages
		// and no commands, so this runs before the `len(services) == 0` skip
		// below. Every topic goes into the lock, whether or not we generate for
		// this file — the lock is about the contract, not about codegen.
		events, err := eventsOf(file, resolver)
		if err != nil {
			return err
		}
		if !isSpikePackage(file.GetPackage()) {
			for _, e := range events {
				lockedTopics[file.GetPackage()+"."+e.message] = e.topic
			}
		}
		if generate[file.GetName()] && len(events) > 0 {
			base := strings.TrimSuffix(file.GetName(), ".proto")
			if lang == "go" {
				content, err := renderEventsGo(file, events)
				if err != nil {
					return err
				}
				formatted, ferr := format.Source([]byte(content))
				if ferr != nil {
					return fmt.Errorf("%s: generated invalid Go events: %w", file.GetName(), ferr)
				}
				resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
					Name:    proto.String(base + ".events.pb.go"),
					Content: proto.String(string(formatted)),
				})
			} else {
				content, err := renderEventsTS(file, events)
				if err != nil {
					return err
				}
				resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
					Name:    proto.String(base + ".events.pb.ts"),
					Content: proto.String(content),
				})
			}
		}

		services, err := servicesOf(file, resolver)
		if err != nil {
			return err
		}
		if len(services) == 0 {
			continue
		}
		// Every service contributes to the lock, even from a dependency we are
		// not generating — the lock is about the wire, not about codegen.
		// Except spikes: they are throwaway proofs kept as regression tests, not
		// a wire contract anyone speaks, so locking them is pure noise in the
		// diff that a real id change would have to compete with.
		if !isSpikePackage(file.GetPackage()) {
			for _, svc := range services {
				for _, m := range svc.methods {
					locked[file.GetPackage()+"."+svc.name+"."+m.name] = m.id
				}
				// Reservations are locked as well: an id held for an unbuilt
				// command is as much a commitment as one in use, and dropping a
				// reservation should show up in review like any other id change.
				for _, r := range svc.reserved {
					locked[fmt.Sprintf("%s.%s.[reserved]", file.GetPackage(), svc.name)+fmt.Sprintf(".%d", r)] = r
				}
			}
		}
		if !generate[file.GetName()] {
			continue
		}
		content, name, err := renderFor(lang, file, services)
		if err != nil {
			return err
		}
		// Generated code is code someone will read in a diff. gofmt it here so
		// `gofmt -l .` stays clean and nobody has to reformat generated output.
		// Go only, obviously — running a Go formatter over TypeScript reports
		// the TS as invalid Go, which is a confusing way to learn you forgot
		// this guard.
		if lang == "go" {
			formatted, ferr := format.Source([]byte(content))
			if ferr != nil {
				return fmt.Errorf("%s: generated invalid Go: %w", file.GetName(), ferr)
			}
			content = string(formatted)
		}
		resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
			Name:    proto.String(name),
			Content: proto.String(content),
		})

		// The CLI surface (Decision 29), emitted for BOTH languages. The web tier
		// reads the same data as the native host — that is what keeps an in-page
		// terminal and the CLI from drifting apart, and it is the only way the
		// "tier-neutral surface" claim gets tested rather than asserted.
		cmds, cerr := cliCommandsOf(file, services, resolver)
		if cerr != nil {
			return cerr
		}
		base := strings.TrimSuffix(file.GetName(), ".proto")

		if lang == "go" {
			cliContent, cerr := renderCLI(file, services, cmds)
			if cerr != nil {
				return cerr
			}
			formatted, ferr := format.Source([]byte(cliContent))
			if ferr != nil {
				return fmt.Errorf("%s: generated invalid Go CLI spec: %w", file.GetName(), ferr)
			}
			resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
				Name:    proto.String(base + ".cli.pb.go"),
				Content: proto.String(string(formatted)),
			})
		} else {
			cliContent, cerr := renderCLITS(file, services, cmds)
			if cerr != nil {
				return cerr
			}
			resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
				Name:    proto.String(base + ".cli.pb.ts"),
				Content: proto.String(cliContent),
			})
		}
	}

	if err := checkLock(paramValue(req.GetParameter(), "lock"), locked); err != nil {
		return err
	}
	if err := checkTopicLock(paramValue(req.GetParameter(), "lock"), lockedTopics); err != nil {
		return err
	}

	out, err := proto.Marshal(&resp)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}

// ---- descriptors ---------------------------------------------------------

type method struct {
	name       string
	id         uint32
	input      string // Go type name in this package
	output     string
	fieldParam string // lowerCamel param name
	cliName    string // (cli_name) override; empty means kebab(name)
}

type service struct {
	name     string
	methods  []method
	reserved []uint32
}

// extensionResolver registers every extension declared anywhere in the request,
// so custom options can be re-parsed off the raw *Options messages.
func extensionResolver(req *pluginpb.CodeGeneratorRequest) (*protoregistry.Types, error) {
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: req.ProtoFile})
	if err != nil {
		return nil, fmt.Errorf("protodesc.NewFiles: %w", err)
	}
	types := &protoregistry.Types{}
	var rangeErr error
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		for i := 0; i < fd.Extensions().Len(); i++ {
			if err := types.RegisterExtension(dynamicpb.NewExtensionType(fd.Extensions().Get(i))); err != nil {
				rangeErr = err
				return false
			}
		}
		return true
	})
	return types, rangeErr
}

func servicesOf(file *descriptorpb.FileDescriptorProto, resolver *protoregistry.Types) ([]service, error) {
	var out []service
	for _, svc := range file.Service {
		s := service{name: svc.GetName()}
		sopts, err := resolveServiceOpts(svc.GetOptions(), resolver)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", svc.GetName(), err)
		}
		s.reserved = extUint32List(sopts, reservedMethodExt)
		for _, m := range svc.Method {
			opts, err := resolveMethodOpts(m.GetOptions(), resolver)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", svc.GetName(), m.GetName(), err)
			}
			id, ok := extUint32(opts, methodIDExt)
			if !ok {
				// A service without method_id is not a command surface — skip
				// the whole file rather than guessing an id for it.
				return nil, fmt.Errorf("%s.%s: missing (%s); every rpc in a command service needs one",
					svc.GetName(), m.GetName(), methodIDExt)
			}
			for _, r := range s.reserved {
				if r == id {
					return nil, fmt.Errorf("%s.%s: method_id %d is RESERVED by this service — pick another, or drop the reservation deliberately",
						svc.GetName(), m.GetName(), id)
				}
			}
			cliName, _ := extString(opts, cliNameExt)
			if err := checkCLIToken(cliName); err != nil {
				return nil, fmt.Errorf("%s.%s: cli_name: %w", svc.GetName(), m.GetName(), err)
			}
			s.methods = append(s.methods, method{
				name:       m.GetName(),
				id:         id,
				input:      shortName(m.GetInputType()),
				output:     shortName(m.GetOutputType()),
				fieldParam: lowerFirst(m.GetName()) + "Fn",
				cliName:    cliName,
			})
		}
		if len(s.methods) > 0 {
			// Emit in ascending id order, not declaration order. The id is the
			// permanent thing — the rpc name is cosmetic — so sorting by it makes
			// the band structure legible at a glance (1..99 core, 100.. filesystem,
			// 10000+ app) and keeps the generated file stable when an rpc is moved
			// around in the .proto. Applied once here so the constants, the handler
			// parameters, and the map all read in the same order.
			sort.Slice(s.methods, func(i, j int) bool { return s.methods[i].id < s.methods[j].id })
			out = append(out, s)
		}
	}
	return out, nil
}

func resolveServiceOpts(src *descriptorpb.ServiceOptions, resolver *protoregistry.Types) (*descriptorpb.ServiceOptions, error) {
	if src == nil {
		return &descriptorpb.ServiceOptions{}, nil
	}
	b, err := proto.Marshal(src)
	if err != nil {
		return nil, err
	}
	out := &descriptorpb.ServiceOptions{}
	if err := (proto.UnmarshalOptions{Resolver: resolver}).Unmarshal(b, out); err != nil {
		return nil, err
	}
	return out, nil
}

func resolveMethodOpts(src *descriptorpb.MethodOptions, resolver *protoregistry.Types) (*descriptorpb.MethodOptions, error) {
	if src == nil {
		return &descriptorpb.MethodOptions{}, nil
	}
	b, err := proto.Marshal(src)
	if err != nil {
		return nil, err
	}
	out := &descriptorpb.MethodOptions{}
	if err := (proto.UnmarshalOptions{Resolver: resolver}).Unmarshal(b, out); err != nil {
		return nil, err
	}
	return out, nil
}

// extUint32List reads a REPEATED custom option by full name.
func extUint32List(m proto.Message, name protoreflect.FullName) []uint32 {
	var out []uint32
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.FullName() != name {
			return true
		}
		list := v.List()
		for i := 0; i < list.Len(); i++ {
			out = append(out, uint32(list.Get(i).Uint()))
		}
		return false
	})
	return out
}

// extUint32 reads a custom option by full name. Range, not HasExtension: the
// latter is unreliable across dynamicpb ExtensionType identities.
func extUint32(m proto.Message, name protoreflect.FullName) (uint32, bool) {
	var out uint32
	var found bool
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.FullName() == name {
			out = uint32(v.Uint())
			found = true
			return false
		}
		return true
	})
	return out, found
}

// ---- rendering -----------------------------------------------------------

// renderFor picks the language renderer and the output filename.
func renderFor(lang string, file *descriptorpb.FileDescriptorProto, services []service) (content, name string, err error) {
	base := strings.TrimSuffix(file.GetName(), ".proto")
	if lang == "ts" {
		content, err = renderTS(file, services)
		return content, base + ".registry.pb.ts", err
	}
	content, err = render(file, services)
	return content, base + ".registry.pb.go", err
}

// renderTS emits the method ids for the web tier. Constants only — a TypeScript
// host builds requests with the es-lite messages and calls execute(id, bytes),
// so it needs the numbers and nothing else.
func renderTS(file *descriptorpb.FileDescriptorProto, services []service) (string, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by protoc-gen-dlc-registry. DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// source: %s\n//\n", file.GetName())
	fmt.Fprintf(&b, "// Permanent method ids. The wire is the number, not the name — see the id\n")
	fmt.Fprintf(&b, "// lock in proto/method-ids.lock.\n\n")
	for _, svc := range services {
		fmt.Fprintf(&b, "/** Method ids for %s. */\n", svc.name)
		for _, m := range svc.methods {
			fmt.Fprintf(&b, "export const Method%s = %d;\n", m.name, m.id)
		}
		fmt.Fprintf(&b, "\n/** Every %s id, for hosts that enumerate. */\n", svc.name)
		fmt.Fprintf(&b, "export const %sMethods = {\n", svc.name)
		for _, m := range svc.methods {
			fmt.Fprintf(&b, "  %s: %d,\n", m.name, m.id)
		}
		fmt.Fprintf(&b, "} as const;\n")
	}
	return b.String(), nil
}

func render(file *descriptorpb.FileDescriptorProto, services []service) (string, error) {
	pkg, err := goPackageName(file)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by protoc-gen-dlc-registry. DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// source: %s\n//\n", file.GetName())
	fmt.Fprintf(&b, "// Command ids and dispatch for this file's service(s). The ids are the\n")
	fmt.Fprintf(&b, "// permanent method_id values from the .proto and are guarded by the id lock —\n")
	fmt.Fprintf(&b, "// changing one is a wire-breaking change, not a rename.\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)

	for _, svc := range services {
		fmt.Fprintf(&b, "// Method ids for %s.\nconst (\n", svc.name)
		for _, m := range svc.methods {
			fmt.Fprintf(&b, "\tMethod%s uint32 = %d\n", m.name, m.id)
		}
		fmt.Fprintf(&b, ")\n\n")
	}

	for _, svc := range services {
		fmt.Fprintf(&b, "// %sHandlers builds the dispatch map for %s from typed handlers.\n", svc.name, svc.name)
		fmt.Fprintf(&b, "// The returned map goes to platform.RegisterRaw; the closures below own the\n")
		fmt.Fprintf(&b, "// decode/encode, so an app never touches request bytes or an id.\n")
		fmt.Fprintf(&b, "func %sHandlers(\n", svc.name)
		for _, m := range svc.methods {
			fmt.Fprintf(&b, "\t%s func(*%s) (*%s, error),\n", m.fieldParam, m.input, m.output)
		}
		fmt.Fprintf(&b, ") map[uint32]func([]byte) ([]byte, error) {\n")
		fmt.Fprintf(&b, "\treturn map[uint32]func([]byte) ([]byte, error){\n")
		for _, m := range svc.methods {
			fmt.Fprintf(&b, "\t\tMethod%s: func(request []byte) ([]byte, error) {\n", m.name)
			fmt.Fprintf(&b, "\t\t\tvar req %s\n", m.input)
			fmt.Fprintf(&b, "\t\t\tif err := req.UnmarshalVT(request); err != nil {\n")
			fmt.Fprintf(&b, "\t\t\t\treturn nil, err\n\t\t\t}\n")
			fmt.Fprintf(&b, "\t\t\tresp, err := %s(&req)\n", m.fieldParam)
			fmt.Fprintf(&b, "\t\t\tif err != nil {\n\t\t\t\treturn nil, err\n\t\t\t}\n")
			fmt.Fprintf(&b, "\t\t\treturn resp.MarshalVT()\n")
			fmt.Fprintf(&b, "\t\t},\n")
		}
		fmt.Fprintf(&b, "\t}\n}\n\n")
	}
	return b.String(), nil
}

// goPackageName pulls the package name out of `option go_package = "path;name"`.
func goPackageName(file *descriptorpb.FileDescriptorProto) (string, error) {
	opt := file.GetOptions().GetGoPackage()
	if opt == "" {
		return "", fmt.Errorf("%s: no go_package option", file.GetName())
	}
	if i := strings.LastIndex(opt, ";"); i >= 0 {
		return opt[i+1:], nil
	}
	if i := strings.LastIndex(opt, "/"); i >= 0 {
		return opt[i+1:], nil
	}
	return opt, nil
}

// isSpikePackage reports whether a proto package belongs to a de-risking spike
// (devalbo.*spike*.v1) rather than to shipped code.
func isSpikePackage(pkg string) bool {
	return strings.Contains(pkg, "spike")
}

func shortName(fullType string) string {
	return fullType[strings.LastIndex(fullType, ".")+1:]
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func paramValue(params, key string) string {
	for _, part := range strings.Split(params, ",") {
		if k, v, ok := strings.Cut(part, "="); ok && k == key {
			return v
		}
	}
	return ""
}

// ---- the id lock ---------------------------------------------------------

// checkLock compares the ids in this build against a committed lock file. This
// is the guard `buf breaking` cannot provide: it validates message wire
// compatibility, but a method_id is a custom option's VALUE, so renumbering a
// command looks like nothing to it while breaking every deployed host.
func checkLock(path string, current map[string]uint32) error {
	if path == "" {
		return nil // no lock configured; ids are unguarded
	}
	// Two names claiming one id is a runtime panic at registration; catching it
	// here turns it into a build error with both names in it.
	byID := map[uint32]string{}
	for name, id := range current {
		if other, dup := byID[id]; dup {
			a, b := name, other
			if b < a {
				a, b = b, a
			}
			return fmt.Errorf("method_id %d is claimed by BOTH %s and %s", id, a, b)
		}
		byID[id] = name
	}

	existing, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		fmt.Fprintf(os.Stderr, "protoc-gen-dlc-registry: creating id lock %s\n", path)
		return os.WriteFile(path, []byte(renderLock(current)), 0o644)
	}

	before := parseLock(string(existing))
	var changed, removed []string
	added := 0
	for name, id := range current {
		switch prev, ok := before[name]; {
		case !ok:
			added++
		case prev != id:
			changed = append(changed, fmt.Sprintf("  ! %s: %d -> %d", name, prev, id))
		}
	}
	for name, id := range before {
		if _, ok := current[name]; !ok {
			removed = append(removed, fmt.Sprintf("  - %s = %d", name, id))
		}
	}

	// ADDING a command is the ordinary case — a new id was never promised to
	// anyone, so it cannot break a deployed host. Failing on it would mean
	// re-blessing for every new command, which teaches people to set
	// DLC_ID_LOCK_UPDATE reflexively and defeats the guard for the cases that
	// matter. Additions update the lock silently; the diff is the review.
	//
	// CHANGING or REMOVING an id is different: both break every host that
	// already speaks it, and neither is visible to `buf breaking` (a method_id
	// is an option's VALUE, not a field number). Those still stop the build.
	if len(changed) == 0 && len(removed) == 0 {
		if added > 0 {
			fmt.Fprintf(os.Stderr, "protoc-gen-dlc-registry: %s: +%d new id(s)\n", path, added)
			return os.WriteFile(path, []byte(renderLock(current)), 0o644)
		}
		return nil
	}
	if os.Getenv("DLC_ID_LOCK_UPDATE") == "1" {
		fmt.Fprintf(os.Stderr, "protoc-gen-dlc-registry: updating id lock %s\n", path)
		return os.WriteFile(path, []byte(renderLock(current)), 0o644)
	}

	sort.Strings(changed)
	sort.Strings(removed)
	detail := strings.Join(append(changed, removed...), "\n")
	return fmt.Errorf(
		"method_id lock mismatch in %s\n\n%s\n\nA method_id is permanent: changing or removing one breaks every host\n"+
			"that already speaks it, and `buf breaking` cannot see it (it is an option\n"+
			"value, not a field number). Adding commands is fine and needs no action.\n"+
			"If this change is deliberate, re-bless with:\n\n"+
			"    DLC_ID_LOCK_UPDATE=1 make gen\n",
		path, detail)
}

func parseLock(s string) map[string]uint32 {
	out := map[string]uint32{}
	for _, line := range strings.Split(s, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		if id, err := strconv.ParseUint(strings.TrimSpace(v), 10, 32); err == nil {
			out[k] = uint32(id)
		}
	}
	return out
}

func renderLock(ids map[string]uint32) string {
	keys := make([]string, 0, len(ids))
	for k := range ids {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b bytes.Buffer
	b.WriteString("# method_id lock — generated by protoc-gen-dlc-registry. COMMIT THIS FILE.\n")
	b.WriteString("# Each line is a permanent wire id. A change here is a breaking change.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s %d\n", k, ids[k])
	}
	return b.String()
}
