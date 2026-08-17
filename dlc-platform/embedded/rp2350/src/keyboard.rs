//! Typing a string with three buttons (WORLD-INPUT-PLAN.md D3).
//!
//! # Why a keyboard rather than a list of names
//!
//! The first design mapped A/B/C to Alice/Bob/Charlie. It suited exactly one
//! app — a filename or a port number would have got three answers that make no
//! sense — and it left the badge as the only tier that could not express an
//! arbitrary value. A character picker costs two rows and makes every string
//! input reachable.
//!
//! # The layout
//!
//! ```text
//!  Alice_                             [SHIFT->ABC]
//!  abcdefghijklmnopqrstuvwxyz^_<#
//!                         ^ active, drawn inverse
//! ```
//!
//! One column per key, so all 30 fit in 30 of the 40 columns. The specials are
//! single characters — `^` shift, `_` space, `<` backspace, `#` enter — which
//! are unreadable on their own, and that is what the label at the right of row 1
//! is for: it names whatever is selected, so a symbol is ambiguous only until
//! you land on it.
//!
//! SHIFT IS A MODE and the strip shows it: press `^` and the letters become
//! `ABCDEFG...`. With three buttons there is no modifier to hold, so a one-shot
//! shift would be invisible state to remember between two presses; a mode is
//! legible in the row you are already looking at.
//!
//! The active key is drawn INVERSE rather than marked from below. A marker costs
//! a third row, and this panel is already shared with the app.
//!
//! # Buttons
//!
//! | button | does |
//! | --- | --- |
//! | A | move left, wrapping |
//! | C | move right, wrapping |
//! | B | select the active key |
//! | DOWN | hide / show the whole section |
//!
//! `^` toggles case, `<` deletes the last character, `#` submits.
//!
//! Wrapping matters more than it sounds: with hard ends, reaching `z` from `a`
//! is 25 presses. Wrapping makes it one.
//!
//! DOWN HIDES RATHER THAN CANCELS — the buffer survives, so hiding is a way to
//! see what is underneath and come back, not a way to lose what you typed.
//! There is deliberately no cancel: a missing input is not an error (D5), so
//! cancelling and timing out would mean the same thing, and one of them would be
//! a button nobody needed.
//!
//! # This is a WORLD component
//!
//! It lives beside [`crate::menu`], which does the same job for a different
//! question: draw something, read the measured buttons, time out sensibly, hand
//! back a value. An app never sees it and cannot tell it existed — the value
//! arrives as an ordinary request field, exactly as it would from a terminal.

use core::fmt::Write;

use embedded_graphics::mono_font::ascii::FONT_8X13;
use embedded_graphics::mono_font::MonoTextStyle;
use embedded_graphics::pixelcolor::Rgb565;
use embedded_graphics::prelude::*;
use embedded_graphics::primitives::{PrimitiveStyle, Rectangle};
use embedded_graphics::text::Text;
use embedded_hal::digital::InputPin;

use crate::display::Display;

/// How long to wait with nobody pressing anything before giving up.
///
/// Longer than the menu's 8 s because typing is slower than choosing, and a
/// person mid-word should not lose it to a timer. On expiry the buffer is
/// returned AS TYPED rather than discarded — a partial name is closer to what
/// somebody wanted than no name at all.
const TIMEOUT_MS: u32 = 30_000;

/// Sampling interval, and the debounce. Same reasoning as the menu: a mechanical
/// button bounces for a few milliseconds and sampling slower than that is the
/// cheapest debounce there is.
const POLL_MS: u32 = 40;

/// The longest string this can produce.
///
/// A fixed buffer because the heap belongs to the guest — it holds 2.9 MB of
/// component and this runs before that is safe to disturb. 32 also happens to be
/// `catalog::NAME_MAX`, which is the other place a person types a string here.
pub const MAX_LEN: usize = 32;

const CELL_W: i32 = 8;
const LINE_H: i32 = 16;
const TOP: i32 = 40;

const TEXT: Rgb565 = Rgb565::new(31, 63, 31);
const DIM: Rgb565 = Rgb565::new(12, 24, 12);
const HEADING: Rgb565 = Rgb565::new(0, 63, 31);
/// The active cell's background. Text on it is drawn black.
const ACTIVE_BG: Rgb565 = Rgb565::new(31, 63, 31);
const ACTIVE_FG: Rgb565 = Rgb565::new(0, 0, 0);

/// What a key does when selected.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Key {
    Char(u8),
    Space,
    Backspace,
    Enter,
    /// Toggles case for the letters. See [`Key::glyph`] on why it is a MODE
    /// rather than a one-shot.
    Shift,
}

