# Build tic-tac-toe with `dlc` — a tutorial

**For a person, typing.** Every command here has been run; where something does not work yet, this document
says so instead of pretending.

You will build one game of tic-tac-toe with **one engine** and **three front ends**: a terminal, a browser,
and a handheld badge. The point is not the game — it is that the rules exist exactly once, and no front end
can second-guess them.

| Part | Front end | State today |
| --- | --- | --- |
| 1–5 | **terminal** | ✅ works, start to finish |
| 6 | **browser** | ✅ works, start to finish |
| 7 | front ends agree | ✅ works |
| 8 | **badge** (RP2350 / RP2040) | ⚠️ **blocked** — three framework gaps, listed in the order they must be fixed |

Part 8 is honest rather than aspirational. If you want a badge running tic-tac-toe today you cannot have one;
what you can have is a precise account of what stands in the way.

> **A reference implementation already exists** at `$ILC/example-apps/tictactoe/`. This tutorial builds the same
> app under a different name so nothing is overwritten, and you can diff against it when stuck. If you would
> rather read than type, read that instead.

---

## 0. Before you start

**You need `dlc` installed** — [do that first](./INSTALL-DLC-STEPS.md), it takes about ten minutes and you
never repeat it. From here on this tutorial assumes:

- `dlc --help` works from any directory
- you know the path to your `devalbo-ilc` checkout (call it `$ILC` below), because unpublished packages mean a
  new project points at it

```bash
export ILC=/path/to/devalbo-ilc
```

Everything else you need arrives with the project: `dlc new` writes a `devbox.json`, so each app provisions
its own pinned toolchain.

**One habit worth adopting now:** work inside the project's `devbox shell`. Codegen needs plugins that live
there, and the commands below assume it.

---

## 1. Scaffold the project

Anywhere you keep projects — this does not have to live inside the framework checkout:

```bash
cd ~/projects
dlc new ttt --tiers native --tiers web \
  --module github.com/you/ttt \
  --platform-path "$ILC"
```

Two things about that command, each of which cost a failed attempt while writing this:

- **`--tiers` is repeatable, not comma-separated.** `--tiers native,web` fails with
  `tier "native,web" is not supported yet`.
- **`--platform-path` is written into the project verbatim**, so an absolute path (`$ILC`) is the safe form.
  A relative one has to be relative to the *new project*, not to your shell.

Now generate and check:

```bash
cd ttt
devbox shell                        # the project ships its own
make gen && go mod tidy && make verify
```

Expected, roughly:

```
cd proto && buf lint
dlc gen
gen: wrote gen/go/dlcconfig/config.go
go build -o ttt ./hosts/native
ttt 0.1.0
hello, ILC — from ttt
```

**Do not continue until that works.** The scaffold is meant to run before you have written a line.

### What you got

```
ttt/
├── dlc.toml                      what this project IS: name, version, tiers
├── proto/ttt/v1/commands.proto   the command surface — the file you will edit most
├── engine/                       ALL the business logic; every tier shares it
├── hosts/native/                 the terminal front end: argv in, a request out
├── hosts/web/                    the browser front end
└── cmd/engine-component/         the wasm entry point the browser loads
```

The split that matters: **`engine/` is shared by every tier; `hosts/<tier>/` is one tier's presentation and
input.** A tier is a directory of host code plus an entry in `dlc.toml`, and the entry is checked — a declared
tier with no directory fails the build.

---

## 2. Declare the game in proto

The command surface **is** the schema: flags, help text, positionals and enum menus are all generated from
`.proto`. Designing the game means writing this file.

In `proto/ttt/v1/commands.proto`, keep the scaffolded `syntax`, `package`, `option go_package` and the
`import "devalbo/options/v1/options.proto"` lines, and replace the service and messages with:

