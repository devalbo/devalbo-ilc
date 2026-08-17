//! The bring-up log, over the USB-C cable that is already plugged in.
//!
//! # Why this exists
//!
//! The badge had two output channels and both failed us. The screen was the
//! thing under test, so it could not report on itself; the UART needs a 3.3 V
//! adapter clipped to two crocodile pads, which not everyone has. That left the
//! backlight — one bit — and four flash cycles were spent guessing because a
//! guess was cheaper than a measurement.
//!
//! Pimoroni's own firmware exposes a USB CDC serial port on this same cable. So
//! can we, and `usb-device` + `usbd-serial` do the protocol — this file is
//! plumbing, not a USB stack.
//!
//! # IT IS DEFAULT BEHAVIOUR, not a debug build
//!
//! No `cfg`, no feature flag: every badge build exposes this, including the
//! `BADGE_BEAT_MS=0` one that ships. It costs ~17 KB against a firmware whose
//! Wasmtime is a megabyte, and a badge that cannot say what it did is a badge
//! that gets diagnosed by reflashing — which is how four cycles went on guesses
//! that a single line of output would have settled.
//!
//! The rule this earns: **a device with no console is a device you debug by
//! rebuilding it.** Give it one before it needs one.
//!
//! # The shape, and why it is not "just print"
//!
//! USB needs polling to enumerate, and the bring-up is a straight-line sequence
//! that must not stall waiting for a host that may never attach. So the log is
//! **buffered while the badge runs** and **served afterwards**, from the same
//! idle loop the firmware already ends in. Nothing blocks, no output is lost to
//! a late-connecting host, and a run that ends in a hang still yields whatever
//! was written before it — which is exactly the case worth having.
//!
//! # Served from an INTERRUPT, which is what closes the important gap
//!
//! This used to buffer during the run and serve from the idle loop, and the gap
//! was written down here as known: a run that HANGS never reaches the idle loop,
//! so it never serves — and a hang is precisely when the log matters most. The
//! badge would sit frozen with the answer in RAM and no way to say it.
//!
//! That cost a real debugging session. A stalled `execute` showed one unresolved
//! line on the panel and produced no serial device at all, so the only available
//! move was to reflash with a guess — the exact failure this module was written
//! to prevent, reintroduced by where the serving happened.
//!
//! So the device is now driven by `USBCTRL_IRQ`. Enumeration and delivery
//! continue no matter what the main loop is doing, which buys three things:
//!
//!   - output appears LIVE, rather than after the run
//!   - a hang still surrenders everything written before it
//!   - a host that attaches AFTER a hang still gets the log, because the
//!     interrupt is still running to answer it
//!
//! The last one is why polling from a hook in the report was not enough. A hook
//! only runs when main runs.
//!
//! # The concurrency, stated plainly
//!
//! One producer (main, appending) and one consumer (the ISR, draining). No lock:
//! `len` is an `AtomicUsize` published with `Release` after the bytes are
//! written and read with `Acquire` before they are, so the ISR can never observe
//! a length that covers bytes not yet stored. Appends are the only mutation and
//! nothing ever rewrites or reorders what is already there.
//!
//! Disabling interrupts around every log write would also have worked and was
//! rejected: the report writes constantly during bring-up, and masking USB
//! service across all of it would stall enumeration in the middle of the run.

use core::fmt::Write;
use core::sync::atomic::{AtomicBool, AtomicUsize, Ordering};

/// Everything printed during a run.
///
/// 8 KB: the report is a few hundred bytes, and the slack is for probes added
/// while chasing something. A full buffer TRUNCATES rather than wrapping — the
/// beginning of a bring-up log is the part that matters, and a wrap would eat it
/// to keep output nobody asked for.
pub struct LogBuffer {
    bytes: [u8; 8192],
    /// PUBLISHED, not merely stored. The ISR reads this to learn how much is
    /// safe to send, so it is written with `Release` AFTER the bytes it covers
    /// and read with `Acquire` BEFORE them. Get that backwards and the ISR
    /// transmits uninitialised memory over a serial port — which would look like
    /// line noise and send someone hunting a hardware fault.
    len: AtomicUsize,
    truncated: AtomicBool,
    /// Where the line being written began.
    ///
    /// Only main touches it, and only inside `write_str`, so it needs no
    /// synchronisation — unlike `len`, the ISR never reads it.
    line_start: usize,
}

impl LogBuffer {
    pub const fn new() -> Self {
        Self {
            bytes: [0; 8192],
            len: AtomicUsize::new(0),
            truncated: AtomicBool::new(false),
            line_start: 0,
        }
    }

    /// How much has been published. Safe to call from the ISR.
    pub fn len(&self) -> usize {
        self.len.load(Ordering::Acquire)
    }

    pub fn as_bytes(&self) -> &[u8] {
        &self.bytes[..self.len()]
    }

    /// The published bytes from `from` onward — what the ISR still owes a host.
    pub fn pending(&self, from: usize) -> &[u8] {
        let len = self.len();
        if from >= len {
            return &[];
        }
        &self.bytes[from..len]
    }

    pub fn truncated(&self) -> bool {
        self.truncated.load(Ordering::Relaxed)
    }
}

impl Write for LogBuffer {
    fn write_str(&mut self, s: &str) -> core::fmt::Result {
        // Read once: this is the only producer, so nothing else moves it.
        let mut len = self.len.load(Ordering::Relaxed);
        for byte in s.bytes() {
            if len == self.bytes.len() {
                self.truncated.store(true, Ordering::Relaxed);
                break;
            }
            self.bytes[len] = byte;
            len += 1;

            // A COMPLETED LINE IS AN EVENT. Framing here rather than at the call
            // sites is what keeps the promise that a frame reader never has less
            // than a terminal would have shown: EVERY line reaches this, whether
            // it came through `report.rs` with a stage and a level attached or
            // through a bare `writeln!` somewhere in main.
            if byte == b'\n' {
                let mut end = len - 1;
                if end > self.line_start && self.bytes[end - 1] == b'\r' {
                    end -= 1;
                }
                #[cfg(badge_control)]
                {
                    // Lossy on invalid UTF-8, which the text stream already
                    // carried verbatim; a frame is not the place to fight over
                    // bytes that will never decode.
                    let text = core::str::from_utf8(&self.bytes[self.line_start..end]).unwrap_or("");
                    notices::line_completed(text);
                }
                #[cfg(not(badge_control))]
                let _ = end;
                self.line_start = len;
            }
        }
        // PUBLISH LAST. Everything above must be visible to the ISR before the
        // length that covers it becomes visible, or the ISR sends bytes that
        // have not been written yet.
        self.len.store(len, Ordering::Release);
        Ok(())
    }
}

