// Package cli turns a generated clispec surface into a working command line
// (Decision 29).
//
// The division of labour, chosen so that as little as possible is written here:
//
//	protowire (google.golang.org/protobuf)  encoding a field by number + wire type
//	ff/v3/ffcli + stdlib flag               subcommands, flags, defaults, -h, usage
//	this package                            required flags, enum names, and the
//	                                        hook where a slot prints a response
//
// `protowire` matters more than it looks: building request bytes without the
// concrete Go type is the one genuinely fiddly part, and go-lite messages carry
// no protoreflect support to do it the usual way. protowire is the official
// low-level encoder for exactly this, so varint and length-prefix handling is
// borrowed rather than re-derived.
//
// HOST-SIDE ONLY. This imports google.golang.org/protobuf and ffcli, neither of
// which may enter the engine's TinyGo build — and neither does, because the
// engine never imports this package. `clispec` is the leaf the generated code
// sees; this is the leaf only a native host sees.
package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/devalbo/dlc-platform/clispec"
)

// encodeRequest builds request bytes from parsed flag values.
//
// Two stages, deliberately separate: RESOLVE the value (literal, a file, stdin)
// and then ENCODE it for its wire type. A field's source and its type vary
// independently — a bundle is bytes-from-a-file, a long note body is
// string-from-a-file — so collapsing the two would mean re-deciding the
// convention per type.
//
// Fields are emitted in ascending field-number order. Proto does not require it,
// but a deterministic encoding means two hosts building the same request produce
// the same bytes — which is what lets a parity vector compare them at all.
func encodeRequest(cmd clispec.Command, values map[string][]string, stdin io.Reader) ([]byte, error) {
	var out []byte
	for _, f := range fieldsInOrder(cmd) {
		vals, ok := values[f.Name]
		if !ok || len(vals) == 0 {
			continue
		}
		if !f.Repeated && len(vals) > 1 {
			return nil, fmt.Errorf("--%s given %d times but is not repeated", f.Name, len(vals))
		}
		for _, v := range vals {
			resolved, err := resolve(f, v, stdin)
			if err != nil {
				return nil, err
			}
			out, err = appendField(out, f, resolved)
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// resolve turns what the user typed into the value the field wants.
func resolve(f clispec.Flag, raw string, stdin io.Reader) (string, error) {
	switch f.Source {
	case clispec.SourceFile:
		// `-` is the long-standing convention for "actually, stdin" and costs
		// one line, so a caller can pipe into a flag declared as a file.
		if raw == "-" {
			return readAll(f, stdin)
		}
		b, err := os.ReadFile(raw)
		if err != nil {
			return "", fmt.Errorf("--%s: %w", f.Name, err)
		}
		return string(b), nil

	case clispec.SourceStdin:
		return readAll(f, stdin)

	default:
		return raw, nil
	}
}

func readAll(f clispec.Flag, stdin io.Reader) (string, error) {
	if stdin == nil {
		return "", fmt.Errorf("--%s reads stdin, but this host supplied none", f.Name)
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("--%s: reading stdin: %w", f.Name, err)
	}
	return string(b), nil
}

func appendField(dst []byte, f clispec.Flag, raw string) ([]byte, error) {
	num := protowire.Number(f.Field)
	switch f.Kind {
	case clispec.KindString, clispec.KindBytes:
		// Same wire type. `bytes` differs only in where the value came from,
		// which resolve() has already handled.
		dst = protowire.AppendTag(dst, num, protowire.BytesType)
		return protowire.AppendString(dst, raw), nil

	case clispec.KindBool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("--%s: %q is not a boolean", f.Name, raw)
		}
		dst = protowire.AppendTag(dst, num, protowire.VarintType)
		var n uint64
		if b {
			n = 1
		}
		return protowire.AppendVarint(dst, n), nil

	case clispec.KindInt32, clispec.KindInt64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("--%s: %q is not an integer", f.Name, raw)
		}
		dst = protowire.AppendTag(dst, num, protowire.VarintType)
		// Signed proto ints are varint-encoded as two's complement, which is what
		// AppendVarint does with the unsigned reinterpretation.
		return protowire.AppendVarint(dst, uint64(n)), nil

	case clispec.KindUint32, clispec.KindUint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("--%s: %q is not a non-negative integer", f.Name, raw)
		}
		dst = protowire.AppendTag(dst, num, protowire.VarintType)
		return protowire.AppendVarint(dst, n), nil

	case clispec.KindEnum:
		idx, err := enumNumber(f, raw)
		if err != nil {
			return nil, err
		}
		dst = protowire.AppendTag(dst, num, protowire.VarintType)
		return protowire.AppendVarint(dst, uint64(idx)), nil

	default:
		return nil, fmt.Errorf("--%s: unsupported field kind", f.Name)
	}
}

// enumNumber resolves an enum VALUE NAME to its number.
//
// By position in the declaration list, which is correct for proto3: the first
// value must be zero and the generator emits them in declaration order. Names
// are accepted rather than numbers because a number on a command line is
// unreadable and unstable to a reader, even though it is the wire form.
func enumNumber(f clispec.Flag, raw string) (int, error) {
	for i, name := range f.EnumValues {
		if name == raw {
			return i, nil
		}
	}
	return 0, fmt.Errorf("--%s: %q is not one of %v", f.Name, raw, f.EnumValues)
}

// fieldsInOrder sorts a command's flags by field number without mutating the
// generated slice — the spec is a package-level var shared by every caller.
func fieldsInOrder(cmd clispec.Command) []clispec.Flag {
	out := append([]clispec.Flag(nil), cmd.Flags...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Field < out[j-1].Field; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
