#[path = "../admission.rs"]
mod admission;
fn main() {
    let out = std::env::args().nth(1).expect("output directory");
    std::fs::create_dir_all(&out).unwrap();
    if !admission::run(&out) {
        std::process::exit(1);
    }
}
