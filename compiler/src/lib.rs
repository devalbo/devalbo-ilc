pub mod emit;
pub mod ir;
pub mod naming;
pub mod wit;

use std::path::Path;

use anyhow::Result;

/// Compile WIT from `wit_dir` and write generated artifacts under `packages_dir`.
pub fn compile(wit_dir: &Path, packages_dir: &Path) -> Result<()> {
    let pkg = wit::load_package(wit_dir)?;

    let ts_out = packages_dir.join("ilc-ts/src/generated/types.ts");
    emit::ts::write_typescript(&pkg, &ts_out)?;

    let py_out = packages_dir.join("ilc-py/src/devalbo_ilc/generated/types.py");
    emit::py::write_python(&pkg, &py_out)?;

    let rs_out = packages_dir.join("ilc-rs/src/generated/types.rs");
    emit::rs::write_rust(&pkg, &rs_out)?;

    Ok(())
}
