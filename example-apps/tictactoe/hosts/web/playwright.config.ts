import { defineConfig } from "@playwright/test";

// Headed / watch options:
//   npx playwright test --headed
//   tictactoe_WEB_WATCH=1 npx playwright test --headed   (slowMo + no timeout)
const watch = process.env.WEB_WATCH === "1";

export default defineConfig({
  testDir: "./test",
  timeout: watch ? 0 : 90_000,
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL: "http://127.0.0.1:5273",
    headless: !watch,
    launchOptions: { slowMo: watch ? 300 : 0 },
  },
  webServer: {
    // A DIFFERENT PORT from `npm run dev` (5173), and no reuse.
    //
    // Both halves are load-bearing. `reuseExistingServer` plus the dev port
    // means that if any Vite is running — your own dev server, another app's —
    // the tests silently run against THAT app and report on it. That happened:
    // a scaffolded app's suite ran entirely against `notes`, and one test even
    // passed, because the terminal echoes what you type and the assertion
    // matched the echo. A port clash now fails loudly instead.
    command: "npx vite --host 127.0.0.1 --port 5273 --strictPort",
    url: "http://127.0.0.1:5273",
    reuseExistingServer: false,
    timeout: 90_000,
  },
});
