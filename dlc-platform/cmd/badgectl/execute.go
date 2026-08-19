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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/devalbo/devalbo-ilc/dlc-platform/clispec"
	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// runExecute performs one app command and prints what it answered.
func runExecute(port, typed string, sets []string, wait time.Duration) error {
	file, err := openRaw(port)
	if err != nil {
		return err
	}
	defer file.Close()

	method, err := resolve(file, typed, wait)
	if err != nil {
		return err
	}

	var request []byte
	// FETCHED ONCE, used twice: the same spec describes what to send and how to
	// read what comes back.
	var command *ilcv1.SpecCommand
	if len(sets) > 0 {
		var err error
		if command, err = fetchSpec(file, method, wait); err != nil {
			return err
		}
		if request, err = encodeRequest(command, sets); err != nil {
			return err
		}
	}

	result, err := passThrough(file, method, request, wait)
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
		// BY NAME, from the app's own response spec — the other half of the round
		// trip. Until `SpecResult` existed this could only print a byte count.
		if command == nil {
			var err error
			if command, err = fetchSpec(file, method, wait); err != nil {
				return err
			}
		}
		renderResponse(command, out)
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

// renderResponse prints an app's answer using the app's own description of it.
//
// # Why this decodes generically rather than with a generated type
//
// This tool has no idea which app is on the badge. It cannot import
// `countdownv1.CountResponse`, because the payload is whatever somebody dragged
// onto the flash region — so the only thing that can name these bytes is what
// the app itself said about them.
//
// # Unknown fields are SHOWN, not skipped
//
// A field the spec does not describe still gets a line, by number. The spec
// cannot express nested messages, so an app whose response contains one would
// otherwise appear to have returned less than it did — and "the value is
// missing" and "I could not read the value" are different facts.
func renderResponse(command *ilcv1.SpecCommand, out []byte) {
	byField := map[uint32]*ilcv1.SpecResult{}
	for _, r := range command.GetResults() {
		byField[r.GetField()] = r
	}

	for len(out) > 0 {
		tag, n := binary.Uvarint(out)
		if n <= 0 {
			fmt.Printf("output   %d undecodable bytes: %x\n", len(out), out)
			return
		}
		out = out[n:]
		field, wire := uint32(tag>>3), tag&7

		var raw []byte
		var number uint64
		switch wire {
		case 0:
			value, n := binary.Uvarint(out)
			if n <= 0 {
				return
			}
			number, out = value, out[n:]
		case 2:
			length, n := binary.Uvarint(out)
			if n <= 0 || uint64(len(out[n:])) < length {
				return
			}
			raw, out = out[n:n+int(length)], out[n+int(length):]
		case 5:
			if len(out) < 4 {
				return
			}
			out = out[4:]
			continue
		case 1:
			if len(out) < 8 {
				return
			}
			out = out[8:]
			continue
		default:
			return
		}

		result, described := byField[field]
		if !described {
			fmt.Printf("  field %-2d %s\n", field, undescribed(wire, raw, number))
			continue
		}
		fmt.Printf("  %-10s %s\n", result.GetName(), renderValue(result, raw, number))
	}
}

func undescribed(wire uint64, raw []byte, number uint64) string {
	if wire == 2 {
		return fmt.Sprintf("(not in the spec) %d bytes: %x", len(raw), raw)
	}
	return fmt.Sprintf("(not in the spec) %d", number)
}

func renderValue(result *ilcv1.SpecResult, raw []byte, number uint64) string {
	switch result.GetKind() {
	case ilcv1.SpecKind_SPEC_KIND_STRING:
		return string(raw)
	case ilcv1.SpecKind_SPEC_KIND_BYTES:
		return fmt.Sprintf("%d bytes: %x", len(raw), raw)
	case ilcv1.SpecKind_SPEC_KIND_BOOL:
		return strconv.FormatBool(number != 0)
	case ilcv1.SpecKind_SPEC_KIND_INT32, ilcv1.SpecKind_SPEC_KIND_INT64:
		return strconv.FormatInt(int64(number), 10)
	case ilcv1.SpecKind_SPEC_KIND_ENUM:
		// NUMBER -> NAME, the mirror of encoding. `3` is what the wire carries
		// and `words` is what a person reads; the app declared both.
		short := clispec.ShortEnum(result.GetEnumValues())
		for i := range result.GetEnumValues() {
			if numbers := result.GetEnumNumbers(); i < len(numbers) && uint64(numbers[i]) == number {
				return short[i]
			}
		}
		// A NUMBER THE SPEC DOES NOT NAME is shown as a number, not guessed at:
		// an app built from a newer proto than the spec on the badge can return
		// a value this list has never heard of.
		return fmt.Sprintf("%d (unnamed)", number)
	default:
		return strconv.FormatUint(number, 10)
	}
}

// resolve turns what a person typed into a method id.
//
// # Nothing human-facing should require knowing a number
//
// This tool took `-execute 10002`, so using it meant looking up an id in a
// .proto — for a command whose NAME the app already publishes. Method ids are
// the wire: permanent, locked, and dispatched on. They are not an interface.
//
// A NUMBER IS STILL ACCEPTED, and deliberately: an id that the spec does not
// describe is exactly the case where a name cannot help, and refusing it would
// leave no way to reach a command whose description is missing or broken. That
// is a debugging tool's job.
func resolve(file *os.File, typed string, wait time.Duration) (uint32, error) {
	if id, err := strconv.ParseUint(typed, 10, 32); err == nil {
		return uint32(id), nil
	}

	// ZERO MEANS EVERY COMMAND, which is what makes name lookup one round trip
	// rather than one per candidate.
	commands, err := fetchCommands(file, 0, wait)
	if err != nil {
		return 0, err
	}
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		if strings.EqualFold(command.GetName(), typed) {
			return command.GetMethod(), nil
		}
		names = append(names, command.GetName())
	}
	slices.Sort(names)
	return 0, fmt.Errorf("this app has no command %q — it has %s",
		typed, strings.Join(names, ", "))
}

