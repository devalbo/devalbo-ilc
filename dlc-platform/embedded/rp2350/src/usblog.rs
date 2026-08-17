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
}

impl LogBuffer {
    pub const fn new() -> Self {
        Self {
            bytes: [0; 8192],
            len: AtomicUsize::new(0),
            truncated: AtomicBool::new(false),
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

/// The allocator must outlive the device and the port that borrow from it, and
/// both live for the rest of the program — so it is a static, which is the
/// standard shape for `usb-device` on this family.
///
/// SAFETY for all three: written exactly once by `start`, before the interrupt
/// is unmasked, and read only by the ISR afterwards. Single core, and `start`
/// refuses a second call.
static mut USB_BUS: Option<usb_device::bus::UsbBusAllocator<hal::usb::UsbBus>> = None;
static mut USB_SERIAL: Option<usbd_serial::SerialPort<hal::usb::UsbBus>> = None;
static mut USB_RESET: Option<PicoToolReset<'static, hal::usb::UsbBus>> = None;
static mut USB_DEVICE: Option<usb_device::device::UsbDevice<hal::usb::UsbBus>> = None;

/// How much of the log the host has already been given.
///
/// Touched only by the ISR, so it needs no synchronisation with main — but it is
/// an atomic anyway, because the repeat-on-idle logic below resets it and a
/// plain `static mut` read from one context and reset from another is exactly
/// the pattern that stops being true when someone adds a second caller.
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
    // SAFETY: single core, interrupt not yet unmasked, so nothing else can be
    // looking at these.
    unsafe {
        if (*core::ptr::addr_of!(USB_BUS)).is_some() {
            // A second call would leak the first device and hand the ISR a
            // dangling borrow. Refusing is the only safe answer and this is a
            // programming error, not a runtime condition.
            return;
        }

        let bus = usb_device::bus::UsbBusAllocator::new(hal::usb::UsbBus::new(
            usb, dpram, clock, true, resets,
        ));
        USB_BUS = Some(bus);
        let bus_ref = (*core::ptr::addr_of!(USB_BUS)).as_ref().unwrap();

        // ORDER MATTERS: the port must be allocated before the device is built,
        // because the device enumerates the interfaces that exist at build time.
        // Build the device first and the host sees no CDC endpoints at all.
        USB_SERIAL = Some(usbd_serial::SerialPort::new(bus_ref));
        USB_RESET = Some(PicoToolReset::new(bus_ref));
        USB_DEVICE = Some(
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
            // COMPOSITE, because there are now two functions on this cable: the
            // CDC log and picotool's reset interface. Declaring CDC at the DEVICE
            // level would tell a host the whole device is a serial port, and the
            // vendor interface beside it becomes a descriptor the host has no
            // reason to bind a driver to.
            .composite_with_iads()
            .build(),
        );

        cortex_m::peripheral::NVIC::unmask(hal::pac::Interrupt::USBCTRL_IRQ);
    }
}

/// Service the port: answer the host, then hand over whatever is unsent.
///
/// Shared by the ISR and the idle loop. The idle loop still calls it so that a
/// badge which finishes normally keeps draining even if interrupts were never
/// unmasked — a belt-and-braces path that costs one function call.
fn service() {
    // SAFETY: `start` published these before unmasking, and only this function
    // touches them afterwards.
    let (Some(device), Some(serial), Some(reset)) = (
        unsafe { (*core::ptr::addr_of_mut!(USB_DEVICE)).as_mut() },
        unsafe { (*core::ptr::addr_of_mut!(USB_SERIAL)).as_mut() },
        unsafe { (*core::ptr::addr_of_mut!(USB_RESET)).as_mut() },
    ) else {
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

    // Drain anything the host typed; an unread RX buffer stalls some stacks.
    let mut discard = [0u8; 64];
    let _ = serial.read(&mut discard);

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
            unsafe {
                hal::rom_data::reboot(REBOOT_TYPE_BOOTSEL, 10, 0, 0);
            }
        }
    }
}
