use std::io::{self, BufRead, Write};
use std::sync::Arc;

use async_trait::async_trait;

use crate::generated::types::{ConsoleIo, Environment};
use crate::hosts::shared::box_console_io;

pub struct ProcessConsoleIo {
    non_interactive: bool,
}

#[async_trait]
impl ConsoleIo for ProcessConsoleIo {
    async fn info(&self, message: &str) {
        let mut out = io::stdout().lock();
        let _ = writeln!(out, "{message}");
    }

    async fn error(&self, message: &str) {
        let mut err = io::stderr().lock();
        let _ = writeln!(err, "{message}");
    }

    async fn readLine(&self) -> Option<String> {
        if self.non_interactive {
            return None;
        }
        let stdin = io::stdin();
        let mut line = String::new();
        match stdin.lock().read_line(&mut line) {
            Ok(0) => None,
            Ok(_) => {
                let trimmed = line.trim_end_matches(['\n', '\r']).to_string();
                if trimmed.is_empty() {
                    None
                } else {
                    Some(trimmed)
                }
            }
            Err(_) => None,
        }
    }
}

pub struct ProcessEnvironment {
    console: Arc<dyn ConsoleIo + Send + Sync>,
}

impl ProcessEnvironment {
    pub fn new(non_interactive: bool) -> Self {
        Self {
            console: Arc::new(ProcessConsoleIo { non_interactive }),
        }
    }
}

#[async_trait]
impl Environment for ProcessEnvironment {
    async fn consoleIo(&self) -> Box<dyn ConsoleIo + Send + '_> {
        box_console_io(self.console.clone())
    }
}

pub fn create_process_environment() -> ProcessEnvironment {
    ProcessEnvironment::new(false)
}

pub fn create_serverless_environment() -> ProcessEnvironment {
    ProcessEnvironment::new(true)
}
