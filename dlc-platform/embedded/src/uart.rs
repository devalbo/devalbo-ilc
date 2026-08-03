//! `wasi:cli/stdout` over a byte sink — the UART, once this is on the badge.
//!
//! WHY THIS IS THE FIRST THING THE BADGE NEEDS, and not an output nicety:
//! TinyGo's `runtime.initAll` calls `get-stdout` during `_initialize`, so a
//! component whose stdout traps never instantiates. Discovered by walking the
//! trap chain (EMBEDDED-PLAN D4); it is the reason stdio cannot be scheduled
//! after "get it booting".
//!
//! WHAT IS BORROWED AND WHAT IS OURS. `wasmtime-wasi-io` implements
//! `wasi:io/{error,poll,streams}` — 15 functions across two resources, plus a
//! `pollable` and a `stream-error` variant — and asks for an `impl
//! OutputStream`. That crate is `no_std`-capable (verified compiling for
//! `thumbv8m`), so the badge inherits all of it and writes only what follows.

use alloc::boxed::Box;
use alloc::vec::Vec;

use bytes::Bytes;
use wasmtime_wasi_io::poll::Pollable;
use wasmtime_wasi_io::streams::{
    DynInputStream, DynOutputStream, InputStream, OutputStream, StreamError, StreamResult,
};

/// Where bytes go. On the badge this is a UART; here it is a buffer, so the
/// same code can be tested with a debugger attached.
pub trait ByteSink: Send + 'static {
    fn write(&mut self, bytes: &[u8]);
}

/// A sink that keeps everything, for tests and for the laptop harness.
#[derive(Default)]
pub struct BufferSink(pub Vec<u8>);

impl ByteSink for BufferSink {
    fn write(&mut self, bytes: &[u8]) {
        self.0.extend_from_slice(bytes);
    }
}

/// An ILC output stream over any byte sink.
pub struct SinkStream<S: ByteSink>(pub S);

/// ALWAYS READY, and that is what makes this viable on bare metal.
///
/// `Pollable::ready` is `async`, so `wasi:io` is wired through
/// `add_to_linker_async` and component calls become async — which normally
/// implies an executor. A UART is never "not ready" from the guest's point of
/// view, so this future resolves immediately, and an executor that only ever
/// polls once is enough. On a chip with no scheduler that is the difference
/// between "needs an async runtime" and "needs a loop".
#[async_trait::async_trait]
impl<S: ByteSink> Pollable for SinkStream<S> {
    async fn ready(&mut self) {}
}

impl<S: ByteSink> OutputStream for SinkStream<S> {
    fn write(&mut self, bytes: Bytes) -> StreamResult<()> {
        self.0.write(&bytes);
        Ok(())
    }

    /// Nothing is buffered above the sink, so a flush has nothing to do. A real
    /// UART drains on its own; if one ever needs draining, it happens here.
    fn flush(&mut self) -> StreamResult<()> {
        Ok(())
    }

    /// How much the guest may write in one go. A fixed generous number rather
    /// than a real limit: back-pressure from a UART would mean blocking, and
    /// this method must not block.
    fn check_write(&mut self) -> StreamResult<usize> {
        Ok(4096)
    }
}

/// Boxed as the exact alias `wasmtime-wasi-io` registers the resource under.
///
/// The alias matters: pushing a differently-spelled but structurally identical
/// box yields "resource type mismatch" at instantiation, because the linker
/// matches resource types nominally.
pub fn boxed<S: ByteSink>(sink: S) -> DynOutputStream {
    Box::new(SinkStream(sink))
}

/// stdin on a device that has none.
///
/// CLOSED rather than empty-and-waiting: a badge has no keyboard, and a stream
/// that blocks forever would hang the engine on any read. Closed is the honest
/// answer and the one an app can handle.
pub struct ClosedStream;

#[async_trait::async_trait]
impl Pollable for ClosedStream {
    async fn ready(&mut self) {}
}

impl InputStream for ClosedStream {
    fn read(&mut self, _size: usize) -> StreamResult<Bytes> {
        Err(StreamError::Closed)
    }
}

/// Boxed as the alias the linker registers, same as `boxed` above.
pub fn boxed_input() -> DynInputStream {
    Box::new(ClosedStream)
}