/// Write to two sinks at once.
///
/// The UART keeps working for anyone who has an adapter, and it is the only
/// channel alive before USB enumerates. Sending to both costs nothing and means
/// the two never disagree about what happened.
pub struct Tee<'a, A: Write, B: Write>(pub &'a mut A, pub &'a mut B);

impl<A: Write, B: Write> Write for Tee<'_, A, B> {
    fn write_str(&mut self, s: &str) -> core::fmt::Result {
        // Both, unconditionally: a failure on one must not silence the other.
        let first = self.0.write_str(s);
        let second = self.1.write_str(s);
        first.and(second)
    }
}

// ---------------------------------------------------------------------------
// The device, and the interrupt that drives it
// ---------------------------------------------------------------------------

use rp235x_hal as hal;
// IMPORTED, not path-qualified. The PAC exports `interrupt` in two namespaces —
// an alias for the `Interrupt` enum and the attribute macro from `cortex-m-rt` —
// so `#[hal::pac::interrupt]` resolves to the type and fails with a message
// about an unlinked crate, which points nowhere near the cause.
use hal::pac::interrupt;

/// EVERYTHING THE USB SERVICE TOUCHES, in one cell.
///
/// It was four `static mut`s reached through `addr_of_mut!`, and `service` was
/// called from BOTH the interrupt and `pump()`. `usb-device`'s `poll` and
/// `write` are not reentrant, so the interrupt preempting main mid-call
/// corrupted the stack — output stopped the instant a widget started pumping,
/// and the badge looked hung while it was working perfectly.
///
/// ONE CELL RATHER THAN FOUR, because they are only ever used together and four
/// separately-guarded values would let a caller take two and think it was safe.
/// See `shared.rs` for what that guarantee is and what it costs.
struct Usb {
    /// The allocator must outlive the device and the port that borrow from it.
    /// `'static` because it lives here for the rest of the program.
    bus: Option<&'static usb_device::bus::UsbBusAllocator<hal::usb::UsbBus>>,
    serial: Option<usbd_serial::SerialPort<'static, hal::usb::UsbBus>>,
    device: Option<usb_device::device::UsbDevice<'static, hal::usb::UsbBus>>,
    reset: Option<PicoToolReset<'static, hal::usb::UsbBus>>,
}

static USB: crate::shared::Shared<Usb> = crate::shared::Shared::new(Usb {
    bus: None,
    serial: None,
    device: None,
    reset: None,
});

/// The allocator itself, which the borrows above point into.
///
/// Still a `static mut`, and it is the one place that cannot be a `Shared`: the
/// device and the port hold `&'static` references INTO it for their whole lives,
/// so it can never be handed out mutably again. It is written exactly once, in
/// `start`, before the interrupt is unmasked — and never touched afterwards.
static mut USB_BUS_STORAGE: Option<usb_device::bus::UsbBusAllocator<hal::usb::UsbBus>> = None;

/// WHAT THE WORLD IS DOING, for anyone who asks (BADGE-CONTROL-PLAN D3).
///
/// An atomic, and compiled in whatever the control setting: the callers are
/// scattered through the bring-up and a `cfg` at each one would be five places
/// to forget. Without a control channel nothing reads it, which costs a word.
static ACTIVITY: AtomicUsize = AtomicUsize::new(0);

/// Record what the world is doing now. Cheap enough to call at every stage.
pub fn set_activity(activity: u32) {
    ACTIVITY.store(activity as usize, Ordering::Relaxed);
}

/// How much of the log the host has already been given.
///
/// An atomic rather than a `Shared`: it is a single word, and a torn read is
/// impossible. The values that needed guarding were the ones with INVARIANTS
/// between them — a buffer and its length, a device and its endpoints.
static SENT: AtomicUsize = AtomicUsize::new(0);

/// Whether the truncation notice has been delivered ahead of the body.
static NOTICE_SENT: AtomicBool = AtomicBool::new(false);

const TRUNCATION_NOTICE: &[u8] = b"(log truncated -- later lines were dropped)\r\n";

/// Bring the CDC port up and hand it to the interrupt controller.
///
/// CALL THIS EARLY — before the bring-up report, not after it. The whole point
/// is that the log is available while the badge is working and, more
/// importantly, while it is stuck. Called late, this module is worth no more
/// than the idle-loop version it replaced.
///
/// Enumeration takes a host handshake and does not complete before this returns.
/// That is fine and is the reason for the interrupt: the device answers whenever
/// the host gets around to asking, including long after main has stopped making
/// progress.
pub fn start(
    usb: hal::pac::USB,
    dpram: hal::pac::USB_DPRAM,
    clock: hal::clocks::UsbClock,
    resets: &mut hal::pac::RESETS,
) {
    // SAFETY: single core, and the interrupt is not unmasked until the end of
    // this function, so nothing else can be looking at the storage.
    let bus_ref: &'static usb_device::bus::UsbBusAllocator<hal::usb::UsbBus> = unsafe {
        if (*core::ptr::addr_of!(USB_BUS_STORAGE)).is_some() {
            // A second call would leak the first device and leave the interrupt
            // holding a dangling borrow. This is a programming error, not a
            // runtime condition, and refusing is the only safe answer.
            return;
        }
        USB_BUS_STORAGE = Some(usb_device::bus::UsbBusAllocator::new(
            hal::usb::UsbBus::new(usb, dpram, clock, true, resets),
        ));
        (*core::ptr::addr_of!(USB_BUS_STORAGE)).as_ref().unwrap()
    };

    USB.with(|state| {
        state.bus = Some(bus_ref);
        // ORDER MATTERS: the port and the reset interface must be allocated
        // BEFORE the device is built, because the device enumerates the
        // interfaces that exist at build time. Build it first and the host sees
        // a device with no endpoints.
        state.serial = Some(usbd_serial::SerialPort::new(bus_ref));
        state.reset = Some(PicoToolReset::new(bus_ref));
        state.device = Some(
            usb_device::device::UsbDeviceBuilder::new(
                bus_ref,
                // 0x2e8a/0x000a — Raspberry Pi's vendor id, which picotool
                // requires before it will probe a device at all.
                usb_device::device::UsbVidPid(0x2e8a, 0x000a),
            )
            .strings(&[usb_device::device::StringDescriptors::default()
                .manufacturer("devalbo")
                .product("DLC badge bring-up")
                .serial_number("ilc")])
            .unwrap()
            // COMPOSITE: there are two functions on this cable now, the CDC log
            // and picotool's reset interface. Declaring CDC at the DEVICE level
            // would tell a host the whole device is a serial port.
            .composite_with_iads()
            .build(),
        );
    });

    // SAFETY: everything the interrupt reads is published above.
    unsafe {
        cortex_m::peripheral::NVIC::unmask(hal::pac::Interrupt::USBCTRL_IRQ);
    }
}

