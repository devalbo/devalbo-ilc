// badgectl — ask a world what it is and what it is doing.
//
// The host half of BADGE-CONTROL-PLAN Phase 1. Everything the badge has said
// until now arrived through a buffered one-way log, which cannot distinguish "the
// world is stuck" from "the log is stuck" — an ambiguity that cost three
// debugging cycles in one session.
//
//	badgectl -port /dev/cu.usbmodemilc1
//
// The port also carries the human-readable log, so a reply is FRAMED and this
// scans for the magic rather than assuming the next bytes are its answer.
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	platform "github.com/devalbo/devalbo-ilc/dlc-platform"
	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// tracing turns this tool into an instrument.
//
// # Why this exists
//
// Diagnosing "the badge did not answer" meant hand-writing throwaway Python to
// frame a request and dump what came back — twice, badly, with a parser that
// misread its own output and sent the investigation somewhere wrong. The tool
// that already speaks this protocol correctly should be the one that shows it.
//
// What it prints is the LAYER BELOW the command: every frame in each direction,
// with its kind, length and correlation id. That is exactly the layer where the
// interesting failures live — a reply that never came, a reply meant for
// somebody else, a frame that arrived split.
var tracing bool

func trace(format string, args ...any) {
	if tracing {
		fmt.Fprintf(os.Stderr, "  trace  "+format+"\n", args...)
	}
}

// kindName spells a frame kind for a person reading a trace.
func kindName(kind byte) string {
	switch kind {
	case kindRequest:
		return "request"
	case kindResponse:
		return "response"
	case kindLog:
		return "log"
	case kindHeartbeat:
		return "heartbeat"
	default:
		return fmt.Sprintf("kind-%d", kind)
	}
}

// THE FRAMING, FROM THE ONE SOURCE.
//
// These were hand-written here — a magic string, an overhead sum, four kind
// numbers — and matched the badge only because one person wrote both. They are
// now generated from dlc-platform/protocol/FRAMING.json into Go, Rust and
// TypeScript together, so the badge, this tool and a browser cannot disagree.
var magic = platform.FrameMagic

const overhead = platform.FrameOverhead

const (
	kindRequest   = platform.FrameKindRequest
	kindResponse  = platform.FrameKindResponse
	kindLog       = platform.FrameKindLog
	kindHeartbeat = platform.FrameKindHeartbeat
)

