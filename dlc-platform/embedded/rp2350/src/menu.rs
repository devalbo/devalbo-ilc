//! Pick an app, with the five buttons.
//!
//! # When this appears, and when it does not
//!
//! **Only when there is a choice.** One payload is not a menu, it is a delay —
//! so the badge runs it. A menu that appears for a single option teaches people
//! to dismiss menus without reading them.
//!
//! # It always ends, and that is the important part
//!
//! A badge is worn, sat on, and powered up in a bag. **The selection times out
//! and runs the highlighted entry**, so a board nobody is touching still boots
//! into something. A menu that waits forever for input is how a wearable becomes
//! a brick with a nice font.
//!
//! # GUARD ONE OF TWO: what can be CHOSEN
//!
//! Corrupt payloads are **listed and not selectable**. Listing them matters — a
//! broken file that vanishes is indistinguishable from one never installed, and
//! sends someone to re-drag a payload that is already there. Making them
//! unselectable matters because the alternative is letting a user pick a thing
//! that cannot work.
//!
//! **This is not the launch guard.** `main.rs` re-checks before instantiating,
//! because a payload can be chosen without ever passing through here: a single
//! app skips the menu, a timeout picks without a press, and a baked-in payload
//! never touches the region. A guard that assumes an earlier guard ran is not a
//! guard.
//!
//! # Button polarity: ACTIVE-LOW, and this was measured on the board
//!
//! An earlier revision asserted the opposite — "active-HIGH with external
//! pull-downs" — and would have read every button as permanently held, so the
//! menu would have confirmed instantly and launched slot 0 before anyone saw it.
//!
//! **Measured 2026-08-16, before this code ever ran**, by asking Pimoroni's own
//! MicroPython on the device rather than reading a pinout: it configures the
//! buttons `Pin(GPIO7, mode=IN, pull=PULL_UP)`, and sampling them gave `1` at
//! idle and `0` with A held. So the buttons pull to ground and the pins want
//! internal pull-UPs.
//!
//! Worth repeating as a technique: the shipped firmware is a working reference
//! implementation for the same hardware, and it can be interrogated over the
//! USB REPL in less time than it takes to find the schematic.

use core::fmt::Write;

use embedded_graphics::mono_font::ascii::FONT_8X13;
use embedded_graphics::mono_font::MonoTextStyle;
use embedded_graphics::pixelcolor::Rgb565;
use embedded_graphics::prelude::*;
use embedded_graphics::text::Text;

use embedded_hal::digital::InputPin;

use dlc_platform_embedded::catalog::Payload;
use dlc_platform_embedded::names;

use crate::board;
use crate::display::Display;
use crate::payload::Payloads;

/// How long the menu waits before running the highlighted entry.
///
/// Long enough to read five names and press a button; short enough that a badge
/// in a bag is running its app within seconds.
const TIMEOUT_MS: u32 = 8_000;

/// How often buttons are sampled. Also the debounce: a mechanical button bounces
/// for a few milliseconds, and sampling slower than that is the cheapest debounce
/// there is — no timers, no state machine.
const POLL_MS: u32 = 40;

const LINE_H: i32 = 16;
const TOP: i32 = 40;

const SELECTED: Rgb565 = Rgb565::new(31, 63, 31);
const UNSELECTED: Rgb565 = Rgb565::new(12, 24, 12);
const HEADING: Rgb565 = Rgb565::new(0, 63, 31);
const DIM: Rgb565 = Rgb565::new(10, 20, 10);
/// Corrupt entries are GRAYED OUT, not reddened.
///
/// Gray is the accurate signal: the entry is *disabled*, and red would say
/// *error* — which is a different claim and the one people react to. Nothing is
/// going wrong when a broken file sits in the list; it simply cannot be picked.
///
/// Deliberately dimmer than [`UNSELECTED`] rather than a different hue, so
/// "greyed out" reads as unavailable at a glance and does not depend on anyone
/// distinguishing two similar colours.
const DISABLED: Rgb565 = Rgb565::new(6, 12, 6);