/// Service the port: answer the host, then hand over whatever is unsent.
///
/// Shared by the ISR and the idle loop. The idle loop still calls it so that a
/// badge which finishes normally keeps draining even if interrupts were never
/// unmasked — a belt-and-braces path that costs one function call.
fn service() {
    // TRY, DO NOT INSIST. The interrupt and `pump()` both land here, and if one
    // finds the other mid-update it has nothing useful to do with the value —
    // and every reason not to bring the board down over it. Skipping costs one
    // service call; panicking costs the badge.
    USB.try_with(service_locked);
}

fn service_locked(usb: &mut Usb) {
    let (Some(device), Some(serial), Some(reset)) =
        (usb.device.as_mut(), usb.serial.as_mut(), usb.reset.as_mut())
    else {
        return;
    };

    // POLL, THEN WRITE REGARDLESS. `poll` reports whether there was USB
    // ACTIVITY, not whether the port is usable — gating the write on it meant an
    // idle host (the normal case: a terminal that only reads) never triggered a
    // single byte.
    // BOTH CLASSES, or the reset interface never sees its control transfer and
    // `picotool reboot -u -f` reports no reset interface on a device that has
    // one.
    device.poll(&mut [serial, reset]);

    // A HOST JUST OPENED THE PORT: replay the run for it, once.
    //
    // `dtr` rises when a terminal opens the device and falls when it closes, so
    // this is the moment — and the ONLY moment — that someone needs the history
    // they missed. Replaying on a timer instead was noise; replaying never was
    // the bug where a second reader saw an empty port and a healthy badge looked
    // hung.
    let open = serial.dtr();
    if open && !WAS_OPEN.swap(true, Ordering::Relaxed) {
        rewind();
    } else if !open {
        WAS_OPEN.store(false, Ordering::Relaxed);
    }

    // Drain anything the host typed; an unread RX buffer stalls some stacks.
    let mut incoming = [0u8; 64];
    let read = serial.read(&mut incoming).unwrap_or(0);

    #[cfg(badge_control)]
    {
        control_receive(&incoming[..read]);
        // A REPLY GOES BEFORE THE LOG. Somebody is waiting on it, and the log is
        // a stream nobody is blocked on.
        //
        // THIS IS ONLY SAFE BECAUSE REPLIES ARE OCCASIONAL. Framed LOG LINES were
        // added here and reverted: every report line queued one, so the queue was
        // never empty, this early return fired on every service call, and the
        // text log — the last-resort diagnostic — was never sent at all. The
        // decision to frame the log stands (D8b); emitting it unconditionally
        // does not. It needs subscriptions, so a world stays silent until a
        // client asks.
        if control_send(serial) {
            return;
        }
    }
    #[cfg(not(badge_control))]
    let _ = read;

    // SAFETY: the ISR only ever READS the buffer, through methods that use the
    // published length. Main is the only writer.
    let log: &LogBuffer = unsafe { &*core::ptr::addr_of!(crate::LOG) };

    // THE TRUNCATION NOTICE LEADS, because a reader needs it before they start
    // trusting what follows. It cannot be appended to the buffer: the buffer
    // being full is precisely what truncation means.
    if log.truncated() && !NOTICE_SENT.load(Ordering::Relaxed) {
        if let Ok(n) = serial.write(TRUNCATION_NOTICE) {
            if n == TRUNCATION_NOTICE.len() {
                NOTICE_SENT.store(true, Ordering::Relaxed);
            }
        }
        return;
    }

    let sent = SENT.load(Ordering::Relaxed);
    let pending = log.pending(sent);
    if !pending.is_empty() {
        if let Ok(n) = serial.write(pending) {
            SENT.store(sent + n, Ordering::Relaxed);
        }
        return;
    }

    // NOTICES COME AFTER THE TEXT THEY DUPLICATE, and the ordering is the whole
    // guard against repeating D8b.
    //
    // The reverted version put framed log lines ABOVE this, sharing the reply's
    // early return. Every report line queued one, so the queue was never empty,
    // the early return fired on every service call, and the text log — the
    // diagnostic that needs no tooling — was never transmitted at all.
    //
    // Below the text log, that cannot happen: `pending` is finite and drains, so
    // a notice can only ever use a wire the text stream has finished with. A
    // frame can never delay the words it is a copy of.
    #[cfg(badge_control)]
    if control_notices(serial) {
        return;
    }

    // A HEARTBEAT, NOT A REPLAY.
    //
    // The first version of this re-sent the WHOLE LOG when idle, so that a
    // terminal attached late saw the run. It worked and it was the wrong signal:
    // 674 bytes to say "still here", repeated forever, and a reader could not
    // tell a replay from a fresh run — which is the same ambiguity the heartbeat
    // exists to remove.
    //
    // So the two needs are separated:
    //
    //   liveness  -> this line, small and unmistakable
    //   history   -> replayed ONCE when a host opens the port (below)
    //
    // NOT APPENDED TO `LOG`. Written straight to the endpoint, because a
    // heartbeat in the buffer would fill 8 KB with "still here" and truncate the
    // run it was reporting on.
    // DRIVEN BY TIME, NOT BY CALL COUNT.
    //
    // The first version counted service calls and never fired. `service` runs
    // from the USB interrupt, and once there is nothing left to send the host's
    // polls are NAK'd and generate almost no interrupts — so the counter crawled
    // and a heartbeat that was supposed to prove liveness proved nothing.
    //
    // A clock is the right basis anyway: "every second" is what a person
    // watching means, and it stays true however often this happens to run.
    if HEARTBEAT_MS == 0 {
        return;
    }
    let Some(clock) = dlc_platform_embedded::clock::installed() else {
        return;
    };
    let now_us = clock();
    let last = LAST_BEAT.load(Ordering::Relaxed) as u64;
    if now_us.saturating_sub(last) >= HEARTBEAT_MS as u64 * 1_000 {
        LAST_BEAT.store(now_us as usize, Ordering::Relaxed);
        let uptime_ms = now_us / 1000;
        let mut beat = LineBuffer::new();
        use core::fmt::Write;
        let _ = write!(beat, "~ alive {uptime_ms} ms\r\n");
        let _ = serial.write(beat.as_bytes());
    }
}

