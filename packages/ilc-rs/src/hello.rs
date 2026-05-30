use crate::generated::types::Environment;

pub async fn hello(env: &impl Environment) {
    let console_io = env.consoleIo().await;
    console_io.info("hello from ILC").await;
}
