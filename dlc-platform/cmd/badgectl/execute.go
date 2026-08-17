package main

// Running an app's own command, encoded from the app's own schema.
//
// # Why this file does not know anything about any app
//
// A pass-through carries `method_id` plus the app's request bytes. The obvious
// way to build those bytes is to teach this tool the app's fields — and that
// would make the shell the second place the app's schema is written down, which
// is exactly the drift this project keeps removing (a stage that was prose, a
// world that was a map of strings).
//
// The app already declares its own shape, and the badge already reads it: an
// engine answers `GetCommandSpec` with a `SpecFlag` per field carrying the FIELD
// NUMBER, the kind, the default and the permitted enum names, generated from the
// app's `.proto`. The badge uses it to decide whether to show a keyboard or a
// spinner. This uses the same answer to decide how to encode.
//
// So `-set from=8` works for any app that declares a `from`, and works for a
// field this tool has never heard of, because the app said what it was. Nothing
// here is per-app, and adding a command to an app adds nothing here.
//
// # It is fetched through the very verb it serves
//
// `GetCommandSpec` is method 5 — an ordinary engine method, dispatched like any
// other. So the spec is fetched by a pass-through `VERB_EXECUTE` with method 5,
// and no verb had to be invented to ask.

import (
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// runExecute performs one app command and prints what it answered.
func runExecute(port string, method uint, sets []string, wait time.Duration) error {
	file, err := openRaw(port)
	if err != nil {
		return err
	}
	defer file.Close()

	var request []byte
	if len(sets) > 0 {
		command, err := fetchSpec(file, uint32(method), wait)
		if err != nil {
			return err
		}
		if request, err = encodeRequest(command, sets); err != nil {
			return err
		}
	}

	result, err := passThrough(file, uint32(method), request, wait)
	if err != nil {
		return err
	}
	// THE APP'S VERDICT, which is not the world's. Both are reported because a
	// command that ran and said no is a different outcome from one that could not
	// be run, and a caller acts differently on each.
	if !result.GetSuccess() {
		if e := result.GetError(); e != "" {
			return fmt.Errorf("the app failed: %s", e)
		}
		return fmt.Errorf("the app reported failure")
	}
	if out := result.GetOutput(); len(out) > 0 {
		// OPAQUE, AND SAID SO. `GetCommandSpec` describes what a command TAKES
		// and not what it returns, so this tool can encode `-set from=3` by name
		// and can only count the bytes that come back. Printing a length dressed
		// up as a result would suggest the tool understood it.
		//
		// The hex is what a person can actually act on today: it can be pasted
		// into a decoder, and it makes an empty response distinguishable from a
		// short one. A response spec would replace this with field names.
		fmt.Printf("output   %d bytes, undecoded (no response spec): %x\n", len(out), out)
	}
	fmt.Println("ok")
	return nil
}

// passThrough runs one command on the badge and returns the app's answer.
func passThrough(file *os.File, method uint32, request []byte, wait time.Duration) (*ilcv1.ExecuteResponse, error) {
	payload, err := (&ilcv1.ExecuteRequest{MethodId: method, Request: request}).MarshalVT()
	if err != nil {
		return nil, err
	}
	// THE DEADLINE COVERS THE APP, not the link. The world queues the request,
	// runs it at the top of its next turn, and answers when the app returns — so
	// a countdown from 8 takes eight seconds to reply and that is correct.
	response, err := ask(file, ilcv1.Verb_VERB_EXECUTE, payload, wait)
	if err != nil {
		return nil, err
	}
	if !response.GetOk() {
		return nil, fmt.Errorf("the world refused: %s", response.GetError())
	}
	var result ilcv1.ExecuteResponse
	if err := result.UnmarshalVT(response.GetPayload()); err != nil {
		return nil, fmt.Errorf("decoding the app's answer: %w", err)
	}
	return &result, nil
}

// fetchSpec asks the app what a command takes.
func fetchSpec(file *os.File, method uint32, wait time.Duration) (*ilcv1.SpecCommand, error) {
	ask, err := (&ilcv1.GetCommandSpecRequest{MethodId: method}).MarshalVT()
	if err != nil {
		return nil, err
	}
	result, err := passThrough(file, ilcv1.MethodGetCommandSpec, ask, wait)
	if err != nil {
		return nil, fmt.Errorf("asking what method %d takes: %w", method, err)
	}
	if !result.GetSuccess() {
		return nil, fmt.Errorf("the app would not describe method %d: %s", method, result.GetError())
	}
	var spec ilcv1.GetCommandSpecResponse
	if err := spec.UnmarshalVT(result.GetOutput()); err != nil {
		return nil, fmt.Errorf("decoding the command spec: %w", err)
	}
	for _, command := range spec.GetCommands() {
		if method == 0 || command.GetMethod() == method {
			return command, nil
		}
	}
	return nil, fmt.Errorf("the app describes no method %d", method)
}

// showSpec prints what an app's commands take, in the app's own words.
func showSpec(port string, method uint, wait time.Duration) error {
	file, err := openRaw(port)
	if err != nil {
		return err
	}
	defer file.Close()

	command, err := fetchSpec(file, uint32(method), wait)
	if err != nil {
		return err
	}
	fmt.Printf("%s  (method %d)  %s\n", command.GetName(), command.GetMethod(), command.GetSummary())
	for _, flag := range command.GetFlags() {
		kind := strings.ToLower(strings.TrimPrefix(flag.GetKind().String(), "SPEC_KIND_"))
		line := fmt.Sprintf("  -set %s=<%s>   field %d", flag.GetName(), kind, flag.GetField())
		if d := flag.GetDefaultValue(); d != "" {
			line += "  default " + d
		}
		if values := flag.GetEnumValues(); len(values) > 0 {
			short := make([]string, 0, len(values))
			for _, name := range values {
				short = append(short, strings.ToLower(shortEnum(name)))
			}
			line += "  one of " + strings.Join(short, "|")
		}
		fmt.Println(line)
	}
	// LISTED, NOT DROPPED. The app says which of its fields this description
	// cannot express, and a command that silently ignores one is worse than one
	// that admits it.
	for _, name := range command.GetUnsupported() {
		fmt.Printf("  (%s cannot be set from a spec)\n", name)
	}
	return nil
}

// encodeRequest builds an app's request from `-set` pairs and the app's spec.
//
// BY FIELD NUMBER AND KIND, both taken from the spec. Nothing here is per-app,
// and a field this tool has never seen encodes correctly because the app said
// what it was.
func encodeRequest(command *ilcv1.SpecCommand, sets []string) ([]byte, error) {
	byName := map[string]*ilcv1.SpecFlag{}
	for _, flag := range command.GetFlags() {
		byName[flag.GetName()] = flag
	}

	var out []byte
	for _, set := range sets {
		name, value, found := strings.Cut(set, "=")
		if !found {
			return nil, fmt.Errorf("-set %q: expected name=value", set)
		}
		flag, ok := byName[name]
		if !ok {
			known := make([]string, 0, len(byName))
			for key := range byName {
				known = append(known, key)
			}
			return nil, fmt.Errorf("%s takes no %q — it takes %s",
				command.GetName(), name, strings.Join(known, ", "))
		}
		encoded, err := encodeField(flag, value)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded...)
	}
	return out, nil
}