/// The five buttons, as inputs.
pub struct Buttons<UP, DOWN, A> {
    pub up: UP,
    pub down: DOWN,
    pub a: A,
}

/// Show the list and return the chosen index.
///
/// Returns immediately with `0` when there is nothing to choose between, so the
/// caller does not need to know whether a menu happened.
pub fn choose<UP, DOWN, A>(
    payloads: &Payloads,
    screen: &mut Option<Display>,
    buttons: &mut Buttons<UP, DOWN, A>,
    sys_hz: u32,
    log: &mut impl Write,
) -> usize
where
    UP: InputPin,
    DOWN: InputPin,
    A: InputPin,
{
    // One app is not a choice. Nor is a badge with no screen, where a menu would
    // be an invisible prompt for a button nobody knows to press.
    if payloads.len() < 2 || screen.is_none() {
        return 0;
    }

    let _ = writeln!(log, "menu: {} apps, {} ms to choose", payloads.len(), TIMEOUT_MS);

    // Start on the MARKED default, so the app that runs when the timeout expires
    // is the one somebody chose — not whichever entry happened to be written
    // first. Falls back to the first runnable entry, which is also what an image
    // built before the flag existed gets.
    let mut selected = payloads.default_choice().map(|(i, _)| i).unwrap_or(0);
    let mut dirty = true;
    // Edge detection: without it, one physical press moves the highlight for as
    // long as a finger rests on the button.
    let (mut was_up, mut was_down) = (false, false);
    let mut elapsed = 0u32;

    loop {
        if dirty {
            draw(payloads, screen, selected, elapsed);
            dirty = false;
        }

        cortex_m::asm::delay(sys_hz / 1000 * POLL_MS);
        // SERVICE USB WHILE WAITING. The interrupt only fires on bus activity,
        // and a host reading an idle port produces almost none — so a heartbeat
        // driven by the interrupt alone never beats while a widget is waiting,
        // which is most of the time a person is looking at the badge.
        crate::usblog::pump();
        elapsed += POLL_MS;

        // ACTIVE-LOW: pulled up, and a press shorts to ground. `unwrap_or(false)`
        // stays "not pressed" on an error, which is the safe default here — a
        // stuck read must not confirm a selection.
        let up = buttons.up.is_low().unwrap_or(false);
        let down = buttons.down.is_low().unwrap_or(false);
        let confirm = buttons.a.is_low().unwrap_or(false);

        if confirm {
            // GUARD ONE. Refuse rather than launch; the highlight should never be
            // on a corrupt entry, and if it somehow is, this is where that stops.
            if payloads.get(selected).map(|p| p.runnable()) == Some(false) {
                let _ = writeln!(log, "menu: {selected} is corrupt, refusing");
                continue;
            }
            let _ = writeln!(log, "menu: selected {selected}");
            return selected;
        }

        // ANY press restarts the clock: someone is clearly here, and timing out
        // under an active finger is the rudest possible behaviour.
        if up || down {
            elapsed = 0;
        }

        // Navigation SKIPS corrupt entries: they are visible, and the highlight
        // does not stop on them.
        if up && !was_up {
            selected = step(payloads, selected, -1);
            dirty = true;
        }
        if down && !was_down {
            selected = step(payloads, selected, 1);
            dirty = true;
        }
        was_up = up;
        was_down = down;

        // Redraw about once a second so the countdown moves without repainting
        // the whole list every 40 ms on a bit-banged bus.
        if elapsed % 1000 < POLL_MS {
            dirty = true;
        }

        if elapsed >= TIMEOUT_MS {
            let _ = writeln!(log, "menu: timed out, running {selected}");
            return selected;
        }
    }
}

/// Move the highlight, skipping anything corrupt and wrapping around.
///
/// Bounded by the list length so a catalog of nothing but corrupt entries cannot
/// spin forever — it simply does not move.
fn step(payloads: &Payloads, from: usize, direction: isize) -> usize {
    let len = payloads.len();
    let mut index = from;
    for _ in 0..len {
        index = if direction < 0 {
            if index == 0 { len - 1 } else { index - 1 }
        } else {
            (index + 1) % len
        };
        if payloads.get(index).map(|p| p.runnable()).unwrap_or(false) {
            return index;
        }
    }
    from
}

