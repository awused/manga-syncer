use std::collections::HashMap;
use std::fs::{File, read_dir};
use std::io::{self, BufWriter, Read, Write};
use std::path::{Path, PathBuf};

use anyhow::{Context, Result, bail};
use once_cell::sync;
use once_cell::unsync::Lazy;
use rayon::iter::{ParallelBridge, ParallelIterator};
use rayon::{ThreadPool, ThreadPoolBuilder};
use serde::Deserialize;
use serde_with::{DefaultOnNull, NoneAsEmptyString, serde_as};
use zip::ZipWriter;
use zip::write::FileOptions;

use crate::CONFIG;
use crate::groups::{get_all_groups, groups_in_chapter};
use crate::manga::{MangaInfo, get_or_create_dir};
use crate::util::{FindResult, convert_filename, convert_uuid, find_existing, http_get, json_get};

static DOWNLOADERS: sync::Lazy<ThreadPool> = sync::Lazy::new(|| {
    ThreadPoolBuilder::new()
        .num_threads(CONFIG.parallel_downloads.get() as usize)
        .thread_name(|i| format!("downloader-{i}"))
        .build()
        .unwrap()
});

pub fn sync_single_chapter(chapter_id: String) -> Result<()> {
    info!("Syncing single chapter {chapter_id}");
    let chapter: ChapterInfo = json_get(format!("https://api.mangadex.org/chapter/{chapter_id}"))?;

    let manga_id = chapter
        .data
        .relationships
        .iter()
        .filter(|r| r.type_field == "manga")
        .map(|r| &*r.id)
        .next()
        .context("Chapter has no associated manga")?;

    let info: MangaInfo = json_get(format!("https://api.mangadex.org/manga/{manga_id}"))?;
    let manga_dir = get_or_create_dir(&info)?;

    let chapters = [chapter.data];
    let groups = get_all_groups(&chapters)?;

    sync_chapters(chapters.into_iter(), &manga_dir, &groups, false)
}

fn download_chapter(chapter: &Chapter, archive_path: PathBuf) -> Result<()> {
    let mut builder = tempfile::Builder::new();
    builder.prefix("manga-syncer");
    let tmp_dir = CONFIG
        .temp_directory
        .as_ref()
        .map_or_else(|| builder.tempdir(), |d| builder.tempdir_in(d))?;

    let at_home: AtHomeResponse =
        json_get(format!("https://api.mangadex.org/at-home/server/{}", chapter.id))?;

    if chapter.attributes.external_url.is_some() && at_home.chapter.data.is_empty() {
        debug!("Skipping chapter {} with external url and no pages", chapter.id);
        return Ok(());
    } else if at_home.chapter.data.is_empty() {
        bail!("Got chapter with no pages: {chapter:?}\n{at_home:?}");
    }


    let mut paths = at_home
        .chapter
        .data
        .iter()
        .enumerate()
        .par_bridge()
        .map(|(i, p)| {
            let ext = Path::new(&p)
                .extension()
                .with_context(|| format!("No extension for {p}"))?
                .to_str()
                .unwrap();
            let filename = format!("{:03}.{ext}", (i + 1));
            let filepath = tmp_dir.path().join(filename);

            let url = at_home.base_url.clone() + "/data/" + &at_home.chapter.hash + "/" + p;

            trace!("Downloading {url:?} to {filepath:?}");

            let download = || {
                let mut file = BufWriter::new(File::create(&filepath)?);
                let mut contents = http_get(&url)?;

                let n = io::copy(&mut contents, &mut file)?;
                if n == 0 {
                    bail!("Wrote empty file to {filepath:?}");
                }
                Ok(())
            };

            let mut r = download();
            for _i in 0..3 {
                let Err(e) = &r else {
                    break;
                };
                error!("Retrying download of {url:?} due to {e}");
                r = download();
            }
            r?;

            Ok(filepath)
        })
        .collect::<Result<Vec<_>>>()?;

    paths.sort();

    let temp_zip = tmp_dir.path().join("output.zip");
    let outfile = BufWriter::new(File::create(&temp_zip)?);

    let mut zip = ZipWriter::new(outfile);
    let options = FileOptions::<()>::default().unix_permissions(0o755);

    let mut buffer = Vec::new();
    for p in paths {
        zip.start_file(p.file_name().unwrap().to_str().unwrap(), options)?;
        let mut f = File::open(p)?;
        f.read_to_end(&mut buffer)?;
        zip.write_all(&buffer)?;
        buffer.clear();
    }

    zip.finish()?;

    if std::fs::rename(&temp_zip, &archive_path).is_err() {
        std::fs::copy(temp_zip, archive_path)?;
    }


    Ok(())
}

