#[macro_use]
extern crate log;

use std::collections::HashSet;
use std::num::NonZeroU8;
use std::path::PathBuf;

use anyhow::{Context, Result};
use clap::Parser;
use once_cell::sync::Lazy;
use serde::Deserialize;
use serde_with::{serde_as, NoneAsEmptyString};

use crate::chapter::sync_single_chapter;

mod chapter;
mod closing;
mod groups;
mod manga;
mod util;

#[derive(Debug, Parser)]
#[clap(name = "manga-syncer", about = "Sync manga from Mangadex")]
#[clap(args_conflicts_with_subcommands = true, subcommand_negates_reqs = true)]
pub struct Opt {
    #[command(subcommand)]
    cmd: Option<Command>,

    /// Manga UUIDs.
    /// https://mangadex.org/title/f110d48a-8461-428e-bbcb-5ae3c0d53d25 has a UUID of
    /// f110d48a-8461-428e-bbcb-5ae3c0d53d25
    #[arg(allow_hyphen_values = true, required = true, num_args=1..)]
    manga_ids: Vec<String>,
}

#[derive(Debug, Parser)]
enum Command {
    /// Download a single chapter
    #[command(visible_alias = "c")]
    Chapter { chapter_id: String },
    /// Sync any number of manga
    #[command(visible_alias = "m")]
    Manga {
        /// Manga UUIDs.
        /// https://mangadex.org/title/f110d48a-8461-428e-bbcb-5ae3c0d53d25 has a UUID of
        /// f110d48a-8461-428e-bbcb-5ae3c0d53d25
        #[arg(allow_hyphen_values = true, required=true, num_args=1..)]
        manga_ids: Vec<String>,
    },
}

#[serde_as]
#[derive(Debug, Deserialize)]
struct Config {
    language: String,
    output_directory: PathBuf,
    #[serde_as(as = "NoneAsEmptyString")]
    temp_directory: Option<PathBuf>,
    rename_chapters: bool,
    rename_manga: bool,
    allow_question_marks: bool,
    blocked_groups: HashSet<String>,
    parallel_downloads: NonZeroU8,
    ignored_chapters: HashSet<String>,
}

static CONFIG: Lazy<Config> = Lazy::new(|| {
    awconf::load_config::<Config>("manga-syncer", None::<&str>, None::<&str>)
        .unwrap()
        .0
});


fn main() -> Result<()> {
    env_logger::init();
    closing::init()?;

    let cli = Opt::parse();
    Lazy::force(&CONFIG);

    match cli {
        Opt {
            cmd: Some(Command::Chapter { chapter_id }),
            ..
        } => sync_single_chapter(chapter_id),
        Opt {
            cmd: Some(Command::Manga { manga_ids }), ..
        }
        | Opt { manga_ids, .. } => manga_ids
            .into_iter()
            .map(|mid| manga::sync_manga(&mid).with_context(|| format!("Failed during {mid}")))
            .collect::<Result<Vec<_>>>()
            .map(|_| ()),
    }
}