fn draw(payloads: &Payloads, screen: &mut Option<Display>, selected: usize, elapsed: u32) {
    let Some(panel) = screen.as_mut() else {
        return;
    };
    panel.fill(0x0000);

    text(panel, 0, 16, "choose an app", HEADING);

    for (index, found) in payloads.iter().enumerate() {
        let y = TOP + index as i32 * LINE_H;
        let corrupt = !found.runnable();
        // Marked so the timeout's behaviour is visible BEFORE it fires.
        let is_default = found.is_default();
        let (color, marker) = if corrupt {
            // Shown, never highlighted, and dimmed. The `x` carries the meaning
            // for anyone who cannot rely on the colour.
            (DISABLED, "x")
        } else if index == selected {
            (SELECTED, ">")
        } else {
            (UNSELECTED, " ")
        };
        let mut line = Line::new();
        // COLUMNS, not parentheses. Sizes are the reason to look at this list —
        // "will it fit" and "which of these is the big one" — and left-aligned
        // sizes after a variable-length name cannot be compared by eye. The name
        // is truncated rather than allowed to push the number out of line.
        // THE NAME IS THE FILESYSTEM'S NAME. Someone who mounts the badge over
        // USB sees `HELLO.CWA`; a menu calling the same payload `hello` would be
        // a second name for one thing. Safe as an identifier because the build
        // refuses a catalog whose short names collide.
        let mut filename = [0u8; 12];
        let shown = names::display_filename(found.name, &mut filename);
        if corrupt {
            // The WORD as well as the colour: a status told only in colour is a
            // status half the people looking at it cannot read.
            let _ = write!(line, "{marker} {shown:<14.14}  CORRUPT");
        } else {
            let _ = write!(
                line,
                "{marker} {shown:<14.14}{:>5} KB  {}",
                found.bytes.len() / 1024,
                if is_default { "*" } else { " " }
            );
        }
        text(panel, 0, y, line.as_str(), color);
    }

    // WHAT IS LEFT, which is the other half of a size being useful. A payload
    // region filling up has no other symptom until a drag silently does nothing.
    let used: usize = payloads.iter().map(|p| p.bytes.len()).sum();
    let mut usage = Line::new();
    let _ = write!(
        usage,
        "{} apps  {} of {} KB used",
        payloads.len(),
        used / 1024,
        board::PAYLOAD_LEN / 1024
    );
    text(panel, 0, 208, usage.as_str(), DIM);

    let remaining = TIMEOUT_MS.saturating_sub(elapsed) / 1000;
    let mut footer = Line::new();
    let _ = write!(footer, "UP/DOWN  A=run   * auto in {remaining}s");
    text(panel, 0, 224, footer.as_str(), DIM);
}

fn text(panel: &mut Display, x: i32, y: i32, s: &str, color: Rgb565) {
    let style = MonoTextStyle::new(&FONT_8X13, color);
    let _ = Text::new(s, Point::new(x, y), style).draw(panel.target());
}

/// A stack line buffer — the menu runs before anything should be allocating, and
/// on a path where a panic is a board that stops with no explanation.
struct Line {
    bytes: [u8; 40],
    len: usize,
}

impl Line {
    fn new() -> Self {
        Self {
            bytes: [0; 40],
            len: 0,
        }
    }
    fn as_str(&self) -> &str {
        core::str::from_utf8(&self.bytes[..self.len]).unwrap_or("")
    }
}

impl Write for Line {
    fn write_str(&mut self, s: &str) -> core::fmt::Result {
        for byte in s.bytes() {
            if self.len == self.bytes.len() {
                break; // Truncate; never panic.
            }
            self.bytes[self.len] = if (0x20..0x7f).contains(&byte) { byte } else { b'.' };
            self.len += 1;
        }
        Ok(())
    }
}

/// Unused today, kept honest: the menu shows what `discover` found, so a payload
/// that cannot be named cannot be chosen.
#[allow(dead_code)]
fn _assert_payload_has_a_name(p: Payload) -> &'static str {
    p.name
}
