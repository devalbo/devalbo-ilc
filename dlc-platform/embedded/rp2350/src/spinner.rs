//! Entering a number with three buttons (WORLD-INPUT-PLAN.md D3d).
//!
//! # Why the character strip could not do this
//!
//! It offers 26 letters and no digits, so an `int32` field was not
//! inconvenient — it was uncollectable. `countdown`'s `from` is declared a
//! STRING for exactly that reason, and pays for it with a `strconv.Atoi` and an
//! error path for input it should never have been handed.
//!
//! A number wants a different affordance anyway. Typing `4` as a character is
//! three presses of travel down a 30-cell strip; stepping to it from a default
//! is one.
//!
//! # The layout
//!
//! ```text
//!  count down from this number
//!
//!            5
//!
//!  A -1   C +1   B ok   DOWN step:1
//! ```
//!
//! One line, one number, large. There is nothing else to show: no cursor, no
//! selection, no mode except the step.
//!
//! # Buttons
//!
//! | button | does |
//! | --- | --- |
//! | A | subtract the step |
//! | C | add the step |
//! | B | confirm |
//! | DOWN | cycle the step: 1, 10, 100 |
//!
//! **The step is what buys us out of bounds.** Without it, reaching 250 from 5
//! is 245 presses and the only fix is a `max` field option — new schema, for an
//! affordance. With it, 250 is three presses at step 100 and five at step 10.
//! Bounds may still be worth adding one day; they are no longer the only way to
//! make large numbers reachable (D3d).
//!
//! # Bool is the same widget
//!
//! Two positions instead of a range, rendered as `false` / `true`. A separate
//! widget would be a second interaction to learn for a strictly simpler case.

use core::fmt::Write;

use embedded_graphics::mono_font::ascii::{FONT_10X20, FONT_8X13};
use embedded_graphics::mono_font::MonoTextStyle;
use embedded_graphics::pixelcolor::Rgb565;
use embedded_graphics::prelude::*;
use embedded_hal::digital::InputPin;

use dlc_platform_embedded::control;

use crate::display::Display;
use crate::keyboard::Keys;

/// Same as the character strip: long enough to think, restarted by any press.
const TIMEOUT_MS: u32 = 30_000;
const POLL_MS: u32 = 40;

const TOP: i32 = 40;
const LINE_H: i32 = 16;

const TEXT: Rgb565 = Rgb565::new(31, 63, 31);
const DIM: Rgb565 = Rgb565::new(12, 24, 12);
const HEADING: Rgb565 = Rgb565::new(0, 63, 31);

/// The steps, cycled by DOWN. Powers of ten because that is how people think
/// about magnitude, and three of them because a fourth is rarely worth the
/// press it costs to reach.
const STEPS: [i64; 3] = [1, 10, 100];

/// What kind of number is being entered.
#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Shape {
    /// A signed or unsigned integer.
    Integer,
    /// Two positions, rendered as `false` and `true`.
    Boolean,
}

