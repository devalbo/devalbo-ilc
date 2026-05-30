use devalbo_ilc::{create_process_environment, create_test_environment, hello};

#[tokio::main]
async fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.iter().any(|a| a == "--test") {
        let env = create_test_environment(vec![]);
        hello(&env).await;
        let logs = env.logs.lock().unwrap();
        let ok = logs
            .iter()
            .any(|e| e.level == "info" && e.message == "hello from ILC");
        if !ok {
            eprintln!("unexpected logs: {logs:?}");
            std::process::exit(1);
        }
        eprintln!("ok: test host captured hello");
        return;
    }

    let env = create_process_environment();
    hello(&env).await;
}
