// tic-tac-toe — native tier slot (Decision 34).
//
// An ASCII board. The web slot draws a DOM grid from the SAME events, and the
// two share no markup, no layout, and no code — only the schema.
//
// THIS FILE DECIDES NOTHING. It does not know the rules, cannot tell whose turn
// it is, and never works out a winner: it prints `state.Winner` because the
// engine put it there, and highlights `state.WinningLine` because the engine
// named it. That is the rule this app exists to make testable — comment out the
// engine's win detection and BOTH slots go wrong identically, which is the proof
// that presentation carries no logic.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/devalbo/devalbo-ilc/engine/platform"
	"github.com/devalbo/devalbo-ilc/engine/platform/cli"
	"github.com/devalbo/devalbo-ilc/engine/platform/clispec"
	"github.com/devalbo/devalbo-ilc/example-apps/tictactoe/gen/go/dlcconfig"
	ilcv1 "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/v1"

	_ "github.com/devalbo/devalbo-ilc/example-apps/tictactoe/engine" // registers the commands
	tictactoev1 "github.com/devalbo/devalbo-ilc/example-apps/tictactoe/gen/go/tictactoe/v1"
)

func main() {
	// GRANT the filesystem before anything can touch it — the native equivalent
	// of the WASI preopen a browser host installs before instantiating.
	//
	// `./.tictactoe/`: project-local like git, so running in two projects keeps
	// two stores, but CONFINED — which matters because `reset-fs` is inherited
	// and would otherwise clear whatever directory you happened to be in.
	if err := platform.SetRoot(platform.AppRoot(dlcconfig.Name)); err != nil {
		os.Stderr.WriteString("tictactoe: " + err.Error() + "\n")
		os.Exit(2)
	}
	os.Exit(app(platform.Live, os.Stdout, os.Stderr, os.Stdin).Run(os.Args[1:]))
}

func app(port platform.EnginePort, stdout, stderr io.Writer, stdin io.Reader) cli.App {
	return cli.App{
		Name:     "tictactoe",
		Short:    "tic-tac-toe — one engine, a terminal board and a browser board",
		Commands: append(append([]clispec.Command{}, tictactoev1.GameServiceCLI...), ilcv1.PlatformServiceCLI...),
		Port:     port,
		Stdout:   stdout,
		Stderr:   stderr,
		Stdin:    stdin,

		Render: map[uint32]cli.Renderer{
			tictactoev1.MethodGetState: render(func(out io.Writer, r *tictactoev1.GetStateResponse) error {
				_, err := io.WriteString(out, Projection(r.GetState()))
				return err
			}),
			tictactoev1.MethodPlay: render(func(out io.Writer, r *tictactoev1.PlayResponse) error {
				_, err := io.WriteString(out, Projection(r.GetState()))
				return err
			}),
			tictactoev1.MethodNewGame: render(func(out io.Writer, r *tictactoev1.NewGameResponse) error {
				_, err := io.WriteString(out, Projection(r.GetState()))
				return err
			}),

			ilcv1.MethodVersion: render(func(out io.Writer, r *ilcv1.VersionResponse) error {
				_, err := fmt.Fprintln(out, r.GetVersion())
				return err
			}),
			ilcv1.MethodExportFs: render(func(out io.Writer, r *ilcv1.ExportFsResponse) error {
				_, err := out.Write(r.GetBundle())
				return err
			}),
			ilcv1.MethodImportFs: render(func(out io.Writer, r *ilcv1.ImportFsResponse) error {
				for _, f := range r.GetFiles() {
					if _, err := fmt.Fprintln(out, "  + "+f); err != nil {
						return err
					}
				}
				return nil
			}),
			ilcv1.MethodResetFs: render(func(out io.Writer, r *ilcv1.ResetFsResponse) error {
				// The engine returns the TOP-LEVEL entries it removed, not a
				// file count — `records/` is one entry holding many notes. This
				// printed "removed 1 file(s)" after deleting two, which reads
				// as though something survived. Name what was actually removed.
				removed := r.GetRemoved()
				if len(removed) == 0 {
					_, err := fmt.Fprintln(out, "nothing to remove")
					return err
				}
				for _, rm := range removed {
					if _, err := fmt.Fprintln(out, "  - "+rm); err != nil {
						return err
					}
				}
				return nil
			}),
		},
	}
}

