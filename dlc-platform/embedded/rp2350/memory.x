/* Memory layout for the Pimoroni Tufty 2350 (RP2350B).
 *
 * ADAPTED FROM rp-hal's own `rp235x-hal-examples/memory.x` — deliberately, after
 * three failed attempts to write it from first principles. Only FLASH's length
 * differs: this board has 16 MB where the reference assumes 2 MB, and that
 * matters because the AOT component is 1.59 MB.
 */
MEMORY {
    FLASH : ORIGIN = 0x10000000, LENGTH = 16384K
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