```proto
service GameService {
  // Print the board.
  rpc GetState(GetStateRequest) returns (GetStateResponse) {
    option (devalbo.options.v1.method_id) = 10000;
    option (devalbo.options.v1.cli_name) = "state";
  }
  // Play a square, 1-9.
  rpc Play(PlayRequest) returns (PlayResponse) {
    option (devalbo.options.v1.method_id) = 10001;
    option (devalbo.options.v1.cli_name) = "play";
  }
  // Start over.
  rpc NewGame(NewGameRequest) returns (NewGameResponse) {
    option (devalbo.options.v1.method_id) = 10002;
    option (devalbo.options.v1.cli_name) = "new-game";
  }
}

// What is in a square, or whose turn it is. ONE representation of "nothing", and
// it is the zero value: proto3 defaults to zero, so a separate EMPTY would mean
// an unset square said UNSPECIFIED while the engine wrote EMPTY, and every reader
// would have to know the two meant the same thing.
enum Mark {
  MARK_UNSPECIFIED = 0; // empty
  MARK_X = 1;
  MARK_O = 2;
}

// How the game stands, as ONE value the engine computed.
enum Outcome {
  OUTCOME_UNSPECIFIED = 0;
  OUTCOME_IN_PROGRESS = 1;
  OUTCOME_WINNER_X = 2;
  OUTCOME_WINNER_O = 3;
  OUTCOME_DRAW = 4;
}

message GameState {
  repeated Mark board = 1;           // nine squares, row-major from the top left
  Mark turn = 2;                     // UNSPECIFIED once the game is over
  Outcome outcome = 3;
  repeated uint32 winning_line = 4;  // board indexes of the winning three
}

message GetStateRequest {}
message GetStateResponse { GameState state = 1; }

message PlayRequest {
  uint32 square = 1 [
    (devalbo.options.v1.help) = "square to play, 1-9 (row-major from top left)",
    (devalbo.options.v1.required) = true,
    (devalbo.options.v1.cli_positional) = 1
  ];
}
message PlayResponse { GameState state = 1; }

message NewGameRequest {}
message NewGameResponse { GameState state = 1; }

// The semantic event: the game changed, and here is what it now IS.
message StateChangedEvent {
  option (devalbo.options.v1.topic) = "game.state-changed";
  GameState state = 1;
}
```

Then regenerate:

```bash
make gen
```

### Why the schema looks like this

**`Outcome` is one field, not `winner` + `draw` + `over`.** The multi-field version is the obvious first draft
and it is wrong twice. It makes every front end interpret a *combination* — "winner set? else draw? else in
progress" — a small decision repeated in each front end that two of them can make differently. And it puts
nonsense on the wire: a winner *and* a draw is representable, with nothing to reject it. One enum makes the
illegal states unrepresentable and leaves a renderer with a `switch` over values the engine named.

**`method_id`s are permanent.** 1–9999 belongs to the framework; yours start at 10000.
`proto/method-ids.lock` is committed and the build fails if a number changes. **Never write an id in Go or
TypeScript** — they are generated. Reserve ids you are not using yet *in the proto* with
`reserved_method_id`, never in a comment.

**`cli_positional`** is why you will type `play 5` rather than `play --square 5`. **`cli_name`** is why the rpc
`GetState` is spelled `state` on the command line: dispatch is on the id, so the name is cosmetic.

**`winning_line`** is the difference between "a front end may draw what the engine named" and "a front end may
find a winner itself". Keep it.

---

## 3. Write the rules

This is the whole app. Replace the scaffolded `greet` handler in `engine/commands.go` with:

```go
package engine

import (
	"errors"

	"github.com/devalbo/devalbo-ilc/dlc-platform"

	"github.com/you/ttt/gen/go/dlcconfig"
	tttv1 "github.com/you/ttt/gen/go/ttt/v1"
)

const gameFile = "game.json"

func init() {
	platform.RegisterAll() // the inherited verbs: version, export-fs, import-fs, reset-fs
	platform.SetVersion(dlcconfig.Display())
	platform.RegisterRaw(tttv1.GameServiceHandlers(
		handleGetState, handlePlay, handleNewGame,
	))
}

func handleGetState(*tttv1.GetStateRequest) (*tttv1.GetStateResponse, error) {
	state, err := load()
	if err != nil {
		return nil, err
	}
	return &tttv1.GetStateResponse{State: state}, nil
}

func handlePlay(req *tttv1.PlayRequest) (*tttv1.PlayResponse, error) {
	state, err := load()
	if err != nil {
		return nil, err
	}
	// Every one of these refusals is a DECISION, and every one lives here rather
	// than in a front end. A browser that greyed out taken squares by itself
	// would be a second rulebook.
	if state.Outcome != tttv1.Outcome_OUTCOME_IN_PROGRESS {
		return nil, errors.New("play: the game is over — start a new one")
	}
	if req.Square < 1 || req.Square > 9 {
		return nil, errors.New("play: square must be 1-9")
	}
	i := int(req.Square) - 1
	if state.Board[i] != tttv1.Mark_MARK_UNSPECIFIED {
		return nil, errors.New("play: that square is taken")
	}

	state.Board[i] = state.Turn
	decide(state)
	if err := save(state); err != nil {
		return nil, err
	}
	platform.EmitEvent(&tttv1.StateChangedEvent{State: state}) // AFTER the write
	return &tttv1.PlayResponse{State: state}, nil
}

func handleNewGame(*tttv1.NewGameRequest) (*tttv1.NewGameResponse, error) {
	state := fresh()
	if err := save(state); err != nil {
		return nil, err
	}
	platform.EmitEvent(&tttv1.StateChangedEvent{State: state})
	return &tttv1.NewGameResponse{State: state}, nil
}

// The eight ways to win, as board indexes.
var lines = [8][3]int{
	{0, 1, 2}, {3, 4, 5}, {6, 7, 8}, // rows
	{0, 3, 6}, {1, 4, 7}, {2, 5, 8}, // columns
	{0, 4, 8}, {2, 4, 6}, // diagonals
}

// decide works out what the board now MEANS: outcome, winning line, whose turn.
// This function is the point of the whole app. If a front end ever needs to know
// any of it, the front end ASKS.
func decide(s *tttv1.GameState) {
	for _, line := range lines {
		a, b, c := s.Board[line[0]], s.Board[line[1]], s.Board[line[2]]
		if a != tttv1.Mark_MARK_UNSPECIFIED && a == b && b == c {
			if a == tttv1.Mark_MARK_X {
				s.Outcome = tttv1.Outcome_OUTCOME_WINNER_X
			} else {
				s.Outcome = tttv1.Outcome_OUTCOME_WINNER_O
			}
			s.WinningLine = []uint32{uint32(line[0]), uint32(line[1]), uint32(line[2])}
			s.Turn = tttv1.Mark_MARK_UNSPECIFIED // nobody's turn once it is won
			return
		}
	}
	full := true
	for _, m := range s.Board {
		if m == tttv1.Mark_MARK_UNSPECIFIED {
			full = false
			break
		}
	}
	if full {
		s.Outcome = tttv1.Outcome_OUTCOME_DRAW
		s.Turn = tttv1.Mark_MARK_UNSPECIFIED
		return
	}
	if s.Turn == tttv1.Mark_MARK_X {
		s.Turn = tttv1.Mark_MARK_O
	} else {
		s.Turn = tttv1.Mark_MARK_X
	}
}

func fresh() *tttv1.GameState {
	return &tttv1.GameState{
		Board:   make([]tttv1.Mark, 9), // "empty" is the zero value, so make() is the whole job
		Turn:    tttv1.Mark_MARK_X,
		Outcome: tttv1.Outcome_OUTCOME_IN_PROGRESS,
	}
}

// A missing file is a NEW GAME, not an error: a first run must not need setup.
func load() (*tttv1.GameState, error) {
	data, err := platform.ReadFile(gameFile)
	if err != nil {
		return fresh(), nil
	}
	var state tttv1.GameState
	if err := state.UnmarshalJSON(data); err != nil {
		return nil, errors.New("game.json is not readable as a game: " + err.Error())
	}
	// A hand-edited board would index out of range everywhere downstream; refuse
	// it here, where the message can name the file.
	if len(state.Board) != 9 {
		return nil, errors.New("game.json does not have 9 squares")
	}
	return &state, nil
}

func save(s *tttv1.GameState) error {
	body, err := s.MarshalJSON() // canonical JSON: the file is meant to be read and diffed
	if err != nil {
		return err
	}
	return platform.WriteTree(platform.Root(), []platform.File{{Path: gameFile, Content: body}})
}
```

### Three constraints you are working under

**No reflection, no `encoding/json`.** This code compiles with TinyGo for the browser, so use the generated
`MarshalJSON`. Reaching for `encoding/json` breaks the web build.