func encodeField(flag *ilcv1.SpecFlag, value string) ([]byte, error) {
	field := flag.GetField()
	switch flag.GetKind() {
	case ilcv1.SpecKind_SPEC_KIND_STRING, ilcv1.SpecKind_SPEC_KIND_BYTES:
		return appendDelimited(field, []byte(value)), nil

	case ilcv1.SpecKind_SPEC_KIND_BOOL:
		on, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("-set %s=%q: expected true or false", flag.GetName(), value)
		}
		if !on {
			// PROTO3 OMITS A FALSE, and so must this: sending an explicit zero
			// and omitting the field are the same message, and the shorter one
			// is what a generated encoder would have produced.
			return nil, nil
		}
		return appendVarint(field, 1), nil

	case ilcv1.SpecKind_SPEC_KIND_INT32, ilcv1.SpecKind_SPEC_KIND_INT64:
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("-set %s=%q: expected a number", flag.GetName(), value)
		}
		if number == 0 {
			return nil, nil
		}
		// A NEGATIVE int32/int64 IS A TEN-BYTE VARINT in proto3, not a zigzag —
		// that is `sint`, a different declared type. Casting through uint64 is
		// what produces the sign extension the wire format expects.
		return appendVarint(field, uint64(number)), nil

	case ilcv1.SpecKind_SPEC_KIND_UINT32, ilcv1.SpecKind_SPEC_KIND_UINT64:
		number, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("-set %s=%q: expected a non-negative number", flag.GetName(), value)
		}
		if number == 0 {
			return nil, nil
		}
		return appendVarint(field, number), nil

	case ilcv1.SpecKind_SPEC_KIND_ENUM:
		// BY NAME, from the names the APP declared, matched on the SHORT form as
		// well as the full one: the spec carries proto's `STYLE_ROCKET` and a
		// person types `rocket`. Matching only the full name would make the
		// declared default — which is written short — unusable as an argument.
		//
		// AND BY THE DECLARED NUMBER, not by position. `enum_numbers` is the
		// value the app gave that name; using the index is right only for an
		// enum numbered densely from zero, and wrong SILENTLY otherwise, because
		// the wrong number is still a legal one.
		for index, name := range flag.GetEnumValues() {
			if !strings.EqualFold(name, value) && !strings.EqualFold(shortEnum(name), value) {
				continue
			}
			number := int32(index)
			if numbers := flag.GetEnumNumbers(); index < len(numbers) {
				number = numbers[index]
			}
			if number == 0 {
				return nil, nil
			}
			return appendVarint(field, uint64(number)), nil
		}
		number, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			// THE SHORT NAMES IN THE ERROR, because they are what the caller
			// should type. Listing `STYLE_PLAIN, STYLE_ROCKET` invites a second
			// failed attempt spelling it exactly that way.
			short := make([]string, 0, len(flag.GetEnumValues()))
			for _, name := range flag.GetEnumValues() {
				short = append(short, strings.ToLower(shortEnum(name)))
			}
			return nil, fmt.Errorf("-set %s=%q: expected one of %s",
				flag.GetName(), value, strings.Join(short, ", "))
		}
		if number == 0 {
			return nil, nil
		}
		return appendVarint(field, number), nil

	default:
		return nil, fmt.Errorf("-set %s: this tool cannot encode a %s",
			flag.GetName(), flag.GetKind())
	}
}

// shortEnum drops proto's value-name prefix: `STYLE_ROCKET` -> `ROCKET`.
//
// Every value of an enum carries the same prefix, so it is the part of the name
// with no information in it — and the part nobody types.
func shortEnum(name string) string {
	if at := strings.LastIndex(name, "_"); at >= 0 && at+1 < len(name) {
		return name[at+1:]
	}
	return name
}

func appendVarint(field uint32, value uint64) []byte {
	out := binary.AppendUvarint(nil, uint64(field)<<3)
	return binary.AppendUvarint(out, value)
}

func appendDelimited(field uint32, value []byte) []byte {
	out := binary.AppendUvarint(nil, uint64(field)<<3|2)
	out = binary.AppendUvarint(out, uint64(len(value)))
	return append(out, value...)
}
