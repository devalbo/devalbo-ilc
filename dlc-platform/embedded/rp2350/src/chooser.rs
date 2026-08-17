//! Picking one of the app's own choices (WORLD-INPUT-PLAN D3e).
//!
//! # Why the options are not this world's business
//!
//! The character strip offers 30 keys and the spinner offers every integer,
//! because letters and numbers are universal. An enum is not: `ADD SUBTRACT
//! MULTIPLY DIVIDE` means something to a calculator and nothing to a badge, and
//! the next app's enum will be `X O EMPTY` or `RED AMBER GREEN`.
//!
//! So this widget has no vocabulary of its own. It renders `enum_values` from
//! the command spec — the app's own declared names, generated from the app's
//! own `.proto` — and encodes the wire number the app gave them. It is the
//! general case the other two widgets are special cases of: a world offering a
//! surface, an app supplying the meaning.
//!
//! # The layout
//!
//! ```text
//!  what should it do?
//!
//!  add   [subtract]   multiply   divide
//!
//!  A back   C next   B ok
//! ```
//!
//! One row, the selection bracketed. Everything is on screen at once where it
//! fits, so the choice is a choice rather than a sequence of guesses — and what
//! does not fit scrolls, because an app is entitled to more options than a
//! 40-column panel has room for.
//!
//! # Buttons
//!
//! The same three as everywhere else: A back, C next, B confirm. A fourth
//! meaning (the spinner's DOWN for step size) would have nothing to do here, and
//! inventing one per widget is how a badge stops being learnable.
//!
//! # What it refuses to do
//!
//! No wrap-around at the ends. With four options on one screen, wrapping saves
//! one press and costs the ability to tell "I am at the end" from "I have gone
//! round" — and the second is what a person leaning on C needs to know.

use core::fmt::Write;

use dlc_platform_embedded::control;
use embedded_graphics::mono_font::ascii::FONT_8X13;
use embedded_graphics::mono_font::MonoTextStyle;
use embedded_graphics::pixelcolor::Rgb565;
use embedded_graphics::prelude::*;
use embedded_hal::digital::InputPin;

use crate::display::Display;
use crate::keyboard::Keys;

/// Same as the other widgets: long enough to think, restarted by any press.
const TIMEOUT_MS: u32 = 30_000;
const POLL_MS: u32 = 40;

const TOP: i32 = 40;
const LINE_H: i32 = 16;
const CELL_W: i32 = 8;
const COLS: i32 = 40;

const TEXT: Rgb565 = Rgb565::new(31, 63, 31);
const DIM: Rgb565 = Rgb565::new(12, 24, 12);
const HEADING: Rgb565 = Rgb565::new(0, 63, 31);

/// Show the choices and return the WIRE NUMBER of the one picked.
///
/// `spec` is the command-spec buffer and `flag` the field being collected; the
/// choices are read back out of them rather than copied, because an app may
/// declare more of them than a fixed array here would allow.
pub fn read<A, B, C, DOWN>(
    prompt: &str,
    spec: &[u8],
    flag: &dlc_platform_embedded::spec::Flag,
    screen: &mut Option<Display>,
    keys: &mut Keys<A, B, C, DOWN>,
    sys_hz: u32,
    log: &mut impl Write,
) -> Option<i64>
where
    A: InputPin,
    B: InputPin,
    C: InputPin,
    DOWN: InputPin,
{
    use dlc_platform_embedded::spec;

    let count = spec::enum_count(spec, flag);
    if count == 0 {
        // AN ENUM WITH NO DECLARED VALUES is a spec this world cannot act on.
        // Skipping is Decision 33's rule — the app takes its default — and
        // saying so is what stops it looking like the widget failed.
        let _ = writeln!(log, "input: no choices declared, skipping");
        return None;
    }
    // NO SCREEN, NO CHOICE — offering options nobody can see would sit for
    // thirty seconds looking hung.
    if screen.is_none() {
        let _ = writeln!(log, "input: no screen, skipping");
        return None;
    }

    let _ = writeln!(log, "input: {prompt} ({count} choices)");

    // START ON THE DECLARED DEFAULT if the app named one, so confirming
    // immediately means the same as not answering — the same rule the spinner
    // follows.
    let default = spec::default_of(spec, flag);
    let mut active = 0usize;
    if let Some(wanted) = default {
        for index in 0..count {
            if let Some((name, _)) = spec::enum_choice(spec, flag, index) {
                // AGAINST THE SHORT FORM, because that is what an app writes.
                // The spec carries `STYLE_PLAIN` — proto's full value name — and
                // the declared default says `plain`, which is what a person
                // types on a command line. Comparing them whole never matches,
                // so the default would silently be ignored and the first choice
                // would look like the app's preference.
                if short(name).eq_ignore_ascii_case(wanted) || name.eq_ignore_ascii_case(wanted) {
                    active = index;
                    break;
                }
            }
        }
    }

    let mut dirty = true;
    let (mut was_a, mut was_b, mut was_c) = (false, false, false);
    let mut elapsed = 0u32;

    loop {
        if dirty {
            draw(prompt, spec, flag, screen, active, count);
            dirty = false;
        }

        cortex_m::asm::delay(sys_hz / 1000 * POLL_MS);
        crate::usblog::pump();
        // A CONTROL CLIENT HAS ALREADY SUPPLIED THE INPUT (D2).
        if crate::passthrough::waiting() {
            let _ = writeln!(log, "input: superseded by a control request");
            return None;
        }
        elapsed += POLL_MS;

        let a_now = keys.a.is_low().unwrap_or(false) || crate::buttons::taken(control::BUTTON_A);
        let b_now = keys.b.is_low().unwrap_or(false) || crate::buttons::taken(control::BUTTON_B);
        let c_now = keys.c.is_low().unwrap_or(false) || crate::buttons::taken(control::BUTTON_C);

        let (a_press, b_press, c_press) = (a_now && !was_a, b_now && !was_b, c_now && !was_c);
        was_a = a_now;
        was_b = b_now;
        was_c = c_now;

        if a_press || b_press || c_press {
            elapsed = 0;
        }

        if a_press && active > 0 {
            active -= 1;
            dirty = true;
        }
        if c_press && active + 1 < count {
            active += 1;
            dirty = true;
        }
        if b_press {
            break;
        }

        if elapsed >= TIMEOUT_MS {
            // THE SELECTION AS LEFT, for the same reason the spinner keeps its
            // value: somebody who moved to `multiply` and walked away meant
            // multiply more than they meant the default.
            let _ = writeln!(log, "input: timed out");
            break;
        }
    }

    let picked = spec::enum_choice(spec, flag, active);
    if let Some((name, number)) = picked {
        // THE FULL NAME IN THE LOG, the short one on the panel: a log is read
        // later by someone matching it against a .proto, and the prefix is what
        // makes that unambiguous.
        let _ = writeln!(log, "input: {name} = {number}");
    }
    picked.map(|(_, number)| number)
}