/// Show the spinner and return the value.
///
/// Starts from `start` — the field's declared default, so the first thing shown
/// is what the app would have used anyway, and confirming immediately is the
/// same as not answering.
pub fn read<A, B, C, DOWN>(
    prompt: &str,
    shape: Shape,
    start: i64,
    screen: &mut Option<Display>,
    keys: &mut Keys<A, B, C, DOWN>,
    sys_hz: u32,
    log: &mut impl Write,
) -> i64
where
    A: InputPin,
    B: InputPin,
    C: InputPin,
    DOWN: InputPin,
{
    // NO SCREEN, NO PROMPT — asking for a value nobody can see would sit for
    // thirty seconds looking hung.
    if screen.is_none() {
        let _ = writeln!(log, "input: no screen, skipping");
        return start;
    }

    let mut value = match shape {
        Shape::Boolean => start.clamp(0, 1),
        Shape::Integer => start,
    };
    let _ = writeln!(log, "input: {prompt} (number, from {value})");

    let mut step_index = 0usize;
    let mut dirty = true;
    let (mut was_a, mut was_b, mut was_c, mut was_down) = (false, false, false, false);
    let mut elapsed = 0u32;

    loop {
        if dirty {
            draw(prompt, screen, value, shape, STEPS[step_index]);
            dirty = false;
        }

        cortex_m::asm::delay(sys_hz / 1000 * POLL_MS);
        // SERVICE USB WHILE WAITING. The interrupt only fires on bus activity,
        // and a host reading an idle port produces almost none — so a heartbeat
        // driven by the interrupt alone never beats while a widget is waiting,
        // which is most of the time a person is looking at the badge.
        crate::usblog::pump();
        // A CONTROL CLIENT HAS ALREADY SUPPLIED THE INPUT, so asking a person for
        // it is a thirty-second wait for something nobody is going to type. What
        // is collected here is discarded either way — the turn runs on the
        // client's bytes (D2).
        if crate::passthrough::waiting() {
            let _ = writeln!(log, "input: superseded by a control request");
            break;
        }
        elapsed += POLL_MS;

        // ACTIVE-LOW, and a failed read counts as not pressed — a stuck line
        // must not spin a value nobody is touching.
        // OR THE CONTROL CHANNEL. A press consumed here is indistinguishable
        // from a finger, which is D6's rule (see buttons.rs).
        let a_now = keys.a.is_low().unwrap_or(false) || crate::buttons::taken(control::BUTTON_A);
        let b_now = keys.b.is_low().unwrap_or(false) || crate::buttons::taken(control::BUTTON_B);
        let c_now = keys.c.is_low().unwrap_or(false) || crate::buttons::taken(control::BUTTON_C);
        let down_now = keys.down.is_low().unwrap_or(false) || crate::buttons::taken(control::BUTTON_DOWN);

        let (a_press, b_press, c_press, down_press) = (
            a_now && !was_a,
            b_now && !was_b,
            c_now && !was_c,
            down_now && !was_down,
        );
        was_a = a_now;
        was_b = b_now;
        was_c = c_now;
        was_down = down_now;

        if a_press || b_press || c_press || down_press {
            elapsed = 0;
        }

        let step = STEPS[step_index];
        match shape {
            Shape::Boolean => {
                // EITHER DIRECTION TOGGLES. With two positions there is no
                // meaningful up or down, and making A and C do the same thing is
                // less surprising than making one of them do nothing.
                if a_press || c_press {
                    value = 1 - value;
                    dirty = true;
                }
            }
            Shape::Integer => {
                if a_press {
                    value = value.saturating_sub(step);
                    dirty = true;
                }
                if c_press {
                    value = value.saturating_add(step);
                    dirty = true;
                }
            }
        }

        if down_press && shape == Shape::Integer {
            step_index = (step_index + 1) % STEPS.len();
            dirty = true;
        }

        if b_press {
            break;
        }

        if elapsed >= TIMEOUT_MS {
            // THE VALUE AS LEFT, not the default. Somebody who spun to 12 and
            // walked away meant 12 more than they meant the default, and there
            // is no way to ask.
            let _ = writeln!(log, "input: timed out");
            break;
        }
    }

    let _ = writeln!(log, "input: {value}");
    value
}

fn draw(prompt: &str, screen: &mut Option<Display>, value: i64, shape: Shape, step: i64) {
    let Some(panel) = screen.as_mut() else {
        return;
    };
    panel.fill(0x0000);

    let heading = MonoTextStyle::new(&FONT_8X13, HEADING);
    panel.text(prompt, Point::new(0, TOP - LINE_H), heading);

    // THE VALUE, in the larger font. It is the only thing on this screen worth
    // reading from arm's length, and the panel has room.
    let mut shown = Line::new();
    match shape {
        Shape::Boolean => {
            let _ = write!(shown, "{}", if value != 0 { "true" } else { "false" });
        }
        Shape::Integer => {
            let _ = write!(shown, "{value}");
        }
    }
    let big = MonoTextStyle::new(&FONT_10X20, TEXT);
    panel.text(shown.as_str(), Point::new(0, TOP + LINE_H), big);

    let mut hint = Line::new();
    match shape {
        Shape::Boolean => {
            let _ = write!(hint, "A/C toggle   B ok");
        }
        Shape::Integer => {
            let _ = write!(hint, "A -{step}   C +{step}   B ok   DOWN step");
        }
    }
    let dim = MonoTextStyle::new(&FONT_8X13, DIM);
    panel.text(hint.as_str(), Point::new(0, TOP + LINE_H * 3), dim);
}

/// A stack line buffer, because formatting needs somewhere to land and this runs
/// where the heap belongs to the guest.
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
