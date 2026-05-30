use std::sync::Arc;

use async_trait::async_trait;

use crate::generated::types::ConsoleIo;

/// Share one `ConsoleIo` implementation behind `Box<dyn ConsoleIo>`.
pub struct SharedConsoleIo(pub Arc<dyn ConsoleIo + Send + Sync>);

#[async_trait]
impl ConsoleIo for SharedConsoleIo {
    async fn info(&self, message: &str) {
        self.0.info(message).await;
    }

    async fn error(&self, message: &str) {
        self.0.error(message).await;
    }

    async fn readLine(&self) -> Option<String> {
        self.0.readLine().await
    }
}

pub fn box_console_io(io: Arc<dyn ConsoleIo + Send + Sync>) -> Box<dyn ConsoleIo + Send + 'static> {
    Box::new(SharedConsoleIo(io))
}
