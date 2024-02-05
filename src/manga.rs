use std::fs::read_dir;
use std::path::PathBuf;

use anyhow::{bail, Context, Result};
use once_cell::unsync::Lazy;
use reqwest::Url;
use serde::Deserialize;

use crate::chapter::{sync_chapters, Chapter};
use crate::groups::get_all_groups;
use crate::util::{
    convert_filename, convert_uuid, english_or_first, find_existing, json_get, FindResult,
    LocalizedString, PAGE_SIZE,
};
use crate::CONFIG;

pub fn sync_manga(manga_id: &str) -> Result<()> {
    debug!("Syncing {manga_id}");
    let info: MangaInfo = json_get(format!("https://api.mangadex.org/manga/{manga_id}"))?;
    let dir = get_or_create_dir(&info)?;

    let chapters = get_all_chapters(manga_id)?;
    let groups = get_all_groups(&chapters)?;
    debug!(
        "Got {} chapters for \"{}\"",
        chapters.len(),
        english_or_first(&info.data.attributes.title).unwrap()
    );

    sync_chapters(chapters.into_iter(), &dir, &groups)
}


pub fn get_or_create_dir(info: &MangaInfo) -> Result<PathBuf> {
    let converted_id = convert_uuid(&info.data.id)?;
    let title = english_or_first(&info.data.attributes.title).context("No title present")?;
    let dir_name = format!("{} - {converted_id}", convert_filename(&title));
    let mut dir_path = CONFIG.output_directory.join(dir_name);

    // Could be more efficient with some kind of producer closure returning an iterator
    // Not likely to be worth it. We only really care for chapters in manga, not all manga.
    let existing: Lazy<Result<Vec<_>>> = Lazy::new(|| {
        let dirs: std::result::Result<Vec<_>, _> = read_dir(&CONFIG.output_directory)?.collect();
        Ok(dirs?)
    });

    match find_existing(&dir_path, &existing, &converted_id, true)? {
        FindResult::Missing => {
            debug!("Creating {dir_path:?}");
            std::fs::create_dir(&dir_path)?;
        }
        FindResult::AlreadyExists => trace!("Directory already exists for \"{title}\""),
        FindResult::RenameCandidate(path) => {
            if CONFIG.rename_manga {
                info!("Renaming existing directory from {path:?} to {dir_path:?}");
                std::fs::rename(path, &dir_path)?;
            } else {
                debug!("Found existing directory {path:?}, not renaming");
                dir_path = path;
            }
        }
    }
    Ok(dir_path)
}

fn get_all_chapters(manga_id: &str) -> Result<Vec<Chapter>> {
    let mut total = 1;
    let mut offset = 0;

    let mut page_url = Url::parse(&format!("https://api.mangadex.org/manga/{manga_id}/feed"))?;

    page_url
        .query_pairs_mut()
        .append_pair("limit", &PAGE_SIZE.to_string())
        .append_pair("translatedLanguage[]", &CONFIG.language)
        .append_pair("order[chapter]", "desc");

    let mut chapters = Vec::new();

    while offset < total {
        let mut url = page_url.clone();
        url.query_pairs_mut().append_pair("offset", &offset.to_string());

        let page: ChapterList = json_get(url)?;

        total = page.total as usize;
        if page.data.len() != PAGE_SIZE && offset + page.data.len() < total {
            bail!(
                "Manga {manga_id}: invalid chapter pagination. Requested {PAGE_SIZE} chapters at \
                 offset {offset} with {total} total but got {}",
                page.data.len()
            );
        }

        chapters.extend(page.data.into_iter().filter(|c| {
            if c.attributes.external_url.is_some() {
                // Can any chapters have external urls but also pages on mangadex?
                debug!("Filtering out chapter {} with external url", c.id);
                return false;
            }

            if c.relationships.iter().any(|r| {
                r.type_field == "scanlation_group" && CONFIG.blocked_groups.contains(&r.id)
            }) {
                debug!("Filtering out chapter {} with blacklisted group", c.id);
                false
            } else {
                true
            }
        }));

        offset += PAGE_SIZE;
    }

    Ok(chapters)
}


#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(super) struct MangaInfo {
    pub data: Manga,
}

#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(super) struct Manga {
    pub id: String,
    pub attributes: MangaAttributes,
}

#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(super) struct MangaAttributes {
    pub title: LocalizedString,
}


#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct ChapterList {
    pub data: Vec<Chapter>,
    pub total: i64,
}
