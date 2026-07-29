// tic-tac-toe's web tier slot — the entry point.
//
// Chooses an engine and publishes a handle. The view takes an `EnginePort`, so
// the only difference between the real app and a slot test is which port is
// passed — no build flag, no mock injection, no branch inside the view.
import { enginePort } from "@devalbo/dlc-web/port";

import { gameStyles, mountGame } from "./view";
import type { GameView } from "./view";

document.head.appendChild(document.createElement("style")).textContent = gameStyles();

const game = mountGame(enginePort);

document.getElementById("new-game")!.addEventListener("click", () => {
  void game.newGame();
});

// COLD START (Decision 34 D5): events are ephemeral — no log, no replay — so a
// slot that rendered only from the stream would show an empty board on reload,
// or in a second tab opened mid-game. Prime with a query; events do the rest.
void game.refresh();

declare global {
  interface Window {
    game: GameView;
  }
}
window.game = game;
