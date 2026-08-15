//! The catalog, as a FAT12 volume a PC can mount — **generated, never stored**.
//!
//! WHAT THIS IS FOR (D11): plug in a running badge and see the apps it holds, by
//! name, in a directory — instead of an opaque flash region whose contents can
//! only be inferred from a serial log.
//!
//! **Nothing here is written to flash.** A PC reading a USB drive asks for 512-byte
//! blocks, and this module answers each one by computing what a FAT12 volume WOULD
//! contain if the catalog were a filesystem. The boot sector, the FAT, and the
//! root directory are synthesised per request; a data block is answered from the
//! payload bytes already in flash. That is exactly how the RP2350's own bootloader
//! fakes its `RP2350` drive, and it buys three things worth having:
//!
//!   1. **No flash writes**, so none of the XIP-disabled hazard that makes the
//!      write path (D11) something to schedule deliberately.
//!   2. **Payloads never move.** `deserialize_raw` keeps borrowing them in place
//!      at 81 KB rather than 890 KB — we choose the layout, so contiguity and
//!      16-byte alignment are ours to guarantee, not the host OS's to break.
//!   3. **No state to corrupt.** There is no volume to get out of sync with; the
//!      view is a pure function of the catalog.
//!
//! **READ-ONLY, and the volume says so.** Writes are rejected at the SCSI layer.
//! Installing an app is still the UF2 drag through BOOTSEL until D11's write half
//! lands.
//!
//! # Why FAT12 rather than FAT16 or exFAT
//!
//! FAT12 is the format with the smallest possible metadata — a 4 KB FAT covers
//! more clusters than this volume will ever have — and every OS mounts it without
//! comment. The cluster count is what picks the format, and staying under
//! FAT12's 4085-cluster ceiling is a matter of choosing a large enough cluster.

use crate::catalog::Catalog;

/// One block, as USB mass storage counts them.
pub const BLOCK_SIZE: usize = 512;

/// Blocks per cluster. 64 gives a 32 KB cluster, which keeps a 12 MB region
/// comfortably under FAT12's 4085-cluster ceiling (12 MB / 32 KB = 384) while
/// wasting at most 32 KB per payload — nothing, against an 890 KB artifact.
pub const BLOCKS_PER_CLUSTER: u8 = 64;

/// Root directory entries, each 32 bytes. One block holds 16, which is exactly
/// [`crate::catalog::MAX_ENTRIES`] — plus room for the volume label and
/// `INFO.TXT`, so two blocks.
pub const ROOT_DIR_BLOCKS: usize = 2;

const RESERVED_BLOCKS: usize = 1; // the boot sector
const FAT_COUNT: usize = 1; // one copy; this volume is regenerated, not repaired
const FAT_BLOCKS: usize = 8; // 4 KB of FAT12 — far more than 384 clusters needs

const FAT_START: usize = RESERVED_BLOCKS;
const ROOT_START: usize = FAT_START + FAT_BLOCKS * FAT_COUNT;
const DATA_START: usize = ROOT_START + ROOT_DIR_BLOCKS;

/// A FAT12 view of a catalog.
///
/// Holds no buffers: every block is computed when asked for, so this costs a
/// handful of words regardless of how many payloads there are.
pub struct FatView<'a> {
    catalog: &'a Catalog,
    /// Total blocks the volume advertises. The host sizes its own structures
    /// from this, so it must not change while mounted.
    blocks: u32,
}

impl<'a> FatView<'a> {
    /// A view over `catalog`, advertising a volume of `region_len` bytes.
    pub fn new(catalog: &'a Catalog, region_len: usize) -> Self {
        Self {
            catalog,
            blocks: (region_len / BLOCK_SIZE) as u32,
        }
    }

    /// How many blocks the host thinks this volume has.
    pub fn block_count(&self) -> u32 {
        self.blocks
    }

    /// Answer one block. **The whole interface**: SCSI READ(10) lands here.
    ///
    /// Unknown or out-of-range blocks read as zeros rather than failing, which is
    /// what an unallocated region of a real volume does — a host that speculatively
    /// reads past what it needs should see empty space, not an I/O error.
    pub fn read_block(&self, index: u32, out: &mut [u8; BLOCK_SIZE]) {
        out.fill(0);
        let index = index as usize;

        if index < FAT_START {
            self.boot_sector(out);
        } else if index < ROOT_START {
            self.fat_block(index - FAT_START, out);
        } else if index < DATA_START {
            self.root_block(index - ROOT_START, out);
        } else {
            self.data_block(index - DATA_START, out);
        }
    }

