/* Memory layout for the Pimoroni Tufty 2350 (RP2350B).
 *
 * ADAPTED FROM rp-hal's own `rp235x-hal-examples/memory.x` — deliberately, after
 * three failed attempts to write it from first principles.
 *
 * FLASH IS 4 MB, NOT THE BOARD'S 16, and that is a partition rather than a
 * mistake. Everything from 0x10400000 up is the payload catalog — the region a
 * payload UF2 is dragged onto — and declaring it out of FLASH's reach is what
 * makes the split real: firmware that grew into the region would fail to LINK
 * instead of quietly overwriting an app someone flashed last week.
 *
 * WHY 4 MB AND NOT 1, which was the first attempt and did not link. FLASH must
 * hold the firmware AND its BUILT-IN PAYLOAD, because `BADGE_PAYLOAD` becomes
 * `.rodata`: 189 KB of bring-up image plus hello's 869 KB overflowed a 1 MB cap
 * by 360 KB. 4 MB clears the worst case that matters — a tictactoe-sized default
 * at ~2.2 MB — and still leaves 12 MB of region, which is more payloads than the
 * catalog will scan.
 *
 * `board::PAYLOAD_BASE` must equal 0x10000000 + this LENGTH. Only one of the two
 * files can produce an error, so changing either means changing both.
 */
MEMORY {
    FLASH : ORIGIN = 0x10000000, LENGTH = 4096K
    RAM   : ORIGIN = 0x20000000, LENGTH = 512K
    SRAM8 : ORIGIN = 0x20080000, LENGTH = 4K
    SRAM9 : ORIGIN = 0x20081000, LENGTH = 4K
}

/* The image definition block, which the bootloader scans for in the first few KB
 * of flash. Without it correctly placed the link still succeeds, the UF2 still
 * builds, and `picotool info` says family `absolute` instead of `rp2350-arm-s` —
 * the only symptom being a badge that does nothing.
 */
SECTIONS {
    .start_block : ALIGN(4)
    {
        __start_block_addr = .;
        KEEP(*(.start_block));
        KEEP(*(.boot_info));
    } > FLASH
} INSERT AFTER .vector_table;

/* THE LINE THAT WAS MISSING, and the reason two earlier attempts failed with
 * ".start_block overlaps .text". cortex-m-rt places .text at `_stext`, which
 * defaults to just after the vector table — exactly where .start_block now sits.
 * Moving `_stext` past the block is what makes room; INSERT alone does not.
 */
_stext = ADDR(.start_block) + SIZEOF(.start_block);

/* Binary info: the picotool metadata that `picotool info` reads back. The
 * `binary-info` HAL feature was disabled earlier when it failed to link with
 * "undefined symbol: __bi_entries_start" — that was this section missing, not
 * the feature being wrong.
 */
SECTIONS {
    .bi_entries : ALIGN(4)
    {
        __bi_entries_start = .;
        KEEP(*(.bi_entries));
        . = ALIGN(4);
        __bi_entries_end = .;
    } > FLASH
} INSERT AFTER .text;

SECTIONS {
    .end_block : ALIGN(4)
    {
        __end_block_addr = .;
        KEEP(*(.end_block));
        __flash_binary_end = .;
    } > FLASH
} INSERT AFTER .uninit;

PROVIDE(start_to_end = __end_block_addr - __start_block_addr);
PROVIDE(end_to_start = __start_block_addr - __end_block_addr);
