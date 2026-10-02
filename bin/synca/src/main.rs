fn main() {
    if let Err(e) = synca_cli::main_entry() {
        eprintln!("error: {e:#}");
        std::process::exit(1);
    }
}
