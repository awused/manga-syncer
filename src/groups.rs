use std::cell::RefCell;
use std::collections::{HashMap, HashSet};

use anyhow::Result;
use reqwest::Url;
use serde::Deserialize;

use crate::chapter::Chapter;
use crate::util::json_get;

pub fn groups_in_chapter(c: &Chapter) -> impl Iterator<Item = &str> {
    c.relationships
        .iter()
        .filter(|r| r.type_field == "scanlation_group")
        .map(|r| &*r.id)
}

thread_local! {
    static GROUP_CACHE: RefCell<HashMap<&'static str, &'static str>> = RefCell::default();
}

pub fn get_all_groups(chapters: &[Chapter]) -> Result<HashMap<&'static str, &'static str>> {
    GROUP_CACHE.with_borrow_mut(|cache| {
        // Making a new map to return it is wasteful but avoids making it easy to forget to prime
        // the cache.
        let mut out = HashMap::new();

        let group_ids: Vec<_> = chapters
            .iter()
            .flat_map(groups_in_chapter)
            .collect::<HashSet<_>>()
            .into_iter()
            .filter(|g| {
                if let Some((group, name)) = cache.get_key_value(*g) {
                    out.insert(*group, *name);
                    false
                } else {
                    true
                }
            })
            .collect();


        let url = Url::parse("https://api.mangadex.org/group")?;

        group_ids
            .chunks(50)
            .map(|chunk| {
                let mut url = url.clone();
                for id in chunk {
                    url.query_pairs_mut().append_pair("ids[]", id);
                }

                let groups: GroupList = json_get(url)?;

                Ok(groups.data.into_iter().map(|g| (g.id, g.attributes.name)))
            })
            .collect::<Result<Vec<_>>>()?
            .into_iter()
            .flatten()
            .for_each(|(group, name)| {
                let group = group.leak();
                let name = name.leak();
                cache.insert(group, name);
                out.insert(group, name);
            });

        Ok(out)
    })
}

#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct GroupList {
    pub data: Vec<Group>,
}

#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct Group {
    pub id: String,
    pub attributes: GroupAttributes,
}

#[derive(Default, Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct GroupAttributes {
    pub name: String,
}
