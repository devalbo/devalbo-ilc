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

	"github.com/devalbo/devalbo-ilc/dlc-platform"

	"github.com/devalbo/devalbo-ilc/example-apps/tictactoe/gen/go/dlcconfig"
	tictactoev1 "github.com/devalbo/devalbo-ilc/example-apps/tictactoe/gen/go/tictactoe/v1"
)

// gameFile is the whole store. One small file, canonical JSON — §7.1: the file
// is the truth, and a human can read the game without the app.
const gameFile = "game.json"

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

	// WHAT THE COMMANDS TAKE, so a host that cannot compile this app's
	// schema can still collect input for it — a badge running payloads it
	// was never built for, or a browser that would otherwise hand-write an
	// <input> per field. Without this the description is stripped from the
	// wasm as dead code, because only the native CLI referenced it.
	platform.RegisterCommandSpec(tictactoev1.GameServiceCLI)
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
	// THE SETUP SURVIVES THE REPLAY. `fresh` starts a default game — two humans,
	// player one is X — so reconstructing an earlier board without carrying these
	// would report a computer game as a human one, and the mark player one took
	// as X whatever they chose.
	replay.Opponent = s.Opponent
	replay.PlayerOne = s.PlayerOne
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
		return nil, errors.New("play: the game is over - start a new one")
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

	// AND THE COMPUTER ANSWERS, in the same command. A person plays and gets a
	// board back that it is their turn on again — there is no moment between
	// where the game is waiting on the engine, because an app cannot ask for a
	// turn it was not given (D2). See `autoplay`.
	autoplay(state)

	if err := save(state); err != nil {
		return nil, err
	}
	// AFTER the write, once per command (AGENTS.md §3).
	emitStateChanged(state)
	return &tictactoev1.PlayResponse{State: state}, nil
}

func handleNewGame(req *tictactoev1.NewGameRequest) (*tictactoev1.NewGameResponse, error) {
	state := fresh()

	// The zero values ARE the defaults, so an empty request is the old
	// behaviour: two humans, player one is X. Nothing has to special-case
	// "unset" because unset already means what we want.
	state.Opponent = req.GetOpponent()
	if state.Opponent == tictactoev1.Opponent_OPPONENT_UNSPECIFIED {
		state.Opponent = tictactoev1.Opponent_OPPONENT_HUMAN
	}
	state.PlayerOne = req.GetPlayerOne()
	if state.PlayerOne == tictactoev1.Mark_MARK_UNSPECIFIED {
		state.PlayerOne = tictactoev1.Mark_MARK_X
	}

	// THE OPPONENT MAY OPEN. Player one chose O, X still moves first, so the
	// computer's move belongs to this command — see `autoplay` for why it cannot
	// wait for the next one.
	autoplay(state)

	if err := save(state); err != nil {
		return nil, err
	}
	emitStateChanged(state)
	return &tictactoev1.NewGameResponse{State: state}, nil
}

// opponentMark is the mark the computer plays, or MARK_UNSPECIFIED in a game
// between two people.
func opponentMark(s *tictactoev1.GameState) tictactoev1.Mark {
	if s.Opponent != tictactoev1.Opponent_OPPONENT_COMPUTER {
		return tictactoev1.Mark_MARK_UNSPECIFIED
	}
	if s.PlayerOne == tictactoev1.Mark_MARK_O {
		return tictactoev1.Mark_MARK_X
	}
	return tictactoev1.Mark_MARK_O
}

// autoplay lets the computer take its turn, if it is the computer's turn.
//
// IT HAPPENS INSIDE THE COMMAND THAT CAUSED IT, and that is D2 rather than
// convenience: an app is request/response and cannot ask for another turn, so
// there is no later moment for the engine to move in. A state saved with the
// computer to play would sit there until a client happened to send something,
// and "the board is waiting on the computer" would be a state every host had to
// recognise and poll out of — a rule leaking into the slots.
//
// So a command returns a board it is the PERSON's turn to play, always.
func autoplay(s *tictactoev1.GameState) {
	mark := opponentMark(s)
	if mark == tictactoev1.Mark_MARK_UNSPECIFIED {
		return
	}
	// A loop, though it can only ever run once: the computer moves, and then it
	// is the person's turn or the game is over. Written as a condition rather
	// than a single call so it stays correct if a variant ever gives someone two
	// moves in a row.
	for s.Outcome == tictactoev1.Outcome_OUTCOME_IN_PROGRESS && s.Turn == mark {
		square := computerMove(s, mark)
		s.History = append(s.History, &tictactoev1.Move{Square: uint32(square + 1), Mark: mark})
		s.Board[square] = mark
		decide(s)
	}
}