// fetchCommands asks the app to describe one command, or all of them.
func fetchCommands(file *os.File, method uint32, wait time.Duration) ([]*ilcv1.SpecCommand, error) {
	ask, err := (&ilcv1.GetCommandSpecRequest{MethodId: method}).MarshalVT()
	if err != nil {
		return nil, err
	}
	result, err := passThrough(file, ilcv1.MethodGetCommandSpec, ask, wait)
	if err != nil {
		return nil, fmt.Errorf("asking what this app takes: %w", err)
	}
	if !result.GetSuccess() {
		return nil, fmt.Errorf("the app would not describe itself: %s", result.GetError())
	}
	var spec ilcv1.GetCommandSpecResponse
	if err := spec.UnmarshalVT(result.GetOutput()); err != nil {
		return nil, fmt.Errorf("decoding the command spec: %w", err)
	}
	return spec.GetCommands(), nil
}

// fetchSpec asks the app what a command takes.
func fetchSpec(file *os.File, method uint32, wait time.Duration) (*ilcv1.SpecCommand, error) {
	commands, err := fetchCommands(file, method, wait)
	if err != nil {
		return nil, err
	}
	for _, command := range commands {
		if method == 0 || command.GetMethod() == method {
			return command, nil
		}
	}
	return nil, fmt.Errorf("the app describes no method %d", method)
}

// showSpec prints what an app's commands take, in the app's own words.
func showSpec(port, typed string, wait time.Duration) error {
	file, err := openRaw(port)
	if err != nil {
		return err
	}
	defer file.Close()

	// EVERY COMMAND when nothing was named — the useful default for a person who
	// does not yet know what this app is.
	if typed == "" {
		commands, err := fetchCommands(file, 0, wait)
		if err != nil {
			return err
		}
		for _, command := range commands {
			fmt.Printf("%-12s (method %d)  %s\n",
				command.GetName(), command.GetMethod(), command.GetSummary())
		}
		return nil
	}

	method, err := resolve(file, typed, wait)
	if err != nil {
		return err
	}
	command, err := fetchSpec(file, method, wait)
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
			line += "  one of " + strings.Join(clispec.ShortEnum(values), "|")
		}
		fmt.Println(line)
	}
	// LISTED, NOT DROPPED. The app says which of its fields this description
	// cannot express, and a command that silently ignores one is worse than one
	// that admits it.
	for _, name := range command.GetUnsupported() {
		fmt.Printf("  (%s cannot be set from a spec)\n", name)
	}

	// BOTH HALVES OF THE CONTRACT. A spec that described only the input told you
	// how to ask and nothing about what an answer would mean.
	if results := command.GetResults(); len(results) > 0 {
		fmt.Printf("  answers %s:\n", command.GetResponse())
		for _, r := range results {
			kind := strings.ToLower(strings.TrimPrefix(r.GetKind().String(), "SPEC_KIND_"))
			line := fmt.Sprintf("    %-10s <%s>   field %d", r.GetName(), kind, r.GetField())
			if values := r.GetEnumValues(); len(values) > 0 {
				line += "  one of " + strings.Join(clispec.ShortEnum(values), "|")
			}
			if help := r.GetHelp(); help != "" {
				line += "  " + help
			}
			fmt.Println(line)
		}
	}
	for _, name := range command.GetResponseUnsupported() {
		fmt.Printf("    (%s cannot be shown from a spec)\n", name)
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
		short := clispec.ShortEnum(flag.GetEnumValues())
		for index, name := range flag.GetEnumValues() {
			if !strings.EqualFold(name, value) && !strings.EqualFold(short[index], value) {
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
			return nil, fmt.Errorf("-set %s=%q: expected one of %s",
				flag.GetName(), value, strings.Join(clispec.ShortEnum(flag.GetEnumValues()), ", "))
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

func appendVarint(field uint32, value uint64) []byte {
	out := binary.AppendUvarint(nil, uint64(field)<<3)
	return binary.AppendUvarint(out, value)
}

func appendDelimited(field uint32, value []byte) []byte {
	out := binary.AppendUvarint(nil, uint64(field)<<3|2)
	out = binary.AppendUvarint(out, uint64(len(value)))
	return append(out, value...)
}