**Emit after the write, once per command.** A subscriber that re-reads on the event must find the new state
already there.

**Nothing here knows what tier it is on.** No branching on native-versus-browser anywhere. That is the
invariant the whole architecture exists to protect.

---

## 4. Play it in the terminal

The generated surface already knows your three commands. What it needs is a **renderer** — the one
hand-written part of a command, because how a board should *look* is presentation, and presentation belongs to
the front end.

### 4a. The projection

New file, `hosts/native/projection.go`:

```go
package main

import (
	"fmt"
	"strings"

	tttv1 "github.com/you/ttt/gen/go/ttt/v1"
)

// Projection is what this front end is showing, as text.
//
// EXPORTED because it is the front end's contract, not a detail: §7 feeds the
// same state to this and to the browser and compares the two. A test that
// scraped stdout instead would be asserting about padding.
//
// THIS FILE DECIDES NOTHING. It prints the outcome because the engine computed
// it, and highlights the winning line because the engine named it.
func Projection(s *tttv1.GameState) string {
	if s == nil {
		return "(no game)\n"
	}
	won := map[uint32]bool{}
	for _, i := range s.GetWinningLine() {
		won[i] = true
	}

	var b strings.Builder
	for row := 0; row < 3; row++ {
		cells := make([]string, 3)
		for col := 0; col < 3; col++ {
			i := uint32(row*3 + col)
			cells[col] = cell(s, i, won[i])
		}
		// Each cell is padded to three characters, so a row is exactly as wide
		// as the separator below it. Get this wrong and §7 will tell you.
		b.WriteString(strings.Join(cells, "|") + "\n")
		if row < 2 {
			b.WriteString("---+---+---\n")
		}
	}
	b.WriteString(status(s))
	return b.String()
}

// cell draws one square. An empty square shows its NUMBER, because a player
// typing `play 5` needs to know which 5 that is — pure presentation, and exactly
// the sort of thing a front end is for.
func cell(s *tttv1.GameState, i uint32, winning bool) string {
	sym := symbol(s.GetBoard()[i])
	if sym == "" {
		return fmt.Sprintf(" %d ", i+1)
	}
	if winning {
		return "[" + sym + "]" // the line the ENGINE named
	}
	return " " + sym + " "
}

// status is a SWITCH OVER ONE ENGINE-COMPUTED VALUE, not an interpretation of
// several. This is what `Outcome` being a single enum buys: no "winner set? else
// draw? else in progress" for each front end to get subtly different.
func status(s *tttv1.GameState) string {
	switch s.GetOutcome() {
	case tttv1.Outcome_OUTCOME_WINNER_X:
		return "X wins\n"
	case tttv1.Outcome_OUTCOME_WINNER_O:
		return "O wins\n"
	case tttv1.Outcome_OUTCOME_DRAW:
		return "a draw\n"
	default:
		return symbol(s.GetTurn()) + " to play\n"
	}
}

func symbol(m tttv1.Mark) string {
	switch m {
	case tttv1.Mark_MARK_X:
		return "X"
	case tttv1.Mark_MARK_O:
		return "O"
	}
	return ""
}
```

### 4b. Wire it into the command line

`hosts/native/main.go` came scaffolded and **will not compile right now** — it references `MethodGreet` and
`AppServiceCLI`, and you deleted both in §2. Two edits fix it.

**First**, in the `Commands:` line, point at your service:

```go
Commands: append(append([]clispec.Command{}, tttv1.GameServiceCLI...), ilcv1.PlatformServiceCLI...),
```

**Second**, in the `Render:` map, replace the single `MethodGreet` entry with three — one per command, all
printing the same projection:

```go
		Render: map[uint32]cli.Renderer{
			tttv1.MethodGetState: render(func(out io.Writer, r *tttv1.GetStateResponse) error {
				_, err := io.WriteString(out, Projection(r.GetState()))
				return err
			}),
			tttv1.MethodPlay: render(func(out io.Writer, r *tttv1.PlayResponse) error {
				_, err := io.WriteString(out, Projection(r.GetState()))
				return err
			}),
			tttv1.MethodNewGame: render(func(out io.Writer, r *tttv1.NewGameResponse) error {
				_, err := io.WriteString(out, Projection(r.GetState()))
				return err
			}),

			// LEAVE THE FOUR BELOW ALONE. version / export-fs / import-fs /
			// reset-fs are inherited verbs you did not write and did not ask
			// for — every ILC app has them, so every ILC app renders them.
			ilcv1.MethodVersion: /* … as scaffolded … */
```

