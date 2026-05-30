#![allow(non_snake_case)]

pub mod generated;
pub mod hello;
pub mod hosts;

pub use generated::types::{ConsoleIo, Environment};
pub use hello::hello;
pub use hosts::{
    create_process_environment, create_serverless_environment, create_test_environment,
    LogEntry, ProcessEnvironment, TestEnvironment,
};