func main() {
	port := flag.String("port", "", "serial port (e.g. /dev/cu.usbmodemilc1)")
	wait := flag.Duration("wait", 3*time.Second, "how long to wait for a reply")
	follow := flag.Duration("follow", 0, "instead of asking, print framed log lines for this long")
	flag.BoolVar(&tracing, "trace", false, "show every frame sent and received")
	beat := flag.Int("beat", 0, "with -follow: ask for a heartbeat every N ms (0 = the world's default)")
	press := flag.String("press", "", "press a button: a, b, c, up, down")
	screen := flag.Bool("screen", false, "print what the panel says")
	list := flag.Bool("list", false, "print the installed payloads")
	sel := flag.Int("select", -1, "choose the payload the next menu resolves to")
	reboot := flag.Bool("reboot", false, "restart the world")
	exec := flag.String("execute", "", "run an app command by NAME (an id also works)")
	spec := flag.String("spec", "", "print what a command takes; -spec=all lists them")
	var sets multiFlag
	flag.Var(&sets, "set", "name=value for -execute, encoded from the app's own spec (repeatable)")
	flag.Parse()

	if *port == "" {
		fmt.Fprintln(os.Stderr, "badgectl: -port is required")
		os.Exit(2)
	}
	if *press != "" {
		if err := pressButton(*port, *press, *wait); err != nil {
			fmt.Fprintln(os.Stderr, "badgectl:", err)
			os.Exit(1)
		}
		return
	}
	if *screen {
		if err := showScreen(*port, *wait); err != nil {
			fmt.Fprintln(os.Stderr, "badgectl:", err)
			os.Exit(1)
		}
		return
	}
	if *spec != "" {
		if *spec == "all" {
			*spec = ""
		}
		if err := showSpec(*port, *spec, *wait); err != nil {
			fmt.Fprintln(os.Stderr, "badgectl:", err)
			os.Exit(1)
		}
		return
	}
	if *exec != "" {
		if err := runExecute(*port, *exec, sets, *wait); err != nil {
			fmt.Fprintln(os.Stderr, "badgectl:", err)
			os.Exit(1)
		}
		return
	}
	if *list {
		if err := listPayloads(*port, *wait); err != nil {
			fmt.Fprintln(os.Stderr, "badgectl:", err)
			os.Exit(1)
		}
		return
	}
	if *sel >= 0 {
		if err := simple(*port, ilcv1.Verb_VERB_SELECT_PAYLOAD,
			&ilcv1.SelectPayloadRequest{Index: uint32(*sel)}, *wait,
			fmt.Sprintf("payload %d selected for the next menu", *sel)); err != nil {
			fmt.Fprintln(os.Stderr, "badgectl:", err)
			os.Exit(1)
		}
		return
	}
	if *reboot {
		if err := simple(*port, ilcv1.Verb_VERB_REBOOT, nil, *wait, "rebooting"); err != nil {
			fmt.Fprintln(os.Stderr, "badgectl:", err)
			os.Exit(1)
		}
		return
	}
	if *follow > 0 {
		if err := followLog(*port, *follow, uint32(*beat)); err != nil {
			fmt.Fprintln(os.Stderr, "badgectl:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(*port, *wait); err != nil {
		fmt.Fprintln(os.Stderr, "badgectl:", err)
		os.Exit(1)
	}
}

// openRaw opens the port and puts it in raw mode ON THE DESCRIPTOR WE KEEP.
//
// # The bug this fixes
//
// The previous version ran `stty` and then opened the port:
//
//	exec.Command("stty", "-f", port, "raw", "-echo").Run()
//	file, _ := os.OpenFile(port, os.O_RDWR, 0)
//
// `stty -f` opens the device, applies the settings, and CLOSES it. A tty's line
// discipline is reset when its last descriptor closes, so by the time we opened
// the port the raw mode we had just asked for was gone and we were talking to a
// cooked terminal. The settings were real, applied to the right device, and
// discarded a microsecond later — which is why the command looked correct in
// review and in `stty -f <port> -a` run by hand afterwards.
//
// A cooked tty does not fail loudly. It CORRUPTS SELECTIVELY:
//
//   - OPOST/ONLCR rewrites 0x0A to 0x0D 0x0A on the way out, so any frame whose
//     length, checksum or payload happens to contain a newline byte arrives one
//     byte longer than its header claims.
//   - ICRNL rewrites 0x0D to 0x0A on the way in, corrupting replies.
//   - IXON eats 0x11 and 0x13 as flow control, so those bytes never arrive at all.
//
// Frames containing none of those bytes go through untouched. That is the worst
// possible failure mode: it depends on the payload, so it presents as an
// intermittent, message-dependent fault in the badge rather than a broken flag
// in the host tool.
//
// Ordering the calls the other way — open first, then `stty` — would also work,
// since our descriptor keeps the tty open and stty's close is no longer the
// last. Setting the discipline on our own descriptor is preferred because it
// does not depend on that reasoning holding, and it cannot be undone by another
// process opening and closing the port mid-session.
func openRaw(port string) (*os.File, error) {
	file, err := os.OpenFile(port, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", port, err)
	}

	// THROUGH SyscallConn, not file.Fd(): `Fd` takes the descriptor out of
	// non-blocking mode, and `readFrame` depends on `SetReadDeadline`, which
	// needs it registered with the runtime poller.
	conn, err := file.SyscallConn()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("%s: %w", port, err)
	}
	var inner error
	if err := conn.Control(func(fd uintptr) { inner = makeRaw(fd) }); err != nil {
		file.Close()
		return nil, fmt.Errorf("%s: %w", port, err)
	}
	if inner != nil {
		file.Close()
		return nil, fmt.Errorf("%s: raw mode: %w", port, inner)
	}
	return file, nil
}

func run(port string, wait time.Duration) error {
	file, err := openRaw(port)
	if err != nil {
		return err
	}
	defer file.Close()

	id := nextRequestID()
	request := &ilcv1.ControlRequest{Verb: ilcv1.Verb_VERB_GET_WORLD_STATE, RequestId: id}
	body, err := request.MarshalVT()
	if err != nil {
		return err
	}
	if _, err := file.Write(frame(body)); err != nil {
		return fmt.Errorf("writing the request: %w", err)
	}

	payload, err := readFrame(file, wait, kindResponse, id)
	if err != nil {
		return err
	}

	var response ilcv1.ControlResponse
	if err := response.UnmarshalVT(payload); err != nil {
		return fmt.Errorf("decoding the response: %w", err)
	}
	if !response.GetOk() {
		return fmt.Errorf("the world refused: %s", response.GetError())
	}

	var state ilcv1.WorldState
	if err := state.UnmarshalVT(response.GetPayload()); err != nil {
		return fmt.Errorf("decoding the world state: %w", err)
	}

	// THE ENUM NAMES, PRINTED FROM THE ENUMS. These used to be strings the
	// world rendered and this reprinted; now the generated type supplies the
	// name, so the two cannot disagree and a new variant shows up here for free.
	fmt.Printf("world     %s\n", state.GetWorld())
	fmt.Printf("tier      %s\n", state.GetTier())
	fmt.Printf("version   %s\n", state.GetVersion())
	// WHICH BUILD, as opposed to which release. `version` has read 0.1.0 since
	// the first commit; this changes whenever the firmware does, which is what
	// makes "is the board running what I just built" answerable.
	if id := state.GetBuildId(); id != "" {
		fmt.Printf("build     %s\n", id)
	}
	fmt.Printf("screen    %s\n", state.GetScreen())
	fmt.Printf("input     %s\n", state.GetInput())
	fmt.Printf("text      %s\n", state.GetText())
	fmt.Printf("activity  %s\n", state.GetActivity())
	// HOW FAR IT HAS GOT, at both resolutions. Printed next to `activity`
	// because they answer neighbouring questions: activity is what KIND of thing
	// the world is doing, phase/stage is how far along it is.
	//
	// This is the pair worth having when a badge comes back from a reboot and
	// says nothing useful — "hardware / psram" is a diagnosis, where silence and
	// a coarse `STARTING` are the same non-answer the control channel exists to
	// replace.
	fmt.Printf("phase     %s", state.GetPhase())
	if stage := state.GetStage(); stage != ilcv1.Stage_STAGE_UNSPECIFIED {
		fmt.Printf(" / %s", stage)
	}
	// A TRANSITION THE WORLD SAYS IT CANNOT MAKE. Printed on the same line
	// because it qualifies the phase above rather than standing alone — a phase
	// reported by a world that has already taken an impossible edge is a phase
	// worth doubting.
	//
	// Shown only when non-zero: a healthy world should not have to say so.
	if faults := state.GetPhaseFaults(); faults > 0 {
		fmt.Printf("  !! %d invalid transition(s)", faults)
	}
	fmt.Println()
	// WHAT THE APP IS DOING, which the world's own activity cannot say: `RUNNING`
	// covers a countdown on tick 3 and an import on file 400 alike.
	if app := state.GetAppActivity(); app != "" {
		fmt.Printf("app       %s\n", app)
	}
	// WHICH BUILD OF THE APP. The world asks once at instantiation, so this is
	// what the running instance calls itself — `app` above says which app.
	if v := state.GetAppVersion(); v != "" {
		fmt.Printf("app ver   %s\n", v)
	}
	// THE VERDICT, as a value. It has always existed as a colour on the panel and
	// a word at the end of the log; both reach a person, and a client had to
	// scrape prose to learn it.
	if v := state.GetVerdict(); v != ilcv1.Verdict_VERDICT_UNSPECIFIED {
		fmt.Printf("verdict   %s\n", v)
	}
	fmt.Printf("uptime    %d ms\n", state.GetUptimeMs())
	// PASS-THROUGH BOOKKEEPING. Printed always, because the moment you want it
	// is the moment a request went unanswered — and asking again with a flag is
	// one round trip too late to see the state that caused it.
	fmt.Printf("requests  %d offered, %d taken, instance %s\n",
		state.GetRequestsOffered(), state.GetRequestsTaken(),
		map[bool]string{true: "open", false: "closed"}[state.GetInstanceOpen()])
	return nil
}

// multiFlag collects a flag given more than once.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// buttons maps what a person types to what the proto calls it.
//
// SPELLED OUT rather than derived from the enum name, because `-press b` is what
// somebody at a terminal will type and `-press BUTTON_B` is not. The proto names
// stay canonical on the wire; this is only the keyboard shortcut for them.
var buttons = map[string]ilcv1.Button{
	"a":    ilcv1.Button_BUTTON_A,
	"b":    ilcv1.Button_BUTTON_B,
	"c":    ilcv1.Button_BUTTON_C,
	"up":   ilcv1.Button_BUTTON_UP,
	"down": ilcv1.Button_BUTTON_DOWN,
}

// pressButton asks the world to act as though somebody pressed something.
func pressButton(port, name string, wait time.Duration) error {
	button, ok := buttons[strings.ToLower(name)]
	if !ok {
		valid := make([]string, 0, len(buttons))
		for key := range buttons {
			valid = append(valid, key)
		}
		slices.Sort(valid)
		return fmt.Errorf("no button %q — try one of %s", name, strings.Join(valid, ", "))
	}

	file, err := openRaw(port)
	if err != nil {
		return err
	}
	defer file.Close()

	payload, err := (&ilcv1.PressButtonRequest{Button: button}).MarshalVT()
	if err != nil {
		return err
	}
	response, err := ask(file, ilcv1.Verb_VERB_PRESS_BUTTON, payload, wait)
	if err != nil {
		return err
	}
	if !response.GetOk() {
		return fmt.Errorf("the world refused: %s", response.GetError())
	}
	fmt.Printf("pressed %s\n", button)
	return nil
}

// showScreen prints the panel's text.
func showScreen(port string, wait time.Duration) error {
	file, err := openRaw(port)
	if err != nil {
		return err
	}
	defer file.Close()

	response, err := ask(file, ilcv1.Verb_VERB_GET_SCREEN, nil, wait)
	if err != nil {
		return err
	}
	if !response.GetOk() {
		return fmt.Errorf("the world refused: %s", response.GetError())
	}
	var screen ilcv1.ScreenResponse
	if err := screen.UnmarshalVT(response.GetPayload()); err != nil {
		return fmt.Errorf("decoding the screen: %w", err)
	}
	for _, row := range screen.GetRows() {
		fmt.Println(row)
	}
	return nil
}

// listPayloads prints what the badge could run, and what it will refuse.
func listPayloads(port string, wait time.Duration) error {
	file, err := openRaw(port)
	if err != nil {
		return err
	}
	defer file.Close()

	response, err := ask(file, ilcv1.Verb_VERB_LIST_PAYLOADS, nil, wait)
	if err != nil {
		return err
	}
	if !response.GetOk() {
		return fmt.Errorf("the world refused: %s", response.GetError())
	}
	var list ilcv1.ListPayloadsResponse
	if err := list.UnmarshalVT(response.GetPayload()); err != nil {
		return fmt.Errorf("decoding the payload list: %w", err)
	}
	for _, p := range list.GetPayloads() {
		marks := ""
		if p.GetIndex() == list.GetSelected() {
			marks += ">"
		} else {
			marks += " "
		}
		if p.GetIsDefault() {
			marks += "*"
		} else {
			marks += " "
		}
		// WHY IT WILL NOT RUN, not just that it will not. `runnable` is the
		// world's verdict and `integrity` is the evidence for it; a corrupt file
		// and one built for another engine need different things done to them.
		state := strings.ToLower(strings.TrimPrefix(p.GetIntegrity().String(), "INTEGRITY_"))
		if !p.GetRunnable() {
			state += " (will not run)"
		}
		fmt.Printf("%s %d  %-16s %8d B  method %-6d %s\n",
			marks, p.GetIndex(), p.GetName(), p.GetSize(), p.GetEntryMethod(), state)
	}
	return nil
}

// simple sends a verb whose answer is only yes or no.
func simple(port string, verb ilcv1.Verb, request vtMessage, wait time.Duration, done string) error {
	file, err := openRaw(port)
	if err != nil {
		return err
	}
	defer file.Close()

	var payload []byte
	if request != nil {
		if payload, err = request.MarshalVT(); err != nil {
			return err
		}
	}
	response, err := ask(file, verb, payload, wait)
	if err != nil {
		return err
	}
	if !response.GetOk() {
		return fmt.Errorf("the world refused: %s", response.GetError())
	}
	fmt.Println(done)
	return nil
}

// vtMessage is the half of the generated API this tool uses.
type vtMessage interface{ MarshalVT() ([]byte, error) }

// ask sends one verb and returns the world's answer.
func ask(file *os.File, verb ilcv1.Verb, payload []byte, wait time.Duration) (*ilcv1.ControlResponse, error) {
	id := nextRequestID()
	body, err := (&ilcv1.ControlRequest{Verb: verb, Payload: payload, RequestId: id}).MarshalVT()
	if err != nil {
		return nil, err
	}
	trace("-> %-9s verb=%s request_id=%d payload=%d bytes",
		"request", strings.TrimPrefix(verb.String(), "VERB_"), id, len(payload))
	if _, err := file.Write(frame(body)); err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	reply, err := readFrame(file, wait, kindResponse, id)
	if err != nil {
		return nil, err
	}
	var response ilcv1.ControlResponse
	if err := response.UnmarshalVT(reply); err != nil {
		return nil, fmt.Errorf("decoding the response: %w", err)
	}
	return &response, nil
}

// frame wraps a payload the way `control::frame` does.
func frame(payload []byte) []byte {
	out := make([]byte, 0, overhead+len(payload))
	out = append(out, magic...)
	out = append(out, kindRequest)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(payload)))
	out = append(out, payload...)
	return binary.LittleEndian.AppendUint32(out, checksum(payload))
}