// HOW OFTEN TO SAY SO — a flash-time world parameter (build.rs). Zero disables
// it, and the branch below compiles away entirely in that build.
include!(concat!(env!("OUT_DIR"), "/heartbeat.rs"));

/// Microseconds at the last heartbeat.
static LAST_BEAT: AtomicUsize = AtomicUsize::new(0);

/// Whether a host had the port open last time we looked.
static WAS_OPEN: AtomicBool = AtomicBool::new(false);

/// A short line built without the heap, for the heartbeat.
struct LineBuffer {
    bytes: [u8; 40],
    len: usize,
}

impl LineBuffer {
    fn new() -> Self {
        Self { bytes: [0; 40], len: 0 }
    }
    fn as_bytes(&self) -> &[u8] {
        &self.bytes[..self.len]
    }
}

impl core::fmt::Write for LineBuffer {
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

/// Re-send the log from the beginning.
///
/// So a terminal attached after the fact sees the whole run rather than an empty
/// port. The idle loop calls this on a long timer; nothing else should.
pub fn rewind() {
    SENT.store(0, Ordering::Relaxed);
    NOTICE_SENT.store(false, Ordering::Relaxed);
}

/// Service the port from outside the interrupt. See `service`.
///
/// The exclusion lives in `Shared` now, not here — which is the point of the
/// refactor: a caller cannot forget it, because there is no way to reach the
/// device without it.
pub fn pump() {
    service();
}

/// THE INTERRUPT. This is what makes the log survive a hang.
///
/// It runs regardless of what main is doing — mid-`execute`, mid-stall, or
/// finished — which is the entire justification for the machinery above.
#[interrupt]
fn USBCTRL_IRQ() {
    service();
}

// ---------------------------------------------------------------------------
// Rebooting into BOOTSEL, so the badge can be reflashed without being touched
// ---------------------------------------------------------------------------

/// The interface `picotool` looks for when asked to reboot a running board.
///
/// # Why this is written here and not taken from a crate
///
/// `usbd-picotool-reset` implements exactly this protocol and was the obvious
/// dependency. It is WRONG FOR THIS CHIP: it calls
/// `rp2040_hal::rom_data::reset_to_usb_boot`, an RP2040 bootrom entry point, and
/// pulls `rp2040-hal` and `rp2040-pac` in behind it. The RP2350 bootrom has a
/// different table and a different function — `reboot(flags, delay_ms, p0, p1)`
/// — so that call would jump somewhere arbitrary on this board.
///
/// It would also have LOOKED fine: the crate compiles, the interface enumerates,
/// picotool finds it, and the fault appears only when someone actually asks for
/// a reboot. Reading the source took a minute and was the whole difference.
///
/// The protocol is the crate's; only the reset call is ours.
///
/// # What it buys
///
/// Without it, reflashing needs a hand on the board — hold BOOT, tap RESET. With
/// it, `picotool reboot -u -f` does it over the cable already carrying the log,
/// so the build/flash/read loop runs without touching the badge.
///
/// # Why the device must claim to be a Pi
///
/// picotool only probes devices with Raspberry Pi's vendor id (0x2e8a) and a
/// known product id. The builder in `start` already uses `0x2e8a/0x000a`, which
/// is what makes this discoverable at all.
struct PicoToolReset<'a, B: usb_device::bus::UsbBus> {
    interface: usb_device::class_prelude::InterfaceNumber,
    name: usb_device::class_prelude::StringIndex,
    _bus: core::marker::PhantomData<&'a B>,
}

/// Vendor-specific class, with the subclass/protocol pair picotool matches on.
/// From pico-sdk's `pico/usb_reset_interface.h`.
const RESET_CLASS_VENDOR: u8 = 0xFF;
const RESET_SUBCLASS: u8 = 0x00;
const RESET_PROTOCOL: u8 = 0x01;
const RESET_REQUEST_BOOTSEL: u8 = 0x01;

/// RP2350 bootrom reboot type: a normal restart, back into this firmware.
#[cfg(badge_control)]
const REBOOT_TYPE_NORMAL: u32 = 0x0000;

/// RP2350 bootrom reboot type: into BOOTSEL. Datasheet 5.5.10.1.
const REBOOT_TYPE_BOOTSEL: u32 = 0x0002;

impl<'a, B: usb_device::bus::UsbBus> PicoToolReset<'a, B> {
    fn new(alloc: &'a usb_device::bus::UsbBusAllocator<B>) -> Self {
        Self {
            interface: alloc.interface(),
            name: alloc.string(),
            _bus: core::marker::PhantomData,
        }
    }
}

impl<B: usb_device::bus::UsbBus> usb_device::class::UsbClass<B> for PicoToolReset<'_, B> {
    fn get_configuration_descriptors(
        &self,
        writer: &mut usb_device::descriptor::DescriptorWriter,
    ) -> usb_device::Result<()> {
        writer.interface_alt(
            self.interface,
            0,
            RESET_CLASS_VENDOR,
            RESET_SUBCLASS,
            RESET_PROTOCOL,
            Some(self.name),
        )
    }

    fn get_string(
        &self,
        index: usb_device::class_prelude::StringIndex,
        _lang: usb_device::LangID,
    ) -> Option<&str> {
        (index == self.name).then_some("Reset")
    }

    fn control_out(&mut self, xfer: usb_device::class_prelude::ControlOut<B>) {
        let request = xfer.request();
        // NARROW ON PURPOSE. Every class sees every control transfer, so a class
        // that does not check the recipient and interface number will act on a
        // request meant for the CDC port sitting beside it — and this particular
        // action is "reboot the board", which is not one to take on a maybe.
        if !(request.request_type == usb_device::control::RequestType::Class
            && request.recipient == usb_device::control::Recipient::Interface
            && request.index == u8::from(self.interface) as u16)
        {
            return;
        }

        if request.request == RESET_REQUEST_BOOTSEL {
            // No accept/reject: the board is gone before a reply could be sent.
            // A short delay lets the host finish the transfer first, which is
            // what stops the reboot looking like a USB fault.
            //
            // p0 is the activity-LED gpio mask and p1 the interface-disable mask;
            // zero for both means "no LED, leave mass storage and PicoBoot
            // enabled", which is what a plain `picotool reboot -u -f` expects.
            hal::rom_data::reboot(REBOOT_TYPE_BOOTSEL, 10, 0, 0);
        }
    }
}

