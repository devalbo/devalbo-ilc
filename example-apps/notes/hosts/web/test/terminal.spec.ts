import { test, expect, type Page } from "@playwright/test";

// The in-page terminal, and the PARSE VECTORS that keep it honest.
//
// The terminal is the second consumer of the generated command surface; the Go
// CLI is the first. Parsing happens BEFORE the boundary the parity check guards
// — parity compares command results, written filesystems and event streams, all
// downstream of a request that already exists — so the two front ends could each
// be self-consistent while turning the same line into different requests, with
// every other check in this repo still green.
//
// So: the same command lines and the same expected bytes appear in
// hosts/native/parsevector_test.go. The duplication is deliberate. Two
// independent implementations asserting identical bytes IS the check; generating
// both sides from one source would prove only that the generator is consistent
// with itself.
//
// No engine runs here — a fake port records what the terminal built.

const DRIVER = "/test/terminal-driver.ts";

async function drive<T>(page: Page, call: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    async ({ call, args, driver }) => {
      const m = (await import(/* @vite-ignore */ driver)) as Record<
        string,
        (...a: unknown[]) => unknown
      >;
      return (await m[call](...args)) as T;
    },
    { call, args, driver: DRIVER },
  );
}

test.beforeEach(async ({ page }) => {
  await page.goto("/test/slot.html");
  await drive(page, "setup");
});

// KEEP IN SYNC with hosts/native/parsevector_test.go.
const vectors = [
  {
    name: "create with title and body",
    line: 'create --title "Buy milk" --body "two litres"',
    hex: "0a08427579206d696c6b120a74776f206c69747265731880e2cfaa06",
  },
  {
    // Signed ints are 64-bit two's complement varints, so -1 is TEN bytes.
    // The likeliest place for two hand-written encoders to disagree.
    name: "negative int64 (two's complement)",
    line: "create --title x --created-at -1",
    hex: "0a017818ffffffffffffffffff01",
  },
  {
    name: "create without body",
    line: 'create --title "Buy milk"',
    hex: "0a08427579206d696c6b1880e2cfaa06",
  },
  { name: "list takes no fields", line: "list", hex: "" },
  { name: "delete by id", line: "delete --id buy-milk", hex: "0a086275792d6d696c6b" },
];

for (const v of vectors) {
  test(`parse vector: ${v.name}`, async ({ page }) => {
    await drive(page, "run", v.line);
    const got = await drive<string>(page, "lastRequestHex");
    expect(
      got,
      `the Go runner builds a different request for the same line — see hosts/native/parsevector_test.go`,
    ).toBe(v.hex);
  });
}

// Beyond the vectors: the terminal has to be a usable command line.

test("runs a command and renders the response", async ({ page }) => {
  const out = await drive<string>(page, "run", 'create --title "Buy milk"');
  expect(out).toBe("created buy-milk -> records/buy-milk.json");
});

test("quoting keeps a value together", async ({ page }) => {
  await drive(page, "run", 'create --title "Buy milk and bread"');
  const hex = await drive<string>(page, "lastRequestHex");
  // The whole quoted string lands in field 1, rather than "and"/"bread"
  // arriving as stray positionals.
  expect(Buffer.from(hex, "hex").toString()).toContain("Buy milk and bread");
});

test("a required flag is enforced, and names itself", async ({ page }) => {
  const out = await drive<string>(page, "run", "create");
  expect(out).toContain("--title is required");
});

test("an unknown command is refused, not ignored", async ({ page }) => {
  const out = await drive<string>(page, "run", "crate --title x");
  expect(out).toContain("unknown command");
});

test("an unknown flag is refused", async ({ page }) => {
  const out = await drive<string>(page, "run", "create --titel x");
  expect(out).toContain("unknown flag --titel");
});

// `-h` comes from the .proto: the summary is the rpc's doc comment and the flag
// help is the field's `help` option, so the terminal documents itself.
test("help is generated from the schema", async ({ page }) => {
  const list = await drive<string>(page, "run", "help");
  expect(list).toContain("create");
  expect(list).toContain("Write a new note");

  const one = await drive<string>(page, "run", "create -h");
  expect(one).toContain("note title");
  expect(one).toContain("(required)");
});

// Errors ride the result envelope on every tier.
test("an engine error is reported, not swallowed", async ({ page }) => {
  const out = await drive<string>(page, "run", "delete --id nope");
  expect(out).not.toBe("");
});

// Tab completion, entirely from the generated surface — the payoff for the
// command surface being DATA. A hand-written CLI would need a second table for
// this, and the second table is the one that goes stale.
test("completes subcommands", async ({ page }) => {
  const r = await drive<{ candidates: string[]; prefix: string }>(
    page,
    "completions",
    "cr",
  );
  expect(r.candidates).toEqual(["create"]);
  expect(r.prefix).toBe("create");
});

test("completes a command's own flags", async ({ page }) => {
  const r = await drive<{ candidates: string[] }>(page, "completions", "create --t");
  expect(r.candidates).toEqual(["--title"]);
});

test("offers the common prefix when several match", async ({ page }) => {
  const r = await drive<{ candidates: string[]; prefix: string }>(
    page,
    "completions",
    "create --",
  );
  // Every flag of `create`, and the longest shared start.
  expect(r.candidates.length).toBeGreaterThan(1);
  expect(r.prefix).toBe("--");
});

test("a bad command completes to nothing rather than guessing", async ({ page }) => {
  const r = await drive<{ candidates: string[] }>(page, "completions", "crate --t");
  expect(r.candidates).toEqual([]);
});