**Leave the rest of the file exactly as it is.** In particular:

- **`platform.Boot(...)`** at the top grants the filesystem root before anything can touch it — the native
  equivalent of the WASI preopen a browser installs before instantiating. It is why your game lands in
  `./.ttt/` and why the inherited `reset-fs` can only ever clear that subtree.
- **the `render[T, PT]` generic helper** at the bottom adapts a typed printer to the byte-level `Renderer`, so
  each printer says what it prints and nothing about decoding. Generics, not reflection — the same reason the
  engine's typed handlers work under TinyGo.
- **the four platform renderers.** Delete one and you get
  `command "version" (method 1) has no renderer registered` — deliberately an error rather than silence,
  because a command that prints nothing looks like one that succeeded quietly.

Notice what is *not* in that file: no `switch args[0]`, no usage string, no flag declarations. Which
subcommands exist, what flags they take, which are required, what the help says — all generated from
`commands.proto`.

### 4c. Play

```bash
make build
./ttt new-game
./ttt play 5
./ttt play 1
./ttt state
```

```
 1 | 2 | 3
---+---+---
 4 | X | 6
---+---+---
 7 | 8 | 9
O to play
```

Then try the refusals. They come from the engine, not the front end:

```bash
./ttt play 5     # play: that square is taken
./ttt play 99    # play: square must be 1-9
```

And read what it wrote — the file is meant to be legible:

```bash
cat .ttt/game.json
```

**Where did `.ttt/` come from?** The host *granted* it. The engine never chooses a location; it writes
`game.json` and lands wherever the host said.

---

## 5. Test the rules, then break the test

The rules are the risky part, and they need no front end at all:

```bash
cat > engine/commands_test.go <<'EOF'
package engine_test

import (
	"testing"

	"github.com/devalbo/devalbo-ilc/dlc-platform"

	_ "github.com/you/ttt/engine" // registers the commands
	tttv1 "github.com/you/ttt/gen/go/ttt/v1"
)

// Commands are tested THROUGH the registry — the same path every front end uses —
// so a passing test means the wiring works, not just the function.
func TestXWinsTopRow(t *testing.T) {
	platform.SetRoot(t.TempDir())
	call(t, tttv1.MethodNewGame, &tttv1.NewGameRequest{})
	for _, sq := range []uint32{1, 4, 2, 5} { // X:1 O:4 X:2 O:5
		call(t, tttv1.MethodPlay, &tttv1.PlayRequest{Square: sq})
	}
	out := call(t, tttv1.MethodPlay, &tttv1.PlayRequest{Square: 3}) // X takes the top row

	var resp tttv1.PlayResponse
	if err := resp.UnmarshalVT(out); err != nil {
		t.Fatal(err)
	}
	if resp.State.Outcome != tttv1.Outcome_OUTCOME_WINNER_X {
		t.Fatalf("outcome = %v, want WINNER_X", resp.State.Outcome)
	}
	if len(resp.State.WinningLine) != 3 {
		t.Fatalf("winning_line = %v, want three squares", resp.State.WinningLine)
	}
}

func call(t *testing.T, method uint32, req interface{ MarshalVT() ([]byte, error) }) []byte {
	t.Helper()
	in, err := req.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	r := platform.Execute(method, in)
	if !r.Success {
		t.Fatalf("method %d: %s", method, r.Err)
	}
	return r.Output
}
EOF
go test ./engine/
```

**Now break it on purpose.** Delete one diagonal from `lines` and run the test again. It should still pass —
which tells you the test covers one line and not eight. Add a case that plays that diagonal, watch it fail,
then put the line back.

That habit is the house style: **a check nobody has watched fail is indistinguishable from a broken one.**

---

## 6. Play it in the browser

The same engine, compiled to WebAssembly, running in a worker with its filesystem bound to the browser's
origin-private filesystem. **Nothing in `engine/` changes.**

### 6a. The markup

In `hosts/web/index.html`, replace the scaffolded `<input>`, `<button>` and `<pre>` with a board, a status line
and a new-game button:

