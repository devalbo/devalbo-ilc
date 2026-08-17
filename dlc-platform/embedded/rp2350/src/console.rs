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

/// THE APP'S SHARE OF THE SCREEN, in characters.
///
/// Under `Split` the world keeps a band and the app gets the rest; under `Full`
/// the app gets everything and the world falls back to the backlight and the
/// status colour.
///
/// These are the numbers the world SENDS IN ITS MANIFEST (`TextOut.cols/rows`),
/// so an app formats for the space that exists rather than the space it hoped
/// for. They travel ONLY in the manifest: an allocation belongs on the channel
/// that can correct it when this world takes rows back
/// (docs/ENVIRONMENT-PLAN.md D12).
///
/// `world::text_sink` derives the outlet from `APP_ROWS` rather than declaring
/// it separately, which is what stops the outlet and the budget contradicting
/// each other.
pub const APP_COLS: usize = COLS;
pub const APP_ROWS: usize = match crate::world::SCREEN {
    crate::world::ScreenLayout::Split => ROWS - crate::world::WORLD_BAND_ROWS,
    crate::world::ScreenLayout::Full => ROWS,
};

// THE APP MUST ACTUALLY HAVE ROWS, checked at build time.
//
// `Split` subtracts the world's band from ROWS. A band taller than the screen
// would underflow, and a zero budget would have this world sending a manifest
// that promises a display while `text_sink` still says "display" — an app would
// format for a screen that cannot show anything.
//
// A compile-time fact, so a compile-time error.
const _: () = {
    assert!(APP_ROWS > 0, "Split leaves the app no rows: shrink WORLD_BAND_ROWS");
    assert!(APP_ROWS <= ROWS, "the app cannot have more rows than the screen");
};

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
    // THE APP GETS ITS BUDGET AND NOT A ROW MORE. Sending `rows` in the manifest
    // and then drawing past it would make the number a suggestion, and an app
    // that trusted it would have its last line silently eaten.
    let mut row = 0usize;
    for line in wrap(body) {
        if row >= APP_ROWS {
            break;
        }
        let y = TEXT_TOP + row as i32 * LINE_H;
        // FOLDED AT THE LAST MOMENT, so wrapping still measures the text the app
        // actually produced. Folding earlier would change the character count -
        // `...` is three where the ellipsis was one - and the app was told its
        // budget in the manifest, so the two must agree about what a line holds.
        let mut folded = [0u8; COLS * 3];
        let drawable = fold_into(line, &mut folded);
        let _ = Text::new(drawable, Point::new(0, y), style).draw(panel.target());
        row += 1;
    }
}

/// Fold a character into something `FONT_8X13` can actually draw.
///
/// # Why a badge needs this and a terminal does not
///
/// The font is ASCII. `embedded-graphics` substitutes a fallback glyph for
/// anything it lacks, so an em dash arrives as `?` — which does not read as a
/// dash, it reads as an ERROR. The first time it happened the reasonable
/// question was "what is wrong with the encoding", and the answer was nothing:
/// Component Model `string` is UTF-8, `wasi:cli/stdout` is opaque `list<u8>`,
/// and the bytes crossed every boundary intact. They died at the last step,
/// where characters become pixels.
///
/// Our own apps now stick to ASCII, but THIS WORLD RUNS APPS IT WAS NOT BUILT
/// FOR — that is the entire point of the payload region. So the text it renders
/// is not text it controls, and folding is the only place the problem can be
/// solved for a payload someone else wrote.
///
/// # Folded, not stripped
///
/// A dash becomes `-` rather than vanishing, because losing a character silently
/// is worse than showing a plainer one: `10-20` and `1020` are different claims.
/// Anything with no sensible ASCII stand-in still becomes `?`, which is honest —
/// the badge cannot show it, and pretending otherwise would be worse.
///
/// Kept deliberately SMALL. This is the punctuation that shows up in ordinary
/// prose — the dashes and quotes editors substitute automatically, which is how
/// they get into an app's strings without anyone typing them. It is not an
/// attempt at Unicode support and should not grow into one: a badge with a
/// 40-column ASCII font is not the place to solve text rendering.
fn fold(c: char) -> &'static str {
    match c {
        // Dashes an editor substitutes for a typed hyphen.
        '\u{2010}' | '\u{2011}' | '\u{2012}' | '\u{2013}' | '\u{2014}' | '\u{2015}' => "-",
        // Quotes, likewise - the commonest way non-ASCII enters prose.
        '\u{2018}' | '\u{2019}' | '\u{201A}' | '\u{201B}' => "'",
        '\u{201C}' | '\u{201D}' | '\u{201E}' | '\u{201F}' => "\"",
        // Spaces that are not the space bar: non-breaking, thin, narrow.
        '\u{00A0}' | '\u{2007}' | '\u{2009}' | '\u{202F}' => " ",
        '\u{2026}' => "...",
        '\u{2022}' => "*",
        '\u{00D7}' => "x",
        // Already drawable: ASCII graphics and the space bar. Empty means "keep
        // the character as it is" - see fold_into.
        c if c.is_ascii_graphic() || c == ' ' => "",
        // Everything else, including control codes. `?` is what the font would
        // have drawn anyway; making it explicit keeps substitution in one place.
        _ => "?",
    }
}