fn draw(
    prompt: &str,
    spec: &[u8],
    flag: &dlc_platform_embedded::spec::Flag,
    screen: &mut Option<Display>,
    active: usize,
    count: usize,
) {
    use dlc_platform_embedded::spec;

    let Some(panel) = screen.as_mut() else {
        return;
    };
    panel.fill(0x0000);

    let heading = MonoTextStyle::new(&FONT_8X13, HEADING);
    panel.text(prompt, Point::new(0, TOP - LINE_H), heading);

    // SCROLL TO KEEP THE SELECTION VISIBLE. An app may declare more options than
    // fit; the alternative is a widget that works until the fifth choice and
    // then silently points off the edge of the panel.
    let mut first = 0usize;
    loop {
        let mut x = 0i32;
        let mut fits = false;
        for index in first..count {
            let Some((name, _)) = spec::enum_choice(spec, flag, index) else {
                break;
            };
            let width = (short(name).len() as i32 + 4) * CELL_W;
            if x + width > COLS * CELL_W {
                break;
            }
            x += width;
            if index == active {
                fits = true;
            }
        }
        if fits || first >= active {
            break;
        }
        first += 1;
    }

    let mut x = 0i32;
    for index in first..count {
        let Some((name, _)) = spec::enum_choice(spec, flag, index) else {
            break;
        };
        let name = short(name);
        let width = (name.len() as i32 + 4) * CELL_W;
        if x + width > COLS * CELL_W {
            break;
        }
        // BRACKETED RATHER THAN INVERSE. The character strip fills a single cell
        // to show the selection, which works because every cell is one glyph
        // wide; here the options have different widths and a filled rectangle
        // would need measuring text this module does not otherwise have to do.
        let mut cell = Line::new();
        if index == active {
            let _ = write!(cell, "[{name}]");
        } else {
            let _ = write!(cell, " {name} ");
        }
        let style = if index == active {
            MonoTextStyle::new(&FONT_8X13, TEXT)
        } else {
            MonoTextStyle::new(&FONT_8X13, DIM)
        };
        panel.text(cell.as_str(), Point::new(x, TOP + LINE_H), style);
        x += width;
    }

    let mut hint = Line::new();
    let _ = write!(hint, "A back   C next   B ok   {}/{}", active + 1, count);
    let dim = MonoTextStyle::new(&FONT_8X13, DIM);
    panel.text(hint.as_str(), Point::new(0, TOP + LINE_H * 3), dim);
}

/// The readable half of a proto enum value name.
///
/// `STYLE_ROCKET` is the right identifier on the wire and the wrong thing to put
/// on a 40-column panel beside three others — four values would not fit, and the
/// prefix is the same on every one of them, so it is the part carrying no
/// information at all.
///
/// The ENCODED VALUE is untouched: this changes what a person reads, never what
/// the app receives.
fn short(name: &str) -> &str {
    match name.rfind('_') {
        Some(at) if at + 1 < name.len() => &name[at + 1..],
        _ => name,
    }
}

/// A stack line buffer, because formatting needs somewhere to land and the heap
/// belongs to the guest.
struct Line {
    bytes: [u8; 48],
    len: usize,
}

impl Line {
    fn new() -> Self {
        Self { bytes: [0; 48], len: 0 }
    }

    fn as_str(&self) -> &str {
        core::str::from_utf8(&self.bytes[..self.len]).unwrap_or("")
    }
}

impl Write for Line {
    fn write_str(&mut self, s: &str) -> core::fmt::Result {
        for byte in s.bytes() {
            if self.len == self.bytes.len() {
                break;
            }
            self.bytes[self.len] = byte;
            self.len += 1;
        }
        Ok(())
    }
}