// checksum is FNV-1a, matching `catalog::checksum`.
//
// NEVER ZERO there, because 0 means "not checksummed" for a payload — a
// distinction this protocol does not need but must not contradict, since the two
// share the function.
func checksum(bytes []byte) uint32 {
	hash := platform.ChecksumOffsetBasis
	for _, b := range bytes {
		hash ^= uint32(b)
		hash *= platform.ChecksumPrime
	}
	if hash == 0 {
		return 1
	}
	return hash
}

// nextRequestID hands out a correlation number.
//
// # Why a counter and not a random number
//
// Nothing here needs unpredictability — this is not a security boundary, it is
// a way to tell one answer from another. A counter makes a failure legible: a
// reply carrying id 3 while we wait on id 5 says "two questions ago", which is
// a sentence a person can act on. A random 64-bit value says nothing.
//
// STARTED FROM THE CLOCK so two runs of this tool against the same badge do not
// both begin at 1 — a stale reply to the PREVIOUS run's id 1 would otherwise
// look exactly like the answer to this run's.
var requestCounter = uint64(time.Now().UnixNano()) &^ 0xFFFF

func nextRequestID() uint64 {
	requestCounter++
	return requestCounter
}

// subscribe asks the world for a set of notices, and waits for it to agree.
//
// THE WHOLE SET, every time — the message declares what the subscription IS, not
// how it should change. So calling this with no notices is the unsubscribe, and
// calling it twice with the same set is not a bug to defend against.
//
// It waits for the reply rather than firing and forgetting, because "the world
// is not sending me anything" and "the world never heard me ask" are the two
// explanations a follower has to distinguish, and only one of them is worth
// waiting around for.
func subscribe(file *os.File, beatMs uint32, notices ...ilcv1.Notice) error {
	payload, err := (&ilcv1.Subscription{Notices: notices, HeartbeatMs: beatMs}).MarshalVT()
	if err != nil {
		return err
	}
	id := nextRequestID()
	body, err := (&ilcv1.ControlRequest{
		Verb:      ilcv1.Verb_VERB_SUBSCRIBE,
		Payload:   payload,
		RequestId: id,
	}).MarshalVT()
	if err != nil {
		return err
	}
	if _, err := file.Write(frame(body)); err != nil {
		return fmt.Errorf("subscribing: %w", err)
	}

	reply, err := readFrame(file, 3*time.Second, kindResponse, id)
	if err != nil {
		return fmt.Errorf("subscribing: %w", err)
	}
	var response ilcv1.ControlResponse
	if err := response.UnmarshalVT(reply); err != nil {
		return fmt.Errorf("decoding the subscription reply: %w", err)
	}
	if !response.GetOk() {
		return fmt.Errorf("the world refused the subscription: %s", response.GetError())
	}

	// WHAT WAS GRANTED, not what was asked for. A world grants the intersection
	// with what it supports, so a client built against a newer proto finds out
	// here that something is not coming rather than waiting for it forever.
	var granted ilcv1.Subscription
	if err := granted.UnmarshalVT(response.GetPayload()); err != nil {
		return fmt.Errorf("decoding the granted subscription: %w", err)
	}
	for _, wanted := range notices {
		if !slices.Contains(granted.GetNotices(), wanted) {
			return fmt.Errorf("the world does not send %s", wanted)
		}
	}
	// THE RATE IT AGREED TO, which may not be the one asked for — a world
	// clamps to what it can sustain, and a client that assumed otherwise would
	// wait the wrong length of time before calling a silence a hang.
	if beatMs != 0 && granted.GetHeartbeatMs() != beatMs {
		fmt.Fprintf(os.Stderr, "badgectl: asked for a beat every %d ms, granted %d ms\n",
			beatMs, granted.GetHeartbeatMs())
	}
	return nil
}