```html
      <div id="game"></div>
      <p id="status" data-testid="status"></p>
      <button id="new-game" data-testid="new-game">new game</button>
```

### 6b. The view

Replace `hosts/web/src/view.ts` entirely:

```ts
// ttt — web tier slot.
//
// A DOM grid of nine buttons. The terminal draws ASCII from the SAME state and
// shares no markup, no layout and no code with this — only the schema.
//
// THIS FILE DECIDES NOTHING. It disables a square because the engine filled it,
// highlights a line because the engine named it, and says who won because
// `outcome` says so. Break the engine's win detection and BOTH front ends go
// wrong identically — which is the proof that presentation carries no logic.
import type { EnginePort } from "@devalbo/dlc-web/port";

import { StateChangedEventTopic } from "@gen/ttt/v1/commands.events.pb";
import {
  GetStateRequest,
  GetStateResponse,
  Mark,
  Outcome,
  NewGameRequest,
  PlayRequest,
  StateChangedEvent,
  type GameState,
} from "@gen/ttt/v1/commands.pb";
import {
  MethodGetState,
  MethodNewGame,
  MethodPlay,
} from "@gen/ttt/v1/commands.registry.pb";

export type GameView = {
  play(square: number): Promise<void>;
  newGame(): Promise<void>;
  /** Re-read from the engine. Cold start calls this; events do the rest. */
  refresh(): Promise<void>;
  /** What this front end is showing, as text — compared against the terminal in §7. */
  projection(): string;
  unmount(): Promise<void>;
};

export function mountGame(
  port: EnginePort,
  root: HTMLElement = document.getElementById("game")!,
  statusEl: HTMLElement = document.getElementById("status")!,
): GameView {
  let state: GameState | null = null;

  const cells: HTMLButtonElement[] = [];
  const grid = document.createElement("div");
  grid.className = "ttt-grid";
  grid.dataset.testid = "board";
  for (let i = 0; i < 9; i++) {
    const b = document.createElement("button");
    b.dataset.testid = `square-${i + 1}`;
    b.className = "ttt-cell";
    b.addEventListener("click", () => void play(i + 1));
    cells.push(b);
    grid.append(b);
  }
  root.append(grid);

  function draw() {
    if (!state) return;
    const board = state.board ?? [];
    const won = new Set(state.winningLine ?? []);
    board.forEach((m, i) => {
      const b = cells[i];
      b.textContent = symbol(m) ?? String(i + 1);
      // Disabled because the engine FILLED it or ENDED the game — not because
      // this file worked out that the move would be illegal.
      b.disabled = m !== Mark.UNSPECIFIED || isOver(state!);
      b.classList.toggle("won", won.has(i));
    });
    statusEl.textContent = status(state);
  }

  async function refresh(): Promise<void> {
    // COLD START: events are ephemeral, so a front end that rendered only from
    // the stream shows an empty board on reload. Prime with a query.
    const r = await port.execute(MethodGetState, GetStateRequest.toBinary({}));
    if (!r.success) return;
    state = GetStateResponse.fromBinary(r.output).state ?? null;
    draw();
  }

  async function play(square: number): Promise<void> {
    const r = await port.execute(MethodPlay, PlayRequest.toBinary({ square }));
    if (!r.success) {
      // The engine's refusal, shown verbatim. This file has no idea why the move
      // was illegal and does not need one.
      statusEl.textContent = r.error ?? "(refused)";
      return;
    }
    // NO redraw here. The engine emits `game.state-changed` and the subscription
    // below renders it — so if events stop working the board visibly freezes
    // rather than the capability rotting unnoticed.
  }

  async function newGame(): Promise<void> {
    await port.execute(MethodNewGame, NewGameRequest.toBinary({}));
  }

  const unsubscribing = port.subscribe((topic, payload) => {
    if (topic !== StateChangedEventTopic) return;
    // The whole state arrives in the payload. Rendered, never written back from:
    // a stale render is fixed by the next event, a write-back would make the
    // event a second source of truth.
    state = StateChangedEvent.fromBinary(payload).state ?? null;
    draw();
  });

  return {
    play,
    newGame,
    refresh,
    projection: () => projectionOf(state),
    async unmount() {
      (await unsubscribing)();
    },
  };
}

/**
 * The text projection — written to match the terminal's output EXACTLY, because
 * §7 compares them. What must agree is the semantics, not the pixels.
 */
export function projectionOf(s: GameState | null): string {
  if (!s) return "(no game)\n";
  const board = s.board ?? [];
  const won = new Set(s.winningLine ?? []);
  const lines: string[] = [];
  for (let row = 0; row < 3; row++) {
    const cells: string[] = [];
    for (let col = 0; col < 3; col++) {
      const i = row * 3 + col;
      const sym = symbol(board[i]);
      if (!sym) cells.push(` ${i + 1} `);
      else if (won.has(i)) cells.push(`[${sym}]`);
      else cells.push(` ${sym} `);
    }
    lines.push(cells.join("|"));
    if (row < 2) lines.push("---+---+---");
  }
  return lines.join("\n") + "\n" + status(s);
}

function symbol(m: Mark | undefined): string | null {
  if (m === Mark.X) return "X";
  if (m === Mark.O) return "O";
  return null;
}

function isOver(s: GameState): boolean {
  // One value, one comparison — what a single `Outcome` enum buys.
  return (s.outcome ?? Outcome.UNSPECIFIED) !== Outcome.IN_PROGRESS;
}

function status(s: GameState): string {
  switch (s.outcome) {
    case Outcome.WINNER_X:
      return "X wins\n";
    case Outcome.WINNER_O:
      return "O wins\n";
    case Outcome.DRAW:
      return "a draw\n";
    default:
      return `${symbol(s.turn) ?? "nobody"} to play\n`;
  }
}

/** Styling, shipped with the front end. */
export function gameStyles(): string {
  return `
