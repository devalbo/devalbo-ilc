// tic-tac-toe's business logic — ALL of it.
//
// Every decision in this app is here: whether a square is free, whose turn it
// is, whether someone has won and on which line. That is not tidiness, it is
// the rule the app exists to demonstrate (Decision 34): a slot renders, it never
// decides. Both hosts draw a board; neither works one out.
//
// Why that rule needs an app to make it testable: parity compares command
// results, the written filesystem, and the event stream — all engine-side — so
// a tier slot is invisible to it by construction. Two hosts that each computed
// the winner would eventually disagree on one tier only, with every existing
// check green.
//
// Reflection-free and TinyGo-safe, like any ILC engine.
package engine

import (
	"errors"
	"strings"

	"github.com/devalbo/devalbo-ilc/engine/platform"

	"github.com/devalbo/devalbo-ilc/example-apps/tictactoe/gen/go/dlcconfig"
	tictactoev1 "github.com/devalbo/devalbo-ilc/example-apps/tictactoe/gen/go/tictactoe/v1"
)

// gameFile is the whole store. One small file, canonical JSON — §7.1: the file
// is the truth, and a human can read the game without the app.
const gameFile = "game.json"

// TopicStateChanged is this app's own topic. The app names it; nothing
// registers it (Decision 33 D3).
const TopicStateChanged = "game.state-changed"

func init() {
	// The INHERITED verbs — version, export-fs, import-fs, reset-fs. Explicit
	// rather than an import side effect, so an app can see what it is getting.
	platform.RegisterAll()
	platform.SetVersion(dlcconfig.Display())
	platform.RegisterRaw(tictactoev1.GameServiceHandlers(
		handleGetState,
		handlePlay,
		handleNewGame,
	))
}

// handleGetState is the COLD-START path (Decision 34 D5).
//
// Events are ephemeral — no log, no replay — so a host that rendered only from
// the stream would show an empty board on reload, or in a second tab opened
// mid-game. Priming with a query and taking events as deltas is the pattern,
// and it is an ordinary command precisely so every tier gets it the same way.
func handleGetState(req *tictactoev1.GetStateRequest) (*tictactoev1.GetStateResponse, error) {
	state, err := load()
	if err != nil {
		return nil, err
	}
	if req.At > 0 {
		// TIME TRAVEL, engine-side. A client asks what the board looked like;
		// it does not replay the history itself. The rules live here, so the
		// reconstruction does too — and every tier gets this through the
		// generated command surface without writing any code for it.
		state, err = at(state, req.At)
		if err != nil {
			return nil, err
		}
	}
	return &tictactoev1.GetStateResponse{State: state}, nil
}

// at rebuilds the game as of move n, from the recorded history.
func at(s *tictactoev1.GameState, n uint32) (*tictactoev1.GameState, error) {
	if int(n) > len(s.History) {
		return nil, errors.New("state: the game is only " + itoa(len(s.History)) + " move(s) long")
	}
	replay := fresh()
	for _, m := range s.History[:n] {
		replay.Board[m.GetSquare()-1] = m.GetMark()
		replay.History = append(replay.History, m)
		decide(replay)
	}
	return replay, nil
}

func handlePlay(req *tictactoev1.PlayRequest) (*tictactoev1.PlayResponse, error) {
	state, err := load()
	if err != nil {
		return nil, err
	}

	// Every one of these refusals is a DECISION, and every one of them is here
	// rather than in a host. A browser that greyed out taken squares by itself
	// would be a second rulebook.
	if state.Outcome != tictactoev1.Outcome_OUTCOME_IN_PROGRESS {
		// One field to check, and no combination to interpret — the same
		// simplification every slot gets.
		return nil, errors.New("play: the game is over — start a new one")
	}
	if req.Square < 1 || req.Square > 9 {
		return nil, errors.New("play: square must be 1-9")
	}
	i := int(req.Square) - 1
	if state.Board[i] != tictactoev1.Mark_MARK_UNSPECIFIED {
		return nil, errors.New("play: that square is taken")
	}

	state.History = append(state.History, &tictactoev1.Move{Square: req.Square, Mark: state.Turn})
	state.Board[i] = state.Turn
	decide(state)

	if err := save(state); err != nil {
		return nil, err
	}
	// AFTER the write, once per command (AGENTS.md §3).
	emitStateChanged(state)
	return &tictactoev1.PlayResponse{State: state}, nil
}