pub fn sync_chapters(
    chapters: impl Iterator<Item = Chapter>,
    manga_dir: &Path,
    groups: &HashMap<&str, &str>,
    continue_on_error: bool,
) -> Result<()> {
    let existing: Lazy<Result<Vec<_>>, _> = Lazy::new(|| {
        let dirs: std::result::Result<Vec<_>, _> = read_dir(manga_dir)?.collect();
        Ok(dirs?)
    });

    for c in chapters {
        let converted_id = convert_uuid(&c.id)?;
        let chap_number = c.attributes.chapter.as_deref().unwrap_or("0");
        let groups = groups_in_chapter(&c)
            .filter_map(|g| groups.get(g).copied())
            .collect::<Vec<_>>()
            .join(", ");

        let name = match (&c.attributes.volume, &c.attributes.title) {
            (Some(v), Some(t)) => {
                format!("Vol. {v} Ch. {chap_number} {t}")
            }
            (Some(v), None) => {
                format!("Vol. {v} Ch. {chap_number}")
            }
            (None, Some(t)) => {
                format!("Ch. {chap_number} {t}")
            }
            (None, None) => format!("Ch. {chap_number}"),
        };

        let filename = if groups.is_empty() {
            convert_filename(&name)
        } else {
            convert_filename(&format!("{name} [{groups}]"))
        };
        let filename = filename + " - " + &converted_id + ".zip";

        let chapter_path = manga_dir.join(filename);

        match find_existing(&chapter_path, &existing, &converted_id, false)? {
            FindResult::Missing => info!("Syncing chapter {chapter_path:?}"),
            FindResult::AlreadyExists => {
                trace!("Chapter already exists {chapter_path:?}");
                continue;
            }
            FindResult::RenameCandidate(path) => {
                if CONFIG.rename_chapters {
                    info!("Renaming existing chapter from {path:?} to {chapter_path:?}");
                    std::fs::rename(path, &chapter_path)?;
                } else {
                    debug!("Found existing chatper {path:?}, not renaming");
                }
                continue;
            }
        }

        if let Err(e) = DOWNLOADERS
            .install(|| download_chapter(&c, chapter_path))
            .with_context(|| format!("Failed while downloading chapter {}", c.id))
        {
            if continue_on_error {
                // Mangadex@Home servers are often very unreliable.
                error!("{e:?}, proceeding with other chapters");
            } else {
                return Err(e);
            }
        }
    }
    Ok(())
}

#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AtHomeResponse {
    pub base_url: String,
    pub chapter: AtHomeChapter,
}

#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AtHomeChapter {
    pub hash: String,
    pub data: Vec<String>,
}

#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct ChapterInfo {
    pub data: Chapter,
}

#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Chapter {
    pub id: String,
    pub attributes: ChapterAttributes,
    pub relationships: Vec<Relationship>,
}

#[serde_as]
#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ChapterAttributes {
    #[serde_as(as = "NoneAsEmptyString")]
    pub volume: Option<String>,
    #[serde_as(deserialize_as = "DefaultOnNull")]
    pub chapter: Option<String>,
    #[serde_as(as = "NoneAsEmptyString")]
    pub title: Option<String>,
    pub external_url: Option<String>,
}

#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Relationship {
    pub id: String,
    #[serde(rename = "type")]
    pub type_field: String,
}