    /// The BPB. Field offsets are from the FAT spec and are not negotiable —
    /// a host validates several of them and silently refuses to mount if they
    /// disagree, with no indication of which one was wrong.
    fn boot_sector(&self, out: &mut [u8; BLOCK_SIZE]) {
        out[0..3].copy_from_slice(&[0xEB, 0x3C, 0x90]); // jump, as every FAT image has
        out[3..11].copy_from_slice(b"MSWIN4.1"); // OEM name; the most-tested value
        out[11..13].copy_from_slice(&(BLOCK_SIZE as u16).to_le_bytes());
        out[13] = BLOCKS_PER_CLUSTER;
        out[14..16].copy_from_slice(&(RESERVED_BLOCKS as u16).to_le_bytes());
        out[16] = FAT_COUNT as u8;
        out[17..19].copy_from_slice(&((ROOT_DIR_BLOCKS * 16) as u16).to_le_bytes());

        // Total blocks goes in the 16-bit field when it fits and the 32-bit field
        // otherwise, and writing BOTH is how a volume becomes unmountable.
        if self.blocks < 0x1_0000 {
            out[19..21].copy_from_slice(&(self.blocks as u16).to_le_bytes());
        } else {
            out[32..36].copy_from_slice(&self.blocks.to_le_bytes());
        }

        out[21] = 0xF8; // fixed disk
        out[22..24].copy_from_slice(&(FAT_BLOCKS as u16).to_le_bytes());
        out[24..26].copy_from_slice(&32u16.to_le_bytes()); // sectors per track
        out[26..28].copy_from_slice(&2u16.to_le_bytes()); // heads
        out[38] = 0x29; // extended boot signature: the three fields below are present
        out[39..43].copy_from_slice(&0x494C_4300u32.to_le_bytes()); // volume id
        out[43..54].copy_from_slice(b"ILC BADGE  ");
        out[54..62].copy_from_slice(b"FAT12   ");
        out[510..512].copy_from_slice(&[0x55, 0xAA]); // the signature a host checks first
    }

    /// The FAT itself: one chain per payload, plus one for `INFO.TXT`.
    ///
    /// Clusters are laid out **contiguously and in catalog order**, which is what
    /// lets `data_block` map a cluster back to a payload with arithmetic instead
    /// of a lookup table — and what guarantees the contiguity `deserialize_raw`
    /// depends on.
    fn fat_block(&self, block: usize, out: &mut [u8; BLOCK_SIZE]) {
        // Cluster 0 and 1 are reserved; the media byte goes in 0.
        let mut entries = [0u16; 2 + MAX_CLUSTERS];
        entries[0] = 0xFF8;
        entries[1] = 0xFFF;

        let mut cluster = 2usize;
        for file in self.files() {
            let clusters = file.clusters();
            for step in 0..clusters {
                // Each cluster points at the next, and the last says "end of chain".
                entries[cluster] = if step + 1 == clusters {
                    0xFFF
                } else {
                    (cluster + 1) as u16
                };
                cluster += 1;
            }
        }

        // FAT12 packs two 12-bit entries into three bytes, which is the fiddly
        // part and the reason this is tested rather than eyeballed.
        let first = block * BLOCK_SIZE * 2 / 3;
        for slot in 0..(BLOCK_SIZE * 2 / 3 + 2) {
            let index = first + slot;
            if index + 1 >= entries.len() {
                break;
            }
            let pair = (entries[index] as u32) | ((entries[index + 1] as u32) << 12);
            if index % 2 != 0 {
                continue;
            }
            let byte = index * 3 / 2;
            let offset = byte as isize - (block * BLOCK_SIZE) as isize;
            for (n, value) in pair.to_le_bytes()[..3].iter().enumerate() {
                let at = offset + n as isize;
                if at >= 0 && (at as usize) < BLOCK_SIZE {
                    out[at as usize] = *value;
                }
            }
        }
    }

