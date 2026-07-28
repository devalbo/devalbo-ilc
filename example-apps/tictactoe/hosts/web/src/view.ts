// tic-tac-toe — web tier slot (Decision 34).
//
// A DOM grid of nine buttons. The native slot draws ASCII from the SAME events
// and shares no markup, no layout, and no code with this — only the schema.
//
// THIS FILE DECIDES NOTHING. It does not know the rules, cannot tell whose turn
// it is, and never works out a winner. It disables a square because the engine
// filled it, highlights a line because the engine named it in `winningLine`, and
// says who won because `winner` says so. Comment out the engine's win detection
// and BOTH slots go wrong identically — that is the proof presentation carries
// no logic, and it is the only mechanical check this layer can have, because
// parity compares results, filesystem and events, all engine-side.
//
// SEMANTIC RENDER PATH: the engine emits what is TRUE (`game.state-changed`
// carrying the whole state) and this decides what that looks like. No draw
// commands, no widget tree, no Display capability.
import type { EnginePort } from "@devalbo/ilc-web/port";

import { StateChangedEventTopic } from "@gen/tictactoe/v1/commands.events.pb";
import {
  GetStateRequest,
  GetStateResponse,
  Mark,
  Outcome,
  PlayRequest,
  PlayResponse,
  NewGameRequest,
  StateChangedEvent,
  type GameState,
} from "@gen/tictactoe/v1/commands.pb";
import {
  MethodGetState,
  MethodNewGame,
  MethodPlay,
} from "@gen/tictactoe/v1/commands.registry.pb";

// The topic is NOT written here — it is declared on `StateChangedEvent` in
// commands.proto and generated for both tiers, so the engine's emit and this
// subscriber read one declaration.
export { StateChangedEventTopic as TopicStateChanged } from "@gen/tictactoe/v1/commands.events.pb";

export type GameView = {
  play(square: number): Promise<void>;
  newGame(): Promise<void>;
  /** Re-read from the engine. Cold start calls this; events do the rest. */
  refresh(): Promise<void>;
  /**
   * What this slot is showing, as text.
   *
   * The slot's CONTRACT, not a detail: host parity feeds the same state here
   * and to the native slot and compares the two. Deliberately the same shape the
   * native slot produces — a DOM scrape could not be compared with ASCII.
   */
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
      // GAME TIME, straight from the engine: this is the square that just
      // changed. A browser can use it for emphasis or animation where a
      // terminal marks it with brackets — same payload, different fidelity,
      // neither slot working out which move was most recent.
      b.classList.toggle("last", lastMove(state!) === i + 1);
      b.dataset.mark = markName(m);
    });

    statusEl.textContent = status(state);
    statusEl.dataset.testid = "status";
  }

  async function refresh(): Promise<void> {
    // COLD START (D5): events are ephemeral, so a slot that rendered only from
    // the stream would show an empty board on reload or in a second tab. Prime
    // with a query; take events as deltas.
    const r = await port.execute(MethodGetState, GetStateRequest.toBinary({}));
    if (!r.success) return;
    state = GetStateResponse.fromBinary(r.output).state ?? null;
    draw();
  }

  async function play(square: number): Promise<void> {
    const r = await port.execute(MethodPlay, PlayRequest.toBinary({ square }));
    if (!r.success) {
      // The engine's refusal, shown verbatim. This slot has no idea why the
      // move was illegal and does not need one.
      statusEl.textContent = r.error ?? "(refused)";
      return;
    }
    // NO redraw here. The engine emits `game.state-changed` and the
    // subscription below renders it — so if events stop working the board
    // visibly freezes rather than the capability rotting unnoticed.
    void PlayResponse.fromBinary(r.output);
  }

  async function newGame(): Promise<void> {
    await port.execute(MethodNewGame, NewGameRequest.toBinary({}));
    // No redraw here either, for the same reason.
  }

  const unsubscribing = port.subscribe((topic, payload) => {
    if (topic !== StateChangedEventTopic) return;
    // The whole state arrives in the payload — the semantic path. Rendered,
    // never written back from (§7.1): a stale render is fixed by the next
    // event, a write-back would make the event a second source of truth.
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
 * The shared text projection.
 *
 * Exported and written to match the native slot's output exactly, because host
 * parity compares the two. Where they must agree is the SEMANTICS — which
 * squares, whose turn, who won — not the pixels.
 */
export function projectionOf(s: GameState | null): string {
  if (!s) return "(no game)\n";
  const board = s.board ?? [];
  const won = new Set(s.winningLine ?? []);
  const last = lastMove(s);
  const lines: string[] = [];
  for (let row = 0; row < 3; row++) {
    const cells: string[] = [];
    for (let col = 0; col < 3; col++) {
      const i = row * 3 + col;
      const sym = symbol(board[i]);
      if (!sym) {
        cells.push(` ${i + 1} `);
      } else if (won.has(i)) {
        cells.push(`[${sym}]`); // the line the ENGINE named
      } else if (last === i + 1) {
        cells.push(`>${sym}<`); // the move the ENGINE stamped as latest
      } else {
        cells.push(` ${sym} `);
      }
    }
    // No extra leading space: each cell is padded to three already, so a row is
    // exactly as wide as the separator. Matches the native slot, which is what
    // host parity compares.
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

function markName(m: Mark | undefined): string {
  return symbol(m) ?? "empty";
}

function isOver(s: GameState): boolean {
  // One value, one comparison. Previously this asked whether a winner was set
  // OR a draw was true — a combination each slot had to interpret, and one that
  // allowed nonsense (both at once) to exist on the wire.
  return (s.outcome ?? Outcome.UNSPECIFIED) !== Outcome.IN_PROGRESS;
}

/**
 * The square just played, or 0 before the first move.
 *
 * Read off the history rather than sent as a field: the last element of a list
 * is not a rule, and two implementations cannot disagree about it. Contrast
 * `winningLine`, which needs the eight lines and the win rule, so the engine
 * has to name it.
 */
function lastMove(s: GameState): number {
  const h = s.history ?? [];
  return h.length === 0 ? 0 : (h[h.length - 1].square ?? 0);
}

function status(s: GameState): string {
  const moves = (s.history ?? []).length;
  switch (s.outcome) {
    case Outcome.WINNER_X:
      return `X wins in ${moves}\n`;
    case Outcome.WINNER_O:
      return `O wins in ${moves}\n`;
    case Outcome.DRAW:
      return `a draw in ${moves}\n`;
    default:
      return `${symbol(s.turn) ?? "nobody"} to play (move ${moves + 1})\n`;
  }
}

/** Styling, shipped with the slot. */
export function gameStyles(): string {
  return `
.ttt-grid { display: grid; grid-template-columns: repeat(3, 3rem); gap: 2px; }
.ttt-cell { height: 3rem; font: 700 1.25rem ui-monospace, Menlo, monospace; cursor: pointer; }
.ttt-cell:disabled { cursor: default; }
.ttt-cell.won { outline: 2px solid currentColor; }
.ttt-cell.last { text-decoration: underline; }
`.trim();
}
