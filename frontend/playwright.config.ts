import { defineConfig } from "@playwright/test";

// Headed / watch options (any of these make the browser visible):
//   make verify-web-watch   → headed + slowMo + pause at the end
//   DLC_WEB_WATCH=1         → same
//   DLC_WEB_HEADED=1        → headed only (no pause)
//   npx playwright test --headed
const watch = process.env.DLC_WEB_WATCH === "1";
const headed = process.env.DLC_WEB_HEADED === "1" || watch;

export default defineConfig({
  testDir: "./test",
  // xtier.spec.ts is an exporter, not an assertion — it is run explicitly by
  // scripts/verify-bundle-xtier.sh, which needs XTIER_OUT set.
  testIgnore: process.env.XTIER_OUT ? [] : ["xtier.spec.ts"],
  timeout: watch ? 0 : 90_000, // watch mode: no limit (page.pause)
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL: "http://127.0.0.1:4173",
    headless: !headed,
    launchOptions: { slowMo: watch ? 300 : 0 },
    trace: "on-first-retry",
  },
  webServer: {
    command: "npx vite --host 127.0.0.1 --port 4173",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: !process.env.CI,
    timeout: 90_000,
  },
});