// ---------------------------------------------------------------------------
// The control channel (BADGE-CONTROL-PLAN Phase 1)
// ---------------------------------------------------------------------------
//
// TRANSPORT ONLY. The framing, the decode and the answer's shape all live in
// `dlc_platform_embedded::control`, because none of them names a peripheral and
// a browser world needs the same ones (D6a). What is here is the part that is
// genuinely about this board: a USB endpoint and two buffers.

/// The control channel's buffers, guarded together (BUG 2 in `shared.rs`).
///
/// These were four `static mut`s written from the interrupt AND from main, with
/// a comment claiming they could not interleave. They could, and both channels
/// went silent when they did.
///
/// ONE CELL because the invariants are BETWEEN them: a buffer and its length, a
/// queue and how much of it has gone. Guarding them separately would let a
/// caller take one and believe it was safe.
#[cfg(badge_control)]
struct Control {
    /// Bytes received but not yet consumed. A frame may arrive split across USB
    /// packets, so what cannot be parsed yet has to be kept.
    rx: [u8; 512],
    rx_len: usize,
    /// A reply waiting to go out, and how much of it has gone. The CDC endpoint
    /// takes 64 bytes at a time, so a reply drains across several interrupts.
    tx: [u8; 512],
    tx_len: usize,
    tx_sent: usize,
    /// The notice frame in flight, and how much of it has gone.
    ///
    /// SEPARATE FROM `tx` because a reply has someone blocked on it and a notice
    /// does not. Sharing one buffer would let a log line overwrite an answer
    /// somebody was waiting for.
    nx: [u8; 512],
    nx_len: usize,
    nx_sent: usize,
    /// Which notice frame goes next — an index into the run, not into a buffer.
    ///
    /// Backfill is what makes it a frame NUMBER: subscribing sets it to zero, and
    /// zero means "the first thing this world ever said", however long ago that
    /// was. A client that attaches at t=10s still gets the boot that failed,
    /// which is the case a bring-up log exists for.
    notice_cursor: usize,
    /// Which notices the client asked for, as `1 << notice`.
    subscribed: u32,
}

#[cfg(badge_control)]
static CONTROL: crate::shared::Shared<Control> = crate::shared::Shared::new(Control {
    rx: [0; 512],
    rx_len: 0,
    tx: [0; 512],
    tx_len: 0,
    tx_sent: 0,
    nx: [0; 512],
    nx_len: 0,
    nx_sent: 0,
    notice_cursor: 0,
    subscribed: 0,
});

/// Take bytes off the wire and answer any complete frame.
#[cfg(badge_control)]
fn control_receive(bytes: &[u8]) {
    use dlc_platform_embedded::control;

    if bytes.is_empty() {
        return;
    }
    CONTROL.with(|c| {
        for byte in bytes {
            if c.rx_len < c.rx.len() {
                let at = c.rx_len;
                c.rx[at] = *byte;
                c.rx_len += 1;
            } else {
                // FULL MEANS SOMETHING IS WRONG — a sender streaming garbage, or
                // a frame larger than this world accepts. Dropping the oldest
                // byte keeps the buffer moving so a later, valid frame still
                // lands, rather than wedging until reset.
                c.rx.copy_within(1.., 0);
                let last = c.rx.len() - 1;
                c.rx[last] = *byte;
            }
        }

        loop {
            match control::scan(&c.rx[..c.rx_len]) {
                control::Found::Incomplete => return,
                control::Found::Skip(n) => {
                    c.rx.copy_within(n..c.rx_len, 0);
                    c.rx_len -= n;
                }
                control::Found::Frame(request, consumed) => {
                    // TOO EARLY TO ANSWER PROPERLY, so say so in a frame built
                    // on the stack. Silence here would be indistinguishable from
                    // a wedged badge, which is the ambiguity this whole channel
                    // exists to remove.
                    if !HEAP_READY.load(Ordering::Acquire) {
                        let mut body = [0u8; 96];
                        let mut framed = [0u8; 128];
                        let reply = control::response_into(
                            &mut body,
                            false,
                            "the world is still starting; no heap yet",
                        )
                        .and_then(|len| {
                            control::frame_into(&mut framed, control::KIND_RESPONSE, &body[..len])
                        });
                        c.rx.copy_within(consumed..c.rx_len, 0);
                        c.rx_len -= consumed;
                        if let Some(len) = reply {
                            c.tx[..len].copy_from_slice(&framed[..len]);
                            c.tx_len = len;
                            c.tx_sent = 0;
                        }
                        return;
                    }

                    // SUBSCRIBE IS HANDLED HERE, not in `answer`. It is the one
                    // verb that CHANGES this cell, and `answer` is pure so that
                    // it can never reach for a borrow its caller already holds —
                    // which would be a reentrant `with` and a panic.
                    let reply = if request.verb == control::VERB_SUBSCRIBE {
                        // The mask is read out of `rx` in a block of its own, so
                        // the immutable borrow ends before the cell is written.
                        let granted = {
                            let body = match request.payload {
                                Some((start, end)) if end <= c.rx_len => &c.rx[start..end],
                                // NO PAYLOAD IS A VALID UNSUBSCRIBE, not an
                                // error: an empty `Subscription` encodes to zero
                                // bytes, so "stop sending me things" arrives
                                // looking exactly like this.
                                _ => &[][..],
                            };
                            control::parse_subscription(body).unwrap_or(0)
                                & control::NOTICES_SUPPORTED
                        };
                        let began = c.subscribed;
                        c.subscribed = granted;
                        // FROM THE TOP OF THE RUN, whenever the log is newly
                        // subscribed to. Re-subscribing is how a client asks for
                        // the history again, and re-sending it is cheaper than a
                        // second verb meaning "replay".
                        let log_wanted = 1 << control::NOTICE_LOG;
                        if granted & log_wanted != 0 && began & log_wanted == 0 {
                            c.notice_cursor = 0;
                        }
                        // A SUBSCRIPTION CHANGE ABANDONS THE FRAME IN FLIGHT.
                        // Its bytes belong to a cursor that may have just moved,
                        // and half a frame followed by the start of another is
                        // how a reader desynchronises.
                        c.nx_len = 0;
                        c.nx_sent = 0;
                        control::frame(
                            control::KIND_RESPONSE,
                            &control::response(true, "", &control::subscription(granted)),
                        )
                    } else {
                        // The payload is borrowed out of `rx` for the call and
                        // the borrow ends with it, which is what lets `answer`
                        // stay free of this cell.
                        let payload = match request.payload {
                            Some((start, end)) if end <= c.rx_len => &c.rx[start..end],
                            _ => &[][..],
                        };
                        // `None` means the verb was accepted and will be answered
                        // later, by main. Nothing is queued now.
                        match answer(&request, payload) {
                            Some(reply) => reply,
                            None => {
                                c.rx.copy_within(consumed..c.rx_len, 0);
                                c.rx_len -= consumed;
                                return;
                            }
                        }
                    };
                    c.rx.copy_within(consumed..c.rx_len, 0);
                    c.rx_len -= consumed;
                    // Queue it here, inside the same section, so a reply cannot
                    // be overwritten by a second frame arriving between the two.
                    let n = reply.len().min(c.tx.len());
                    c.tx[..n].copy_from_slice(&reply[..n]);
                    c.tx_len = n;
                    c.tx_sent = 0;
                    // ONE ANSWER AT A TIME: the outbound buffer holds one reply,
                    // and a second would overwrite the first before it was sent.
                    return;
                }
            }
        }
    });
}