.ttt-grid { display: grid; grid-template-columns: repeat(3, 3rem); gap: 2px; }
.ttt-cell { height: 3rem; font: 700 1.25rem ui-monospace, Menlo, monospace; cursor: pointer; }
.ttt-cell:disabled { cursor: default; }
.ttt-cell.won { outline: 2px solid currentColor; }
`.trim();
}
```

### 6c. The entry point

Replace `hosts/web/src/main.ts`:

```ts
import { enginePort } from "@devalbo/dlc-web/port";

import { gameStyles, mountGame } from "./view";
import type { GameView } from "./view";

document.head.appendChild(document.createElement("style")).textContent = gameStyles();

const game = mountGame(enginePort);

document.getElementById("new-game")!.addEventListener("click", () => {
  void game.newGame();
});

// COLD START — prime with a query; events do the rest. Delete this line and the
// board is blank until you click, which is the most common way a new front end
// is subtly wrong.
void game.refresh();

declare global {
  interface Window {
    game: GameView;
  }
}
window.game = game;
```

**`window.game` is not a debug back door** — it is the same functions the buttons call. A console API that
took a different path would be a second front end able to disagree with the first.

### 6d. Run it

```bash
make build-web        # TinyGo -> wasip2 component -> jco transpile
make dev-web          # serves the page; open the URL it prints
```

Click squares. Then **refresh the page** — the game is still there, because `game.json` is in OPFS. Then run
the browser tests the scaffold shipped:

```bash
cd hosts/web && npx playwright test
```

### What just happened

The browser ran **the same engine source** as your terminal. Not a port, not a reimplementation: the Go in
`engine/` compiled to a wasm component, with its filesystem bound to OPFS.

The two front ends share **no** presentation code — one prints ASCII, one builds DOM. Both read `outcome`,
`turn` and `winningLine`; neither computes them. And notice the one thing `play` does *not* do in either: it
never redraws. Both wait for `game.state-changed`, so a broken event path is visible rather than silent.

---

## 7. Prove the front ends agree

Two independent renderers will eventually disagree, and the disagreement will show up on one tier only, with
every other check green. So compare them directly: feed both the *same* synthetic states and check their
normalized output matches. The reference implementation does this in
`$ILC/example-apps/tictactoe/hosts/native/projection_test.go` and its `hosts/web/test/parity.spec.ts`.

**Then write the probe that gives this app its point.** Hand both front ends a state whose `board` has three
in a row while `outcome` is `IN_PROGRESS` — something the engine would never send. **Neither may announce a
winner.** A front end that does is *deciding* rather than *rendering*, and this is the only check that can see
it.