func handleNewGame(*tictactoev1.NewGameRequest) (*tictactoev1.NewGameResponse, error) {
	state := fresh()
	if err := save(state); err != nil {
		return nil, err
	}
	emitStateChanged(state)
	return &tictactoev1.NewGameResponse{State: state}, nil
}

// lines are the eight ways to win, as board indexes.
var lines = [8][3]int{
	{0, 1, 2}, {3, 4, 5}, {6, 7, 8}, // rows
	{0, 3, 6}, {1, 4, 7}, {2, 5, 8}, // columns
	{0, 4, 8}, {2, 4, 6}, // diagonals
}

// decide works out what the board now MEANS: winner, winning line, draw, and
// whose turn it is next.
//
// This function is the point of the whole app. If a host ever needs to know any
// of it, the host asks — it does not recompute.
func decide(s *tictactoev1.GameState) {
	for _, line := range lines {
		a, b, c := s.Board[line[0]], s.Board[line[1]], s.Board[line[2]]
		if a != tictactoev1.Mark_MARK_UNSPECIFIED && a == b && b == c {
			if a == tictactoev1.Mark_MARK_X {
				s.Outcome = tictactoev1.Outcome_OUTCOME_WINNER_X
			} else {
				s.Outcome = tictactoev1.Outcome_OUTCOME_WINNER_O
			}
			s.WinningLine = []uint32{uint32(line[0]), uint32(line[1]), uint32(line[2])}
			s.Turn = tictactoev1.Mark_MARK_UNSPECIFIED // nobody's turn once it is won
			return
		}
	}

	full := true
	for _, m := range s.Board {
		if m == tictactoev1.Mark_MARK_UNSPECIFIED {
			full = false
			break
		}
	}
	if full {
		s.Outcome = tictactoev1.Outcome_OUTCOME_DRAW
		s.Turn = tictactoev1.Mark_MARK_UNSPECIFIED
		return
	}

	if s.Turn == tictactoev1.Mark_MARK_X {
		s.Turn = tictactoev1.Mark_MARK_O
	} else {
		s.Turn = tictactoev1.Mark_MARK_X
	}
}

func fresh() *tictactoev1.GameState {
	// Nine empty squares — "empty" is the zero value, so make() is the whole job.
	board := make([]tictactoev1.Mark, 9)
	return &tictactoev1.GameState{
		Board:   board,
		Turn:    tictactoev1.Mark_MARK_X,
		Outcome: tictactoev1.Outcome_OUTCOME_IN_PROGRESS,
	}
}

// load reads the game, or starts one. A missing file is a new game, not an
// error: an app's first run must not require a setup step.
func load() (*tictactoev1.GameState, error) {
	data, err := platform.ReadFile(gameFile)
	if err != nil {
		return fresh(), nil
	}
	var state tictactoev1.GameState
	if err := state.UnmarshalJSON(data); err != nil {
		return nil, errors.New("game.json is not readable as a game: " + err.Error())
	}
	// A truncated or hand-edited board would index out of range everywhere
	// downstream; refuse it here where the message can name the file.
	if len(state.Board) != 9 {
		return nil, errors.New("game.json has " + itoa(len(state.Board)) + " squares, want 9")
	}
	return &state, nil
}

func save(s *tictactoev1.GameState) error {
	// Canonical JSON, not binary: the file is meant to be read and diffed
	// (§7.2). go-lite emits it without reflection.
	body, err := s.MarshalJSON()
	if err != nil {
		return err
	}
	return platform.WriteTree(platform.Root(), []platform.File{{Path: gameFile, Content: body}})
}

func emitStateChanged(s *tictactoev1.GameState) {
	payload, err := (&tictactoev1.StateChangedEvent{State: s}).MarshalVT()
	if err != nil {
		return // an event that cannot be encoded is not worth failing a move over
	}
	platform.Emit(TopicStateChanged, payload)
}

// itoa avoids strconv, which is fine under TinyGo but not worth an import for
// one error message.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b strings.Builder
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	b.WriteString(digits)
	return b.String()
}
