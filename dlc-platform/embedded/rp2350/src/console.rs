//! The app's output, on the badge's screen — what `normal` actually means.
//!
//! # The bug this fixes
//!
//! The bring-up report already drew a line of app text, and then `show()` flooded
//! the panel with the status colour and **erased it**. Output was visible for
//! about a second and then gone, which is worse than never showing it: it looks
//! like the badge lost the result.
//!
//! So the final frame differs by world, and that difference is the whole point of
//! the split:
//!
//! | | final screen |
//! | --- | --- |
//! | `normal` | the app's text, with a thin status BAR |
//! | `minimal` | the status colour, full screen, no text |
//!
//! A bar rather than a border because a border steals four edges' worth of
//! columns from a 40-column display to say one bit.
//!
//! # Why the status has to survive alongside the text
//!
//! The colour is readable across a room and the text is not. Keeping a band of it
//! means the badge answers "did it work?" at a glance and "what did it say?" up
//! close — which is the same information at two distances, not two features.

use embedded_graphics::mono_font::ascii::FONT_8X13;
use embedded_graphics::mono_font::MonoTextStyle;
use embedded_graphics::pixelcolor::raw::RawU16;
use embedded_graphics::pixelcolor::Rgb565;
use embedded_graphics::prelude::*;
use embedded_graphics::primitives::{PrimitiveStyle, Rectangle};
use embedded_graphics::text::Text;

use crate::display::{Display, HEIGHT, WIDTH};
use crate::world::Status;

/// Characters across at `FONT_8X13`.
const COLS: usize = 40;
const LINE_H: i32 = 14;
/// Height of the status band at the top.
const BAR_H: u32 = 18;
const TEXT_TOP: i32 = BAR_H as i32 + 14;

const TEXT_COLOR: Rgb565 = Rgb565::new(31, 63, 31);
const BAR_TEXT: Rgb565 = Rgb565::new(0, 0, 0);

/// How many lines fit under the bar.
const ROWS: usize = ((HEIGHT as i32 - TEXT_TOP) / LINE_H) as usize;

/// Draw the app's output with a status bar above it.
///
/// Truncates rather than scrolls: this is the LAST frame, drawn once after the
/// command returns, so there is nothing to scroll toward. An app whose output
/// does not fit is telling the badge something the badge cannot show, and the
/// UART still has all of it.
pub fn render(panel: &mut Display, status: Status, label: &str, body: &str) {
    panel.fill(0x0000);

    // The status band. Full width, so the colour is the first thing seen.
    let bar = Rectangle::new(Point::zero(), Size::new(WIDTH as u32, BAR_H));
    let fill = PrimitiveStyle::with_fill(Rgb565::from(RawU16::new(status.rgb565())));
    let _ = bar.into_styled(fill).draw(panel.target());

    // Black on the status colour: every one of them is bright enough that black
    // reads and white does not.
    let style = MonoTextStyle::new(&FONT_8X13, BAR_TEXT);
    let _ = Text::new(label, Point::new(4, 13), style).draw(panel.target());

    let style = MonoTextStyle::new(&FONT_8X13, TEXT_COLOR);
    let mut row = 0usize;
    for line in wrap(body) {
        if row >= ROWS {
            break;
        }
        let y = TEXT_TOP + row as i32 * LINE_H;
        let _ = Text::new(line, Point::new(0, y), style).draw(panel.target());
        row += 1;
    }
}

/// Split text into display lines: on newlines, then on width.
///
/// **Word-wrapped where it can be**, because breaking mid-word is the thing that
/// makes a small screen feel broken rather than small. Falls back to a hard break
/// for a single word longer than the display, which is the only case where there
/// is no better answer.
fn wrap(text: &str) -> Wrap<'_> {
    Wrap { rest: text }
}

struct Wrap<'a> {
    rest: &'a str,
}

impl<'a> Iterator for Wrap<'a> {
    type Item = &'a str;

    fn next(&mut self) -> Option<&'a str> {
        if self.rest.is_empty() {
            return None;
        }

        // A newline always ends a line, before width is considered.
        if let Some(nl) = self.rest.find('\n') {
            if nl <= COLS {
                let (line, rest) = self.rest.split_at(nl);
                self.rest = &rest[1..];
                return Some(line.trim_end_matches('\r'));
            }
        }

        if self.rest.len() <= COLS {
            let line = self.rest;
            self.rest = "";
            return Some(line);
        }

        // Break at the last space that fits; if there is none, the word is longer
        // than the screen and a hard break is the only option.
        let window = &self.rest[..COLS];
        let split = window.rfind(' ').map(|i| i).unwrap_or(COLS);
        let (line, rest) = self.rest.split_at(split);
        self.rest = rest.trim_start_matches(' ');
        Some(line)
    }
}