On the reference implementation that probe caught a real bug the first time it ran: both front ends
mis-indented rows relative to their separators, and disagreed about it.

---

## 8. The badge — what stands in the way

**You cannot follow this part yet**, and not because the design is unsettled. Three specific pieces of
framework are missing. Here they are in the order they must be fixed, so you can judge the distance.

Target hardware, either of:

- **[Badgeware Tufty](https://www.adafruit.com/product/6463)** — RP2350B, 2.8" 320×240 TFT, five buttons.
- **[KB2040](https://www.adafruit.com/product/5302)** — RP2040, **no screen**: it renders over USB serial into
  a terminal on your laptop, with input typed or from a 3×3 key matrix.

Those are **three tiers**, not two boards: `badge-native` (TinyGo linked directly), `badge-wamr` (the same
board running the engine as core wasm under WAMR), and `keeb-native`. A tier is a host binding, so one board
running two runtimes is two tiers.

**What is missing:**

1. **The capability seam has two of its three files.** `dlc-platform` has `caps_native.go`
   (`//go:build !tinygo`) and `caps_wasip2.go` (`//go:build tinygo`). Those tags conflate "TinyGo → wasm" with
   "TinyGo → microcontroller", so a native board build selects the WIT-import file and fails on an import that
   means nothing on a device. Needs a finer build tag and a third file.
2. **`platform.Boot` cannot say "there is no filesystem".** It refuses an empty root with *"grant one … or say
   so explicitly"* — and there is no way to say so; it then reports the filesystem present unconditionally. A
   board with no WASI has nothing to grant. (The browser host does not use `Boot` — it builds its manifest by
   hand — which is why the absent-filesystem path works there and this gap went unnoticed.)
3. **No embedded skeleton, and no wasip1 core build.** `dlc new` will scaffold any tier the template has a
   `hosts/<tier>/` directory for, and there is no embedded one — deliberately, because its shape depends on
   whether WAMR ports to RP2350, and scaffolding an unverifiable stub would teach the wrong thing to every
   project made afterwards. The WAMR tiers additionally need `engine.core.wasm`, which no build target
   produces.

**Nothing in `engine/` changes for any of this.** Your rules are done. What a badge needs is a *host*: a
display driver, a button reader, and a renderer that reads the same `outcome` / `turn` / `winning_line` your
other two front ends already read.

The full design — which tier to build first and why, and the falsification for each phase — is in
[`../example-plans/TIC-TAC-TOE-PLAN.md`](../example-plans/TIC-TAC-TOE-PLAN.md) §10.

---

## Troubleshooting

| Symptom | Cause |
| --- | --- |
| `tier "native,web" is not supported yet` | `--tiers` is repeatable, not comma-separated |
| `unexpected argument "--module"` | an old `dlc`; rebuild it per the [install tutorial](./INSTALL-DLC-STEPS.md) §6 |
| `protoc-gen-es-lite: executable file not found` | you are outside the project's `devbox shell` |
| `[tiers.x] root "hosts/x" does not exist` | a declared tier needs its slot directory. Create it or remove the entry |
| `unknown method_id 1` at run time | the engine package was never imported, so its `init` never ran — the host needs the blank import |
| `command "…" has no renderer registered` | you added an rpc but no `Render` entry. A new rpc *is* a new subcommand |
| the id lock fails the build | you changed a `method_id`. If deliberate: `DLC_ID_LOCK_UPDATE=1 make gen`, then review the diff |
| blank board after a browser refresh | the page renders only from events. Prime with `state` on load (§6) |
| template edits have no effect | `dlc` embeds its templates — rebuild it ([install](./INSTALL-DLC-STEPS.md) §6) |

---

## What to read next

- [`AGENTS.md`](../../AGENTS.md) — the rules this tutorial was following: §1 method ids, §3a the host layer,
  §5 verification.
- [`DEVALBO-ILC-GO-PLAN.md`](../DEVALBO-ILC-GO-PLAN.md) §6.4 and Decisions 34–35 — the three ways an app can
  put something on a screen, and why this one used the cheapest.
- [`$ILC/example-apps/notes/`](../../example-apps/notes/) — the same framework with a *collection* of records
  instead of one document.
