package main

// PARSE VECTORS — the check that two command-line front ends agree.
//
// Parsing happens BEFORE the boundary the parity check guards: parity compares
// command results, written filesystems and event streams, all downstream of a
// request that already exists. So the native CLI and the in-page terminal could
// each be perfectly self-consistent while turning `create --title "Buy milk"`
// into different requests, and every other check in this repo would stay green.
//
// These vectors pin the mapping `argv -> request bytes`. The SAME lines and the
// same expected hex appear in the browser test (hosts/web/test/terminal.spec.ts),
// so a divergence between the Go runner and the TypeScript one fails on one side
// or the other rather than going unnoticed.
//
// If you change a vector here, change it there. That duplication is the point —
// two independent implementations asserting the same bytes is the whole check,
// and generating both from one source would defeat it.

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"

	notesv1 "github.com/devalbo/devalbo-ilc/example-apps/notes/gen/go/notes/v1"
	"github.com/devalbo/dlc-platform"
)

// recordingPort captures the request without running anything.
type recordingPort struct{ request []byte }

func (p *recordingPort) Execute(_ uint32, request []byte) platform.Result {
	p.request = append([]byte(nil), request...)
	return platform.Result{Success: true}
}

// The clock the fill hook supplies, fixed so bytes are comparable.
const fixedNow = 1700000000

func TestParseVectors(t *testing.T) {
	vectors := []struct {
		name string
		argv []string
		hex  string
	}{
		{
			// Field 1 (title), field 2 (body), field 3 (created_at, host-filled).
			name: "create with title and body",
			argv: []string{"create", "--title", "Buy milk", "--body", "two litres"},
			hex:  "0a08427579206d696c6b120a74776f206c69747265731880e2cfaa06",
		},
		{
			// An absent optional field must not be encoded as an explicit zero:
			// proto3 cannot tell them apart, and a partial update would blank it.
			name: "create without body",
			argv: []string{"create", "--title", "Buy milk"},
			hex:  "0a08427579206d696c6b1880e2cfaa06",
		},
		{
			name: "list takes no fields",
			argv: []string{"list"},
			hex:  "",
		},
		{
			// Signed ints are 64-bit two's complement varints, so a negative
			// value is TEN bytes and an int32 is sign-extended first. It is the
			// one genuinely non-obvious rule in protobuf's scalar encoding, and
			// the likeliest place for two hand-written encoders to disagree.
			name: "negative int64 (two's complement)",
			argv: []string{"create", "--title", "x", "--created-at", "-1"},
			hex:  "0a017818ffffffffffffffffff01",
		},
		{
			name: "delete by id",
			argv: []string{"delete", "--id", "buy-milk"},
			hex:  "0a086275792d6d696c6b",
		},
	}

	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			port := &recordingPort{}
			var out, errOut bytes.Buffer
			a := app(port, &out, &errOut, nil, fixedClock)
			if code := a.Run(v.argv); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut.String())
			}
			if got := hex.EncodeToString(port.request); got != v.hex {
				t.Errorf("argv %v\n got %s\nwant %s\n\nIf this is a deliberate change, update the matching vector in hosts/web/test/terminal.spec.ts too — the whole point is that both front ends agree.",
					v.argv, got, v.hex)
			}
		})
	}
}

func TestParseVectorsCoverTheClock(t *testing.T) {
	// The fill hook is part of the mapping: a host that supplies the clock
	// changes the bytes, so a vector that did not exercise it would compare a
	// weaker claim than the one that matters.
	port := &recordingPort{}
	var out, errOut bytes.Buffer
	if code := app(port, &out, &errOut, nil, fixedClock).Run([]string{"create", "--title", "x"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var req notesv1.CreateRecordRequest
	if err := req.UnmarshalVT(port.request); err != nil {
		t.Fatal(err)
	}
	if req.GetCreatedAt() != fixedNow {
		t.Errorf("created_at = %d, want the host-supplied clock %d", req.GetCreatedAt(), fixedNow)
	}
}

func fixedClock() time.Time { return time.Unix(fixedNow, 0) }
