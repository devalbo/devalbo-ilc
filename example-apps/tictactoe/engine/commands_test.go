// The engine holds every rule, so this is where the rules are tested.
//
// Deliberately tier-agnostic: both slots render what these commands return, so
// a behaviour asserted here is asserted for the terminal and the browser at
// once. If one of them needed a tweak, logic would have leaked out of engine/.
package engine_test

import (
	"os"
	"testing"

	"github.com/devalbo/devalbo-ilc/dlc-platform"

	_ "github.com/devalbo/devalbo-ilc/example-apps/tictactoe/engine"
	tictactoev1 "github.com/devalbo/devalbo-ilc/example-apps/tictactoe/gen/go/tictactoe/v1"
)

func inTempRoot(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	// GRANT the root, as a host does. There is no implicit "wherever you are
	// standing" any more: `FSRoot()` panics without a grant, because falling back
	// to the cwd is what let `reset-fs` clear a user's directory.
	if err := platform.SetFSRoot("."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func call(t *testing.T, method uint32, req interface{ MarshalVT() ([]byte, error) }, resp interface{ UnmarshalVT([]byte) error }) error {
	t.Helper()
	body, err := req.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	r := platform.Execute(method, body)
	if !r.Success {
		return errString(r.Err)
	}
	if resp != nil {
		if err := resp.UnmarshalVT(r.Output); err != nil {
			t.Fatal(err)
		}
	}
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

func play(t *testing.T, square uint32) *tictactoev1.GameState {
	t.Helper()
	var resp tictactoev1.PlayResponse
	if err := call(t, tictactoev1.MethodPlay, &tictactoev1.PlayRequest{Square: square}, &resp); err != nil {
		t.Fatalf("play %d: %v", square, err)
	}
	return resp.State
}

func TestFirstStateIsAnEmptyBoard(t *testing.T) {
	inTempRoot(t)
	var resp tictactoev1.GetStateResponse
	if err := call(t, tictactoev1.MethodGetState, &tictactoev1.GetStateRequest{}, &resp); err != nil {
		t.Fatal(err)
	}
	// A missing file is a NEW GAME, not an error: a first run must not require
	// a setup step.
	if len(resp.State.GetBoard()) != 9 {
		t.Fatalf("board has %d squares", len(resp.State.GetBoard()))
	}
	if resp.State.GetTurn() != tictactoev1.Mark_MARK_X {
		t.Errorf("X moves first, got %v", resp.State.GetTurn())
	}
	// A fresh game is IN_PROGRESS, not UNSPECIFIED: a slot switching on the
	// outcome must not have to treat "no value yet" as a fourth case.
	if resp.State.GetOutcome() != tictactoev1.Outcome_OUTCOME_IN_PROGRESS {
		t.Errorf("outcome = %v, want IN_PROGRESS", resp.State.GetOutcome())
	}
}

func TestTurnsAlternate(t *testing.T) {
	inTempRoot(t)
	if got := play(t, 5).GetTurn(); got != tictactoev1.Mark_MARK_O {
		t.Errorf("after X, turn = %v", got)
	}
	if got := play(t, 1).GetTurn(); got != tictactoev1.Mark_MARK_X {
		t.Errorf("after O, turn = %v", got)
	}
}

// The engine names the winning LINE, not just the winner. That is what lets a
// host highlight it without working out which line it was — the difference
// between drawing what the engine said and deciding for itself.
func TestWinnerAndWinningLine(t *testing.T) {
	inTempRoot(t)
	play(t, 1)      // X
	play(t, 4)      // O
	play(t, 2)      // X
	play(t, 5)      // O
	s := play(t, 3) // X wins on the top row

	if s.GetOutcome() != tictactoev1.Outcome_OUTCOME_WINNER_X {
		t.Fatalf("outcome = %v, want WINNER_X", s.GetOutcome())
	}
	if got := s.GetWinningLine(); len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Errorf("winning line = %v, want [0 1 2]", got)
	}
	// Nobody's turn once it is over — a host should not have to infer that from
	// `winner != none`.
	if s.GetTurn() != tictactoev1.Mark_MARK_UNSPECIFIED {
		t.Errorf("turn after a win = %v, want unspecified", s.GetTurn())
	}
}

func TestDraw(t *testing.T) {
	inTempRoot(t)
	// X O X / X O O / O X X — full, no line.
	for _, sq := range []uint32{1, 2, 3, 5, 4, 6, 8, 7, 9} {
		play(t, sq)
	}
	var resp tictactoev1.GetStateResponse
	if err := call(t, tictactoev1.MethodGetState, &tictactoev1.GetStateRequest{}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.State.GetOutcome() != tictactoev1.Outcome_OUTCOME_DRAW {
		t.Errorf("outcome = %v, want DRAW", resp.State.GetOutcome())
	}
}

// Every refusal is the ENGINE's. A host that greyed out taken squares by itself
// would be a second rulebook, free to disagree on one tier.
func TestRefusals(t *testing.T) {
	inTempRoot(t)
	play(t, 5)

	if err := call(t, tictactoev1.MethodPlay, &tictactoev1.PlayRequest{Square: 5}, nil); err == nil {
		t.Error("playing a taken square must be refused")
	}
	if err := call(t, tictactoev1.MethodPlay, &tictactoev1.PlayRequest{Square: 0}, nil); err == nil {
		t.Error("square 0 must be refused — squares are 1-9")
	}
	if err := call(t, tictactoev1.MethodPlay, &tictactoev1.PlayRequest{Square: 10}, nil); err == nil {
		t.Error("square 10 must be refused")
	}
}

func TestPlayAfterTheGameIsOverIsRefused(t *testing.T) {
	inTempRoot(t)
	for _, sq := range []uint32{1, 4, 2, 5, 3} { // X takes the top row
		play(t, sq)
	}
	if err := call(t, tictactoev1.MethodPlay, &tictactoev1.PlayRequest{Square: 9}, nil); err == nil {
		t.Error("a finished game must refuse further moves")
	}
}

// The state survives a restart because it is a FILE (§7.1), not memory.
func TestStatePersists(t *testing.T) {
	inTempRoot(t)
	play(t, 5)

	var resp tictactoev1.GetStateResponse
	if err := call(t, tictactoev1.MethodGetState, &tictactoev1.GetStateRequest{}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.State.GetBoard()[4] != tictactoev1.Mark_MARK_X {
		t.Error("the move was not persisted")
	}
	if _, err := os.Stat("game.json"); err != nil {
		t.Error("the store should be a readable file, not app-private memory")
	}
}

// The history IS game time. `moves` and `last_move` were fields once; they are
// the length of this list and its last element, and a list length is not a rule
// two implementations could disagree about.
func TestNewGameClearsTheBoard(t *testing.T) {
	inTempRoot(t)
	play(t, 5)
	var resp tictactoev1.NewGameResponse
	if err := call(t, tictactoev1.MethodNewGame, &tictactoev1.NewGameRequest{}, &resp); err != nil {
		t.Fatal(err)
	}
	for i, m := range resp.State.GetBoard() {
		if m != tictactoev1.Mark_MARK_UNSPECIFIED {
			t.Errorf("square %d not cleared: %v", i+1, m)
		}
	}
	// History resets too: a fresh game that kept the old moves would make every
	// renderer mark a square nobody just played.
	if got := len(resp.State.GetHistory()); got != 0 {
		t.Errorf("history has %d move(s) after a new game", got)
	}
}

// Mutations announce themselves; the event carries the WHOLE state, which is
// what makes the semantic render path possible.
// The topic is a LITERAL ON PURPOSE, and must stay one.
//
// Everywhere else the string is generated from the `(topic)` option, which is
// the point — the emit side and every subscriber read one declaration. A test
// that also read that declaration would compare a generated value to itself and
// assert nothing. This is the independent pin, the same role the parse vectors
// play for request bytes.
func TestMutationsEmitState(t *testing.T) {
	inTempRoot(t)

	var got []*tictactoev1.GameState
	platform.SetEventSink(func(topic string, payload []byte) {
		if topic != "game.state-changed" {
			return
		}
		var e tictactoev1.StateChangedEvent
		if err := e.UnmarshalVT(payload); err != nil {
			t.Error(err)
			return
		}
		got = append(got, e.State)
	})
	t.Cleanup(func() { platform.SetEventSink(nil) })

	play(t, 5)
	play(t, 1)

	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if got[1].GetBoard()[0] != tictactoev1.Mark_MARK_O {
		t.Error("the event should carry the state AFTER the move")
	}

	// A read emits nothing — an event per query would loop any subscriber that
	// re-reads on an event.
	before := len(got)
	var st tictactoev1.GetStateResponse
	if err := call(t, tictactoev1.MethodGetState, &tictactoev1.GetStateRequest{}, &st); err != nil {
		t.Fatal(err)
	}
	if len(got) != before {
		t.Error("reading the state must not emit")
	}
}

// A refused move must not announce anything: nothing changed.
func TestRefusedMoveEmitsNothing(t *testing.T) {
	inTempRoot(t)
	play(t, 5)

	var count int
	platform.SetEventSink(func(topic string, _ []byte) {
		if topic == platform.ActivityTopic {
			return
		}
		count++
	})
	t.Cleanup(func() { platform.SetEventSink(nil) })

	_ = call(t, tictactoev1.MethodPlay, &tictactoev1.PlayRequest{Square: 5}, nil)
	if count != 0 {
		t.Errorf("a refused move emitted %d event(s)", count)
	}
}

// HISTORY — the game as a sequence, so a client can show it rather than just
// its result.
func TestHistoryRecordsEveryMove(t *testing.T) {
	inTempRoot(t)
	play(t, 5)
	play(t, 1)
	s := play(t, 9)

	h := s.GetHistory()
	if len(h) != 3 {
		t.Fatalf("history has %d moves, want 3", len(h))
	}
	// The MARK is recorded, not left to be inferred from position. "X moves
	// first, so odd moves are X" is a rule, and a rule inferred by a client is
	// a second implementation of it.
	if h[0].GetSquare() != 5 || h[0].GetMark() != tictactoev1.Mark_MARK_X {
		t.Errorf("move 1 = %v", h[0])
	}
	if h[1].GetSquare() != 1 || h[1].GetMark() != tictactoev1.Mark_MARK_O {
		t.Errorf("move 2 = %v", h[1])
	}
}

// REVIEWING A PRIOR STATE IS A QUERY. The engine reconstructs; no client
// replays the history, because replaying is applying the rules and the rules
// are the engine's.
func TestStateAtAnEarlierMove(t *testing.T) {
	inTempRoot(t)
	for _, sq := range []uint32{5, 1, 9} {
		play(t, sq)
	}

	var resp tictactoev1.GetStateResponse
	if err := call(t, tictactoev1.MethodGetState, &tictactoev1.GetStateRequest{At: 1}, &resp); err != nil {
		t.Fatal(err)
	}
	s := resp.State
	if s.GetBoard()[4] != tictactoev1.Mark_MARK_X {
		t.Error("square 5 should hold X as of move 1")
	}
	if s.GetBoard()[0] != tictactoev1.Mark_MARK_UNSPECIFIED {
		t.Error("square 1 was played on move 2 and must be empty as of move 1")
	}
	if got := len(s.GetHistory()); got != 1 {
		t.Errorf("history not rewound: length %d, want 1", got)
	}
	// Whose turn it was then, not now — the reconstruction runs the same
	// `decide` the live game does, so every derived field is consistent.
	if s.GetTurn() != tictactoev1.Mark_MARK_O {
		t.Errorf("turn as of move 1 = %v, want O", s.GetTurn())
	}
}

// A reconstruction of a winning position must also report the win — otherwise
// "review a prior state" would show a board without its meaning.
func TestStateAtTheWinningMove(t *testing.T) {
	inTempRoot(t)
	for _, sq := range []uint32{1, 4, 2, 5, 3} {
		play(t, sq)
	}
	var resp tictactoev1.GetStateResponse
	if err := call(t, tictactoev1.MethodGetState, &tictactoev1.GetStateRequest{At: 5}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.State.GetOutcome() != tictactoev1.Outcome_OUTCOME_WINNER_X {
		t.Errorf("outcome at move 5 = %v, want WINNER_X", resp.State.GetOutcome())
	}
	if len(resp.State.GetWinningLine()) != 3 {
		t.Error("the winning line should be named in a reconstruction too")
	}
}

func TestStateAtZeroIsNow(t *testing.T) {
	inTempRoot(t)
	play(t, 5)
	var resp tictactoev1.GetStateResponse
	if err := call(t, tictactoev1.MethodGetState, &tictactoev1.GetStateRequest{At: 0}, &resp); err != nil {
		t.Fatal(err)
	}
	if got := len(resp.State.GetHistory()); got != 1 {
		t.Errorf("at=0 should mean the current board, got %d move(s)", got)
	}
}

func TestStateBeyondTheGameIsRefused(t *testing.T) {
	inTempRoot(t)
	play(t, 5)
	if err := call(t, tictactoev1.MethodGetState, &tictactoev1.GetStateRequest{At: 99}, nil); err == nil {
		t.Error("asking for a move that never happened must be refused")
	}
}

func TestNewGameClearsHistory(t *testing.T) {
	inTempRoot(t)
	play(t, 5)
	var resp tictactoev1.NewGameResponse
	if err := call(t, tictactoev1.MethodNewGame, &tictactoev1.NewGameRequest{}, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.State.GetHistory()) != 0 {
		t.Errorf("history survived a new game: %v", resp.State.GetHistory())
	}
}
