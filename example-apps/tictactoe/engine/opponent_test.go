// The computer opponent, tested from INSIDE the package.
//
// `commands_test.go` is `package engine_test` — the external form, which is the
// right default: it exercises an app the way a host does and cannot accidentally
// depend on an internal. The search is the exception. `computerMove` and
// `search` are the app's own reasoning, not its surface, and testing perfect
// play through `handlePlay` would mean a filesystem, a saved game and one
// command per ply to assert something that is a pure function of a board.
package engine

import (
	"testing"

	tictactoev1 "github.com/devalbo/devalbo-ilc/example-apps/tictactoe/gen/go/tictactoe/v1"
)

// board builds a state from a nine-character picture: `.` empty, `X`, `O`.
func board(picture string, turn tictactoev1.Mark) *tictactoev1.GameState {
	s := fresh()
	s.Turn = turn
	for i, c := range picture {
		switch c {
		case 'X':
			s.Board[i] = tictactoev1.Mark_MARK_X
		case 'O':
			s.Board[i] = tictactoev1.Mark_MARK_O
		}
	}
	return s
}

// THE SAME ANSWER EVERY TIME, which is a PARITY requirement rather than a
// preference: the same engine bytes run natively, in a browser and on a badge,
// and the harness compares command results across all three. An opponent that
// consulted a clock or a PRNG would answer differently on each — and be
// impossible to tell apart from a real disagreement.
func TestComputerIsDeterministic(t *testing.T) {
	first := computerMove(board(".........", tictactoev1.Mark_MARK_X), tictactoev1.Mark_MARK_X)
	for i := 0; i < 20; i++ {
		if got := computerMove(board(".........", tictactoev1.Mark_MARK_X), tictactoev1.Mark_MARK_X); got != first {
			t.Fatalf("run %d picked %d, first run picked %d", i, got, first)
		}
	}
}

func TestComputerTakesTheWin(t *testing.T) {
	// X on 0 and 1; 2 completes the row. Anything else is a blunder.
	got := computerMove(board("XX.OO....", tictactoev1.Mark_MARK_X), tictactoev1.Mark_MARK_X)
	if got != 2 {
		t.Fatalf("played %d, want 2 — the winning square", got)
	}
}

// WINNING BEATS BLOCKING when both are available. A rule list that checked
// "block" before "win" would draw a game it had already won, which is the
// classic hand-written-heuristic bug a search cannot have.
func TestComputerPrefersWinningToBlocking(t *testing.T) {
	//   O O .        O wins at 2
	//   . . .
	//   X X .        X wins at 8, and it is X to move
	//
	// THE FIXTURE THIS REPLACED WAS WRONG, and the engine was right: it had X on
	// 0 and 1 with O sitting on 2, so the "win" being tested for did not exist
	// and blocking was correct play. A test that asserts a bad move is worse than
	// no test, because it invites someone to "fix" working code.
	got := computerMove(board("OO....XX.", tictactoev1.Mark_MARK_X), tictactoev1.Mark_MARK_X)
	if got != 8 {
		t.Fatalf("played %d, want 8 — winning beats blocking at 2", got)
	}
}

func TestComputerBlocksAThreat(t *testing.T) {
	// O holds 0 and 1 and will win at 2 unless X takes it.
	got := computerMove(board("OO.X.....", tictactoev1.Mark_MARK_X), tictactoev1.Mark_MARK_X)
	if got != 2 {
		t.Fatalf("played %d, want 2 — the block", got)
	}
}

// PERFECT PLAY NEVER LOSES. Played against itself from an empty board, a
// tic-tac-toe engine that searches correctly always draws; anything else means
// the sign, the depth term or the pruning is wrong.
func TestComputerNeverLosesToItself(t *testing.T) {
	s := fresh()
	for s.Outcome == tictactoev1.Outcome_OUTCOME_IN_PROGRESS {
		mark := s.Turn
		square := computerMove(s, mark)
		if square < 0 {
			t.Fatal("no move offered on a board that is still in progress")
		}
		s.Board[square] = mark
		s.History = append(s.History, &tictactoev1.Move{Square: uint32(square + 1), Mark: mark})
		decide(s)
	}
	if s.Outcome != tictactoev1.Outcome_OUTCOME_DRAW {
		t.Fatalf("perfect play produced %v, want a draw", s.Outcome)
	}
}

// A human cannot beat it either — checked from every opening reply, which is
// where a shallow or mis-signed search shows up.
func TestComputerCannotBeBeatenFromAnyOpening(t *testing.T) {
	for opening := 0; opening < 9; opening++ {
		s := fresh()
		// The human is X and opens wherever; the computer is O.
		s.Opponent = tictactoev1.Opponent_OPPONENT_COMPUTER
		s.PlayerOne = tictactoev1.Mark_MARK_X
		s.Board[opening] = tictactoev1.Mark_MARK_X
		s.History = append(s.History, &tictactoev1.Move{Square: uint32(opening + 1), Mark: tictactoev1.Mark_MARK_X})
		decide(s)

		// From here the human plays the lowest free square — a poor strategy, so
		// the computer should win or draw, never lose.
		for s.Outcome == tictactoev1.Outcome_OUTCOME_IN_PROGRESS {
			var square int
			if s.Turn == tictactoev1.Mark_MARK_O {
				square = computerMove(s, tictactoev1.Mark_MARK_O)
			} else {
				for i := 0; i < 9; i++ {
					if s.Board[i] == tictactoev1.Mark_MARK_UNSPECIFIED {
						square = i
						break
					}
				}
			}
			s.Board[square] = s.Turn
			s.History = append(s.History, &tictactoev1.Move{Square: uint32(square + 1), Mark: s.Turn})
			decide(s)
		}
		if s.Outcome == tictactoev1.Outcome_OUTCOME_WINNER_X {
			t.Fatalf("opening %d: the computer lost", opening)
		}
	}
}

// X MOVES FIRST IS A RULE, so choosing O means the computer opens — and the
// board comes back with a move already on it.
func TestChoosingOMakesTheComputerOpen(t *testing.T) {
	s := fresh()
	s.Opponent = tictactoev1.Opponent_OPPONENT_COMPUTER
	s.PlayerOne = tictactoev1.Mark_MARK_O
	autoplay(s)
	if len(s.History) != 1 {
		t.Fatalf("history has %d move(s), want 1 — the computer's opening", len(s.History))
	}
	if s.History[0].GetMark() != tictactoev1.Mark_MARK_X {
		t.Fatalf("the opening move was %v, want X", s.History[0].GetMark())
	}
	if s.Turn != tictactoev1.Mark_MARK_O {
		t.Fatalf("turn is %v, want O — the person's", s.Turn)
	}
}

// A GAME BETWEEN TWO PEOPLE IS UNTOUCHED. The opponent code must be invisible
// when nobody asked for it.
func TestTwoHumansGetNoHelp(t *testing.T) {
	s := fresh()
	s.Opponent = tictactoev1.Opponent_OPPONENT_HUMAN
	autoplay(s)
	if len(s.History) != 0 {
		t.Fatalf("the engine played %d move(s) in a two-human game", len(s.History))
	}
}