impl Key {
    /// The single character drawn in the strip.
    ///
    /// A MODE, NOT A ONE-SHOT. Shift toggles and stays, so the STRIP ITSELF
    /// changes case — press `^` and `abc...` becomes `ABC...`. That is the whole
    /// affordance: with three buttons and no modifier to hold, a one-shot shift
    /// is invisible state that someone has to remember between two presses,
    /// while a mode is legible from across the room in the row you are already
    /// reading.
    ///
    /// The cost is a second press to go back, which is the right trade for names
    /// — capitalise once at the start and never again.
    fn glyph(self, shift: bool) -> char {
        match self {
            Key::Char(c) if shift => c.to_ascii_uppercase() as char,
            Key::Char(c) => c as char,
            Key::Space => '_',
            Key::Backspace => '<',
            Key::Enter => '#',
            Key::Shift => '^',
        }
    }

    /// What row 1 calls it. This is what makes the symbols legible.
    fn label(self, shift: bool) -> &'static str {
        match self {
            Key::Char(_) => "",
            Key::Space => "[SP]",
            Key::Backspace => "[BKSP]",
            Key::Enter => "[ENTER]",
            // Names the STATE it will move to, not the state it is in — a label
            // reading "[SHIFT ON]" while shift is already on is the classic
            // toggle ambiguity, and it is the one people misread.
            Key::Shift if shift => "[shift->abc]",
            Key::Shift => "[SHIFT->ABC]",
        }
    }
}

/// 26 letters, then the specials. Order is the reading order.
///
/// SHIFT SITS BESIDE THE LETTERS rather than at the far end, because it is the
/// key most often wanted right before one of them — capitalising the first
/// letter of a name is the whole use case, and putting it 4 columns from `a`
/// costs less travel than putting it after `enter`.
const KEYS: [Key; 30] = {
    let mut keys = [Key::Enter; 30];
    let mut i = 0;
    while i < 26 {
        keys[i] = Key::Char(b'a' + i as u8);
        i += 1;
    }
    keys[26] = Key::Shift;
    keys[27] = Key::Space;
    keys[28] = Key::Backspace;
    keys[29] = Key::Enter;
    keys
};

/// The three buttons this uses, as inputs.
///
/// Separate from [`crate::menu::Buttons`] because they are different buttons for
/// different jobs, and one struct carrying five pins would let a caller pass the
/// menu's set here and have it silently half-work.
pub struct Keys<A, B, C, DOWN> {
    pub a: A,
    pub b: B,
    pub c: C,
    pub down: DOWN,
}

/// A typed string, in a fixed buffer.
pub struct Typed {
    bytes: [u8; MAX_LEN],
    len: usize,
}

impl Typed {
    pub fn as_str(&self) -> &str {
        // Every byte came from `Key::Char` (ASCII) or a space, so this cannot
        // fail; `unwrap_or` keeps a panic out of the firmware regardless.
        core::str::from_utf8(&self.bytes[..self.len]).unwrap_or("")
    }

    pub fn is_empty(&self) -> bool {
        self.len == 0
    }
}

/// Show the picker and return what was typed.
///
/// Returns an EMPTY value rather than an error when there is no screen, when the
/// timeout expires with nothing entered, or when a person presses enter on an
/// empty buffer. All three mean the same thing to a caller — no input was given
/// — and the app takes its default, which is a no-op rather than a failure
/// (Decision 33 / D5).
pub fn read<A, B, C, DOWN>(
    prompt: &str,
    screen: &mut Option<Display>,
    keys: &mut Keys<A, B, C, DOWN>,
    sys_hz: u32,
    log: &mut impl Write,
) -> Typed
where
    A: InputPin,
    B: InputPin,
    C: InputPin,
    DOWN: InputPin,
{
    let mut typed = Typed {
        bytes: [0; MAX_LEN],
        len: 0,
    };

    // NO SCREEN, NO PROMPT. Asking for input a person cannot see is worse than
    // not asking: the badge would sit for 30 seconds looking hung.
    if screen.is_none() {
        let _ = writeln!(log, "input: no screen, skipping");
        return typed;
    }

    let _ = writeln!(log, "input: {prompt} ({TIMEOUT_MS} ms)");

    let mut active = 0usize;
    let mut shift = false;
    let mut visible = true;
    let mut dirty = true;
    // Edge detection: without it, one physical press repeats for as long as a
    // finger rests on the button.
    let (mut was_a, mut was_b, mut was_c, mut was_down) = (false, false, false, false);
    let mut elapsed = 0u32;

    loop {
        if dirty {
            draw(prompt, screen, &typed, active, visible, shift);
            dirty = false;
        }

        cortex_m::asm::delay(sys_hz / 1000 * POLL_MS);
        elapsed += POLL_MS;

        // ACTIVE-LOW: pulled up, a press shorts to ground. `unwrap_or(false)`
        // reads a failed sample as "not pressed", which is the safe direction —
        // a stuck read must not type characters nobody asked for.
        let a_now = keys.a.is_low().unwrap_or(false);
        let b_now = keys.b.is_low().unwrap_or(false);
        let c_now = keys.c.is_low().unwrap_or(false);
        let down_now = keys.down.is_low().unwrap_or(false);

        let a_press = a_now && !was_a;
        let b_press = b_now && !was_b;
        let c_press = c_now && !was_c;
        let down_press = down_now && !was_down;
        was_a = a_now;
        was_b = b_now;
        was_c = c_now;
        was_down = down_now;

        // ANY PRESS RESTARTS THE CLOCK. Someone typing is present, and timing
        // out mid-word because the total exceeded 30 s would be the timer
        // punishing the one case it exists to serve.
        if a_press || b_press || c_press || down_press {
            elapsed = 0;
        }

        if down_press {
            visible = !visible;
            dirty = true;
            continue;
        }

        // HIDDEN MEANS HIDDEN. Moving or selecting while the strip is invisible
        // would change state nobody can see, and the buffer would come back
        // different from how it was left.
        if !visible {
            if elapsed >= TIMEOUT_MS {
                break;
            }
            continue;
        }

        if a_press {
            active = if active == 0 { KEYS.len() - 1 } else { active - 1 };
            dirty = true;
        }
        if c_press {
            active = (active + 1) % KEYS.len();
            dirty = true;
        }
        if b_press {
            match KEYS[active] {
                Key::Enter => break,
                Key::Backspace => {
                    typed.len = typed.len.saturating_sub(1);
                }
                Key::Space => push(&mut typed, b' '),
                Key::Shift => shift = !shift,
                Key::Char(c) if shift => push(&mut typed, c.to_ascii_uppercase()),
                Key::Char(c) => push(&mut typed, c),
            }
            dirty = true;
        }

        if elapsed >= TIMEOUT_MS {
            // AS TYPED, not discarded. A partial name is closer to what somebody
            // wanted than nothing, and there is no way to ask them.
            let _ = writeln!(log, "input: timed out");
            break;
        }
    }

    let _ = writeln!(log, "input: {:?}", typed.as_str());
    typed
}