/// Write `line` into `out` with every character folded to drawable ASCII.
///
/// Returns the prefix of `out` that was written. Truncates rather than growing:
/// the buffer allows for the slack `...` can add, and a line longer than the
/// screen was already going to be cut.
fn fold_into<'b>(line: &str, out: &'b mut [u8]) -> &'b str {
    let mut len = 0usize;
    for c in line.chars() {
        let replacement = fold(c);
        if replacement.is_empty() {
            // Already drawable, and ASCII by construction - `fold` returns empty
            // only for characters it has just verified are ASCII.
            if len < out.len() {
                out[len] = c as u8;
                len += 1;
            }
            continue;
        }
        for byte in replacement.bytes() {
            if len < out.len() {
                out[len] = byte;
                len += 1;
            }
        }
    }
    // Every byte above came from `fold` (ASCII) or from a char verified ASCII,
    // so this cannot fail; `unwrap_or` keeps the panic out of the firmware.
    core::str::from_utf8(&out[..len]).unwrap_or("")
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
            // `nl` is a BYTE offset and COLS is a character count, so this
            // comparison is conservative rather than exact: a line of multi-byte
            // characters may fall through to the wrapper below even though it
            // would have fitted. That is the safe direction — the wrapper
            // handles it correctly — and `nl` is always a char boundary because
            // `find` returned it.
            if nl <= COLS {
                let (line, rest) = self.rest.split_at(nl);
                self.rest = &rest[1..];
                return Some(line.trim_end_matches('\r'));
            }
        }

        // COLUMNS ARE CHARACTERS, NOT BYTES — and this used to slice by bytes.
        //
        // `&self.rest[..COLS]` PANICS when COLS lands inside a multi-byte
        // character, and app text is UTF-8: hello alone ships an em dash. It
        // never fired only because hello's line is short enough to take the
        // early return below, so the bug was one longer string away from
        // halting the firmware — on a panel whose whole job is to report.
        //
        // Byte-counting was also wrong on its own terms. A 40-byte line holding
        // three em dashes is 34 characters and leaves six columns unused, so the
        // display wrapped early and looked broken at exactly the moment an app
        // used punctuation.
        //
        // `char_indices` gives both answers at once: how many characters fit,
        // and the byte offset where that many end — which is a boundary by
        // construction.
        let mut end = self.rest.len();
        let mut count = 0usize;
        for (offset, _) in self.rest.char_indices() {
            if count == COLS {
                end = offset;
                break;
            }
            count += 1;
        }
        if count < COLS {
            let line = self.rest;
            self.rest = "";
            return Some(line);
        }

        // Break at the last space that fits; if there is none, the word is longer
        // than the screen and a hard break is the only option. `rfind` returns a
        // byte offset into a slice that already ends on a boundary, so the split
        // below is safe either way.
        let window = &self.rest[..end];
        let split = window.rfind(' ').unwrap_or(end);
        let (line, rest) = self.rest.split_at(split);
        self.rest = rest.trim_start_matches(' ');
        Some(line)
    }
}
