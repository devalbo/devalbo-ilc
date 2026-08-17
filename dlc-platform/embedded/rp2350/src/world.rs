//! The badge's two WORLDS — and the first thing to say is what they are not.
//!
//! **NOT two WIT worlds.** A WIT world is a component's import/export set, so
//! forking one would fork the artifact — and "the same wasm across every host" is
//! the constraint EMBEDDED-PLAN calls load-bearing, the one any decision that
//! quietly forks the artifact has failed. Both worlds here import exactly the
//! same interfaces, instantiate the same `.cwasm`, and share `MinimalHost`. What
//! differs is **which channel reaches a human**.
//!
//! (`MinimalHost` and `minimal.rs` mean something else again — "the smallest host
//! a component will accept" — and both worlds use it. The collision is
//! unfortunate and the two senses are unrelated.)
//!
//! | | [`BadgeWorld::Normal`] | [`BadgeWorld::Minimal`] |
//! | --- | --- | --- |
//! | `execute`'s return value | shown as text | folded into a status colour |
//! | `wasi:cli/stdout` | shown as text | provided, not shown |
//! | `devalbo:ilc/events` | listed | **the status colour** |
//!
//! **Both PROVIDE stdout, and that is not a technicality.** TinyGo's
//! `runtime.initAll` calls `get-stdout` during `_initialize`, so a component
//! whose stdout is missing never instantiates at all — a badge cannot opt out of
//! providing it, only out of displaying it. Which is exactly why the app cannot
//! infer anything from its presence, and why the advertisement below exists.
//!
//! **How an app sets the colour without a new capability** (D6, Decision 34): it
//! emits a semantic event, and this tier decides what that looks like. The app
//! never says "red" — it says what happened, and a badge with one LED's worth of
//! output picks a colour while a browser draws a whole DOM from the same event.

use dlc_platform_embedded::command::CommandResult;

/// Which world this firmware was built as. A FLASH-TIME choice (`build.rs`), not
/// a runtime one: the difference is a few hundred bytes of presentation code, and
/// making it a boot-time branch would mean carrying both onto a board that only
/// ever wanted one.
#[derive(Clone, Copy, PartialEq, Eq)]
pub enum BadgeWorld {
    /// Text. The app's output is meant to be READ — a serial console today, and
    /// the TFT rendering text when Phase 3 wires it.
    Normal,
    /// One colour, meaning "how is it going".
    ///
    /// **THIS IS A SIMULATION, AND THAT IS ITS POINT.** The Tufty has a 320×240
    /// screen and a UART, so this world is not what the badge can afford — it is
    /// the badge PRETENDING to be a device that has only a status LED, in order
    /// to answer a question no rich tier can: what can an ILC app still say when
    /// its host has almost no way to speak?
    ///
    /// That makes it a testbed rather than a lesser badge. A sensor node, a
    /// keyfob, a board with one LED and no console is a real target for this
    /// architecture, and the interesting question is whether an app written once
    /// still communicates usefully there. Running it on hardware that COULD have
    /// shown text is what makes the constraint honest and the failure legible:
    /// the same binary, the same events, the same `.cwasm`, and the serial log
    /// still available to the developer to see what the app tried to say.
    ///
    /// Which is also why the capability list is a strict prefix rather than a
    /// separate set — see [`CAPABILITIES_MINIMAL`]. A simulated floor is only
    /// useful if it is genuinely a subset of the real thing.
    Minimal,
}

/// How the panel is shared, decided by `BADGE_SCREEN` at build time.
pub const SCREEN: ScreenLayout = if cfg!(badge_screen_full) {
    ScreenLayout::Full
} else {
    ScreenLayout::Split
};

/// What this firmware is, decided by `BADGE_WORLD` at build time.
pub const WORLD: BadgeWorld = if cfg!(badge_world_minimal) {
    BadgeWorld::Minimal
} else {
    BadgeWorld::Normal
};

impl BadgeWorld {
    /// `WorldKind` in control.proto — what the control channel reports.
    pub const fn code(self) -> u32 {
        match self {
            BadgeWorld::Normal => dlc_platform_embedded::control::WORLD_KIND_NORMAL,
            BadgeWorld::Minimal => dlc_platform_embedded::control::WORLD_KIND_MINIMAL,
        }
    }

