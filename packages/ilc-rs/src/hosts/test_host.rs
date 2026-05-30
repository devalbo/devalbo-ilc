use std::sync::{Arc, Mutex};

use async_trait::async_trait;

use crate::generated::types::{ConsoleIo, Environment};
use crate::hosts::shared::box_console_io;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LogEntry {
    pub level: &'static str,
    pub message: String,
}

struct TestConsoleIo {
    logs: Arc<Mutex<Vec<LogEntry>>>,
    stdin: Arc<Mutex<Vec<String>>>,
    stdin_index: Arc<Mutex<usize>>,
}

#[async_trait]
impl ConsoleIo for TestConsoleIo {
    async fn info(&self, message: &str) {
        self.logs.lock().unwrap().push(LogEntry {
            level: "info",
            message: message.to_string(),
        });
    }

    async fn error(&self, message: &str) {
        self.logs.lock().unwrap().push(LogEntry {
            level: "error",
            message: message.to_string(),
        });
    }

    async fn readLine(&self) -> Option<String> {
        let mut index = self.stdin_index.lock().unwrap();
        let stdin = self.stdin.lock().unwrap();
        if *index >= stdin.len() {
            return None;
        }
        let line = stdin[*index].clone();
        *index += 1;
        if line.is_empty() { None } else { Some(line) }
    }
}

pub struct TestEnvironment {
    console: Arc<dyn ConsoleIo + Send + Sync>,
    pub logs: Arc<Mutex<Vec<LogEntry>>>,
}

#[async_trait]
impl Environment for TestEnvironment {
    async fn consoleIo(&self) -> Box<dyn ConsoleIo + Send + '_> {
        box_console_io(self.console.clone())
    }
}

pub fn create_test_environment(stdin: Vec<String>) -> TestEnvironment {
    let logs = Arc::new(Mutex::new(Vec::new()));
    let console = Arc::new(TestConsoleIo {
        logs: logs.clone(),
        stdin: Arc::new(Mutex::new(stdin)),
        stdin_index: Arc::new(Mutex::new(0)),
    });
    TestEnvironment { console, logs }
}