/// Produce the reply for a request.
///
/// PURE: it reads world state and returns bytes, touching no buffers. That is
/// what lets the caller hold the control cell across the whole receive-and-queue
/// step without this reaching for it too — which would be a reentrant borrow and
/// a panic.
#[cfg(badge_control)]
fn answer(
    request: &dlc_platform_embedded::control::Request,
    payload: &[u8],
) -> Option<alloc::vec::Vec<u8>> {
    use dlc_platform_embedded::control;

    let payload = match request.verb {
        control::VERB_GET_WORLD_STATE => {
            let uptime_ms = dlc_platform_embedded::clock::installed()
                .map(|clock| clock() / 1000)
                .unwrap_or(0);
            let state = control::WorldState {
                world: crate::WORLD.code(),
                tier: control::TIER_RP2350,
                version: env!("CARGO_PKG_VERSION"),
                screen: crate::world::screen_code(),
                input: crate::world::input_code(),
                text: crate::world::text_code(),
                activity: ACTIVITY.load(Ordering::Relaxed) as u32,
                app: "",
                app_activity: dlc_platform_embedded::activity::get(),
                uptime_ms,
            };
            control::response(true, "", &state.encode())
        }
        // DRIVE IT (D3). A press queued here is consumed by the next widget
        // poll and is indistinguishable from a finger — see buttons.rs.
        control::VERB_PRESS_BUTTON => match control::parse_button(payload) {
            Some(button) if crate::buttons::press(button) => control::response(true, "", &[]),
            // NAMED REFUSAL. A caller built against a newer proto that asks for a
            // button this board does not have needs to know that now, not to
            // wait for an effect that is never coming.
            Some(_) => control::response(false, "no such button on this world", &[]),
            None => control::response(false, "malformed PressButtonRequest", &[]),
        },

        // WHAT THE PANEL SAYS, taken from the same draw calls that reached it.
        control::VERB_GET_SCREEN => {
            let mut out = alloc::vec::Vec::new();
            let drawn = crate::display::mirror::rows(|row| control::screen_row(&mut out, row));
            if drawn {
                control::screen_dims(
                    &mut out,
                    crate::display::mirror::COLS as u32,
                    crate::display::mirror::ROWS as u32,
                );
                control::response(true, "", &out)
            } else {
                // BUSY IS NOT BLANK. Main holds the grid while it redraws, and
                // answering with no rows would report an empty screen.
                control::response(false, "the panel is being redrawn; ask again", &[])
            }
        }

        // PASS-THROUGH (D2). The one verb whose answer this interrupt cannot
        // produce: it needs the engine, which lives on main's stack. Parked for
        // main, answered when the app returns — see passthrough.rs.
        control::VERB_EXECUTE => {
            let accepted = match control::parse_execute(payload) {
                Some((method, range)) => {
                    let bytes = match range {
                        Some((start, end)) => &payload[start..end],
                        None => &[][..],
                    };
                    crate::passthrough::offer(method, bytes)
                }
                None => {
                    return Some(control::frame(
                        control::KIND_RESPONSE,
                        &control::response(false, "malformed ExecuteRequest", &[]),
                    ))
                }
            };
            // NO REPLY YET is the whole point, so returning `None` here is not an
            // error path — the answer arrives from main once the app has run.
            return if accepted {
                None
            } else {
                Some(control::frame(
                    control::KIND_RESPONSE,
                    &control::response(
                        false,
                        "a request is already running; one at a time",
                        &[],
                    ),
                ))
            };
        }

        // WHAT IS INSTALLED, including what will not run and why.
        control::VERB_LIST_PAYLOADS => {
            let mut out = alloc::vec::Vec::new();
            let selected = crate::installed::each(|index, payload| {
                control::payload_info(
                    &mut out,
                    index as u32,
                    payload.name,
                    payload.bytes.len() as u32,
                    crate::installed::integrity_code(payload.integrity),
                    payload.entry_method,
                    payload.is_default(),
                    payload.runnable(),
                );
            });
            match selected {
                Some(selected) => {
                    control::payloads_selected(&mut out, selected as u32);
                    control::response(true, "", &out)
                }
                None => control::response(false, "the catalog is busy; ask again", &[]),
            }
        }

        // CHOOSE WHAT RUNS NEXT. Noted, not run — see installed.rs.
        control::VERB_SELECT_PAYLOAD => match control::parse_index(payload) {
            Some(index) if crate::installed::request(index as usize) => {
                control::response(true, "", &[])
            }
            Some(_) => control::response(false, "no payload with that index", &[]),
            None => control::response(false, "malformed SelectPayloadRequest", &[]),
        },

        // START OVER.
        //
        // THE REPLY GOES FIRST. `reboot` does not return, so rebooting here would
        // drop the connection with the answer still queued — and a client cannot
        // tell that from a badge that crashed on the request. Instead this arms a
        // flag that `control_send` acts on once the last byte is out, which turns
        // "it died" into "it said yes, then went".
        control::VERB_REBOOT => {
            REBOOT_WHEN_SENT.store(true, Ordering::Release);
            control::response(true, "", &[])
        }

        // A VERB THIS WORLD DOES NOT KNOW is answered, not ignored. A caller
        // built against a newer proto gets a refusal naming the verb rather than
        // a silence it has to time out on.
        other => control::response(false, "unknown verb", &[other as u8]),
    };
    Some(control::frame(control::KIND_RESPONSE, &payload))
}


