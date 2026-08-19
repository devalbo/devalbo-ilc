//! Every size the control channel commits to.
//!
//! # This file holds no numbers
//!
//! They are GENERATED into `limits_gen.rs` from
//! `dlc-platform/protocol/FRAMING.json`, which is also the source for the Go and
//! TypeScript halves. `make verify-protocol` fails the build when any of the
//! three drifts.
//!
//! # Why the numbers moved out
//!
//! They were literals where they were used — `[u8; 512]` for a reply buffer,
//! `[u8; 64]` for a read, `160` for a framed log line. Each was reasonable where
//! it stood and none said what it was in terms of, so nothing showed when one
//! outgrew another.
//!
//! That is not hypothetical: a 512-byte reply buffer met a 600-byte answer, sent
//! the first 512 bytes, and the client waited forever for a completion that
//! could not come — reporting "no reply within 30s — is BADGE_CONTROL=on in this
//! build?", which blames a build flag for a buffer.
//!
//! # What is NOT here
//!
//! Sizes belonging to one medium rather than to the protocol. A USB CDC bulk
//! packet is 64 bytes because USB says so; a BLE characteristic is not. That
//! number lives with the `Link` implementation that knows it, and every layer
//! above is written to be indifferent to it — partial transfers are normal.

pub use crate::limits_gen::*;

// The relationships are evaluated by the generator; these assert that what
// arrived still holds. A generated number is only as trustworthy as the spec it
// came from, and a spec can be edited into nonsense.
const _: () = assert!(MAX_REQUEST < MAX_PAYLOAD, "a request must fit in a frame");
const _: () = assert!(INBOUND > FRAME_OVERHEAD + MAX_REQUEST, "no room for the envelope");
const _: () = assert!(NOTICE_FRAME > FRAME_OVERHEAD + MAX_LOG_LINE, "no room for the fields");
const _: () = assert!(REFUSAL_FRAME < NOTICE_FRAME, "a refusal is the small case");