// computerMove picks the square the engine will play. **Perfect, and the same
// every time.**
//
// # Determinism is a PARITY REQUIREMENT, not a preference
//
// The same engine bytes run natively, in a browser, and on the badge, and the
// parity harness compares command results across all three. An opponent that
// consulted `wasi:random` would answer differently on each — not a bug in any
// one of them, and impossible to tell apart from one. So there is no randomness
// here at all, and ties break toward the LOWEST square index: an arbitrary rule,
// but one every tier applies identically.
//
// The cost is that it plays the same game against the same moves forever. That
// is the correct trade for an app whose job is to demonstrate that three tiers
// agree; a variety-seeking opponent would need a seed supplied BY the app's own
// state, which is a different feature.
//
// # Why minimax rather than a rule list
//
// Perfect play on 3×3 is a short search, and the rules that encode it by hand
// (win, block, fork, block-fork, centre, opposite corner…) are famously easy to
// get subtly wrong — the fork cases especially. A search has no heuristics to
// mis-state: it is right by construction or it is broken loudly.
//
// # Why it is fast enough on a badge
//
// Alpha-beta cuts the empty-board search from ~550k nodes to a few tens of
// thousands, which the Pulley interpreter handles in well under a second. The
// worst case only arises when the computer plays X and opens; every later move
// is far smaller.
func computerMove(s *tictactoev1.GameState, mark tictactoev1.Mark) int {
	var board [9]tictactoev1.Mark
	copy(board[:], s.Board)

	best, bestScore := -1, -1000
	for i := 0; i < 9; i++ {
		if board[i] != tictactoev1.Mark_MARK_UNSPECIFIED {
			continue
		}
		board[i] = mark
		// NEGATED, because the score comes back from the OPPONENT's point of
		// view: what is good for them is bad for us, and one sign flip is the
		// whole difference between minimax and a bot that plays to lose.
		score := -search(&board, other(mark), -1000, 1000, 1)
		board[i] = tictactoev1.Mark_MARK_UNSPECIFIED
		// STRICTLY GREATER, which is what makes the lowest index win a tie.
		if score > bestScore {
			bestScore, best = score, i
		}
	}
	return best
}

// search scores the position for whoever is to move, with alpha-beta pruning.
//
// `depth` is how many plies deep we are, and it is in the score on purpose: a
// win in two moves beats a win in four, and a loss in four beats a loss in two.
// Without it the engine sees every win as equal and can dawdle while a human
// escapes — perfect play that looks like a mistake.
func search(board *[9]tictactoev1.Mark, turn tictactoev1.Mark, alpha, beta, depth int) int {
	if winner := winnerOn(board); winner != tictactoev1.Mark_MARK_UNSPECIFIED {
		// The player to move has already lost: the win belongs to whoever moved
		// last.
		return depth - 10
	}
	moved := false
	for i := 0; i < 9; i++ {
		if board[i] != tictactoev1.Mark_MARK_UNSPECIFIED {
			continue
		}
		moved = true
		board[i] = turn
		score := -search(board, other(turn), -beta, -alpha, depth+1)
		board[i] = tictactoev1.Mark_MARK_UNSPECIFIED
		if score > alpha {
			alpha = score
		}
		if alpha >= beta {
			// NOTHING BELOW THIS CAN MATTER: the opponent already has a better
			// option higher up and will never let the game reach here.
			break
		}
	}
	if !moved {
		// A full board with no winner.
		return 0
	}
	return alpha
}

// winnerOn reports the mark holding a line, or MARK_UNSPECIFIED.
//
// SEPARATE FROM `decide`, which works on a whole `GameState` and also sets the
// turn, the outcome and the winning line. The search runs this thousands of
// times on a bare array; going through `decide` would allocate a state per node.
func winnerOn(board *[9]tictactoev1.Mark) tictactoev1.Mark {
	for _, line := range lines {
		a := board[line[0]]
		if a != tictactoev1.Mark_MARK_UNSPECIFIED && a == board[line[1]] && a == board[line[2]] {
			return a
		}
	}
	return tictactoev1.Mark_MARK_UNSPECIFIED
}

// other is the mark that is not this one. MARK_UNSPECIFIED has no opposite and
// returns itself, which cannot arise in a search that only ever recurses on a
// real player.
func other(mark tictactoev1.Mark) tictactoev1.Mark {
	switch mark {
	case tictactoev1.Mark_MARK_X:
		return tictactoev1.Mark_MARK_O
	case tictactoev1.Mark_MARK_O:
		return tictactoev1.Mark_MARK_X
	}
	return mark
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
	return platform.WriteTree(platform.FSRoot(), []platform.File{{Path: gameFile, Content: body}})
}

func emitStateChanged(s *tictactoev1.GameState) {
	platform.EmitEvent(&tictactoev1.StateChangedEvent{State: s})
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
