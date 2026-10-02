fn main() {
    if let Err(e) = ast_cli::main_entry() {
        eprintln!("error: {e:#}");
        std::process::exit(1);
    }
}