    pub const fn name(self) -> &'static str {
        match self {
            BadgeWorld::Normal => "normal",
            BadgeWorld::Minimal => "minimal",
        }
    }

    /// What this world can do — and **minimal is literally a PREFIX of normal**.
    ///
    /// Not "a subset by convention, please keep it that way": the two lists are
    /// defined so that [`CAPABILITIES_NORMAL`] begins with all of
    /// [`CAPABILITIES_MINIMAL`], and a `const` assertion below fails the BUILD if
    /// that stops being true. That is what makes "trim the normal world for size
    /// or speed" a coherent operation rather than a port — trimming means
    /// dropping capabilities off the end, and what remains is a world that
    /// already exists.
    pub const fn capabilities(self) -> &'static [Capability] {
        match self {
            BadgeWorld::Normal => CAPABILITIES_NORMAL,
            BadgeWorld::Minimal => CAPABILITIES_MINIMAL,
        }
    }

    /// The `wasi:cli/environment` table, DERIVED from the capability list.
    ///
    /// Derived rather than written out per world, because two hand-maintained
    /// tables are how the subset invariant would quietly stop holding — the
    /// advertisement cannot disagree with the capabilities if there is only one
    /// source for both.
    ///
    /// Filled into a caller-owned buffer: this runs before the heap is trusted,
    /// and it returns borrowed slices of `'static` strings, so nothing allocates.
    pub fn advertise<'a>(
        self,
        buffer: &'a mut [(&'static str, &'static str); ADVERTISEMENT_MAX],
    ) -> &'a [(&'static str, &'static str)] {
        // Always true of every badge world, so they are not capabilities.
        buffer[0] = ("ILC_TIER", "rp2350");
        buffer[1] = ("ILC_WORLD", self.name());
        let mut count = 2;

        for capability in self.capabilities() {
            buffer[count] = capability.advertisement();
            count += 1;
        }

        // NO SCREEN BUDGET HERE. It briefly went out as `ILC_COLS`/`ILC_ROWS`,
        // which cost a const integer-to-string formatter and a lookup table to
        // produce two keys nothing ever read. The budget is an ALLOCATION and
        // belongs in the manifest, which is the only channel that can correct it
        // — see the `manifest` stage in `main.rs` and ENVIRONMENT-PLAN.md D12.

        // THE ABSENCE IS THE MESSAGE, and it has to be said out loud. Every world
        // provides `wasi:cli/stdout` — TinyGo will not instantiate without it — so
        // an app cannot learn anything from its presence. A world lacking the text
        // capability says so explicitly, and an app that reads this can emit an
        // event instead of formatting a string nobody will see.
        if !self.can(Capability::Text) {
            buffer[count] = ("ILC_STDOUT", "none");
            count += 1;
        }

        &buffer[..count]
    }

    /// Whether this world has a capability. The check an app's host-side slot
    /// makes, and the one `advertise` makes about itself.
    pub fn can(self, capability: Capability) -> bool {
        self.capabilities().iter().any(|c| *c == capability)
    }
}

/// CAPABILITY IS NOT ALLOCATION, and they are dynamic to different degrees.
///
/// Two questions that look alike and are not:
///
/// | | question | changes? | channel |
/// | --- | --- | --- | --- |
/// | **capability** | can this tier show text at all, and at most how much? | settled for the session | wasi env, at instantiation |
/// | **allocation** | how much has the app got RIGHT NOW? | yes | the manifest (`SetEnvironment`) |
///
/// The wasi environment is read once, during `_initialize`, before any command
/// has run. That makes it the right home for a fact settled for the session and
/// the WRONG home for one that moves: an app cannot re-read it, so a changing
/// allocation announced there would simply be missed.
///
/// The manifest is the moving half — revision-stamped and re-sent on change, so
/// an app can both POLL it (`platform.Env()`, a cached read with no import) and
/// be TOLD (`platform.OnEnvironmentChange`).
///
/// **This badge sends both.** The wasi keys go out at instantiation and the
/// manifest goes out just before the app's first command — see
/// `main.rs` and `dlc_platform_embedded::manifest`.
///
/// Today the two AGREE, because this world has one app, one layout, and the
/// budget is fixed at flash time. Sending the manifest anyway is what makes the
/// numbers correctable rather than a promise this world cannot keep: the day it
/// takes rows back for a menu, it re-sends with revision 2 and the app finds
/// out. Nothing else has to change.
///
/// The keys below remain the startup bootstrap — what a boot log shows, and all
/// a world that cannot push a manifest would ever have. Nothing in the platform
/// reads them any more (docs/ENVIRONMENT-PLAN.md D12).
///
/// HOW THE PANEL IS DIVIDED between the world and the app.
///
/// The world has things worth showing — which app is running, whether it
/// succeeded, what it cost — and the app has its own output. Both on one 320x240
/// screen is a budget, and somebody has to decide it. The WORLD decides, because
/// it is the only party that knows what else is competing for the space.
///
/// **The app is then TOLD what it got** (`TextOut.cols`/`rows` in the manifest), rather than
/// left to assume a size. That is the same move as `ILC_STDOUT`: a tier states
/// what it can do, and an app that reads it formats for the space that exists
/// instead of the space it hoped for.
#[derive(Clone, Copy, PartialEq, Eq)]
pub enum ScreenLayout {
    /// A world band across the top, the app underneath.
    ///
    /// The default, and the right one while the badge is still being brought up:
    /// an app that fails silently is much harder to diagnose when the screen
    /// gives no indication of which app ran or how it ended.
    Split,
    /// The app gets everything.
    ///
    /// For a badge doing a job rather than being debugged. World status has to
    /// go somewhere, and it goes to the backlight and the status colour — one
    /// bit and one hue, which is exactly what `minimal` proves is workable.
    Full,
}

