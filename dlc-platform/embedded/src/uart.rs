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

/// A buffer the STREAM writes and the HOST reads — the fix for a bug that made
/// app output unreachable.
///
/// **WHAT WAS WRONG.** `get_stdout` handed the guest a fresh `BufferSink`, which
/// the resource table owned and dropped when the handle closed. Everything the
/// app printed went into it and nowhere else, so `MinimalHost::stdout()` always
/// returned empty and the badge could never show an app's output — while
/// advertising `ILC_STDOUT=display`. Nothing failed; the text simply did not
/// exist. Found by running an app that prints, which `hello` effectively did not.
///
/// **Why a lock at all on a single-core firmware.** `ByteSink` requires `Send`,
/// so `Rc<RefCell<..>>` is not available, and the honest alternative to a lock is
/// an `UnsafeCell` with a comment asking everyone to be careful. This is a few
/// instructions and cannot be got wrong later: an interrupt handler that starts
/// logging finds a lock rather than a data race.
#[derive(Clone, Default)]
pub struct SharedBuffer {
    inner: alloc::rc::Rc<Shared>,
}

#[derive(Default)]
struct Shared {
    locked: core::sync::atomic::AtomicBool,
    data: core::cell::UnsafeCell<Vec<u8>>,
}

// SAFETY: every access goes through `with`, which spins on `locked` and so gives
// exclusive access for the duration of the closure. `Rc` is not itself atomic,
// but this host is single-threaded by construction — `block_on` polls on one
// core with no executor and no preemption — and the lock is what keeps a future
// interrupt-time writer from tearing the buffer.
unsafe impl Send for SharedBuffer {}
unsafe impl Sync for SharedBuffer {}

impl SharedBuffer {
    fn with<R>(&self, f: impl FnOnce(&mut Vec<u8>) -> R) -> R {
        use core::sync::atomic::Ordering;
        while self
            .inner
            .locked
            .compare_exchange(false, true, Ordering::Acquire, Ordering::Relaxed)
            .is_err()
        {
            core::hint::spin_loop();
        }
        // SAFETY: the lock above is held for exactly this scope.
        let result = f(unsafe { &mut *self.inner.data.get() });
        self.inner.locked.store(false, Ordering::Release);
        result
    }

    /// Take everything written so far, leaving the buffer empty.
    pub fn take(&self) -> Vec<u8> {
        self.with(core::mem::take)
    }

    /// A copy of the contents, leaving them in place.
    ///
    /// FOR STORAGE RATHER THAN OUTPUT. `take` is right for stdout, where the
    /// host drains what the guest produced; a file has to still be there after
    /// somebody reads it.
    pub fn snapshot(&self) -> Vec<u8> {
        self.with(|buffer| buffer.clone())
    }

    /// Append bytes WITHOUT firing the stdout echo.
    ///
    /// `ByteSink::write` echoes, which is what makes a guest's output appear on
    /// the panel as it happens. A file write must not: it would paint the
    /// contents of every file the app stores onto the screen and into the boot
    /// log. Same buffer, different door.
    pub fn append(&self, bytes: &[u8]) {
        self.with(|buffer| buffer.extend_from_slice(bytes));
    }

    /// Empty the buffer, keeping the sharing. What `O_TRUNC` means.
    pub fn truncate(&self) {
        self.with(|buffer| buffer.clear());
    }
}

/// Called on every guest write, as it happens.
///
/// # Why this exists
///
/// `SinkStream::write` is HOST CODE running mid-`execute`: the guest calls
/// `wasi:cli/stdout.write`, Wasmtime dispatches here, and the guest is suspended
/// until it returns. So the bytes are available while the command is still
/// running — they were simply buffered and read afterwards, which made every
/// tier look like output only arrived at the end.
///
/// A firmware installs this to see output LIVE. The buffer is still filled, so
/// nothing that drains afterwards changes.
///
/// SAFETY: written once at boot, on a single core, before any guest runs.
static mut ECHO: Option<fn(&[u8])> = None;

/// Install the live-output hook. Call at boot.
pub fn set_echo(echo: fn(&[u8])) {
    unsafe { ECHO = Some(echo) };
}

impl ByteSink for SharedBuffer {
    fn write(&mut self, bytes: &[u8]) {
        // ECHO FIRST, buffer second. If a firmware's echo panics or hangs, the
        // buffer should not already claim to hold bytes nobody saw — and the
        // ordering makes the live path the one that cannot be starved by a slow
        // consumer downstream.
        if let Some(echo) = unsafe { ECHO } {
            echo(bytes);
        }
        self.with(|buffer| buffer.extend_from_slice(bytes));
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

#[cfg(test)]
mod tests {
    use super::*;

    /// A GUEST WRITE MUST NEVER FAIL, on any world.
    ///
    /// This is the invariant the whole text story rests on. A world may have no
    /// screen, no UART and nowhere at all to put the bytes — the minimal world
    /// simulates exactly that — but it must still ACCEPT them. An app's
    /// `fmt.Println` is the one channel that works everywhere, and a tier that
    /// turned it into an error would make printing a thing an app had to guard,
    /// on a tier it cannot identify from the inside.
    ///
    /// Discarding is a world's decision to make. Failing is not.
    ///
    /// A world that WANTS an app to skip the work says so in the manifest
    /// (`TEXT_OUTLET_NONE`), and an app may then choose not to format. That is
    /// advice the app is free to ignore, and the difference matters: advice
    /// cannot break anyone, a failing write can.
    #[test]
    fn writes_always_succeed_and_never_block() {
        let mut stream = SinkStream(SharedBuffer::default());

        assert!(stream.write(Bytes::from_static(b"hello")).is_ok());
        assert!(stream.write(Bytes::from_static(b"")).is_ok(), "an empty write is still a write");
        assert!(stream.flush().is_ok());
        assert!(
            stream.check_write().unwrap_or(0) > 0,
            "a guest that is told it may write zero bytes will spin forever"
        );

        // And a big one: back-pressure must not appear as a short write either.
        let big = vec![b'x'; 64 * 1024];
        assert!(stream.write(Bytes::from(big)).is_ok());
    }

    /// Draining must actually empty the buffer.
    ///
    /// A world with no outlet still has to TAKE the bytes — leaving them because
    /// nobody will read them makes the sink unbounded, and an app printing in a
    /// loop then grows the heap until the allocator gives out. On a world with
    /// no screen, nobody would see that happen.
    #[test]
    fn take_empties_the_buffer() {
        let buffer = SharedBuffer::default();
        let mut stream = SinkStream(buffer.clone());
        let _ = stream.write(Bytes::from_static(b"discard me"));

        assert_eq!(buffer.take(), b"discard me");
        assert!(buffer.take().is_empty(), "a second take must not repeat the first");
    }
}