// Projection is what this slot is showing, as text.
//
// Exported because it is the slot's CONTRACT, not a detail: host parity feeds
// the same state to this and to the web slot and compares the two. A test that
// scraped stdout instead would be asserting about padding.
//
// The board is drawn with the square NUMBERS in empty cells, because a player
// typing `play 5` needs to know which 5 is. That is presentation — the engine
// has no opinion about it, which is exactly the sort of thing a slot is for.
func Projection(s *tictactoev1.GameState) string {
	if s == nil {
		return "(no game)\n"
	}
	var b strings.Builder
	won := map[uint32]bool{}
	for _, i := range s.GetWinningLine() {
		won[i] = true
	}

	last := lastMove(s)
	for row := 0; row < 3; row++ {
		cells := make([]string, 3)
		for col := 0; col < 3; col++ {
			i := row*3 + col
			cells[col] = cell(s, uint32(i), won[uint32(i)], last)
		}
		// No extra leading space: each cell is already padded to three, so the
		// row is exactly as wide as the separator below it.
		b.WriteString(strings.Join(cells, "|") + "\n")
		if row < 2 {
			b.WriteString("---+---+---\n")
		}
	}

	b.WriteString(status(s))
	return b.String()
}

// cell draws one square IN GAME TIME.
//
// Three renderings, and every one of them is a fact the engine supplied:
//
//	[X]  part of the winning line   — s.WinningLine
//	>X<  the most recent move       — s.LastMove
//	 X   played earlier             — square.Mark
//
// None of them is worked out here. The winning line is named, the latest move
// is named, and this file decides only what those look like in a terminal —
// which is exactly the boundary D3 draws.
func cell(s *tictactoev1.GameState, i uint32, winning bool, last uint32) string {
	sym := ""
	switch s.GetBoard()[i] {
	case tictactoev1.Mark_MARK_X:
		sym = "X"
	case tictactoev1.Mark_MARK_O:
		sym = "O"
	default:
		return fmt.Sprintf(" %d ", i+1)
	}
	switch {
	case winning:
		return "[" + sym + "]"
	case last == i+1:
		return ">" + sym + "<"
	default:
		return " " + sym + " "
	}
}

// status reads the clock as well as the board: "move N" is game time, and it is
// what tells a reader whether they are looking at a current render or a stale
// one.
//
// A SWITCH OVER ONE ENGINE-COMPUTED VALUE, not an interpretation of several.
// This used to ask "is there a winner? else is it a draw? else …", which is a
// small judgement repeated in every slot and therefore a small judgement two
// slots could make differently.
func status(s *tictactoev1.GameState) string {
	moves := len(s.GetHistory())
	switch s.GetOutcome() {
	case tictactoev1.Outcome_OUTCOME_WINNER_X:
		return fmt.Sprintf("X wins in %d\n", moves)
	case tictactoev1.Outcome_OUTCOME_WINNER_O:
		return fmt.Sprintf("O wins in %d\n", moves)
	case tictactoev1.Outcome_OUTCOME_DRAW:
		return fmt.Sprintf("a draw in %d\n", moves)
	default:
		return fmt.Sprintf("%s to play (move %d)\n", mark(s.GetTurn()), moves+1)
	}
}

// lastMove is the square just played, or 0 before the first move.
//
// Read off the history rather than sent as a field: it is the last element of a
// list, not a rule, and two implementations cannot disagree about that. Contrast
// `winning_line`, which needs the eight lines and the win rule and therefore has
// to come from the engine.
func lastMove(s *tictactoev1.GameState) uint32 {
	h := s.GetHistory()
	if len(h) == 0 {
		return 0
	}
	return h[len(h)-1].GetSquare()
}

func mark(m tictactoev1.Mark) string {
	switch m {
	case tictactoev1.Mark_MARK_X:
		return "X"
	case tictactoev1.Mark_MARK_O:
		return "O"
	default:
		return "nobody"
	}
}

func render[T any, PT interface {
	*T
	UnmarshalVT([]byte) error
}](print func(io.Writer, PT) error) cli.Renderer {
	return func(out io.Writer, response []byte) error {
		msg := PT(new(T))
		if err := msg.UnmarshalVT(response); err != nil {
			return err
		}
		return print(out, msg)
	}
}
