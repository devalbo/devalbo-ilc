use std::path::PathBuf;

use anyhow::Result;
use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "ilc", about = "ILC compiler: WIT → language packages")]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Generate types from WIT into language packages.
    Compile {
        /// Directory containing `.wit` files (e.g. `./wit`).
        wit_dir: PathBuf,
        /// Output packages root (e.g. `./packages`).
        #[arg(long)]
        out: PathBuf,
    },
}

fn main() -> Result<()> {
    let cli = Cli::parse();
    match cli.command {
        Commands::Compile { wit_dir, out } => {
            ilc_compiler::compile(&wit_dir, &out)?;
            eprintln!("wrote TypeScript: {}", out.join("ilc-ts/src/generated/types.ts").display());
            eprintln!("wrote Python:    {}", out.join("ilc-py/src/devalbo_ilc/generated/types.py").display());
            eprintln!("wrote Rust:      {}", out.join("ilc-rs/src/generated/types.rs").display());
        }
    }
    Ok(())
}
