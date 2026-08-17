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
	"syscall"
	"time"

	ilcv1 "github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/v1"
)

// Must match `control::MAGIC` in the embedded crate.
var magic = []byte("DLCC")

const overhead = 4 + 1 + 2 + 4

// Frame kinds, matching `control.rs`.
const (
	kindRequest  = 1
	kindResponse = 2
	kindLog      = 3
)

func main() {
	port := flag.String("port", "", "serial port (e.g. /dev/cu.usbmodemilc1)")
	wait := flag.Duration("wait", 3*time.Second, "how long to wait for a reply")
	follow := flag.Duration("follow", 0, "instead of asking, print framed log lines for this long")
	flag.Parse()

	if *port == "" {
		fmt.Fprintln(os.Stderr, "badgectl: -port is required")
		os.Exit(2)
	}
	if *follow > 0 {
		if err := followLog(*port, *follow); err != nil {
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

	request := &ilcv1.ControlRequest{Verb: ilcv1.Verb_VERB_GET_WORLD_STATE}
	body, err := request.MarshalVT()
	if err != nil {
		return err
	}
	if _, err := file.Write(frame(body)); err != nil {
		return fmt.Errorf("writing the request: %w", err)
	}

	payload, err := readFrame(file, wait)
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

	fmt.Printf("world     %s\n", state.GetWorld())
	fmt.Printf("tier      %s\n", state.GetTier())
	fmt.Printf("version   %s\n", state.GetVersion())
	fmt.Printf("activity  %s\n", state.GetActivity())
	// WHAT THE APP IS DOING, which the world's own activity cannot say: `RUNNING`
	// covers a countdown on tick 3 and an import on file 400 alike.
	if app := state.GetAppActivity(); app != "" {
		fmt.Printf("app       %s\n", app)
	}
	fmt.Printf("uptime    %d ms\n", state.GetUptimeMs())
	for key, value := range state.GetConfig() {
		fmt.Printf("config    %s=%s\n", key, value)
	}
	return nil
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
	hash := uint32(0x811c9dc5)
	for _, b := range bytes {
		hash ^= uint32(b)
		hash *= 0x01000193
	}
	if hash == 0 {
		return 1
	}
	return hash
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
func readFrame(file *os.File, wait time.Duration) ([]byte, error) {
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
			buffered = buffered[consumed:]
			if payload != nil {
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

// kindOf reports a frame's kind, for a caller that has already matched a frame.
func kindOf(bytes []byte) byte { return bytes[4] }

// followLog prints framed log lines as they arrive.
//
// THE POINT OF RENDERING THEM BACK AS TEXT: the badge always emits
// human-readable output on the same wire, so a person with `cat` loses nothing.
// This exists so a TOOL reading frames is not worse off than that person — and
// so a test can assert on a stage and a level rather than grepping prose.
func followLog(port string, window time.Duration) error {
	file, err := openRaw(port)
	if err != nil {
		return err
	}
	defer file.Close()

	levels := map[ilcv1.Level]string{
		ilcv1.Level_LEVEL_STAGE_OK:   "ok  ",
		ilcv1.Level_LEVEL_STAGE_FAIL: "FAIL",
		ilcv1.Level_LEVEL_NOTE:       "    ",
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
			if payload == nil || kind != kindLog {
				continue
			}
			var line ilcv1.LogLine
			if err := line.UnmarshalVT(payload); err != nil {
				continue
			}
			fmt.Printf("%8dms %s %-22s %s\n",
				line.GetUptimeMs(), levels[line.GetLevel()], line.GetStage(), line.GetText())
		}
	}
	return nil
}