    /// The root directory: a volume label, `INFO.TXT`, and one entry per payload.
    fn root_block(&self, block: usize, out: &mut [u8; BLOCK_SIZE]) {
        let mut index = 0usize;
        let mut cluster = 2u16;

        // The volume label lives in the root directory as an entry with the
        // volume-id attribute, not in the boot sector alone.
        let place = |slot: usize, entry: [u8; 32], out: &mut [u8; BLOCK_SIZE]| {
            if slot / 16 == block {
                let at = (slot % 16) * 32;
                out[at..at + 32].copy_from_slice(&entry);
            }
        };

        place(index, volume_label(), out);
        index += 1;

        for file in self.files() {
            place(index, file.dir_entry(cluster), out);
            cluster += file.clusters() as u16;
            index += 1;
        }
    }

    /// A data block: either part of a payload, or part of the generated
    /// `INFO.TXT`.
    fn data_block(&self, block: usize, out: &mut [u8; BLOCK_SIZE]) {
        let cluster = block / BLOCKS_PER_CLUSTER as usize;
        let within = (block % BLOCKS_PER_CLUSTER as usize) * BLOCK_SIZE;

        let mut base = 0usize;
        for file in self.files() {
            let span = file.clusters();
            if cluster < base + span {
                let offset = (cluster - base) * BLOCKS_PER_CLUSTER as usize * BLOCK_SIZE + within;
                file.read(offset, out);
                return;
            }
            base += span;
        }
    }

    /// Every file this volume shows, in the order their clusters are allocated.
    fn files(&self) -> impl Iterator<Item = File<'a>> + '_ {
        core::iter::once(File::Info)
            .chain(self.catalog.iter().map(File::Payload))
    }
}

/// The most clusters this view will ever describe. A 12 MB region at 32 KB per
/// cluster is 384; the margin costs 2 bytes per entry of stack in `fat_block`.
const MAX_CLUSTERS: usize = 512;

/// One file in the synthetic volume.
#[derive(Clone, Copy)]
enum File<'a> {
    /// Generated text naming the region and each payload's entry method — the
    /// thing that makes the drive self-describing rather than a list of opaque
    /// blobs.
    Info,
    Payload(crate::catalog::Payload),
    #[allow(dead_code)]
    Never(core::marker::PhantomData<&'a ()>),
}

impl File<'_> {
    fn len(self) -> usize {
        match self {
            File::Info => INFO_TEXT.len(),
            File::Payload(p) => p.bytes.len(),
            File::Never(_) => 0,
        }
    }

    fn clusters(self) -> usize {
        let bytes = BLOCKS_PER_CLUSTER as usize * BLOCK_SIZE;
        self.len().div_ceil(bytes).max(1)
    }

    /// Read `BLOCK_SIZE` bytes starting at `offset` within this file.
    fn read(self, offset: usize, out: &mut [u8; BLOCK_SIZE]) {
        let source: &[u8] = match self {
            File::Info => INFO_TEXT.as_bytes(),
            File::Payload(p) => p.bytes,
            File::Never(_) => &[],
        };
        if offset >= source.len() {
            return;
        }
        let take = (source.len() - offset).min(BLOCK_SIZE);
        out[..take].copy_from_slice(&source[offset..offset + take]);
    }

    /// A 32-byte 8.3 directory entry.
    fn dir_entry(self, cluster: u16) -> [u8; 32] {
        let mut entry = [0u8; 32];
        entry[..11].copy_from_slice(&self.short_name());
        entry[11] = 0x01; // read-only, which this volume genuinely is
        entry[26..28].copy_from_slice(&cluster.to_le_bytes());
        entry[28..32].copy_from_slice(&(self.len() as u32).to_le_bytes());
        entry
    }

    /// 8.3, space-padded, upper-cased — the only form a FAT12 root directory
    /// holds without long-filename entries, which are not worth their complexity
    /// for names the catalog caps at 32 bytes anyway.
    ///
    /// **SANITISED, and that is not defensive tidiness — it was a real bug.** A
    /// catalog name is free-form up to 32 bytes, and an 8.3 field is not: it has
    /// no dot (the extension is the last three bytes, positionally), and a set of
    /// characters that are illegal outright. A payload called
    /// `hello.pulley32` mounted as `HELLO.PU.CWA` — the embedded dot read as the
    /// separator, so the name a user sees was not the name they gave, and a name
    /// containing `*` or `?` would have produced an entry a host may reject
    /// entirely. Every byte that is not clearly safe becomes `_`.
    fn short_name(self) -> [u8; 11] {
        let mut name = *b"           ";
        match self {
            File::Info => name.copy_from_slice(b"INFO    TXT"),
            File::Payload(p) => {
                // SHARED THREE WAYS. `names::check_set` predicts collisions with
                // this function, and `names::display_filename` builds the string
                // the badge's own menu shows — so the validator, the volume and
                // the picker cannot disagree about what a payload is called.
                name[..8].copy_from_slice(&crate::names::short_stem_83(p.name));
                name[8..].copy_from_slice(b"CWA");
            }
            File::Never(_) => {}
        }
        name
    }
}

