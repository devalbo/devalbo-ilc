package main

// HOST PARITY (host-layer plan Phase 4) — the native half.
//
// This is the only mechanical check the slot layer can have. Parity compares
// command results, the written filesystem, and the event stream — all
// engine-side — so a tier slot is invisible to it by construction. Two hosts
// that each worked something out would eventually disagree on one tier only,
// with every existing check green.
//
// So: feed both slots the SAME state and compare what they say they are
// showing. The identical vectors and expected strings live in
// hosts/web/test/parity.spec.ts. That duplication is the check — generating
// both sides from one source would prove only that the generator agrees with
// itself.
//
// What must match is the SEMANTICS — which squares, whose turn, who won, which
// line — not pixels. The two slots are free to look nothing alike; here they
// happen to share a text form precisely so they can be compared at all.

import (
	"testing"

	tictactoev1 "github.com/devalbo/devalbo-ilc/example-apps/tictactoe/gen/go/tictactoe/v1"
)

// Board cells are plain marks; `e` is an empty square (the zero value).
const e = tictactoev1.Mark_MARK_UNSPECIFIED

// A move, for building a history. `moves` and `last_move` are no longer fields
// — they are the length of this list and its last element, which is why the
// vectors now carry the record rather than a summary of it.
func mv(square uint32, m tictactoev1.Mark) *tictactoev1.Move {
	return &tictactoev1.Move{Square: square, Mark: m}
}

const (
	x = tictactoev1.Mark_MARK_X
	o = tictactoev1.Mark_MARK_O
)

func TestProjectionParityVectors(t *testing.T) {
	cases := []struct {
		name  string
		state *tictactoev1.GameState
		want  string
	}{
		{
			name: "empty board shows square numbers",
			state: &tictactoev1.GameState{
				Board:   []tictactoev1.Mark{e, e, e, e, e, e, e, e, e},
				Turn:    x,
				Outcome: tictactoev1.Outcome_OUTCOME_IN_PROGRESS,
			},
			want: " 1 | 2 | 3 \n---+---+---\n 4 | 5 | 6 \n---+---+---\n 7 | 8 | 9 \nX to play (move 1)\n",
		},
		{
			// The LATEST move is marked `>X<` — game time, straight from
			// `last_move`. Neither slot works out which move was most recent.
			name: "the latest move is marked",
			state: &tictactoev1.GameState{
				Board:   []tictactoev1.Mark{e, e, e, e, x, e, e, e, e},
				Turn:    o,
				Outcome: tictactoev1.Outcome_OUTCOME_IN_PROGRESS,
				History: []*tictactoev1.Move{mv(5, x)},
			},
			want: " 1 | 2 | 3 \n---+---+---\n 4 |>X<| 6 \n---+---+---\n 7 | 8 | 9 \nO to play (move 2)\n",
		},
		{
			// An earlier move renders plainly, so a reader can see the order.
			name: "an earlier move is not marked as latest",
			state: &tictactoev1.GameState{
				Board:   []tictactoev1.Mark{o, e, e, e, x, e, e, e, e},
				Turn:    x,
				Outcome: tictactoev1.Outcome_OUTCOME_IN_PROGRESS,
				History: []*tictactoev1.Move{mv(5, x), mv(1, o)},
			},
			want: ">O<| 2 | 3 \n---+---+---\n 4 | X | 6 \n---+---+---\n 7 | 8 | 9 \nX to play (move 3)\n",
		},
		{
			// The winning line is HIGHLIGHTED because the engine named it, not
			// because either slot found it.
			name: "a win highlights the line the engine named",
			state: &tictactoev1.GameState{
				Board:       []tictactoev1.Mark{x, x, x, o, o, e, e, e, e},
				Outcome:     tictactoev1.Outcome_OUTCOME_WINNER_X,
				WinningLine: []uint32{0, 1, 2},
				History:     []*tictactoev1.Move{mv(1, x), mv(4, o), mv(2, x), mv(5, o), mv(3, x)},
			},
			// The winning line wins over the latest-move marker: the game being
			// over is the more important fact, and both slots must agree on
			// which mark takes precedence — which is precisely the sort of
			// thing two independently written renderers get differently.
			want: "[X]|[X]|[X]\n---+---+---\n O | O | 6 \n---+---+---\n 7 | 8 | 9 \nX wins in 5\n",
		},
		{
			// THE DECISION PROBE — a state the engine would never produce.
			//
			// Three X in a row, but `outcome` says IN_PROGRESS and `winningLine`
			// is empty. It exists to separate a slot that READS from one that
			// COMPUTES, which no valid state can do: for a valid state the
			// engine's judgement and an independently derived one agree, so a
			// slot that quietly worked out the winner itself would render
			// identically and pass every other vector here.
			//
			// A slot that reads renders the contradiction faithfully — plain
			// marks, "O to play". A slot that decides "helpfully corrects" it to
			// "X wins" with the line highlighted, and reveals itself.
			//
			// DO NOT "FIX" THIS STATE. Its impossibility is the mechanism.
			name: "decision probe: a slot must not notice a win the engine did not report",
			state: &tictactoev1.GameState{
				Board:   []tictactoev1.Mark{x, x, x, o, o, e, e, e, e},
				Turn:    o,
				Outcome: tictactoev1.Outcome_OUTCOME_IN_PROGRESS,
				History: []*tictactoev1.Move{mv(1, x), mv(4, o), mv(2, x), mv(5, o), mv(3, x)},
			},
			want: " X | X |>X<\n---+---+---\n O | O | 6 \n---+---+---\n 7 | 8 | 9 \nO to play (move 6)\n",
		},
		{
			name: "a draw",
			state: &tictactoev1.GameState{
				Board:   []tictactoev1.Mark{x, o, x, x, o, o, o, x, x},
				Outcome: tictactoev1.Outcome_OUTCOME_DRAW,
				History: []*tictactoev1.Move{
					mv(1, x), mv(2, o), mv(3, x), mv(5, o), mv(4, x),
					mv(6, o), mv(8, x), mv(7, o), mv(9, x),
				},
			},
			want: " X | O | X \n---+---+---\n X | O | O \n---+---+---\n O | X |>X<\na draw in 9\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Projection(c.state); got != c.want {
				t.Errorf("native projection differs from the vector.\n got:\n%s\nwant:\n%s\n\nIf this is a deliberate change, update hosts/web/test/parity.spec.ts too — the point is that both slots agree.", got, c.want)
			}
		})
	}
}
