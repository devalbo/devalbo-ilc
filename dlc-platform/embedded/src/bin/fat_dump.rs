//! Dump the synthetic FAT volume to a file, so a REAL OS can try to mount it.
//!
//! WHY THIS EXISTS. `fatview.rs`'s unit tests assert that the bytes are what the
//! author believed a FAT12 volume contains — which is exactly the belief most
//! likely to be wrong. A host's FAT driver validates fields this code never
//! thought about and refuses to mount with no indication of which one it
//! disliked. Mounting the output of this tool is the only check that closes that
//! gap before hardware.
//!
//!   cargo run --bin fat-dump -- out.img [payload.cwasm ...]
use dlc_platform_embedded::catalog::{self, HEADER_LEN, MAGIC, SLOT_ALIGN};
use dlc_platform_embedded::fatview::{FatView, BLOCK_SIZE};

fn main() -> std::io::Result<()> {
    let mut args = std::env::args().skip(1);
    let output = args.next().expect("usage: fat-dump <out.img> [payload.cwasm ...]");

    // Build a catalog image the way `payload-image` does.
    let mut region = Vec::new();
    for path in args {
        let bytes = std::fs::read(&path)?;
        let name = std::path::Path::new(&path)
            .file_stem()
            .and_then(|s| s.to_str())
            .unwrap_or("app")
            .to_string();
        let mut header = vec![0u8; HEADER_LEN];
        header[..8].copy_from_slice(&MAGIC);
        header[8..12].copy_from_slice(&(bytes.len() as u32).to_le_bytes());
        header[12..16].copy_from_slice(&(name.len() as u32).to_le_bytes());
        header[16..16 + name.len()].copy_from_slice(name.as_bytes());
        region.extend_from_slice(&header);
        region.extend_from_slice(&bytes);
        region.resize(region.len().div_ceil(SLOT_ALIGN) * SLOT_ALIGN, 0);
        println!("  {name}: {} bytes", bytes.len());
    }

    // 12 MB, matching the badge's region.
    let region_len = 12 * 1024 * 1024;
    region.resize(region_len, 0xFF);
    let catalog = catalog::scan(Vec::leak(region));

    let view = FatView::new(&catalog, region_len);
    let mut image = Vec::with_capacity(region_len);
    let mut block = [0u8; BLOCK_SIZE];
    for index in 0..view.block_count() {
        view.read_block(index, &mut block);
        image.extend_from_slice(&block);
    }
    std::fs::write(&output, &image)?;
    println!("{output}: {} blocks ({} MB)", view.block_count(), image.len() / 1024 / 1024);
    Ok(())
}
