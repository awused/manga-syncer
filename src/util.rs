use std::borrow::Cow;
use std::collections::HashMap;
use std::fmt::Debug;
use std::fs::DirEntry;
use std::io::ErrorKind;
use std::path::{Path, PathBuf};
use std::thread;
use std::time::Duration;

use anyhow::{anyhow, bail, Result};
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine;
use once_cell::sync::Lazy;
use once_cell::unsync;
use regex::Regex;
use reqwest::blocking::{Client, Response};
use reqwest::IntoUrl;
use serde::de::DeserializeOwned;
use uuid::Uuid;

use crate::closing::err_if_closed;
use crate::CONFIG;

const DELAY: Duration = Duration::from_millis(1500);
const USER_AGENT: &str = concat!(env!("CARGO_PKG_NAME"), "/", env!("CARGO_PKG_VERSION"),);
pub const PAGE_SIZE: usize = 100;

static CLIENT: Lazy<Client> = Lazy::new(|| {
    Client::builder()
        .user_agent(USER_AGENT)
        // Yes, some mangadex@home servers are this slow
        .timeout(Duration::from_secs(300))
        .build()
        .unwrap()
});

pub fn http_get(url: impl IntoUrl + Clone + Debug) -> Result<Response> {
    err_if_closed()?;
    let mut resp = CLIENT.get(url.clone()).send();
    // Retry up to three times
    for _ in 0..3 {
        if resp.is_ok() {
            break;
        }
        debug!("Retrying request {url:?} after failure {:?}", resp.err().unwrap());
        resp = CLIENT.get(url.clone()).send();
    }
    err_if_closed()?;
    Ok(resp?)
}

pub fn json_get<T: DeserializeOwned>(url: impl IntoUrl + Clone + Debug) -> Result<T> {
    err_if_closed()?;
    thread::sleep(DELAY);

    let body = http_get(url)?.text()?;
    Ok(serde_path_to_error::deserialize(&mut serde_json::Deserializer::from_str(
        &body,
    ))?)
}

pub type LocalizedString = HashMap<String, String>;

pub fn english_or_first(s: &LocalizedString) -> Option<String> {
    s.get("en").or_else(|| s.values().next()).map(String::clone)
}

pub fn convert_uuid(id: &str) -> Result<String> {
    Ok(URL_SAFE_NO_PAD.encode(Uuid::parse_str(id)?.into_bytes()))
}

// This is much more restrictive than what is truly necessary
static FILENAME_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r#"[^~☆:;’'",#!\(\)!\pL\pN\-_+=\[\]. ]+"#).unwrap());
static FILENAME_QUESTION_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r#"[^?~☆:;’'",#!\(\)!\pL\pN\-_+=\[\]. ]+"#).unwrap());
static HYPHENS: Lazy<Regex> = Lazy::new(|| Regex::new("--+").unwrap());

pub fn convert_filename(name: &str) -> String {
    let name = if CONFIG.allow_question_marks {
        FILENAME_QUESTION_RE.replace_all(name, "-")
    } else {
        FILENAME_RE.replace_all(name, "-")
    };

    let name = HYPHENS.replace_all(&name, "-");
    name.trim_matches(&[' ', '-']).to_string()
}

pub enum FindResult {
    Missing,
    AlreadyExists,
    RenameCandidate(PathBuf),
}

pub fn find_existing(
    expected_abs_path: &Path,
    dir: &unsync::Lazy<Result<Vec<DirEntry>>, impl FnOnce() -> Result<Vec<DirEntry>>>,
    converted_id: &str,
    // We operate on manga directories and chapter zip files
    is_dir: bool,
) -> Result<FindResult> {
    match std::fs::metadata(expected_abs_path) {
        Ok(m) if m.is_dir() == is_dir => return Ok(FindResult::AlreadyExists),
        Ok(_m) => {
            bail!("{expected_abs_path:?} exists but is_dir() wasn't the expected {is_dir}")
        }
        Err(e) if e.kind() != ErrorKind::NotFound => {
            return Err(e.into());
        }
        _ => {}
    }

    let suffix = if is_dir {
        Cow::Borrowed(converted_id)
    } else {
        Cow::Owned(format!("{converted_id}.zip"))
    };

    let r = dir.as_ref().map_err(|e| anyhow!(e.to_string()));
    for p in r? {
        // We're fine with lossy conversions here since we're looking for some valid unicode
        if !p.file_name().to_string_lossy().ends_with(suffix.as_ref()) {
            continue;
        }

        if p.metadata()?.is_dir() == is_dir {
            return Ok(FindResult::RenameCandidate(p.path()));
        }
    }
    Ok(FindResult::Missing)
}
