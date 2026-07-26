// host-introspect — Decision 29 host path: read method_id + field options from
// a buf FileDescriptorSet via google.golang.org/protobuf.
//
// Walks the raw FileDescriptorProto (how custom options ship in a `buf build`
// image — unknown fields on *Options). Re-parses with dynamicpb, then reads
// values via ProtoReflect.Range (HasExtension is unreliable across dynamicpb
// ExtensionType identities).
//
// Usage: host-introspect <image.bin>
package main

import (
	"fmt"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: host-introspect <buf-image.bin>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fatal(err)
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &fds); err != nil {
		fatal(fmt.Errorf("unmarshal FileDescriptorSet: %w", err))
	}

	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		fatal(fmt.Errorf("protodesc.NewFiles: %w", err))
	}
	optsFile, err := files.FindFileByPath("devalbo/options/v1/options.proto")
	if err != nil {
		fatal(fmt.Errorf("find options.proto: %w", err))
	}
	extTypes := &protoregistry.Types{}
	for i := 0; i < optsFile.Extensions().Len(); i++ {
		xd := optsFile.Extensions().Get(i)
		if err := extTypes.RegisterExtension(dynamicpb.NewExtensionType(xd)); err != nil {
			fatal(fmt.Errorf("register %s: %w", xd.FullName(), err))
		}
	}

	var cmds *descriptorpb.FileDescriptorProto
	for _, f := range fds.File {
		if f.GetName() == "devalbo/optionsspike/v1/commands.proto" {
			cmds = f
			break
		}
	}
	if cmds == nil {
		fatal(fmt.Errorf("commands.proto missing from image"))
	}

	type fieldMeta struct {
		name, help, def, short string
		required               bool
	}
	type methodMeta struct {
		name     string
		methodID uint32
		fields   []fieldMeta
	}
	var methods []methodMeta
	msgByName := map[string]*descriptorpb.DescriptorProto{}
	for _, m := range cmds.MessageType {
		msgByName[m.GetName()] = m
	}

	for _, s := range cmds.Service {
		for _, m := range s.Method {
			mopts, err := resolveMethodOpts(m.GetOptions(), extTypes)
			if err != nil {
				fatal(fmt.Errorf("%s: %w", m.GetName(), err))
			}
			methodID, ok := extUint32(mopts, "devalbo.options.v1.method_id")
			if !ok {
				fatal(fmt.Errorf("%s: missing method_id", m.GetName()))
			}
			mm := methodMeta{name: m.GetName(), methodID: methodID}

			inMsg := msgByName[shortName(m.GetInputType())]
			if inMsg == nil {
				fatal(fmt.Errorf("%s: unknown input %s", m.GetName(), m.GetInputType()))
			}
			for _, f := range inMsg.Field {
				fopts, err := resolveFieldOpts(f.GetOptions(), extTypes)
				if err != nil {
					fatal(fmt.Errorf("%s.%s: %w", m.GetName(), f.GetName(), err))
				}
				fm := fieldMeta{name: f.GetName()}
				fm.help, _ = extString(fopts, "devalbo.options.v1.help")
				fm.required, _ = extBool(fopts, "devalbo.options.v1.required")
				fm.def, _ = extString(fopts, "devalbo.options.v1.default")
				fm.short, _ = extString(fopts, "devalbo.options.v1.short")
				mm.fields = append(mm.fields, fm)
			}
			methods = append(methods, mm)
		}
	}

	fail := 0
	check := func(ok bool, msg string) {
		if ok {
			fmt.Printf("  [PASS] %s\n", msg)
		} else {
			fmt.Printf("  [FAIL] %s\n", msg)
			fail++
		}
	}

	fmt.Println("== host (FileDescriptorSet + dynamicpb) ==")
	check(len(methods) == 2, fmt.Sprintf("2 methods (got %d)", len(methods)))

	byID := map[uint32]methodMeta{}
	for _, m := range methods {
		byID[m.methodID] = m
		fmt.Printf("  method %s method_id=%d fields=%d\n", m.name, m.methodID, len(m.fields))
	}
	greet, ok := byID[1]
	check(ok && greet.name == "Greet", "method_id=1 → Greet")
	add, ok := byID[2]
	check(ok && add.name == "Add", "method_id=2 → Add")

	var name, times fieldMeta
	for _, f := range greet.fields {
		switch f.name {
		case "name":
			name = f
		case "times":
			times = f
		}
	}
	check(name.help == "name to greet", "Greet.name help")
	check(name.required, "Greet.name required")
	check(name.short == "n", "Greet.name short=n")
	check(times.def == "1", "Greet.times default=1")
	check(times.help == "repeat count", "Greet.times help")

	if fail > 0 {
		fmt.Println("HOST=RED")
		os.Exit(1)
	}
	fmt.Println("HOST=GREEN")
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

func resolveFieldOpts(src *descriptorpb.FieldOptions, resolver *protoregistry.Types) (*descriptorpb.FieldOptions, error) {
	if src == nil {
		return &descriptorpb.FieldOptions{}, nil
	}
	b, err := proto.Marshal(src)
	if err != nil {
		return nil, err
	}
	out := &descriptorpb.FieldOptions{}
	if err := (proto.UnmarshalOptions{Resolver: resolver}).Unmarshal(b, out); err != nil {
		return nil, err
	}
	return out, nil
}

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

func extString(m proto.Message, name protoreflect.FullName) (string, bool) {
	var out string
	var found bool
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.FullName() == name {
			out = v.String()
			found = true
			return false
		}
		return true
	})
	return out, found
}

func extBool(m proto.Message, name protoreflect.FullName) (bool, bool) {
	var out bool
	var found bool
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.FullName() == name {
			out = v.Bool()
			found = true
			return false
		}
		return true
	})
	return out, found
}

func shortName(full string) string {
	if i := strings.LastIndex(full, "."); i >= 0 {
		return full[i+1:]
	}
	return full
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
