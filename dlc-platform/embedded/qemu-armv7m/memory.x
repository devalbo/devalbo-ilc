/* QEMU's mps2-an505, NOT the badge.
 *
 * The machine has far more RAM than an RP2350 (4 MB vs 520 KB), which would make
 * a "does it fit" test meaningless — so the firmware pins its HEAP to the
 * badge's size instead, and the constraint lives there rather than here.
 */
MEMORY
{
  FLASH : ORIGIN = 0x00000000, LENGTH = 4M
  RAM   : ORIGIN = 0x20000000, LENGTH = 4M
}
