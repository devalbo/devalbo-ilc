import { expect, test } from "@playwright/test";

// HOST PARITY (host-layer plan Phase 4) — the web half.
//
// The identical vectors and expected strings live in
// hosts/native/projection_test.go. Two slots, written in different languages,
// sharing no code, asserting the same rendering of the same state.
//
// WHY THIS EXISTS AT ALL. Parity compares command results, the written
// filesystem and the event stream — every one of them engine-side — so a tier
// slot is invisible to it by construction. This is the only mechanical check
// the layer can have, and without it "a slot renders, it never decides" is a
// sentence in a document rather than a rule.
//
// The duplication between the two files is deliberate: generating both sides
// from one source would prove only that the generator agrees with itself.
//
// No engine runs here. These are pure state → text.

const MARK = { X: 1, O: 2 } as const;
const x = MARK.X;
const o = MARK.O;

// Board cells are plain marks; `e` is an empty square (the zero value).
const e = 0;

// The engine's single judgement about how the game stands — no interpreting a
// winner field against a draw field.
const OUTCOME = { IN_PROGRESS: 1, WINNER_X: 2, WINNER_O: 3, DRAW: 4 } as const;

// A move, for building a history. `moves` and `lastMove` are no longer fields —
// they are this list's length and its last element.
const mv = (square: number, mark: number) => ({ square, mark });

const vectors = [
  {
    name: "empty board shows square numbers",
    state: {
      board: [e, e, e, e, e, e, e, e, e],
      turn: x,
      outcome: OUTCOME.IN_PROGRESS,
    },
    want: " 1 | 2 | 3 \n---+---+---\n 4 | 5 | 6 \n---+---+---\n 7 | 8 | 9 \nX to play (move 1)\n",
  },
  {
    // The LATEST move is marked — game time, straight from `lastMove`.
    name: "the latest move is marked",
    state: {
      board: [e, e, e, e, x, e, e, e, e],
      turn: o,
      outcome: OUTCOME.IN_PROGRESS,
      history: [mv(5, x)],
    },
    want: " 1 | 2 | 3 \n---+---+---\n 4 |>X<| 6 \n---+---+---\n 7 | 8 | 9 \nO to play (move 2)\n",
  },
  {
    name: "an earlier move is not marked as latest",
    state: {
      board: [o, e, e, e, x, e, e, e, e],
      turn: x,
      outcome: OUTCOME.IN_PROGRESS,
      history: [mv(5, x), mv(1, o)],
    },
    want: ">O<| 2 | 3 \n---+---+---\n 4 | X | 6 \n---+---+---\n 7 | 8 | 9 \nX to play (move 3)\n",
  },
  {
    // Highlighted because the ENGINE named the line, not because either slot
    // found it.
    name: "a win highlights the line the engine named",
    state: {
      board: [x, x, x, o, o, e, e, e, e],
      outcome: OUTCOME.WINNER_X,
      winningLine: [0, 1, 2],
      history: [mv(1, x), mv(4, o), mv(2, x), mv(5, o), mv(3, x)],
    },
    // The winning line takes precedence over the latest-move marker — the sort
    // of ordering two independently written renderers get differently.
    want: "[X]|[X]|[X]\n---+---+---\n O | O | 6 \n---+---+---\n 7 | 8 | 9 \nX wins in 5\n",
  },
  {
    // THE DECISION PROBE — a state the engine would never produce.
    //
    // Three X in a row, but `outcome` is IN_PROGRESS and `winningLine` empty.
    // It separates a slot that READS from one that COMPUTES, which no valid
    // state can do: for a valid state the engine's judgement and an
    // independently derived one agree, so a slot quietly working out the winner
    // itself would render identically and pass every other vector here.
    //
    // A slot that reads renders the contradiction. A slot that decides
    // "corrects" it to "X wins" and reveals itself.
    //
    // DO NOT "FIX" THIS STATE. Its impossibility is the mechanism.
    name: "decision probe: a slot must not notice a win the engine did not report",
    state: {
      board: [x, x, x, o, o, e, e, e, e],
      turn: o,
      outcome: OUTCOME.IN_PROGRESS,
      history: [mv(1, x), mv(4, o), mv(2, x), mv(5, o), mv(3, x)],
    },
    want: " X | X |>X<\n---+---+---\n O | O | 6 \n---+---+---\n 7 | 8 | 9 \nO to play (move 6)\n",
  },
  {
    name: "a draw",
    state: {
      board: [x, o, x, x, o, o, o, x, x],
      outcome: OUTCOME.DRAW,
      history: [
        mv(1, x), mv(2, o), mv(3, x), mv(5, o), mv(4, x),
        mv(6, o), mv(8, x), mv(7, o), mv(9, x),
      ],
    },
    want: " X | O | X \n---+---+---\n X | O | O \n---+---+---\n O | X |>X<\na draw in 9\n",
  },
];

test.beforeEach(async ({ page }) => {
  await page.goto("/");
});

for (const v of vectors) {
  test(`host parity: ${v.name}`, async ({ page }) => {
    const got = await page.evaluate(async (state) => {
      const url = "/src/view.ts";
      const { projectionOf } = (await import(/* @vite-ignore */ url)) as typeof import("../src/view");
      return projectionOf(state as never);
    }, v.state);

    expect(
      got,
      "the native slot renders this state differently — see hosts/native/projection_test.go",
    ).toBe(v.want);
  });
}