// ---------------------------------------------------------------------------
// Notices: the log, as frames, kept so a late client still gets the beginning
// ---------------------------------------------------------------------------

/// The structured half of the log (D8b), held as ALREADY-FRAMED bytes.
///
/// # Why a ring of frames and not a cursor into the text
///
/// The first working version pointed a cursor at the text log and framed lines
/// out of it on the way past. That got backfill for free, and it was wrong in
/// two ways that were not obvious until it ran:
///
///   - **It could not date anything.** The text log stores words and no times,
///     so a backfilled line was stamped with the clock at FRAMING time — which
///     said the whole run happened at the instant somebody subscribed. That is
///     the exact failure `LogLine.uptime_ms` was documented against.
///   - **It could not carry structure.** `report.rs` knows the stage, the
///     result and whether only silicon could have answered; by the time a line
///     is text, all of that has been flattened into prose. Recovering it would
///     have meant matching on `[OK]` and leading spaces — a regex over rendered
///     output, which is precisely what frames exist to stop a reader doing.
///
/// Framing at the moment a line is WRITTEN fixes both, because that is the only
/// moment when the time and the structure are both still known.
///
/// # Why the frames are encoded, not the fields
///
/// A slot holds finished bytes. The alternative — keeping the fields and
/// encoding on the way out — would put the encoder in the interrupt, and would
/// need a heap there, which stages 1 to 3 do not have.
///
/// # What it costs, and the warning it is not
///
/// The module header rejects "disable interrupts around every log write" because
/// the report writes constantly and masking USB service across all of it would
/// stall enumeration. This masks for a bounded memcpy on LINE COMPLETION only —
/// about a microsecond at 150 MHz, against a 1 ms USB frame. Same technique,
/// different order of magnitude, and the alternative is a lock-free ring whose
/// correctness rests on "the writer cannot lap the reader mid-frame" — which is
/// the shape of reasoning `shared.rs` exists because we got wrong three times.
#[cfg(badge_control)]
mod notices {
    use dlc_platform_embedded::control;

    /// Big enough for a frame around a full-width line and a stage name.
    const SLOT: usize = 160;
    /// About four screens of bring-up. Beyond that the oldest go, and the count
    /// of what went is reported rather than quietly dropped.
    const SLOTS: usize = 40;

    pub struct Notices {
        slots: [[u8; SLOT]; SLOTS],
        lens: [u16; SLOTS],
        /// Frames ever appended. Monotonic, so a subscriber's cursor is just a
        /// frame number and "have I missed some" is one comparison.
        head: usize,
        /// The oldest frame still held.
        tail: usize,
        /// How many were lost to the ring wrapping.
        dropped: usize,
        /// Metadata for the next line to complete, set by `report.rs`.
        level: u32,
        scope: u32,
        /// `Stage` in control.proto — a number, so remembering "which stage are
        /// we in" costs four bytes and cannot drift from how a label is worded.
        stage: u32,
    }

    static NOTICES: crate::shared::Shared<Notices> = crate::shared::Shared::new(Notices {
        slots: [[0; SLOT]; SLOTS],
        lens: [0; SLOTS],
        head: 0,
        tail: 0,
        dropped: 0,
        level: control::LEVEL_NOTE,
        scope: 0,
        stage: 0,
    });

    /// Declare what the next completed line IS.
    ///
    /// Set immediately before the write that closes the line, and consumed by
    /// it — so a line nobody described is a note, which is what an unannotated
    /// `writeln!` in main actually is.
    pub fn mark(level: u32, scope: u32) {
        NOTICES.with(|n| {
            n.level = level;
            n.scope = scope;
        });
    }

    /// A stage has been announced. Records the name and emits `STAGE_OPEN`.
    ///
    /// THE FRAME GOES OUT BEFORE THE WORK IS ATTEMPTED, which is the whole
    /// point: if the stage hangs, this is the last thing a client hears, and it
    /// names what the world was doing. A frame stream built only on completed
    /// lines said nothing at all in that case.
    pub fn stage_opened(stage: u32, scope: u32) {
        NOTICES.with(|n| {
            n.stage = stage;
            n.level = control::LEVEL_NOTE;
            n.scope = scope;
        });
        append(control::LEVEL_STAGE_OPEN, scope, "");
    }

    /// A line finished. Frames it with whatever was declared, then resets.
    pub fn line_completed(text: &str) {
        let (level, scope) = NOTICES.with(|n| (n.level, n.scope));
        append(level, scope, text);
        NOTICES.with(|n| {
            // BACK TO A NOTE. Metadata describes ONE line; leaving it set would
            // label every following line with the last stage's result.
            n.level = control::LEVEL_NOTE;
        });
    }