// replyID reads a ControlResponse's request_id without a full decode.
//
// A FULL DECODE WOULD ALSO WORK and would be wrong here: this runs on every
// frame the port produces, including ones meant for somebody else and ones that
// are not `ControlResponse` at all. Reading one varint field is cheap and cannot
// fail in a way that matters — an unreadable frame simply has no id, and is
// treated as uncorrelated rather than rejected.
// replyIDField is `ControlResponse.request_id`, GENERATED rather than transcribed.
//
// It was the literal `4`. Asking a descriptor at runtime would be better still,
// but `protoc-gen-go-lite` omits reflection — deliberately, since the badge's
// tooling and the wasm tiers cannot afford a reflection-based codec — so this
// tool has the same problem the badge has and takes the same fix.
//
// A wrong number here makes every reply look uncorrelated, which reads as a
// badge that is not answering at all.
const replyIDField = ilcv1.F_CONTROL_RESPONSE_REQUEST_ID

func replyID(payload []byte) uint64 {
	for len(payload) > 0 {
		tag, n := binary.Uvarint(payload)
		if n <= 0 {
			return 0
		}
		payload = payload[n:]
		field, wire := uint32(tag>>3), tag&7
		switch wire {
		case 0:
			value, n := binary.Uvarint(payload)
			if n <= 0 {
				return 0
			}
			payload = payload[n:]
			if field == replyIDField {
				return value
			}
		case 2:
			length, n := binary.Uvarint(payload)
			if n <= 0 || uint64(len(payload[n:])) < length {
				return 0
			}
			payload = payload[n+int(length):]
		case 5:
			if len(payload) < 4 {
				return 0
			}
			payload = payload[4:]
		case 1:
			if len(payload) < 8 {
				return 0
			}
			payload = payload[8:]
		default:
			return 0
		}
	}
	return 0
}