fn volume_label() -> [u8; 32] {
    let mut entry = [0u8; 32];
    entry[..11].copy_from_slice(b"ILC BADGE  ");
    entry[11] = 0x08; // volume id
    entry
}

/// What `INFO.TXT` says. Static for now; D11's write half makes it dynamic.
const INFO_TEXT: &str = "\
ILC badge — payload catalog\r\n\
\r\n\
Each .CWA file is an AOT-compiled component (pulley32).\r\n\
This volume is READ-ONLY and generated from flash; it is not stored.\r\n\
\r\n\
To install an app: hold BOOT, tap RESET, drag a payload UF2\r\n\
onto the RP2350 drive.\r\n\
\r\n\
The board cannot compile a component — there is no Cranelift in\r\n\
a no_std Wasmtime — so payloads must be precompiled on a PC.\r\n";

#[cfg(test)]
mod tests {
    use super::*;
    use crate::catalog::{self, HEADER_LEN, MAGIC, SLOT_ALIGN};
    use alloc::vec;
    use alloc::vec::Vec;

    fn entry(name: &str, payload: &[u8]) -> Vec<u8> {
        let mut out = vec![0u8; HEADER_LEN];
        out[..8].copy_from_slice(&MAGIC);
        out[8..12].copy_from_slice(&(payload.len() as u32).to_le_bytes());
        out[12..16].copy_from_slice(&(name.len() as u32).to_le_bytes());
        out[16..16 + name.len()].copy_from_slice(name.as_bytes());
        out.extend_from_slice(payload);
        out.resize(out.len().div_ceil(SLOT_ALIGN) * SLOT_ALIGN, 0);
        out
    }

    fn view_over(payloads: &[(&str, &[u8])]) -> Catalog {
        let mut image = Vec::new();
        for (name, bytes) in payloads {
            image.extend_from_slice(&entry(name, bytes));
        }
        catalog::scan(Vec::leak(image))
    }

    fn block(view: &FatView, index: u32) -> [u8; BLOCK_SIZE] {
        let mut out = [0u8; BLOCK_SIZE];
        view.read_block(index, &mut out);
        out
    }

    #[test]
    fn the_boot_sector_is_what_a_host_validates() {
        // A host checks these before mounting and gives no reason when it
        // refuses, so they are asserted rather than trusted.
        let catalog = view_over(&[("hello", b"x")]);
        let view = FatView::new(&catalog, 12 * 1024 * 1024);
        let boot = block(&view, 0);

        assert_eq!(&boot[510..512], &[0x55, 0xAA], "signature");
        assert_eq!(u16::from_le_bytes([boot[11], boot[12]]), 512, "bytes per sector");
        assert_eq!(boot[13], BLOCKS_PER_CLUSTER, "sectors per cluster");
        assert_eq!(boot[21], 0xF8, "media descriptor");
        assert_eq!(&boot[54..62], b"FAT12   ", "filesystem type");
    }

    #[test]
    fn every_payload_gets_a_directory_entry() {
        let catalog = view_over(&[("hello", b"first"), ("ttt", b"second")]);
        let view = FatView::new(&catalog, 12 * 1024 * 1024);
        let root = block(&view, ROOT_START as u32);

        let names: Vec<&[u8]> = (0..16)
            .map(|slot| &root[slot * 32..slot * 32 + 11])
            .filter(|name| name[0] != 0)
            .collect();

        assert!(names.contains(&&b"HELLO   CWA"[..]), "hello missing: {names:?}");
        assert!(names.contains(&&b"TTT     CWA"[..]), "ttt missing: {names:?}");
        assert!(names.contains(&&b"INFO    TXT"[..]), "INFO.TXT missing");
    }

