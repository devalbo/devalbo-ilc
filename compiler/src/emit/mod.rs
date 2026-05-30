pub mod py;
pub mod rs;
pub mod ts;

use std::fs;
use std::path::Path;

use anyhow::Result;

pub(crate) fn write_generated(path: &Path, contents: &str) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }
    fs::write(path, contents)?;
    Ok(())
}