// isIdle reports whether a read error just means "nothing has arrived yet".
//
// io.EOF IS NOT AN END OF STREAM ON A SERIAL PORT. In raw mode with VMIN=0 and
// VTIME=0 the kernel returns a zero-byte read when the port is idle instead of
// EAGAIN, and Go reports a zero-byte read as io.EOF. A badge that simply has
// nothing to say is indistinguishable, at this layer, from a cable that has been
// pulled — so the only workable reading is the optimistic one, and the caller's
// deadline is what actually bounds the wait.
//
// This surfaced the moment raw mode started being applied for real: the previous
// cooked-mode reads blocked, so the zero-byte case never came up.
func isIdle(err error) bool {
	return errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, io.EOF)
}

// readFrame scans the stream for a reply, discarding the log around it.
//
// The port carries human-readable output as well as frames, so anything that is
// not a frame is skipped a byte at a time — the same rule the world applies to a
// person typing into it.
func readFrame(file *os.File, wait time.Duration, want byte, id uint64) ([]byte, error) {
	deadline := time.Now().Add(wait)
	var buffered []byte
	chunk := make([]byte, 512)

	for time.Now().Before(deadline) {
		if err := file.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err == nil {
			n, err := file.Read(chunk)
			if n > 0 {
				buffered = append(buffered, chunk[:n]...)
			}
			if err != nil && !isIdle(err) && n == 0 {
				return nil, err
			}
			if n == 0 {
				// An idle port polls hot otherwise: `read` on a raw tty returns
				// immediately rather than waiting, so this loop would spin a core
				// for the whole timeout.
				time.Sleep(20 * time.Millisecond)
			}
		}

		for {
			payload, consumed, done := scan(buffered)
			if !done {
				break
			}
			// THE KIND, READ BEFORE THE FRAME IS CONSUMED — `kindOf` indexes the
			// frame we just matched, and the slice moves on the next line.
			var kind byte
			if payload != nil {
				kind = kindOf(buffered)
				trace("<- %-9s %4d bytes  request_id=%d", kindName(kind), len(payload), replyID(payload))
			} else {
				trace("<- %d byte(s) of non-frame traffic (log text)", consumed)
			}
			buffered = buffered[consumed:]
			// A PUSHED FRAME ARRIVING WHILE WE WAIT FOR AN ANSWER IS TRAFFIC, NOT
			// AN ERROR. Once a client is subscribed the wire carries log frames
			// too, and a reader that returned the first frame of any kind would
			// hand a `LogLine` to a decoder expecting a `ControlResponse` — which
			// mostly SUCCEEDS, because most byte strings parse as some message,
			// and then reports nonsense.
			if payload != nil && kind == want {
				// A REPLY TO SOMETHING ELSE IS NOT OUR ANSWER, and skipping it
				// is the whole point of correlating. The case that made this
				// necessary: a request times out, the caller asks again, and the
				// LATE reply to the first arrives first — indistinguishable from
				// an answer to the second, and wrong in a way nothing downstream
				// can detect.
				//
				// `id == 0` means this caller did not correlate, which keeps the
				// old behaviour for anything that has not been updated.
				if id != 0 {
					if got := replyID(payload); got != 0 && got != id {
						// SAY SO. A silently discarded reply is indistinguishable
						// from a world that never answered, and the timeout that
						// follows blames the wrong thing entirely — "is
						// BADGE_CONTROL=on?" when the channel is plainly working.
						fmt.Fprintf(os.Stderr,
							"badgectl: ignoring a reply for request %d (waiting for %d)\n", got, id)
						continue
					}
				}
				return payload, nil
			}
		}
	}
	return nil, fmt.Errorf("no reply within %s — is BADGE_CONTROL=on in this build?", wait)
}

