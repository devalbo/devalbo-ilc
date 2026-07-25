import { defineConfig } from "@playwright/test";

// Watch / headed options (any of these make the browser visible):
//   make spike-opfs-watch          → headed + slowMo + pause at end
//   SPIKE_OPFS_WATCH=1             → same
//   SPIKE_OPFS_HEADED=1            → headed only (no pause)
//   npx playwright test --headed   → headed via CLI
const watch = process.env.SPIKE_OPFS_WATCH === "1";
const headedEnv = process.env.SPIKE_OPFS_HEADED === "1" || watch;

export default defineConfig({
  testDir: ".",
  testMatch: "opfs.spec.js",
  timeout: watch ? 0 : 60_000, // watch mode: no limit (page.pause)
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL: "http://127.0.0.1:4173",
    headless: !headedEnv,
    launchOptions: {
      slowMo: watch ? 300 : 0,
    },
    trace: "on-first-retry",
  },
  webServer: {
    command: "npx vite --host 127.0.0.1 --port 4173",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
});