    #[test]
    fn a_payloads_bytes_are_readable_through_the_volume() {
        // THE POINT OF THE WHOLE MODULE: what the host reads back must be the
        // bytes sitting in flash, at the cluster the directory entry names.
        let body: Vec<u8> = (0..200u32).map(|n| (n % 251) as u8).collect();
        let catalog = view_over(&[("hello", &body)]);
        let view = FatView::new(&catalog, 12 * 1024 * 1024);

        // INFO.TXT takes cluster 2, so hello starts at cluster 3.
        let root = block(&view, ROOT_START as u32);
        let hello = (0..16)
            .map(|slot| &root[slot * 32..slot * 32 + 32])
            .find(|e| &e[..11] == b"HELLO   CWA")
            .expect("hello is in the root directory");
        let cluster = u16::from_le_bytes([hello[26], hello[27]]) as usize;
        let size = u32::from_le_bytes([hello[28], hello[29], hello[30], hello[31]]) as usize;
        assert_eq!(size, body.len(), "directory entry reports the payload size");

        let first = DATA_START + (cluster - 2) * BLOCKS_PER_CLUSTER as usize;
        let data = block(&view, first as u32);
        assert_eq!(&data[..body.len()], &body[..], "payload bytes round-trip");
    }

    #[test]
    fn names_illegal_in_8_3_are_sanitised() {
        // FOUND BY MOUNTING, not by reading the spec: `hello.pulley32` appeared as
        // HELLO.PU.CWA, because the embedded dot reads as the separator. The
        // extension is positional in a directory entry, so a dot in the name is a
        // different file than the one the user asked for.
        let catalog = view_over(&[("hello.pulley32", b"x"), ("a b*c", b"y")]);
        let view = FatView::new(&catalog, 12 * 1024 * 1024);
        let root = block(&view, ROOT_START as u32);

        let names: Vec<&[u8]> = (0..16)
            .map(|slot| &root[slot * 32..slot * 32 + 11])
            .filter(|name| name[0] != 0)
            .collect();

        assert!(names.contains(&&b"HELLO_PUCWA"[..]), "dot not sanitised: {names:?}");
        assert!(names.contains(&&b"A_B_C   CWA"[..]), "space/star not sanitised: {names:?}");
    }

    #[test]
    fn an_empty_catalog_still_mounts() {
        // A badge with no apps must present a valid empty volume, not a broken
        // one — otherwise "nothing installed" looks like "the drive is corrupt".
        let catalog = view_over(&[]);
        let view = FatView::new(&catalog, 12 * 1024 * 1024);
        let boot = block(&view, 0);
        assert_eq!(&boot[510..512], &[0x55, 0xAA]);

        let root = block(&view, ROOT_START as u32);
        let names: Vec<&[u8]> = (0..16)
            .map(|slot| &root[slot * 32..slot * 32 + 11])
            .filter(|name| name[0] != 0)
            .collect();
        assert!(names.contains(&&b"INFO    TXT"[..]), "INFO.TXT explains the empty drive");
    }

    #[test]
    fn reads_past_the_end_are_zeros_not_errors() {
        // A host speculatively reads beyond what it needs; unallocated space on a
        // real volume is zeros, and an error here would fail the mount.
        let catalog = view_over(&[("hello", b"x")]);
        let view = FatView::new(&catalog, 12 * 1024 * 1024);
        assert_eq!(block(&view, 20_000), [0u8; BLOCK_SIZE]);
    }

    #[test]
    fn the_fat_chains_each_file_to_its_end() {
        // FAT12's packed 12-bit entries are the fiddly part of this module, so
        // the first two clusters and INFO.TXT's chain are checked by hand.
        let catalog = view_over(&[("hello", b"x")]);
        let view = FatView::new(&catalog, 12 * 1024 * 1024);
        let fat = block(&view, FAT_START as u32);

        let entry = |n: usize, fat: &[u8; BLOCK_SIZE]| -> u16 {
            let byte = n * 3 / 2;
            let pair = u16::from_le_bytes([fat[byte], fat[byte + 1]]);
            if n % 2 == 0 { pair & 0xFFF } else { pair >> 4 }
        };

        assert_eq!(entry(0, &fat), 0xFF8, "media descriptor in cluster 0");
        assert_eq!(entry(1, &fat), 0xFFF, "cluster 1 reserved");
        // INFO.TXT is one cluster, so its chain is a single end-of-chain marker.
        assert_eq!(entry(2, &fat), 0xFFF, "INFO.TXT ends immediately");
        assert_eq!(entry(3, &fat), 0xFFF, "hello is one cluster and ends");
    }
}