/// Rows the world keeps for itself under [`ScreenLayout::Split`].
///
/// One line of text plus padding. Deliberately small: the world's business is a
/// label and a colour, and anything larger is the world competing with the app
/// for the thing the app is there to do.
pub const WORLD_BAND_ROWS: usize = 1;

/// One thing a badge world can do for an app.
///
/// ORDERED BY HOW LITTLE HARDWARE IT NEEDS, which is what lets the lists below
/// nest: a colour needs one GPIO, text needs a UART or a font renderer. Adding a
/// capability means appending it, so every world remains a prefix of the ones
/// above it.
#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Capability {
    /// Show one colour meaning "how is it going". Every badge has a backlight.
    Status,
    /// Show the app's text — its command output and its stdout.
    Text,
}

impl Capability {
    /// How this capability announces itself to the app.
    pub const fn advertisement(self) -> (&'static str, &'static str) {
        match self {
            Capability::Status => ("ILC_STATUS", "color"),
            Capability::Text => ("ILC_STDOUT", text_sink()),
        }
    }
}

/// WHERE THE APP'S TEXT ACTUALLY GOES — **derived from the screen budget, not
/// declared alongside it.**
///
/// `ILC_STDOUT` and the manifest's `cols`/`rows` are the same statement at two
/// resolutions: one says whether an outlet exists, the other how big it is. Left
/// as independent constants they can contradict — a world claiming `display`
/// while budgeting the app zero rows is telling an app to format for a screen it
/// does not have, which is precisely the failure the advertisement exists to
/// prevent.
///
/// So the rule, and it holds by construction rather than by discipline:
///
/// | the app's rows | outlet |
/// | --- | --- |
/// | one or more | `display` — it reaches a screen |
/// | none, but a UART exists | `uart` — a developer may be watching; a user is not |
/// | neither | `none` — do not spend heap formatting |
///
/// The badge always has a UART on the clip pads, so `none` here is only ever a
/// deliberate choice by a world with no text capability at all (`minimal`),
/// never an accident of layout.
/// How the panel is divided, as a word — for the SCREEN and the log, where a
/// person reads it.
pub const fn screen_name() -> &'static str {
    match SCREEN {
        ScreenLayout::Split => "split",
        ScreenLayout::Full => "full",
    }
}

pub const fn text_sink() -> &'static str {
    if crate::console::APP_ROWS > 0 {
        "display"
    } else {
        // The clip pads are always there, so text is not lost — just not shown
        // to anyone who is not looking for it.
        "uart"
    }
}

// WHAT THE CONTROL CHANNEL SENDS, and it is deliberately NOT the words above.
//
// A caller asking what this world is used to get `"split"` and compare it to
// `"split"`. Both ends had an enum; only the wire had prose, so a rename here
// broke every assertion silently and nothing could list the legal values. These
// return the numbers declared in control.proto instead.

/// `ScreenLayout`.
pub const fn screen_code() -> u32 {
    match SCREEN {
        ScreenLayout::Split => dlc_platform_embedded::control::SCREEN_LAYOUT_SPLIT,
        ScreenLayout::Full => dlc_platform_embedded::control::SCREEN_LAYOUT_FULL,
    }
}

/// `InputMode`. Mirrors `BADGE_INPUT`.
pub const fn input_code() -> u32 {
    if cfg!(badge_input_off) {
        dlc_platform_embedded::control::INPUT_MODE_OFF
    } else {
        dlc_platform_embedded::control::INPUT_MODE_KEYBOARD
    }
}

/// `TextOutlet` — the enum platform.proto already declares for this question.
pub const fn text_code() -> u32 {
    if crate::console::APP_ROWS > 0 {
        dlc_platform_embedded::control::TEXT_OUTLET_DISPLAY
    } else {
        dlc_platform_embedded::control::TEXT_OUTLET_UART
    }
}