// scan mirrors `control::scan`: a frame, bytes to skip, or wait for more.
func scan(bytes []byte) (payload []byte, consumed int, progressed bool) {
	if len(bytes) < len(magic) {
		return nil, 0, false
	}
	if string(bytes[:len(magic)]) != string(magic) {
		return nil, 1, true // skip a byte of log and look again
	}
	if len(bytes) < overhead {
		return nil, 0, false
	}
	length := int(binary.LittleEndian.Uint16(bytes[5:7]))
	if len(bytes) < overhead+length {
		return nil, 0, false
	}
	body := bytes[7 : 7+length]
	recorded := binary.LittleEndian.Uint32(bytes[7+length:])
	if recorded != checksum(body) {
		return nil, len(magic), true
	}
	return body, overhead + length, true
}

// stageName renders a `Stage` for a person: the enum name without its prefix.
//
// The PREFIX IS THE PROTO'S, not the reader's. `STAGE_PAYLOAD_REGION` is the
// right identifier on the wire and the wrong thing to put in a column that is
// scanned by eye.
func stageName(stage ilcv1.Stage) string {
	if stage == ilcv1.Stage_STAGE_UNSPECIFIED {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(stage.String(), "STAGE_"))
}

// scopeMark flags the checks only the board can answer.
//
// The same `*` the badge draws on its own panel, and the reason it is worth a
// column: a run where every marked stage passes and an unmarked one fails means
// the board and the emulator DISAGREE — a much more interesting result than a
// loose wire, and invisible without this.
func scopeMark(scope ilcv1.Scope) string {
	if scope == ilcv1.Scope_SCOPE_HARDWARE_ONLY {
		return "*"
	}
	return " "
}