/// Append, silently ignoring a full buffer.
///
/// Refusing beyond MAX_LEN rather than wrapping or truncating from the front: a
/// person watching the row sees the character not appear, which is a clearer
/// signal than text shifting under them.
fn push(typed: &mut Typed, byte: u8) {
    if typed.len < MAX_LEN {
        typed.bytes[typed.len] = byte;
        typed.len += 1;
    }
}

fn draw(
    prompt: &str,
    screen: &mut Option<Display>,
    typed: &Typed,
    active: usize,
    visible: bool,
    shift: bool,
) {
    let Some(panel) = screen.as_mut() else {
        return;
    };
    panel.fill(0x0000);

    let heading = MonoTextStyle::new(&FONT_8X13, HEADING);
    let _ = Text::new(prompt, Point::new(0, TOP - LINE_H), heading).draw(panel.target());

    if !visible {
        // The buffer is still there; only the section is hidden. Saying so is
        // what stops "hidden" reading as "gone" — the difference matters because
        // the whole point of hiding is that you can come back to it.
        let dim = MonoTextStyle::new(&FONT_8X13, DIM);
        let _ = Text::new("(input hidden - DOWN to show)", Point::new(0, TOP), dim).draw(panel.target());
        return;
    }

    // Row 1: what has been typed, with a cursor, and the active key's name.
    let style = MonoTextStyle::new(&FONT_8X13, TEXT);
    let _ = Text::new(typed.as_str(), Point::new(0, TOP), style).draw(panel.target());
    let cursor_x = typed.len as i32 * CELL_W;
    let _ = Text::new("_", Point::new(cursor_x, TOP), style).draw(panel.target());

    let label = KEYS[active].label(shift);
    if !label.is_empty() {
        // Right-aligned, so it does not collide with a long buffer.
        let x = (40 - label.len() as i32) * CELL_W;
        let _ = Text::new(label, Point::new(x, TOP), MonoTextStyle::new(&FONT_8X13, HEADING))
            .draw(panel.target());
    }

    // Row 2: the strip, one column per key.
    let y = TOP + LINE_H;
    for (index, key) in KEYS.iter().enumerate() {
        let x = index as i32 * CELL_W;
        let mut glyph = [0u8; 4];
        let text = key.glyph(shift).encode_utf8(&mut glyph);
        if index == active {
            // INVERSE: fill the cell, knock the glyph out in black. The rectangle
            // is drawn from the text baseline upward, which is why it starts at
            // `y - LINE_H + 3` rather than at `y`.
            let cell = Rectangle::new(
                Point::new(x, y - LINE_H + 3),
                Size::new(CELL_W as u32, LINE_H as u32),
            );
            let _ = cell
                .into_styled(PrimitiveStyle::with_fill(ACTIVE_BG))
                .draw(panel.target());
            let _ = Text::new(text, Point::new(x, y), MonoTextStyle::new(&FONT_8X13, ACTIVE_FG))
                .draw(panel.target());
        } else {
            let _ = Text::new(text, Point::new(x, y), MonoTextStyle::new(&FONT_8X13, DIM))
                .draw(panel.target());
        }
    }

    let hint = MonoTextStyle::new(&FONT_8X13, DIM);
    let _ = Text::new(
        "A/C move   B select   DOWN hide",
        Point::new(0, y + LINE_H + 4),
        hint,
    )
    .draw(panel.target());
}