/// The minimal world: a colour, and nothing else.
pub const CAPABILITIES_MINIMAL: &[Capability] = &[Capability::Status];

/// The normal world: the minimal world, PLUS text.
///
/// Written as an extension rather than as its own list — see the assertion below,
/// which is what keeps that claim true.
pub const CAPABILITIES_NORMAL: &[Capability] = &[Capability::Status, Capability::Text];

/// **THE SUBSET INVARIANT, CHECKED BY THE COMPILER.**
///
/// A comment saying "keep minimal a subset of normal" is a comment somebody
/// eventually does not read. This fails the build instead — reorder
/// `CAPABILITIES_NORMAL`, or drop something from it that minimal still has, and
/// the firmware does not compile.
const _: () = {
    assert!(CAPABILITIES_MINIMAL.len() <= CAPABILITIES_NORMAL.len());
    let mut i = 0;
    while i < CAPABILITIES_MINIMAL.len() {
        // `as u8` because a const fn cannot call `PartialEq` on a slice element.
        assert!(CAPABILITIES_MINIMAL[i] as u8 == CAPABILITIES_NORMAL[i] as u8);
        i += 1;
    }
};

/// Room for the fixed pairs, every capability, and the `ILC_STDOUT=none` note.
/// Sized against the largest world so `advertise` cannot overrun.
pub const ADVERTISEMENT_MAX: usize = 2 + CAPABILITIES_NORMAL.len() + 1;

/// What the badge is showing — the whole vocabulary of the minimal world.
///
/// Four states, because that is what one colour can say without a legend. A
/// richer status is a richer display, and that is Phase 3's problem.
#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Status {
    /// Ran, and the command reported success.
    Ok,
    /// Ran, and the command reported failure. NOT a crash — an app-level error,
    /// which on this badge looks the same and reads differently in the log.
    Failed,
    /// **Alive, with nothing to run.** The empty loader's normal state, and the
    /// reason it is a status rather than a kind of failure: a badge waiting for a
    /// payload has done everything asked of it. Distinguished by BLINKING, which
    /// is the one thing a single GPIO can say that neither on nor off can —
    /// "working" as opposed to "powered" or "dead".
    Idle,
    /// Could not get far enough to have an opinion: instantiation failed, or the
    /// board did not come up.
    Broken,
}

impl Status {
    /// The colour, as RGB565 — the TFT's native format, so Phase 3's fill is a
    /// write rather than a conversion.
    ///
    /// UNUSED UNTIL THE DISPLAY DRIVER EXISTS, and deliberately written now: the
    /// mapping is the part that encodes meaning, and it belongs beside the states
    /// it names rather than inside a driver added later.
    pub const fn rgb565(self) -> u16 {
        match self {
            Status::Ok => 0x07E0,     // green
            Status::Failed => 0xF800, // red
            Status::Idle => 0x001F,   // blue — waiting, not wrong
            Status::Broken => 0xFD20, // amber — distinguishable from red at a glance
        }
    }

    /// Whether this status is shown by BLINKING rather than by a steady light.
    ///
    /// Only [`Status::Idle`], and only because one GPIO has three states rather
    /// than two: off, on, and changing. Spending the third on "alive but
    /// waiting" is what makes an empty loader distinguishable from a board that
    /// did not boot — the single most confusing failure this firmware has, since
    /// its other output needs a serial adapter nobody may have.
    pub const fn blinks(self) -> bool {
        matches!(self, Status::Idle)
    }

    /// What one GPIO can say TODAY.
    ///
    /// The TFT is an 8-bit parallel interface and has no driver yet (Phase 3), so
    /// the minimal world's only real output is the backlight on GPIO26. On is
    /// "fine", off is not — which is a genuine two-state signal rather than a
    /// stand-in for a colour that does not exist yet.
    pub const fn backlight_on(self) -> bool {
        matches!(self, Status::Ok)
    }

    pub const fn name(self) -> &'static str {
        match self {
            Status::Ok => "OK",
            Status::Failed => "FAILED",
            Status::Idle => "IDLE",
            Status::Broken => "BROKEN",
        }
    }
}

/// Turn a command's outcome into what the badge shows.
///
/// The events are read but not yet interpreted: hello emits none, and inventing a
/// topic vocabulary before an app uses one would be guessing at an interface.
/// tictactoe's `StateChangedEvent` is the first real input here.
pub fn status_of(result: &CommandResult) -> Status {
    if result.success {
        Status::Ok
    } else {
        Status::Failed
    }
}