    fn append(level: u32, scope: u32, text: &str) {
        // The clock may not be installed yet — stages 1 to 3 run before it. Zero
        // encodes as absent, so an undated line says so rather than claiming boot.
        let uptime_ms = dlc_platform_embedded::clock::installed()
            .map(|clock| clock() / 1000)
            .unwrap_or(0);

        NOTICES.with(|n| {
            let mut body = [0u8; SLOT];
            let stage = n.stage;
            let Some(len) = control::log_line_into(&mut body, uptime_ms, stage, level, scope, text)
            else {
                // TOO LONG TO FRAME IS DROPPED, NOT TRUNCATED. The text stream
                // still carried it, and a short frame would desynchronise a
                // reader for every frame after it.
                return;
            };

            let index = n.head % SLOTS;
            let Some(framed) = control::frame_into(&mut n.slots[index], control::KIND_LOG, &body[..len])
            else {
                return;
            };
            n.lens[index] = framed as u16;
            n.head += 1;
            if n.head - n.tail > SLOTS {
                n.tail = n.head - SLOTS;
                n.dropped += 1;
            }
        });
    }

    /// The frame at `cursor`, copied into `into`. Returns its length and the
    /// cursor to use next.
    ///
    /// CLAMPS FORWARD if the ring has moved past the cursor: a subscriber that
    /// fell behind gets the oldest frame still held rather than nothing, and the
    /// gap is counted in `dropped`.
    pub fn frame_at(cursor: usize, into: &mut [u8]) -> Option<(usize, usize)> {
        NOTICES.try_with(|n| {
            let at = if cursor < n.tail { n.tail } else { cursor };
            if at >= n.head {
                return None;
            }
            let index = at % SLOTS;
            let len = n.lens[index] as usize;
            if len > into.len() {
                return None;
            }
            into[..len].copy_from_slice(&n.slots[index][..len]);
            Some((len, at + 1))
        })
        .flatten()
    }
}

/// Whether the allocator has memory behind it.
///
/// EVERY REPLY ON THIS CHANNEL ALLOCATES, and the heap does not exist until
/// PSRAM comes up at stage 4. A question asked before that — which is precisely
/// when somebody debugging a bring-up would ask one — allocated against an
/// uninitialised allocator. The board would have gone down answering a question
/// about why it was not well.
static HEAP_READY: AtomicBool = AtomicBool::new(false);

/// Called once `HEAP.init` has run.
pub fn heap_is_ready() {
    HEAP_READY.store(true, Ordering::Release);
}

/// Declare what the next completed log line is. See `notices::mark`.
#[cfg(badge_control)]
pub fn mark(level: u32, scope: u32) {
    notices::mark(level, scope);
}

/// A stage was announced. See `notices::stage_opened`.
#[cfg(badge_control)]
pub fn stage_opened(stage: u32, scope: u32) {
    notices::stage_opened(stage, scope);
}

/// Push one line of the log as a frame. Returns whether anything was sent.
///
/// ONE LINE PER CALL, because a frame must be a whole frame: a reader that gets
/// half of one and then something else has no way back to the stream. Draining a
/// whole run therefore takes as many calls as it has lines, which at USB service
/// rates is milliseconds and is not worth the buffer it would cost to batch.
///
/// The line is carried VERBATIM — the same bytes the text stream sent. Nothing
/// here infers a stage or a level from how a line is punctuated, and it would be
/// easy to: the world writes `... [OK]` and indents its notes. That inference
/// would be a regex over rendered prose dressed up as structure, which is
/// precisely what a frame reader was supposed to stop having to do. Stage and
/// level want to come from the call sites that already know them, and until they
/// do, saying nothing is the honest encoding.
#[cfg(badge_control)]
fn control_notices(serial: &mut usbd_serial::SerialPort<hal::usb::UsbBus>) -> bool {
    use dlc_platform_embedded::control;

    CONTROL.with(|c| {
        if c.subscribed & (1 << control::NOTICE_LOG) == 0 {
            return false;
        }

        // FINISH WHAT IS IN FLIGHT before taking another frame. A frame must be
        // sent whole: half of one followed by the start of another is how a
        // reader desynchronises, and it cannot recover without the next magic.
        if c.nx_sent < c.nx_len {
            if let Ok(n) = serial.write(&c.nx[c.nx_sent..c.nx_len]) {
                c.nx_sent += n;
            }
            return true;
        }

        // ALREADY FRAMED, by main, at the moment the line was written. Nothing
        // here encodes, allocates, or reads a clock — which is what lets this
        // run in the interrupt during stages that have no heap yet.
        let Some((len, next)) = notices::frame_at(c.notice_cursor, &mut c.nx) else {
            return false;
        };
        c.nx_len = len;
        c.nx_sent = 0;
        c.notice_cursor = next;
        if let Ok(n) = serial.write(&c.nx[..c.nx_len]) {
            c.nx_sent += n;
        }
        true
    })
}

/// Queue a reply main produced, for the interrupt to send.
///
/// The counterpart to `answer` returning `None`: pass-through is the one verb
/// whose answer comes from the other side of the machine.
#[cfg(badge_control)]
pub fn reply(success: bool, output: &[u8], error: &str) {
    use dlc_platform_embedded::control;

    let framed = control::frame(
        control::KIND_RESPONSE,
        // `ok` is the WORLD's verdict and is true: it ran what it was asked. What
        // the app made of it is `success`, inside.
        &control::response(
            true,
            "",
            &control::execute_response(success, output, error),
        ),
    );
    CONTROL.with(|c| {
        let len = framed.len().min(c.tx.len());
        c.tx[..len].copy_from_slice(&framed[..len]);
        c.tx_len = len;
        c.tx_sent = 0;
    });
    crate::passthrough::finished();
}

/// Set when a client asked for a reboot, cleared by going.
#[cfg(badge_control)]
static REBOOT_WHEN_SENT: AtomicBool = AtomicBool::new(false);

/// Push any pending reply. Returns whether one is still outstanding.
#[cfg(badge_control)]
fn control_send(serial: &mut usbd_serial::SerialPort<hal::usb::UsbBus>) -> bool {
    CONTROL.with(|c| {
        if c.tx_sent >= c.tx_len {
            c.tx_len = 0;
            c.tx_sent = 0;
            // THE LAST BYTE IS OUT, so the promise made above has been kept and
            // the board can go. `reboot` does not return.
            if REBOOT_WHEN_SENT.load(Ordering::Acquire) {
                // A short delay lets the host read what was just written before
                // the device disappears, the same courtesy the BOOTSEL path pays.
                hal::rom_data::reboot(REBOOT_TYPE_NORMAL, 20, 0, 0);
            }
            return false;
        }
        if let Ok(n) = serial.write(&c.tx[c.tx_sent..c.tx_len]) {
            c.tx_sent += n;
        }
        c.tx_sent < c.tx_len
    })
}
