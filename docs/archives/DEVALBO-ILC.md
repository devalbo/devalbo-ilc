# DEVALBO-ILC

## Inverted Line of Command (ILC)

`ILC` is `CLI` reversed — and so is the execution model. Using **inversion of control**, it turns a command-line program inside out: instead of a `main` reaching into its environment, the environment's capabilities are injected into the logic.

Traditional programs are implemented by defining a `main` method somewhere in a pile of code and invoked by a higher level command specifying that it's time to run that logic.

Instead, when a program is defined, each function can register itself an ILC handler. There's not necessarily a reason for a function to care about how its invoked other than to figure out how it can resolve dependencies (e.g. accessing its invokers interpretation of what a file system is ala traditional CLI vs browser-hosted OPFS vs an embedded route handler in a web-server application).

All of the environment conditions and access should be known before invocation, so why not leverage it dynamically instead of relying on implicit configuration done via `import` commands? Using the Inversion of Control pattern with well-typed arguments, the caller can reach out to its environment for what it needs rather than have it shoved in by its environment like a goose liver being prepped for pate.

A huge benefit of this is enforcing testability. It separates logic from runtime environment. If an command needs to access a filesystem, it does so in a bounded way that the calling infrastructure can reason about, rather than the developer figuring out ad hoc during implementation. Thoughtful developers are doing something like this anyway, it just needs to become more of a convention.

This might be complementary to OpenBindings (https://github.com/openbindings) and normalize the intakes at the app/implementation level.



## Implementations

There are already several libraries that do this type of thing, but each coupled tightly to the domain they run in.

### Python
* CLI - typer, click, etc.
* web-server app - flask, django, etc.
* browser runtime - Pyjamas?


### Typescript
* CLI - oblique, commander, etc
* web-server app - Node libraries
* browser runtime - no *standardized capability-injection host* (browser JS is available — what's missing is an ILC-shaped contract, not the runtime)


### Rust
* CLI - clap, etc
* web-server app - tokio, etc
* browser runtime - Yew, etc


Here is the technical specification summarizing our architectural decisions. You can drop this directly into your team's documentation (Notion, Jira, GitHub Wiki, etc.) to align everyone on the project's vision and implementation plan.

---

## Specification: Inverted Line of Command (ILC) V1

> **Amended 2026-05-27** by the [review decisions](./DEVALBO-ILC-PLAN.md#review-decisions-2026-05-27): async-first contract; `ConsoleIo` (logger-shaped) + stdin + binary-capable `FileSystem` in V1 (`Network` deferred); WIT compiled via an internal IR; host adapters selected at startup (no ambient defaults).

### 1. Overview and Objective

The Inverted Line of Command (ILC) is an architectural framework designed to completely decouple core business logic from its execution runtime (CLI, Web Server, Browser).

Instead of relying on implicit, environment-specific imports (like `import os` or `window.fetch`), ILC enforces Inversion of Control (IoC). Functions explicitly declare the capabilities they require via a standardized `Environment` contract. The host application injects these dependencies at runtime. This guarantees bounded side-effects, "write-once-run-anywhere" portability, and trivial unit testing.

### 2. Target Languages

The framework will provide native SDKs for three distinct ecosystems to validate the cross-language abstraction:

* **TypeScript / JavaScript** (Node.js & Browser)
* **Python**
* **Rust**

---

### 3. System Architecture & Pipeline

To avoid manually maintaining synchronous contracts across three languages, ILC utilizes a "Code Generation to Library" pipeline.

* **The Source of Truth (IDL):** Capability contracts are defined once using WebAssembly Interface Type (WIT) syntax in `.wit` files. We are strictly using WIT as a schema definition language, not targeting WebAssembly compilation.
* **The ILC Compiler:** A custom Rust-based CLI utilizing the `wit-parser` crate. It parses the `.wit` files into an **internal IR** (so the IDL stays swappable), then generates native boilerplate from the IR (TypeScript interfaces, Python protocols, Rust traits).
* **The SDK (`devalbo-ilc-core`):** The generated code is packaged into standard libraries published to NPM, PyPI, and Crates.io. These libraries contain the capability interfaces, standardized error wrappers, and **host adapters selected at startup by runtime detection** (always overridable — not ambient default behavior).

---

### 4. V1 Core Capabilities

All capabilities are **async**. The V1 SDK exposes them via an **introspectable `Environment`** that handlers pull from (with a typed escape hatch to reach host-specific extras). V1 scope is `ConsoleIo` + basic `FileSystem`; `Network` is deferred.

| Capability | Interface Name | Standard Methods | Notes |
| --- | --- | --- | --- |
| **Console I/O** | `ConsoleIo` | `info`, `error`, `readLine` | `info`/`error`: void. `readLine`: `option<string>` (`none` = EOF). WIT: `read-line`. Node: process stdin. Browser/DevTools: `window.prompt()`. Tests: queued lines. |
| **File System** | `FileSystem` | `read_bytes`, `write_bytes` | Binary-capable (text convenience on top); OS / OPFS / in-memory via a virtual-path scheme. |
| **Network** | `Network` | `fetch` | Web `fetch`-shaped; buffered byte bodies. **Deferred past V1** (see plan). |

---

### 5. Standardized Error Handling

To ensure behavioral parity across languages with drastically different error-handling paradigms (Rust's `Result` vs. Python/TS `Exceptions`), all ILC capabilities must return a standardized Result object.

The compiler will generate a strict `IlcResult` type for TypeScript and Python to mirror Rust's native `Result<T, E>`. Network failures or missing files will return an explicit error value rather than throwing an exception. Because the contract is async, `IlcResult` rides *inside* the returned future (`Promise<Result<T, E>>` / `async fn -> Result<T, E>`). See the [error taxonomy](./DEVALBO-ILC-PLAN.md#error-taxonomy-sketch) in the plan.

---

### 6. Developer Experience (DX) & Testing

The SDK is designed to make application development frictionless. Developers will install the core library and use the provided host adapters or testing utilities.

* **The `NativeHost`:** The SDK will ship with pre-built adapters for standard operating system execution. In a production CLI or backend, the developer instantiates `NativeHost` (which wires up the real OS `FileSystem` and `Network`) and passes it to their core logic.
* **The `TestEnvironment`:** The SDK will include a deterministic, memory-backed testing adapter. It implements the exact same `Environment` contract but writes files to memory arrays and captures `ConsoleIo` log lines (and scripted stdin) in memory.
* **Network Mocking:** The `TestEnvironment` will feature a queue-and-expect routing pattern, allowing developers to pre-program HTTP responses before invoking their business logic in a unit test.

---

### 7. Implementation Milestones

**Phase 1: Tooling & Schema**

* Define `console-io.wit` (`ConsoleIo`: `info`, `error`, `readLine`) and minimal `environment.wit` (`consoleIo` only).
* Build the Rust ILC Compiler CLI; emit separate TS / Python / Rust packages from WIT (see [DEVALBO-ILC-PLAN.md](./DEVALBO-ILC-PLAN.md)).

**Phase 2: SDK Generation & Native Adapters**

* Finalize template output for the `IlcResult` wrappers and capability interfaces.
* Implement the `NativeHost` adapters for all three languages (mapping the interfaces to actual `fs`, `requests`/`fetch`, and standard out).

**Phase 3: The Testing Harness**

* Implement the `TestEnvironment` memory-backed adapters in all three languages.
* Build the `expect/respondWith` mock network queue.
* Write cross-language "Hello World" examples to validate DX and architectural parity.