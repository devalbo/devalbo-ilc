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
	"os"
	"os/exec"
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

func run(port string, wait time.Duration) error {
	// RAW MODE FIRST. macOS puts a tty in canonical mode by default, which
	// buffers by line and rewrites control characters — both fatal to a binary
	// frame. `stty` is the portable way to say otherwise without taking a
	// dependency on a serial library for one flag.
	if out, err := exec.Command("stty", "-f", port, "raw", "-echo").CombinedOutput(); err != nil {
		return fmt.Errorf("stty %s: %v: %s", port, err, out)
	}

	file, err := os.OpenFile(port, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("opening %s: %w", port, err)
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
			if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) && n == 0 {
				return nil, err
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
	if out, err := exec.Command("stty", "-f", port, "raw", "-echo").CombinedOutput(); err != nil {
		return fmt.Errorf("stty %s: %v: %s", port, err, out)
	}
	file, err := os.OpenFile(port, os.O_RDWR, 0)
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