// kindOf reports a frame's kind, for a caller that has already matched a frame.
func kindOf(bytes []byte) byte { return bytes[4] }

// followLog prints framed log lines as they arrive.
//
// THE POINT OF RENDERING THEM BACK AS TEXT: the badge always emits
// human-readable output on the same wire, so a person with `cat` loses nothing.
// This exists so a TOOL reading frames is not worse off than that person — and
// so a test can assert on a stage and a level rather than grepping prose.
func followLog(port string, window time.Duration, beatMs uint32) error {
	file, err := openRaw(port)
	if err != nil {
		return err
	}
	defer file.Close()

	// ASK FIRST. A world pushes nothing until someone subscribes, so without
	// this the port stays quiet and the badge looks hung — which is exactly the
	// ambiguity this tool exists to remove.
	//
	// The subscription is RETROACTIVE: the world answers by streaming the log it
	// already has, from the start of the run, before anything new. So attaching
	// late still shows the boot that failed.
	if err := subscribe(file, beatMs, ilcv1.Notice_NOTICE_LOG, ilcv1.Notice_NOTICE_HEARTBEAT); err != nil {
		return err
	}
	// LEAVE IT AS WE FOUND IT. A badge that keeps framing log lines at a client
	// which has gone is spending its outbound wire on nobody. Best-effort: if the
	// port is already gone there is nothing to say and nothing to fix.
	defer func() { _ = subscribe(file, 0) }()

	levels := map[ilcv1.Level]string{
		ilcv1.Level_LEVEL_STAGE_OK:   "ok  ",
		ilcv1.Level_LEVEL_STAGE_FAIL: "FAIL",
		ilcv1.Level_LEVEL_NOTE:       "    ",
		// ANNOUNCED AND UNRESOLVED. If this is the last line printed, it names
		// what the world was doing when it stopped — the diagnosis a stream of
		// completed lines cannot give.
		ilcv1.Level_LEVEL_STAGE_OPEN: "... ",
	}

	deadline := time.Now().Add(window)
	var buffered []byte
	chunk := make([]byte, 512)
	for time.Now().Before(deadline) {
		if err := file.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err == nil {
			n, _ := file.Read(chunk)
			if n > 0 {
				buffered = append(buffered, chunk[:n]...)
			} else {
				time.Sleep(20 * time.Millisecond)
			}
		}
		for {
			payload, consumed, done := scan(buffered)
			if !done {
				break
			}
			kind := byte(0)
			if payload != nil {
				kind = kindOf(buffered)
			}
			buffered = buffered[consumed:]
			if payload == nil {
				continue
			}
			switch kind {
			case kindLog:
				var line ilcv1.LogLine
				if err := line.UnmarshalVT(payload); err != nil {
					continue
				}
				fmt.Printf("%8dms %s %s%-16s %s\n",
					line.GetUptimeMs(), levels[line.GetLevel()],
					scopeMark(line.GetScope()), stageName(line.GetStage()), line.GetText())

			case kindHeartbeat:
				// THE BEAT SAYS ONLY "STILL HERE", so it prints as one line and
				// carries what a watcher would otherwise have to ask for: how
				// long the world has been up, what it is doing, and what the app
				// last said it was doing.
				var state ilcv1.WorldState
				if err := state.UnmarshalVT(payload); err != nil {
					continue
				}
				app := state.GetAppActivity()
				if app != "" {
					app = "  " + app
				}
				fmt.Printf("%8dms ~    %-16s %s%s\n",
					state.GetUptimeMs(), "alive",
					strings.ToLower(strings.TrimPrefix(state.GetActivity().String(), "ACTIVITY_")),
					app)
			}
		}
	}
	return nil
}
