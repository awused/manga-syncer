use std::env::temp_dir;
use std::io::Write;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread::JoinHandle;
use std::{process, thread};

use anyhow::anyhow;
use once_cell::sync::Lazy;

static CLOSED: Lazy<Arc<AtomicBool>> = Lazy::new(|| Arc::new(AtomicBool::new(false)));

pub fn err_if_closed() -> anyhow::Result<()> {
    if CLOSED.load(Ordering::Relaxed) { Err(anyhow!("Closed")) } else { Ok(()) }
}

pub fn close() -> bool {
    !CLOSED.swap(true, Ordering::Relaxed)
}

// Logs the error and closes the application.
// Saves the first fatal error to a crash log file in the system default temp directory.
pub fn fatal(msg: impl AsRef<str>) {
    let msg = msg.as_ref();

    error!("{msg}");

    if close() {
        let path = temp_dir().join(format!("manga-syncer_crash_{}", process::id()));
        let Ok(mut file) = std::fs::File::options().write(true).create_new(true).open(&path) else {
            error!("Couldn't open {path:?} for logging fatal error");
            return;
        };

        drop(file.write_all(msg.as_bytes()));
    }
}

pub fn init() -> std::io::Result<JoinHandle<()>> {
    #[cfg(target_family = "unix")]
    let f = || {
        use std::os::raw::c_int;

        use signal_hook::consts::TERM_SIGNALS;
        use signal_hook::iterator::exfiltrator::SignalOnly;
        use signal_hook::iterator::SignalsInfo;

        if let Err(e) = catch_unwind(AssertUnwindSafe(|| {
            for sig in TERM_SIGNALS {
                // When terminated by a second term signal, exit with exit code 1.
                signal_hook::flag::register_conditional_shutdown(*sig, 1, CLOSED.clone())
                    .expect("Error registering signal handlers.");
            }

            let mut sigs: Vec<c_int> = Vec::new();
            sigs.extend(TERM_SIGNALS);
            let mut it = match SignalsInfo::<SignalOnly>::new(sigs) {
                Ok(i) => i,
                Err(e) => {
                    fatal(format!("Error registering signal handlers: {e:?}"));
                    return;
                }
            };

            if let Some(s) = it.into_iter().next() {
                info!("Received signal {s}, shutting down");
                close();
                it.handle().close();
            }
        })) {
            fatal(format!("Signal thread panicked unexpectedly: {e:?}"));
        };
    };

    #[cfg(windows)]
    let f = || {
        ctrlc::set_handler(|| {
            if err_if_closed() {
                // When terminated by a second term signal, exit with exit code 1.
                std::process::exit(1);
            }

            info!("Received closing signal, shutting down");
            close();
        })
        .expect("Error registering signal handlers.");
    };


    thread::Builder::new().name("signals".to_string()).spawn(f)
}
